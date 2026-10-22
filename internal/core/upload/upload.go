// Package upload 实现上传链路的 Service 层编排:
// 格式校验 → 尺寸限制 → 净化(strip / spoof)→ 哈希(SHA256 + pHash)→ 存渠道 → 落库。
package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/store/driver"
	"github.com/x-haose/imgyoung/store/failover"

	// 注册白名单格式的解码器(gif/jpeg/png 标准库)。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	// 注册 webp 解码器(仅解码,编码走 imaging 包)。
	_ "golang.org/x/image/webp"
)

// 图片 MIME 类型常量(http.DetectContentType 的返回值)。
const (
	mimeJPEG = "image/jpeg"
	mimePNG  = "image/png"
	mimeGIF  = "image/gif"
	mimeWebP = "image/webp"
)

// allowedMIME 图片格式白名单。
// SVG 刻意不在列(XSS 风险)。
var allowedMIME = map[string]struct{}{
	mimeJPEG: {},
	mimePNG:  {},
	mimeGIF:  {},
	mimeWebP: {},
}

// formatMIME image 包解码器的 format 名 → MIME。
// http.DetectContentType 与 image 包是两套独立的 magic 匹配规则,
// 落库的 MIME 必须与实际解码格式交叉验证,不隐式假设两者一致。
var formatMIME = map[string]string{
	"jpeg": mimeJPEG,
	"png":  mimePNG,
	"gif":  mimeGIF,
	"webp": mimeWebP,
}

// 上传链路的业务错误哨兵,由 API 层映射为 HTTP 状态码。
// EXIF 档位枚举与其校验错误见 imaging 包(单一真相)。
var (
	// ErrUnsupportedType 内容不在图片格式白名单内(按 magic bytes 判定)。
	ErrUnsupportedType = errors.New("upload: 不支持的文件类型")
	// ErrTooManyPixels 图片分辨率超出像素预算(内存三道闸之一)。
	ErrTooManyPixels = errors.New("upload: 图片分辨率超出限制")
	// ErrBadDimensions 图片单边或宽高比超出限制。
	ErrBadDimensions = errors.New("upload: 图片边长或宽高比超出限制")
	// ErrInvalidImage 声明格式合法但内容无法解析。
	ErrInvalidImage = errors.New("upload: 图片内容无法解析")
)

// ChannelSelector 写入目标的选择与枚举(由 channel.Manager 实现)。
type ChannelSelector interface {
	// Healthy 返回健康渠道句柄,按优先级降序。
	Healthy() []channel.Handle
	// All 返回全部渠道句柄,按优先级降序。
	All() []channel.Handle
	// HoldWrites 取渠道的写入租约;渠道已标为清理中时 ok 为 false。
	HoldWrites(channelID int64) (release func(), ok bool)
}

// putTimeout 单次渠道同步写入的超时上限——远端渠道半死时不允许无限挂起
// 占用并发闸名额(上限按 50MB 大图过慢速链路放宽)。
const putTimeout = 10 * time.Minute

// ReplicationEnqueuer 副本补齐任务的入队口(由 replication.Runner 实现)。
type ReplicationEnqueuer interface {
	// EnqueueReplication 为目标渠道入队补齐任务(幂等)。
	EnqueueReplication(ctx context.Context, blobID int64, channelIDs []int64) error
}

// Metrics 上传管道的指标记录口(由 observability.Metrics 实现)。
type Metrics interface {
	// ObserveUpload 记录一次上传管道的耗时与结果(ok / rejected / failed)。
	ObserveUpload(outcome string, dur time.Duration)
	// RecordChannelPut 记录一次向渠道同步写对象的成败。
	RecordChannelPut(name string, ok bool)
}

// Config Service 的全部依赖与参数。
type Config struct {
	Conn       *sql.DB                // 数据库连接(事务用)
	Queries    *sqlc.Queries          // sqlc 查询集
	Channels   ChannelSelector        // 渠道选择器:主渠道同步写入,其余渠道记 pending 待补齐
	Replicator ReplicationEnqueuer    // 补齐入队口;可为 nil(pending 行由恢复扫描兜底)
	Settings   func() config.Settings // 运营配置快照读取口(逐次上传取值,热生效);像素预算/最少副本数/全局默认 TTL 出自它
	Metrics    Metrics                // 指标记录口;可为 nil(不记指标)
	Logger     *slog.Logger           // 结构化日志
}

// ErrInsufficientReplicas 同步副本数不足 MinSuccess 要求。
var ErrInsufficientReplicas = errors.New("upload: 副本数不足")

// ErrQuotaExceeded 归属用户的存储配额不足以容纳本次上传。
var ErrQuotaExceeded = errors.New("upload: 存储配额已满")

// Options 一次上传的参数。
type Options struct {
	UserID         string             // 归属用户 id,必填;由认证中间件解析后显式传入,多租户隔离据此强制
	TokenID        int64              // 上传所经 Token 的记录 id,0 表示非 Token 途径
	FileName       string             // 原始文件名(仅存元数据,不参与任何逻辑)
	IdempotencyKey string             // 客户端幂等键,可为空;按用户隔离
	EXIFPolicy     imaging.EXIFPolicy // 本次上传生效的 EXIF 档位,必填;调用方解析"单次指定 > 全局默认"后传入
	TTL            time.Duration      // 请求显式指定的有效时长,仅 TTLSet 为 true 时生效;0 表示显式永久
	TTLSet         bool               // 请求是否显式指定 ttl;false 时按"用户默认 > 全局默认"解析,到期时刻由落库时刻推算
	Private        bool               // 是否作为私有图保存:公开入口只认带有效签名的链接
	ObjectKey      string             // S3 协议上传时客户端指定的对象键,可为空;同用户同键按 S3 语义覆盖(旧图让出别名,短 ID 仍可访问)
}

// Service 上传编排服务,无可变状态、并发安全。
type Service struct {
	cfg      Config
	inflight sync.Map // 正在上传的对象键 → struct{}:从写入渠道之前到落库或清理结束
}

// Result 一次上传的结果。
type Result struct {
	ImageID string // 对外短 ID
}

// New 创建上传服务。
// @param cfg 依赖与参数,字段均必填
// @return 就绪的上传服务
func New(cfg Config) *Service {
	return &Service{cfg: cfg}
}

// Process 执行完整上传管道并返回结果。
// @Description: 幂等键非空时启用幂等:命中已有键直接返回既有结果。单次配置快照贯穿整个上传,
// 同一请求内各配置项取值一致,不受并发热改影响。Put 成功而落库失败会产生孤儿对象,
// 由对账任务回收,不回滚渠道写入。
// @param data 完整的上传字节(调用方已做大小闸)
// @param opts 文件名/幂等键/EXIF 档位等可选参数
// @return 上传结果
// @return 业务错误(见哨兵)或基础设施错误
func (s *Service) Process(ctx context.Context, data []byte, opts Options) (Result, error) {
	started := time.Now()
	res, err := s.runPipeline(ctx, data, opts)
	if s.cfg.Metrics != nil {
		s.cfg.Metrics.ObserveUpload(outcomeOf(err), time.Since(started))
	}
	return res, err
}

// outcomeOf 把管道结果归为指标标签:成功 ok,输入或配额不合规 rejected,其余 failed。
// @param err 管道返回的错误
// @return 结果标签
func outcomeOf(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrUnsupportedType), errors.Is(err, ErrTooManyPixels), errors.Is(err, ErrBadDimensions),
		errors.Is(err, ErrInvalidImage),
		errors.Is(err, ErrQuotaExceeded), errors.Is(err, imaging.ErrUnknownPolicy):
		return "rejected"
	default:
		return "failed"
	}
}

// runPipeline 执行上传管道的全部步骤。
// @param ctx 控制超时与取消
// @param data 完整的上传字节
// @param opts 上传参数
// @return 上传结果
// @return 业务错误(见哨兵)或基础设施错误
func (s *Service) runPipeline(ctx context.Context, data []byte, opts Options) (Result, error) {
	set := s.cfg.Settings()
	// 归属用户必填(多租户隔离的前提),档位必须是明确合法值。
	if opts.UserID == "" {
		return Result{}, errors.New("upload: 缺少归属用户")
	}
	if err := imaging.ValidatePolicy(opts.EXIFPolicy); err != nil {
		return Result{}, err
	}
	win := idempotencyWindow(set)
	if imageID, found, err := s.imageByIdempotencyKey(ctx, opts.UserID, opts.IdempotencyKey, win); err != nil {
		return Result{}, err
	} else if found {
		return Result{ImageID: imageID}, nil
	}
	meta, err := inspect(data, limitsOf(set))
	if err != nil {
		return Result{}, err
	}
	// 用户行一次读取,配额预检与用户级 TTL 解析共用(上传热路径不多打一枪)。
	user, err := s.cfg.Queries.GetUser(ctx, opts.UserID)
	if err != nil {
		return Result{}, fmt.Errorf("查询用户: %w", err)
	}
	// 配额预检:按引用计全额(去重合并不减账面),超限即拒绝、不写渠道。
	if err := s.checkQuota(ctx, user, int64(len(data))); err != nil {
		return Result{}, err
	}
	prep, err := prepare(data, meta, opts.EXIFPolicy, set)
	if err != nil {
		return Result{}, err
	}
	return s.store(ctx, set, win, opts, user, meta, prep)
}

// store 把处理好的图写进渠道并落库,随后为其余渠道触发补齐。
// @Description: 写入与落库都在目标渠道的写入租约内:渠道期间被标为清理中,排空等这次落库后再列举,写下的对象
// 不会遗留。写够份数前失败、落库失败(含落库时仍在役的同步副本不够、幂等键输给另一请求),都删掉本次已写入的
// 对象:对象键带本次的短 ID,没落库就没有记录指向它。
// @param set 本次请求生效的配置快照
// @param win 幂等键的判定时刻,与上传开头查键时相同
// @param opts 上传参数
// @param user 归属用户
// @param meta 原图信息
// @param prep 净化后的字节与哈希
// @return 上传结果
// @return 写入或落库失败时的错误
func (s *Service) store(ctx context.Context, set config.Settings, win idemWindow, opts Options, user sqlc.User,
	meta imageMeta, prep prepared) (Result, error) {
	shortID := id.New()
	key := id.StorageKey(shortID, prep.sha256)
	s.inflight.Store(key, struct{}{})
	defer s.inflight.Delete(key)
	targets, release := s.holdTargets()
	defer release()
	doneIDs, err := s.writeReplicas(ctx, targets, key, prep.data, minSuccess(set))
	if err != nil {
		s.discardWritten(ctx, targets, doneIDs, key)
		return Result{}, err
	}
	all := s.cfg.Channels.All()
	blobID, err := s.persist(ctx, persistParams{
		shortID: shortID, userID: opts.UserID, tokenID: opts.TokenID, fileName: truncateName(opts.FileName),
		idemKey: opts.IdempotencyKey, idemWin: win, doneIDs: doneIDs, channels: all, need: minSuccess(set),
		sha256: prep.sha256, phash: prep.phash, key: key, mime: meta.mime, size: int64(len(prep.data)),
		width: int64(meta.width), height: int64(meta.height),
		ttl: resolveTTL(opts, user.DefaultTtlHours, set.DefaultTTL()), objectKey: opts.ObjectKey, private: opts.Private,
	})
	if err != nil {
		s.discardWritten(ctx, targets, doneIDs, key)
		if errors.Is(err, ErrInsufficientReplicas) {
			return Result{}, err
		}
		// 并发同幂等键竞争:另一请求可能已抢先落库,以先者为准。
		imageID, found, lookupErr := s.imageByIdempotencyKey(ctx, opts.UserID, opts.IdempotencyKey, win)
		if lookupErr != nil {
			return Result{}, errors.Join(err, lookupErr)
		}
		if found {
			return Result{ImageID: imageID}, nil
		}
		return Result{}, err
	}
	s.enqueuePending(ctx, blobID, shortID, all, doneIDs)
	return Result{ImageID: shortID}, nil
}

// Uploading 判断对象键是否属于正在进行的上传:对象可能已写进渠道、记录还没落库,此时它不是缺记录的对象。
// @param key 渠道内对象键
// @return 正在上传为 true
func (s *Service) Uploading(key string) bool {
	_, ok := s.inflight.Load(key)
	return ok
}

// ExistingUpload 查本用户带同一幂等键的既有上传:键在保留期内且所指的图未到期才算命中;幂等键为空视为未命中。
// @Description: 供需要在处理字节之前就判断命中的入口使用(URL 转存据此在抓取远端之前返回第一次的结果)。
// @param userID 归属用户 id
// @param key 幂等键
// @return 既有引用短 ID
// @return 是否命中
// @return 查询失败时的错误
func (s *Service) ExistingUpload(ctx context.Context, userID, key string) (string, bool, error) {
	return s.imageByIdempotencyKey(ctx, userID, key, idempotencyWindow(s.cfg.Settings()))
}

// idemWindow 判定幂等键是否有效的两个时刻;一次上传的查键与写键共用同一组,两处的判定条件互补。
type idemWindow struct {
	since string // 保留期起点,UTC 时间文本;早于它写下的键已过期,与清理扫描的回收边界一致
	now   string // 判定时刻,UTC 时间文本;键所指的图在此刻或之前到期,键随之失效
}

// idempotencyWindow 按当前时刻与配置的保留期算出幂等键的判定时刻。
// @param set 配置快照
// @return 判定时刻
func idempotencyWindow(set config.Settings) idemWindow {
	now := time.Now()
	return idemWindow{since: sqlc.TimeText(now.Add(-set.IdempotencyKeyTTL())), now: sqlc.TimeText(now)}
}

// imageByIdempotencyKey 查本用户同幂等键的既有上传:键在保留期内且所指的图未到期才算命中;幂等键为空视为未命中。
// @param userID 归属用户 id
// @param key 幂等键
// @param win 判定时刻
// @return 既有引用短 ID
// @return 是否命中
// @return 查询失败时的错误
func (s *Service) imageByIdempotencyKey(ctx context.Context, userID, key string, win idemWindow) (string, bool, error) {
	if key == "" {
		return "", false, nil
	}
	imageID, err := s.cfg.Queries.GetIdempotencyImage(ctx, sqlc.GetIdempotencyImageParams{
		UserID: userID, Key: key, Since: win.since, Now: win.now,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("查询幂等键: %w", err)
	}
	return imageID, true, nil
}

// imageMeta 只读图片头得到的格式与尺寸。
type imageMeta struct {
	mime          string // 真实 MIME,与解码格式交叉验证过
	width, height int    // 像素尺寸
}

// limits 图片尺寸的三道闸。
type limits struct {
	pixels int64 // 宽 × 高的上限
	side   int   // 单边像素的上限
	aspect int   // 长边与短边之比的上限
}

// limitsOf 取配置快照里的尺寸限制。
// @param set 配置快照
// @return 尺寸限制
func limitsOf(set config.Settings) limits {
	return limits{pixels: set.MaxPixels(), side: set.MaxSidePx, aspect: set.MaxAspectRatio}
}

// inspect 按 magic bytes 判定格式、只读图片头取尺寸并执行尺寸限制。
// @Description: 不信扩展名与客户端声明;超限直接拒绝、不进完整解码器(内存三道闸之一)。
// @param data 上传字节
// @param lim 尺寸限制
// @return 格式与尺寸
// @return ErrUnsupportedType / ErrInvalidImage / ErrTooManyPixels / ErrBadDimensions
func inspect(data []byte, lim limits) (imageMeta, error) {
	meta, err := sniff(data, lim.pixels)
	if err != nil {
		return imageMeta{}, err
	}
	long, short := max(meta.width, meta.height), min(meta.width, meta.height)
	if long > lim.side || int64(long) > int64(lim.aspect)*int64(short) {
		return imageMeta{}, ErrBadDimensions
	}
	return meta, nil
}

// sniff 按 magic bytes 判定格式、只读图片头取尺寸并执行像素预算,不看边长与宽高比。
// @Description: 像素预算护的是解码内存,任何要解码这份字节的入口都必须过;边长与宽高比是上传时的准入规则。
// @param data 图片字节
// @param maxPixels 像素预算
// @return 格式与尺寸
// @return ErrUnsupportedType / ErrInvalidImage / ErrTooManyPixels
func sniff(data []byte, maxPixels int64) (imageMeta, error) {
	mime := http.DetectContentType(data)
	if _, ok := allowedMIME[mime]; !ok {
		return imageMeta{}, ErrUnsupportedType
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imageMeta{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	if formatMIME[format] != mime {
		return imageMeta{}, fmt.Errorf("%w: 嗅探类型 %s 与解码格式 %s 不一致", ErrInvalidImage, mime, format)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return imageMeta{}, ErrTooManyPixels
	}
	return imageMeta{mime: mime, width: cfg.Width, height: cfg.Height}, nil
}

// prepared 净化后的待存字节及其指纹。
type prepared struct {
	data   []byte // 实际存储字节(strip 与 spoof 档位下已替换元数据)
	sha256 string // 存储字节的 SHA-256 十六进制
	phash  int64  // 感知哈希,按位存入 int64
}

// prepare 按档位净化字节并计算指纹;净化在前、哈希在后,哈希对象即实际存储字节。
// @param data 上传字节
// @param meta 原图的格式与尺寸
// @param policy EXIF 档位
// @param set 配置快照,spoof 档据此取机型与城市的来源
// @return 待存字节与指纹
// @return ErrInvalidImage、伪装配置不可用或哈希计算错误
func prepare(data []byte, meta imageMeta, policy imaging.EXIFPolicy, set config.Settings) (prepared, error) {
	data, err := sanitize(data, meta, policy, set)
	if err != nil {
		return prepared{}, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return prepared{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	ph, err := imaging.PHash(img)
	if err != nil {
		return prepared{}, err
	}
	sum := sha256.Sum256(data)
	return prepared{
		data:   data,
		sha256: hex.EncodeToString(sum[:]),
		phash:  ph,
	}, nil
}

// sanitize 按 EXIF 档位处理元数据;passthrough 原样返回。
// @param data 上传字节
// @param meta 原图的格式与尺寸
// @param policy EXIF 档位
// @param set 配置快照
// @return 处理后的字节
// @return 图片畸形时为 ErrInvalidImage;伪装配置不可用或读取系统熵失败时为对应错误,不降级成别的档位
func sanitize(data []byte, meta imageMeta, policy imaging.EXIFPolicy, set config.Settings) ([]byte, error) {
	switch policy {
	case imaging.PolicyStrip:
		out, err := imaging.StripMetadata(data, meta.mime)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidImage, err)
		}
		return out, nil
	case imaging.PolicySpoof:
		rng, err := imaging.NewSpoofRNG()
		if err != nil {
			return nil, err
		}
		out, err := imaging.Spoof(data, meta.mime, meta.width, meta.height, set.SpoofConfig(), rng, time.Now())
		switch {
		case errors.Is(err, imaging.ErrSpoofConfig):
			return nil, err
		case err != nil:
			return nil, fmt.Errorf("%w: %w", ErrInvalidImage, err)
		}
		return out, nil
	default:
		return data, nil
	}
}

// writeReplicas 按优先级向健康渠道同步写入,直到凑够 need 份;写入失败自动顺延下一渠道。
// @Description: 首个成功者即本次主渠道;其余渠道由补齐任务异步复制。每次写入都记渠道成败指标,
// 失败的写入逐条告警。
// @param ctx 控制超时与取消
// @param key 对象键
// @param data 待存字节
// @param need 最少同步副本数,>= 1
// @return 已写入的渠道 ID,按写入顺序;失败时同样给出,供调用方删掉已写的对象
// @return 无健康渠道时为 channel.ErrNoHealthyChannel,份数不足时为 ErrInsufficientReplicas
func (s *Service) writeReplicas(ctx context.Context, healthy []channel.Handle, key string, data []byte,
	need int) ([]int64, error) {
	if len(healthy) == 0 {
		return nil, fmt.Errorf("选择主渠道: %w", channel.ErrNoHealthyChannel)
	}
	drivers := make([]driver.Driver, len(healthy))
	for i, h := range healthy {
		drivers[i] = h.Driver
	}
	attempts, err := failover.Write(ctx, drivers, key, bytes.NewReader(data), int64(len(data)),
		failover.WriteOptions{Need: need, AttemptTimeout: putTimeout})
	doneIDs := make([]int64, 0, need)
	for _, a := range attempts {
		h := healthy[a.Index]
		if s.cfg.Metrics != nil {
			s.cfg.Metrics.RecordChannelPut(h.Name, a.Err == nil)
		}
		if a.Err != nil {
			s.cfg.Logger.Warn("渠道写入失败,顺延下一渠道", "channel", h.Name, "err", a.Err)
			continue
		}
		doneIDs = append(doneIDs, h.ID)
	}
	if errors.Is(err, failover.ErrInsufficient) {
		return doneIDs, fmt.Errorf("%w: %w", ErrInsufficientReplicas, err)
	}
	if err != nil {
		return doneIDs, err
	}
	return doneIDs, nil
}

// holdTargets 取当前健康渠道的写入租约,作为本次上传的候选渠道;取不到租约的(已标为清理中)不参与。
// @return targets 已取得租约的健康渠道,按优先级降序
// @return release 释放全部租约,落库之后调用
func (s *Service) holdTargets() (targets []channel.Handle, release func()) {
	healthy := s.cfg.Channels.Healthy()
	releases := make([]func(), 0, len(healthy))
	for _, h := range healthy {
		if rel, ok := s.cfg.Channels.HoldWrites(h.ID); ok {
			targets = append(targets, h)
			releases = append(releases, rel)
		}
	}
	return targets, func() {
		for _, rel := range releases {
			rel()
		}
	}
}

// discardWritten 上传没有落库,删掉已写进各渠道的对象。
// @Description: 尽力而为:删不掉的,在役渠道上由对账 GC 当孤儿回收;已标为清理中的渠道没有副本记录指向它,
// 只能留在远端,记日志备查。删除脱离请求的取消,客户端断开也照样清理。
// @param targets 本次上传的候选渠道
// @param doneIDs 已写入的渠道 ID
// @param key 对象键
func (s *Service) discardWritten(ctx context.Context, targets []channel.Handle, doneIDs []int64, key string) {
	ctx = context.WithoutCancel(ctx)
	for _, h := range targets {
		if !slices.Contains(doneIDs, h.ID) {
			continue
		}
		if err := h.Driver.Delete(ctx, key); err != nil && !errors.Is(err, driver.ErrNotFound) {
			s.cfg.Logger.Warn("上传未落库,删除已写入的对象失败", "channel", h.Name, "key", key, "err", err)
		}
	}
}

// enqueuePending 为未同步写入的渠道触发异步补齐;入队失败仅告警,pending 行由恢复扫描兜底。
// @param ctx 控制超时与取消
// @param blobID 物理图主键
// @param shortID 引用短 ID,仅日志用
// @param all 全部渠道句柄
// @param doneIDs 已同步写入的渠道 ID
func (s *Service) enqueuePending(ctx context.Context, blobID int64, shortID string, all []channel.Handle,
	doneIDs []int64) {
	if s.cfg.Replicator == nil {
		return
	}
	pending := pendingIDs(all, doneIDs)
	if len(pending) == 0 {
		return
	}
	if err := s.cfg.Replicator.EnqueueReplication(ctx, blobID, pending); err != nil {
		s.cfg.Logger.Warn("补齐任务入队失败,待恢复扫描兜底", "image", shortID, "err", err)
	}
}

// checkQuota 校验用户配额可容纳本次上传;NULL 配额表示不限。
// @Description: 口径为按引用计全额——每条引用计其 blob 的完整大小,查重合并
// 不减账面;预检用上传原始字节数近似最终存储大小。
// @param ctx 控制超时与取消
// @param user 归属用户行(调用方已读取,避免热路径重复查询)
// @param size 本次上传字节数
// @return ErrQuotaExceeded 或基础设施错误
func (s *Service) checkQuota(ctx context.Context, user sqlc.User, size int64) error {
	if !user.QuotaBytes.Valid {
		return nil
	}
	used, err := s.cfg.Queries.UserStorageUsage(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("统计用户用量: %w", err)
	}
	if used+size > user.QuotaBytes.Int64 {
		return ErrQuotaExceeded
	}
	return nil
}

// resolveTTL 三层解析本次上传的有效时长:请求显式指定(含 0 = 永久)>
// 用户默认 default_ttl_hours(0 = 该用户默认永久)> 全局默认。
// @param opts 上传参数(取显式 TTL 与其标志)
// @param userHours 用户行的 default_ttl_hours;NULL 表示随全局
// @param global 全局默认 TTL
// @return 生效的有效时长,<=0 表示永不过期
func resolveTTL(opts Options, userHours sql.NullInt64, global time.Duration) time.Duration {
	switch {
	case opts.TTLSet:
		return opts.TTL
	case userHours.Valid:
		return time.Duration(userHours.Int64) * time.Hour
	default:
		return global
	}
}

// minSuccess 归一化最少副本数(至少 1)。
// @param set 本次上传生效的配置快照
// @return 最少同步副本数
func minSuccess(set config.Settings) int {
	if set.MinSuccess >= 1 {
		return set.MinSuccess
	}
	return 1
}

// pendingIDs 返回未同步写入的渠道 ID 集(待异步补齐)。
// @param all 全部渠道句柄
// @param doneIDs 已同步写入的渠道 ID
// @return 待补齐渠道 ID
func pendingIDs(all []channel.Handle, doneIDs []int64) []int64 {
	done := make(map[int64]struct{}, len(doneIDs))
	for _, chID := range doneIDs {
		done[chID] = struct{}{}
	}
	out := make([]int64, 0, len(all))
	for _, h := range all {
		if _, ok := done[h.ID]; !ok {
			out = append(out, h.ID)
		}
	}
	return out
}

// maxFileNameBytes 文件名的存储字节上限,超出部分截断。
const maxFileNameBytes = 255

// truncateName 清洗并截断客户端提交的文件名(不可信输入,仅作元数据)。
// @Description: 移除控制字符(防止输出到响应头/日志时被注入),
// 再按 UTF-8 安全截断到 maxFileNameBytes 字节以内(多字节字符不截半)。
// @param name 客户端提交的原始文件名
// @return 清洗截断后的文件名
func truncateName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F {
			return -1
		}
		return r
	}, name)
	if len(name) <= maxFileNameBytes {
		return name
	}
	cut := maxFileNameBytes
	// 回退到 UTF-8 字符边界(非续字节)。
	for cut > 0 && name[cut]&0xC0 == 0x80 {
		cut--
	}
	return name[:cut]
}

// persistParams 一次落库所需的全部字段。
type persistParams struct {
	shortID       string           // 对外短 ID
	userID        string           // 归属用户 id
	tokenID       int64            // 上传所经 Token 记录 id,0 表示非 Token 途径
	fileName      string           // 原始文件名
	idemKey       string           // 幂等键,可为空;按用户隔离
	idemWin       idemWindow       // 幂等键的判定时刻;同键的旧记录已过期或所指的图已到期时,写入前删除
	doneIDs       []int64          // 已同步写入的渠道 ID(至少含主渠道)
	channels      []channel.Handle // 落库时的在役渠道,未同步写入的据此记为待补齐
	need          int              // 落库时仍在役的同步副本至少要有几份,>= 1
	sha256        string           // 净化后字节哈希(十六进制)
	phash         int64            // 感知哈希(按位存入 int64)
	key           string           // 渠道对象键
	mime          string           // 真实 MIME
	size          int64            // 存储字节大小
	width, height int64            // 像素尺寸
	ttl           time.Duration    // 有效时长,<=0 表示永不过期;到期时刻与 ttl_seconds 均由此推算
	objectKey     string           // S3 对象键别名,空表示非 S3 途径
	private       bool             // 是否私有图
}

// persist 在单事务内写入 blob、引用、副本状态与幂等键。
// @return 新建的 blob 主键
// @return 事务执行失败时的错误
func (s *Service) persist(ctx context.Context, p persistParams) (int64, error) {
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()

	q := s.cfg.Queries.WithTx(tx)
	blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
		Sha256:     p.sha256,
		Phash:      p.phash,
		StorageKey: p.key,
		Mime:       p.mime,
		Size:       p.size,
		Width:      p.width,
		Height:     p.height,
	})
	if err != nil {
		return 0, fmt.Errorf("写入 blob 记录: %w", err)
	}
	if err := insertImage(ctx, q, p, blobID); err != nil {
		return 0, err
	}
	if err := insertReplicaStates(ctx, q, p, blobID); err != nil {
		return 0, err
	}
	if err := createIdempotencyKey(ctx, q, p); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务: %w", err)
	}
	return blobID, nil
}

// createIdempotencyKey 记下本次上传的幂等键;幂等键为空时不记。
// @Description: 同键的旧记录已过保留期、或所指的图已到期(清理扫描还没删)时先删掉再写入;删除条件与查键时
// 的命中条件互补,删不掉的同键记录说明另一次并发请求已先落库,插入撞主键失败,由调用方按先落库者返回。
// @param q 事务内查询集
// @param p 落库字段
// @return 写入失败时的错误
func createIdempotencyKey(ctx context.Context, q *sqlc.Queries, p persistParams) error {
	if p.idemKey == "" {
		return nil
	}
	if err := q.ReleaseStaleIdempotencyKey(ctx, sqlc.ReleaseStaleIdempotencyKeyParams{
		UserID: p.userID, Key: p.idemKey, Since: p.idemWin.since, Now: p.idemWin.now,
	}); err != nil {
		return fmt.Errorf("删除失效的同名幂等键: %w", err)
	}
	if err := q.CreateIdempotencyKey(ctx, sqlc.CreateIdempotencyKeyParams{
		UserID: p.userID, Key: p.idemKey, ImageID: p.shortID,
	}); err != nil {
		return fmt.Errorf("写入幂等键: %w", err)
	}
	return nil
}

// insertImage 写入引用记录:推算到期时刻,S3 途径先让旧图让出同名对象键。
// @Description: S3 语义是同键覆盖:先摘掉旧图的别名再插新图,否则唯一索引会挡下二次上传;
// 旧图本身不删——它可能已被分享出去,短 ID 必须继续可用。
// @param ctx 控制超时与取消
// @param q 事务内查询集
// @param p 落库字段
// @param blobID 物理图主键
// @return 写入失败时的错误
func insertImage(ctx context.Context, q *sqlc.Queries, p persistParams, blobID int64) error {
	expireAt, ttlSeconds := sql.NullString{}, sql.NullInt64{}
	if p.ttl > 0 {
		expireAt = sql.NullString{String: sqlc.TimeText(time.Now().Add(p.ttl)), Valid: true}
		ttlSeconds = sql.NullInt64{Int64: int64(p.ttl / time.Second), Valid: true}
	}
	if p.objectKey != "" {
		if _, err := q.ClearObjectKey(ctx, sqlc.ClearObjectKeyParams{
			UserID: p.userID, ObjectKey: sql.NullString{String: p.objectKey, Valid: true},
		}); err != nil {
			return fmt.Errorf("清理同名对象键: %w", err)
		}
	}
	if err := q.CreateImage(ctx, sqlc.CreateImageParams{
		ID:         p.shortID,
		BlobID:     blobID,
		UserID:     p.userID,
		TokenID:    sql.NullInt64{Int64: p.tokenID, Valid: p.tokenID != 0},
		FileName:   p.fileName,
		ObjectKey:  sql.NullString{String: p.objectKey, Valid: p.objectKey != ""},
		ExpireAt:   expireAt,
		TtlSeconds: ttlSeconds,
		Private:    boolInt(p.private),
	}); err != nil {
		return fmt.Errorf("写入引用记录: %w", err)
	}
	return nil
}

// insertReplicaStates 写入副本状态:已同步渠道就绪,其余在役渠道待补齐(outbox,补齐任务据此驱动)。
// @Description: 渠道是否在役以落库这一刻的渠道表为准,与删除渠道的事务串行:写入期间被标为清理中的渠道,
// 副本照样记下(排空据此删掉对象)但不算有效副本;写入期间被删除的渠道不记。有效副本不足 p.need 份时整体失败,
// 不让上传在没有可用副本的情况下报成功。
// @param ctx 控制超时与取消
// @param q 事务内查询集
// @param p 落库字段
// @param blobID 物理图主键
// @return 有效副本不足时包裹 ErrInsufficientReplicas 的错误;写入失败时的错误
func insertReplicaStates(ctx context.Context, q *sqlc.Queries, p persistParams, blobID int64) error {
	done := make(map[int64]struct{}, len(p.doneIDs))
	effective := 0
	for _, chID := range p.doneIDs {
		done[chID] = struct{}{}
		n, err := insertReplica(ctx, q, blobID, chID, channel.StateDone)
		if err != nil {
			return err
		}
		effective += int(n)
	}
	if effective < p.need {
		return fmt.Errorf("%w: 落库时仍在役的同步副本 %d 份,要求 %d 份", ErrInsufficientReplicas, effective, p.need)
	}
	for _, h := range p.channels {
		if _, ok := done[h.ID]; ok {
			continue
		}
		if _, err := q.InsertBlobChannelInService(ctx, sqlc.InsertBlobChannelInServiceParams{
			BlobID: blobID, State: channel.StatePending, ChannelID: h.ID,
		}); err != nil {
			return fmt.Errorf("写入副本状态(渠道 %d): %w", h.ID, err)
		}
	}
	return nil
}

// insertReplica 记下一份已写入的副本:渠道在役时计为有效副本;已标为清理中时照样记下、不计;已删除时不记。
// @param q 事务内查询集
// @param blobID 物理图主键
// @param channelID 渠道主键
// @param state 副本状态
// @return 计入有效副本的份数(0 或 1)
// @return 写入失败时的错误
func insertReplica(ctx context.Context, q *sqlc.Queries, blobID, channelID int64, state string) (int64, error) {
	n, err := q.InsertBlobChannelInService(ctx, sqlc.InsertBlobChannelInServiceParams{
		BlobID: blobID, State: state, ChannelID: channelID,
	})
	if err != nil {
		return 0, fmt.Errorf("写入副本状态(渠道 %d): %w", channelID, err)
	}
	if n > 0 {
		return n, nil
	}
	if _, err := q.InsertBlobChannelIfExists(ctx, sqlc.InsertBlobChannelIfExistsParams{
		BlobID: blobID, State: state, ChannelID: channelID,
	}); err != nil {
		return 0, fmt.Errorf("写入副本状态(渠道 %d): %w", channelID, err)
	}
	return 0, nil
}

// boolInt 布尔转 SQLite 的 0/1 整数。
// @param b 布尔值
// @return 1 表示 true,0 表示 false
func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
