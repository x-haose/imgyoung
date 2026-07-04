package variants

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// ErrBusy 处理并发已满且等待超时,调用方应回退原图。
var ErrBusy = errors.New("处理繁忙,超时未获得处理槽")

// ServiceConfig 按需处理服务依赖。
type ServiceConfig struct {
	Store    *Store                 // 衍生图磁盘缓存
	Proc     *imaging.Processor     // 图像处理引擎
	Settings imaging.EncodeSettings // 编码档位,须与 Proc 一致,用于算缓存键
	Workers  int                    // 并发处理上限
	Timeout  time.Duration          // 单请求获取处理槽 + 处理的总超时
	Logger   *slog.Logger           // 结构化日志
}

// Service 按需处理协调器:命中缓存直取,未命中在并发上限内处理并落缓存,同键并发合并为一次。
type Service struct {
	store   *Store
	proc    *imaging.Processor
	set     imaging.EncodeSettings
	sem     chan struct{}      // 信号量,容量 = Workers
	sf      singleflight.Group // 合并同缓存键的并发首请求,避免重复重编码
	timeout time.Duration
	log     *slog.Logger
}

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
		proc:    cfg.Proc,
		set:     cfg.Settings,
		sem:     make(chan struct{}, cfg.Workers),
		timeout: cfg.Timeout,
		log:     cfg.Logger,
	}
}

// GetOrProcess 取或生成衍生图:命中缓存直接返回;未命中则在并发上限内处理并落缓存。
// @Description: srcFn 惰性拉取源图字节(接处理专用 ctx),仅未命中时调用;并发繁忙超时返回 ErrBusy 供上层回退原图。
// 同一缓存键的并发首请求由 singleflight 合并为一次处理,其余共享结果。处理与调用方连接解耦:
// 单个请求断开不会取消已合并的共享处理,结果仍落缓存供后续命中。
// @param ctx 上下文(仅用于命中缓存的读取;处理本身用解耦的超时 ctx)
// @param blobID 源物理图主键
// @param p 处理参数(宽度档 + 目标格式)
// @param srcFn 源图字节拉取器,惰性调用,接处理专用 ctx
// @return 衍生图字节
// @return 目标 MIME
// @return 错误(ErrBusy 表示应回退原图)
func (s *Service) GetOrProcess(ctx context.Context, blobID int64, p imaging.ProcessParams, srcFn func(context.Context) ([]byte, error)) ([]byte, string, error) {
	quality, tuning := s.encodeParams(p.Format)
	key := Key(blobID, p.Width, p.Format, quality, tuning)

	if data, format, ok, err := s.store.Get(ctx, blobID, key); err != nil {
		return nil, "", err
	} else if ok {
		return data, imaging.MIME(format), nil
	}

	v, err, _ := s.sf.Do(key, func() (any, error) {
		return s.process(blobID, key, p, srcFn)
	})
	if err != nil {
		return nil, "", err
	}
	r := v.(result)
	return r.data, r.mime, nil
}

// process 在超时预算内抢占处理槽,拉源图、重编码、落缓存。
// @Description: 处理 ctx 与触发它的单个请求解耦(WithoutCancel + 独立超时)——因结果经 singleflight
// 供所有并发首请求共享,不应因某一个调用方断开就取消共享工作;超时仅约束抢槽与源图拉取(编码本身不吃 ctx)。
// @param blobID 源物理图主键
// @param key 缓存键
// @param p 处理参数
// @param srcFn 源图字节拉取器
// @return 处理产物
// @return 错误
func (s *Service) process(blobID int64, key string, p imaging.ProcessParams, srcFn func(context.Context) ([]byte, error)) (result, error) {
	// 用 Background 而非调用方 ctx:处理结果供所有并发首请求共享,不因某调用方断开而取消。
	pctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	// 抢占处理槽:排队超时即返回 ErrBusy,不让请求无限等待。
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-pctx.Done():
		return result{}, ErrBusy
	}

	src, err := srcFn(pctx)
	if err != nil {
		return result{}, fmt.Errorf("拉取源图: %w", err)
	}
	out, mime, err := s.proc.Process(src, p)
	if err != nil {
		return result{}, fmt.Errorf("处理图片: %w", err)
	}
	// 落缓存失败不影响本次返回,仅记日志:下次访问会重建。
	if perr := s.store.Put(pctx, blobID, key, p.Format, out); perr != nil {
		s.log.Warn("写衍生图缓存失败", "blob", blobID, "key", key, "err", perr)
	}
	return result{data: out, mime: mime}, nil
}

// encodeParams 返回某格式对应的质量档与编码调参,用于算缓存键(须与实际编码一致)。
// @param format 目标格式
// @return 质量档
// @return 编码调参(webp method / avif speed / jpeg 取 0)
func (s *Service) encodeParams(format string) (quality, tuning int) {
	switch format {
	case imaging.FormatWebP:
		return s.set.QualityWebP, s.set.WebPMethod
	case imaging.FormatJPEG:
		return s.set.QualityJPEG, 0
	default:
		// PNG 无损无调参,其余格式亦无参;质量/调参取 0 即可唯一定键。
		return 0, 0
	}
}
