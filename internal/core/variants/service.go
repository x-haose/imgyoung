package variants

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// ErrBusy 处理并发已满且等待超时,调用方应回退原图。
var ErrBusy = errors.New("处理繁忙,超时未获得处理槽")

// LookupMetrics 衍生图缓存查找的指标记录口(由 observability.Metrics 实现)。
type LookupMetrics interface {
	// RecordVariantLookup 记录一次缓存查找是否命中。
	RecordVariantLookup(hit bool)
}

// ServiceConfig 按需处理服务依赖。
type ServiceConfig struct {
	Store    *Store                 // 衍生图磁盘缓存
	Settings func() config.Settings // 运营配置快照读取口(逐请求取值,热生效);编码档位/并发上限/超时出自它
	Metrics  LookupMetrics          // 指标记录口;可为 nil(不记指标)
	Logger   *slog.Logger           // 结构化日志
}

// Service 按需处理协调器:命中缓存直取,未命中在并发上限内处理并落缓存,同键并发合并为一次。
type Service struct {
	store   *Store
	set     func() config.Settings
	sem     resizableSem       // 处理槽信号量,容量随配置热调整
	sf      singleflight.Group // 合并同缓存键的并发首请求,避免重复重编码
	metrics LookupMetrics      // 指标记录口;nil 表示不记
	log     *slog.Logger
}

// resizableSem 容量可热调整的信号量:容量变更时换新通道,在途持有者向旧通道释放。
// 变更瞬间在途 + 新容量可能短暂超额,随在途请求结束自然收敛——处理槽是防打满
// CPU 的软闸,瞬时超额可接受,换取无锁等待语义。
type resizableSem struct {
	mu  sync.Mutex
	cap int           // 当前通道容量
	ch  chan struct{} // 当前信号量通道
}

// channel 返回容量与 want 一致的信号量通道,不一致时换新。
// @param want 期望容量
// @return 信号量通道
func (s *resizableSem) channel(want int) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil || s.cap != want {
		s.ch = make(chan struct{}, want)
		s.cap = want
	}
	return s.ch
}

// SourceFunc 源图字节拉取器:惰性调用,接处理专用 ctx(与触发请求解耦)。
type SourceFunc = func(context.Context) ([]byte, error)

// result 单次处理产物,经 singleflight 在并发调用间共享。
type result struct {
	data []byte // 衍生图字节
	mime string // 目标 MIME
}

// NewService 构造按需处理服务。
// @param cfg 依赖配置
// @return 服务
func NewService(cfg ServiceConfig) *Service {
	return &Service{
		store:   cfg.Store,
		set:     cfg.Settings,
		metrics: cfg.Metrics,
		log:     cfg.Logger,
	}
}

// GetOrProcess 取或生成衍生图:命中缓存直接返回;未命中则在并发上限内处理并落缓存。
// @Description: srcFn 惰性拉取源图字节(接处理专用 ctx),仅未命中时调用;并发繁忙超时返回 ErrBusy 供上层回退原图。
// 同一缓存键的并发首请求由 singleflight 合并为一次处理,其余共享结果;合并内再查一次缓存,
// 在上一轮处理刚落缓存之后才进入合并的请求不会重复处理。处理与调用方连接解耦:
// 单个请求断开不会取消已合并的共享处理,结果仍落缓存供后续命中。
// @param ctx 上下文(仅用于命中缓存的读取;处理本身用解耦的超时 ctx)
// @param blobID 源物理图主键
// @param p 处理参数(宽度档 + 目标格式)
// @param srcFn 源图字节拉取器,惰性调用,接处理专用 ctx
// @return 衍生图字节
// @return 目标 MIME
// @return 错误(ErrBusy 表示应回退原图)
func (s *Service) GetOrProcess(ctx context.Context, blobID int64, p imaging.ProcessParams,
	srcFn SourceFunc) ([]byte, string, error) {
	set := s.set()
	quality, tuning := encodeParams(set, p.Format)
	key := Key(blobID, p.Width, p.Format, quality, tuning)

	hit, ok, err := s.store.Get(ctx, blobID, key)
	if err != nil {
		return nil, "", err
	}
	if s.metrics != nil {
		s.metrics.RecordVariantLookup(ok)
	}
	if ok {
		return hit.Data, imaging.MIME(hit.Format), nil
	}

	v, err, _ := s.sf.Do(key, func() (any, error) {
		// 进入合并前的缓存检查与此刻之间可能恰好有一次处理完成并落了缓存:再查一次,
		// 否则迟到的首请求会把同一张图再编码一遍。查缓存与调用方连接解耦,同处理一样。
		getCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cachePutTimeout)
		defer cancel()
		if hit, ok, gerr := s.store.Get(getCtx, blobID, key); gerr != nil {
			return result{}, gerr
		} else if ok {
			return result{data: hit.Data, mime: imaging.MIME(hit.Format)}, nil
		}
		return s.process(set, blobID, key, p, srcFn)
	})
	if err != nil {
		return nil, "", err
	}
	r, ok := v.(result)
	if !ok {
		return nil, "", fmt.Errorf("处理结果类型异常: %T", v)
	}
	return r.data, r.mime, nil
}

// process 在超时预算内抢占处理槽,拉源图、重编码、落缓存。
// @Description: 处理 ctx 与触发它的单个请求解耦(WithoutCancel + 独立超时)——因结果经 singleflight
// 供所有并发首请求共享,不应因某一个调用方断开就取消共享工作;超时仅约束抢槽与源图拉取(编码本身不吃 ctx)。
// 编码档位取触发请求见到的配置快照,与缓存键一致。
// @param set 本次处理生效的配置快照
// @param blobID 源物理图主键
// @param key 缓存键
// @param p 处理参数
// @param srcFn 源图字节拉取器
// @return 处理产物
// @return 错误
func (s *Service) process(set config.Settings, blobID int64, key string, p imaging.ProcessParams,
	srcFn SourceFunc) (result, error) {
	// 用 Background 而非调用方 ctx:处理结果供所有并发首请求共享,不因某调用方断开而取消。
	pctx, cancel := context.WithTimeout(context.Background(), time.Duration(set.ProcessTimeoutMS)*time.Millisecond)
	defer cancel()

	// 抢占处理槽:排队超时即返回 ErrBusy,不让请求无限等待。
	sem := s.sem.channel(set.ProcessWorkers)
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-pctx.Done():
		return result{}, ErrBusy
	}

	src, err := srcFn(pctx)
	if err != nil {
		return result{}, fmt.Errorf("拉取源图: %w", err)
	}
	out, mime, err := imaging.NewProcessor(EncodeSettingsFrom(set)).Process(src, p)
	if err != nil {
		return result{}, fmt.Errorf("处理图片: %w", err)
	}
	// 落缓存用独立超时,不吃处理预算的剩余额度——编码恰好耗尽预算时缓存仍应写入,
	// 否则该键每次访问都重复重编码;失败不影响本次返回,仅记日志,下次访问重建。
	putCtx, putCancel := context.WithTimeout(context.WithoutCancel(pctx), cachePutTimeout)
	defer putCancel()
	if perr := s.store.Put(putCtx, blobID, key, p.Width, p.Format, out); perr != nil {
		s.log.Warn("写衍生图缓存失败", "blob", blobID, "key", key, "err", perr)
	}
	return result{data: out, mime: mime}, nil
}

// cachePutTimeout 衍生图落缓存(写文件 + 记索引)的独立超时。
const cachePutTimeout = 10 * time.Second

// EncodeSettingsFrom 从运营配置摘取编码档位,处理引擎与缓存键计算共用同一来源。
// @param set 运营配置快照
// @return 编码档位
func EncodeSettingsFrom(set config.Settings) imaging.EncodeSettings {
	return imaging.EncodeSettings{
		QualityWebP: set.ProcessQualityWebP,
		QualityJPEG: set.ProcessQualityJPEG,
		WebPMethod:  set.ProcessWebPMethod,
		MaxPixels:   set.MaxPixels(),
	}
}

// encodeParams 返回某格式对应的质量档与编码调参,用于算缓存键(须与实际编码一致)。
// @param set 本次处理生效的配置快照
// @param format 目标格式
// @return 质量档
// @return 编码调参(webp method / avif speed / jpeg 取 0)
func encodeParams(set config.Settings, format string) (quality, tuning int) {
	switch format {
	case imaging.FormatWebP:
		return set.ProcessQualityWebP, set.ProcessWebPMethod
	case imaging.FormatJPEG:
		return set.ProcessQualityJPEG, 0
	default:
		// PNG 无损无调参,其余格式亦无参;质量/调参取 0 即可唯一定键。
		return 0, 0
	}
}
