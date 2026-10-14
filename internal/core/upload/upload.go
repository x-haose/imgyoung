// Package upload 实现上传链路的 Service 层编排:
// 格式校验 → 像素预算 → 净化(strip)→ 哈希(SHA256 + pHash)→ 存渠道 → 落库。
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
	"strings"
	"time"

	"github.com/corona10/goimagehash"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/internal/infra/imaging"

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
	// ErrInvalidImage 声明格式合法但内容无法解析。
	ErrInvalidImage = errors.New("upload: 图片内容无法解析")
)

// ChannelSelector 写入目标的选择与枚举(由 channel.Manager 实现)。
type ChannelSelector interface {
	// Healthy 返回健康渠道句柄,按优先级降序。
	Healthy() []channel.Handle
	// All 返回全部渠道句柄,按优先级降序。
	All() []channel.Handle
}

// putTimeout 单次渠道同步写入的超时上限——远端渠道半死时不允许无限挂起
// 占用并发闸名额(上限按 50MB 大图过慢速链路放宽)。
const putTimeout = 10 * time.Minute

// putWithTimeout 带超时地向渠道写入一份对象。
// @param h 目标渠道
// @param key 对象键
// @param data 完整字节
// @return 写入失败或超时时的错误
func putWithTimeout(ctx context.Context, h channel.Handle, key string, data []byte) error {
	putCtx, cancel := context.WithTimeout(ctx, putTimeout)
	defer cancel()
	return h.Driver.Put(putCtx, key, bytes.NewReader(data), int64(len(data)))
}

// ReplicationEnqueuer 副本补齐任务的入队口(由 replication.Runner 实现)。
type ReplicationEnqueuer interface {
	// EnqueueReplication 为目标渠道入队补齐任务(幂等)。
	EnqueueReplication(ctx context.Context, blobID int64, channelIDs []int64) error
}

// Config Service 的全部依赖与参数。
type Config struct {
	Conn       *sql.DB                // 数据库连接(事务用)
	Queries    *sqlc.Queries          // sqlc 查询集
	Channels   ChannelSelector        // 渠道选择器:主渠道同步写入,其余渠道记 pending 待补齐
	Replicator ReplicationEnqueuer    // 补齐入队口;可为 nil(pending 行由恢复扫描兜底)
	Settings   func() config.Settings // 运营配置快照读取口(逐次上传取值,热生效);像素预算/最少副本数/全局默认 TTL 出自它
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
	EXIFPolicy     imaging.EXIFPolicy // 本次上传生效的 EXIF 档位,必填;调用方解析"单次指定 > 全局默认"后传入;strip 按格式分发(JPEG/PNG/WebP 剥离,GIF 直通)
	TTL            time.Duration      // 请求显式指定的有效时长,仅 TTLSet 为 true 时生效;0 表示显式永久
	TTLSet         bool               // 请求是否显式指定 ttl;false 时按"用户默认 > 全局默认"解析,到期时刻由落库时刻推算
	ObjectKey      string             // S3 协议上传时客户端指定的对象键,可为空;同用户同键按 S3 语义覆盖(旧图让出别名,短 ID 仍可访问)
}

// Service 上传编排服务,无可变状态、并发安全。
type Service struct {
	cfg Config
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
	set := s.cfg.Settings()
	// 归属用户必填(多租户隔离的前提),档位必须是明确合法值。
	if opts.UserID == "" {
		return Result{}, errors.New("upload: 缺少归属用户")
	}
	if err := imaging.ValidatePolicy(opts.EXIFPolicy); err != nil {
		return Result{}, err
	}
	if imageID, found, err := s.imageByIdempotencyKey(ctx, opts); err != nil {
		return Result{}, err
	} else if found {
		return Result{ImageID: imageID}, nil
	}
	meta, err := inspect(data, set.MaxPixels())
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
	prep, err := prepare(data, meta.mime, opts.EXIFPolicy)
	if err != nil {
		return Result{}, err
	}
	shortID := id.New()
	// 对象键嵌入 short_id 前缀,作为元数据库全损时反向重建的最后手段。
	key := shortID + "_" + prep.sha256[:8]
	doneIDs, err := s.writeReplicas(ctx, key, prep.data, minSuccess(set))
	if err != nil {
		return Result{}, err
	}
	all := s.cfg.Channels.All()
	blobID, err := s.persist(ctx, persistParams{
		shortID: shortID, userID: opts.UserID, tokenID: opts.TokenID, fileName: truncateName(opts.FileName),
		idemKey: opts.IdempotencyKey, doneIDs: doneIDs, channels: all,
		sha256: prep.sha256, phash: prep.phash, key: key, mime: meta.mime, size: int64(len(prep.data)),
		width: int64(meta.width), height: int64(meta.height),
		ttl: resolveTTL(opts, user.DefaultTtlHours, set.DefaultTTL()), objectKey: opts.ObjectKey,
	})
	if err != nil {
		// 并发同幂等键竞争:另一请求可能已抢先落库,以先者为准。
		if imageID, found, _ := s.imageByIdempotencyKey(ctx, opts); found {
			return Result{ImageID: imageID}, nil
		}
		return Result{}, err
	}
	s.enqueuePending(ctx, blobID, shortID, all, doneIDs)
	return Result{ImageID: shortID}, nil
}

// imageByIdempotencyKey 查本用户同幂等键的既有上传;幂等键为空视为未命中。
// @param ctx 控制超时与取消
// @param opts 上传参数(取归属用户与幂等键)
// @return 既有引用短 ID
// @return 是否命中
// @return 查询失败时的错误
func (s *Service) imageByIdempotencyKey(ctx context.Context, opts Options) (string, bool, error) {
	if opts.IdempotencyKey == "" {
		return "", false, nil
	}
	imageID, err := s.cfg.Queries.GetIdempotencyImage(ctx, sqlc.GetIdempotencyImageParams{
		UserID: opts.UserID, Key: opts.IdempotencyKey,
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

// inspect 按 magic bytes 判定格式、只读图片头取尺寸并执行像素预算。
// @Description: 不信扩展名与客户端声明;超预算直接拒绝、不进完整解码器(内存三道闸之一)。
// @param data 上传字节
// @param maxPixels 像素预算
// @return 格式与尺寸
// @return ErrUnsupportedType / ErrInvalidImage / ErrTooManyPixels
func inspect(data []byte, maxPixels int64) (imageMeta, error) {
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
	data   []byte // 实际存储字节(strip 档位下已剥离元数据)
	sha256 string // 存储字节的 SHA-256 十六进制
	phash  int64  // 感知哈希,按位存入 int64
}

// prepare 按档位净化字节并计算指纹;净化在前、哈希在后,哈希对象即实际存储字节。
// @param data 上传字节
// @param mime 真实 MIME
// @param policy EXIF 档位
// @return 待存字节与指纹
// @return ErrInvalidImage 或哈希计算错误
func prepare(data []byte, mime string, policy imaging.EXIFPolicy) (prepared, error) {
	if policy == imaging.PolicyStrip {
		stripped, err := imaging.StripMetadata(data, mime)
		if err != nil {
			return prepared{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
		}
		data = stripped
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return prepared{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	ph, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return prepared{}, fmt.Errorf("计算感知哈希: %w", err)
	}
	sum := sha256.Sum256(data)
	return prepared{
		data:   data,
		sha256: hex.EncodeToString(sum[:]),
		phash:  int64(ph.GetHash()), //nolint:gosec // 64 位哈希按位存储,数值无语义,仅供汉明距离比较
	}, nil
}

// writeReplicas 按优先级向健康渠道同步写入,直到凑够 need 份;写入失败自动顺延下一渠道。
// @Description: 首个成功者即本次主渠道;其余渠道由补齐任务异步复制。
// @param ctx 控制超时与取消
// @param key 对象键
// @param data 待存字节
// @param need 最少同步副本数,>= 1
// @return 已写入的渠道 ID,按写入顺序
// @return 无健康渠道时为 channel.ErrNoHealthyChannel,份数不足时为 ErrInsufficientReplicas
func (s *Service) writeReplicas(ctx context.Context, key string, data []byte, need int) ([]int64, error) {
	healthy := s.cfg.Channels.Healthy()
	if len(healthy) == 0 {
		return nil, fmt.Errorf("选择主渠道: %w", channel.ErrNoHealthyChannel)
	}
	doneIDs := make([]int64, 0, need)
	for _, h := range healthy {
		if len(doneIDs) >= need {
			break
		}
		if err := putWithTimeout(ctx, h, key, data); err != nil {
			s.cfg.Logger.Warn("渠道写入失败,顺延下一渠道", "channel", h.Name, "err", err)
			continue
		}
		doneIDs = append(doneIDs, h.ID)
	}
	if len(doneIDs) < need {
		return nil, fmt.Errorf("%w: 要求 %d 份,仅写入 %d 份", ErrInsufficientReplicas, need, len(doneIDs))
	}
	return doneIDs, nil
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
	doneIDs       []int64          // 已同步写入的渠道 ID(至少含主渠道)
	channels      []channel.Handle // 本次上传使用的渠道快照(与 doneIDs 同源,避免两次取集合不一致)
	sha256        string           // 净化后字节哈希(十六进制)
	phash         int64            // 感知哈希(按位存入 int64)
	key           string           // 渠道对象键
	mime          string           // 真实 MIME
	size          int64            // 存储字节大小
	width, height int64            // 像素尺寸
	ttl           time.Duration    // 有效时长,<=0 表示永不过期;到期时刻与 ttl_seconds 均由此推算
	objectKey     string           // S3 对象键别名,空表示非 S3 途径
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
	if p.idemKey != "" {
		if err := q.CreateIdempotencyKey(ctx, sqlc.CreateIdempotencyKeyParams{
			UserID: p.userID, Key: p.idemKey, ImageID: p.shortID,
		}); err != nil {
			return 0, fmt.Errorf("写入幂等键: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务: %w", err)
	}
	return blobID, nil
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
	}); err != nil {
		return fmt.Errorf("写入引用记录: %w", err)
	}
	return nil
}

// insertReplicaStates 写入副本状态:已同步渠道就绪,其余渠道待补齐(outbox,补齐任务据此驱动)。
// @param ctx 控制超时与取消
// @param q 事务内查询集
// @param p 落库字段
// @param blobID 物理图主键
// @return 写入失败时的错误
func insertReplicaStates(ctx context.Context, q *sqlc.Queries, p persistParams, blobID int64) error {
	done := make(map[int64]struct{}, len(p.doneIDs))
	for _, chID := range p.doneIDs {
		done[chID] = struct{}{}
	}
	for _, h := range p.channels {
		state := channel.StatePending
		if _, ok := done[h.ID]; ok {
			state = channel.StateDone
		}
		if err := q.UpsertBlobChannel(ctx, sqlc.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: h.ID, State: state,
		}); err != nil {
			return fmt.Errorf("写入副本状态(渠道 %d): %w", h.ID, err)
		}
	}
	return nil
}
