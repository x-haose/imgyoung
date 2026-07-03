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
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/internal/infra/imaging"

	// 注册白名单格式的解码器(gif/jpeg/png 标准库,webp 仅解码)。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

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
	Conn       *sql.DB             // 数据库连接(事务用)
	Queries    *db.Queries         // sqlc 查询集
	Channels   ChannelSelector     // 渠道选择器:主渠道同步写入,其余渠道记 pending 待补齐
	Replicator ReplicationEnqueuer // 补齐入队口;可为 nil(pending 行由恢复扫描兜底)
	MaxPixels  int64               // 像素预算(width × height 上限)
	MinSuccess int                 // 返回成功所需的最少副本数,小于 1 视为 1
	Logger     *slog.Logger        // 结构化日志
}

// ErrInsufficientReplicas 同步副本数不足 MinSuccess 要求。
var ErrInsufficientReplicas = errors.New("upload: 副本数不足")

// Options 一次上传的可选参数。
type Options struct {
	FileName       string             // 原始文件名(仅存元数据,不参与任何逻辑)
	IdempotencyKey string             // 客户端幂等键,可为空
	EXIFPolicy     imaging.EXIFPolicy // 本次上传生效的 EXIF 档位,必填;调用方解析"单次指定 > 全局默认"后传入,本包不做隐式回退;strip 仅对 JPEG 生效
	ExpireAt       time.Time          // 本次上传引用的到期时刻,零值表示永不过期;调用方按"单次指定 > 全局默认"解析后传入
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
// @Description: 幂等键非空时启用幂等:命中已有键直接返回既有结果。
// @param data 完整的上传字节(调用方已做大小闸)
// @param opts 文件名/幂等键/EXIF 档位等可选参数
// @return 上传结果
// @return 业务错误(见哨兵)或基础设施错误
func (s *Service) Process(ctx context.Context, data []byte, opts Options) (Result, error) {
	// 输入验证先行:档位必须是明确合法值,文件名限长(仅元数据,不影响逻辑)。
	if err := imaging.ValidatePolicy(opts.EXIFPolicy); err != nil {
		return Result{}, err
	}
	fileName, idemKey := truncateName(opts.FileName), opts.IdempotencyKey
	if idemKey != "" {
		imageID, err := s.cfg.Queries.GetIdempotencyImage(ctx, idemKey)
		if err == nil {
			return Result{ImageID: imageID}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Result{}, fmt.Errorf("查询幂等键: %w", err)
		}
	}

	// 只信 magic bytes,不信扩展名与客户端声明。
	mime := http.DetectContentType(data)
	if _, ok := allowedMIME[mime]; !ok {
		return Result{}, ErrUnsupportedType
	}

	// 像素预算:只读图片头取尺寸,超限直接拒绝、不进完整解码器(内存三道闸)。
	meta, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	if formatMIME[format] != mime {
		return Result{}, fmt.Errorf("%w: 嗅探类型 %s 与解码格式 %s 不一致", ErrInvalidImage, mime, format)
	}
	if int64(meta.Width)*int64(meta.Height) > s.cfg.MaxPixels {
		return Result{}, ErrTooManyPixels
	}

	// 净化在前、哈希在后:哈希对象 = 实际存储字节。
	if opts.EXIFPolicy == imaging.PolicyStrip && mime == mimeJPEG {
		stripped, serr := imaging.StripJPEGMetadata(data)
		if serr != nil {
			return Result{}, fmt.Errorf("%w: %w", ErrInvalidImage, serr)
		}
		data = stripped
	}
	sum := sha256.Sum256(data)

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	ph, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return Result{}, fmt.Errorf("计算感知哈希: %w", err)
	}

	shortID, err := id.New()
	if err != nil {
		return Result{}, fmt.Errorf("生成短 ID: %w", err)
	}
	// 对象键嵌入 short_id 前缀,作为元数据库全损时反向重建的最后手段。
	key := shortID + "_" + hex.EncodeToString(sum[:4])

	// 写入语义:按优先级向健康渠道同步写入,首个成功者即本次主渠道,
	// 写入失败自动顺延下一渠道;
	// min_success 可要求更多同步副本,其余渠道由补齐任务异步复制。
	healthy := s.cfg.Channels.Healthy()
	if len(healthy) == 0 {
		return Result{}, fmt.Errorf("选择主渠道: %w", channel.ErrNoHealthyChannel)
	}
	doneIDs := make([]int64, 0, s.minSuccess())
	for _, h := range healthy {
		if len(doneIDs) >= s.minSuccess() {
			break
		}
		if perr := putWithTimeout(ctx, h, key, data); perr != nil {
			s.cfg.Logger.Warn("渠道写入失败,顺延下一渠道", "channel", h.Name, "err", perr)
			continue
		}
		doneIDs = append(doneIDs, h.ID)
	}
	if len(doneIDs) < s.minSuccess() {
		return Result{}, fmt.Errorf("%w: 要求 %d 份,仅写入 %d 份", ErrInsufficientReplicas, s.minSuccess(), len(doneIDs))
	}
	all := s.cfg.Channels.All()

	// Put 成功而落库失败会产生孤儿对象,由对账任务回收,此处不回滚渠道写入。
	blobID, err := s.persist(ctx, persistParams{
		shortID:  shortID,
		fileName: fileName,
		idemKey:  idemKey,
		doneIDs:  doneIDs,
		channels: all,
		sha256:   hex.EncodeToString(sum[:]),
		phash:    int64(ph.GetHash()), //nolint:gosec // 64 位哈希按位存储,数值无语义,仅供汉明距离比较
		key:      key,
		mime:     mime,
		size:     int64(len(data)),
		width:    int64(meta.Width),
		height:   int64(meta.Height),
		expireAt: opts.ExpireAt,
	})
	if err != nil {
		// 并发同幂等键竞争:另一请求可能已抢先落库,以先者为准。
		if idemKey != "" {
			if imageID, qerr := s.cfg.Queries.GetIdempotencyImage(ctx, idemKey); qerr == nil {
				return Result{ImageID: imageID}, nil
			}
		}
		return Result{}, err
	}

	// 触发异步补齐;入队失败仅告警——pending 行由恢复扫描兜底(围栏设计)。
	if s.cfg.Replicator != nil {
		if pending := pendingIDs(all, doneIDs); len(pending) > 0 {
			if qerr := s.cfg.Replicator.EnqueueReplication(ctx, blobID, pending); qerr != nil {
				s.cfg.Logger.Warn("补齐任务入队失败,待恢复扫描兜底", "image", shortID, "err", qerr)
			}
		}
	}
	return Result{ImageID: shortID}, nil
}

// minSuccess 归一化最少副本数(至少 1)。
func (s *Service) minSuccess() int {
	if s.cfg.MinSuccess < 1 {
		return 1
	}
	return s.cfg.MinSuccess
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
	fileName      string           // 原始文件名
	idemKey       string           // 幂等键,可为空
	doneIDs       []int64          // 已同步写入的渠道 ID(至少含主渠道)
	channels      []channel.Handle // 本次上传使用的渠道快照(与 doneIDs 同源,避免两次取集合不一致)
	sha256        string           // 净化后字节哈希(十六进制)
	phash         int64            // 感知哈希(按位存入 int64)
	key           string           // 渠道对象键
	mime          string           // 真实 MIME
	size          int64            // 存储字节大小
	width, height int64            // 像素尺寸
	expireAt      time.Time        // 到期时刻,零值表示永不过期
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
	blobID, err := q.CreateBlob(ctx, db.CreateBlobParams{
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
	expireAt := sql.NullString{}
	if !p.expireAt.IsZero() {
		expireAt = sql.NullString{String: db.TimeText(p.expireAt), Valid: true}
	}
	if err = q.CreateImage(ctx, db.CreateImageParams{
		ID:       p.shortID,
		BlobID:   blobID,
		UserID:   sql.NullString{String: db.DefaultUserID, Valid: true},
		FileName: p.fileName,
		ExpireAt: expireAt,
	}); err != nil {
		return 0, fmt.Errorf("写入引用记录: %w", err)
	}
	// 副本状态:已同步渠道就绪,其余渠道待补齐(outbox,补齐任务据此驱动)。
	done := make(map[int64]struct{}, len(p.doneIDs))
	for _, chID := range p.doneIDs {
		done[chID] = struct{}{}
	}
	for _, h := range p.channels {
		state := channel.StatePending
		if _, ok := done[h.ID]; ok {
			state = channel.StateDone
		}
		if err = q.UpsertBlobChannel(ctx, db.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: h.ID, State: state,
		}); err != nil {
			return 0, fmt.Errorf("写入副本状态(渠道 %d): %w", h.ID, err)
		}
	}
	if p.idemKey != "" {
		if err = q.CreateIdempotencyKey(ctx, db.CreateIdempotencyKeyParams{
			Key:     p.idemKey,
			ImageID: p.shortID,
		}); err != nil {
			return 0, fmt.Errorf("写入幂等键: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务: %w", err)
	}
	return blobID, nil
}
