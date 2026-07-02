// Package upload 实现上传链路的 Service 层编排(设计定稿·上传链路):
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
	"net/http"

	"github.com/corona10/goimagehash"

	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/id"
	"github.com/x-haose/imgyoung/internal/imaging"
	"github.com/x-haose/imgyoung/internal/storage"

	// 注册白名单格式的解码器(gif/jpeg/png 标准库,webp 仅解码)。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// allowedMIME 图片格式白名单(设计定稿·滥用防护:格式白名单)。
// SVG 刻意不在列(XSS 风险,设计定稿·格式安全)。
var allowedMIME = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

// 上传链路的业务错误哨兵,由 API 层映射为 HTTP 状态码。
var (
	// ErrUnsupportedType 内容不在图片格式白名单内(按 magic bytes 判定)。
	ErrUnsupportedType = errors.New("upload: 不支持的文件类型")
	// ErrTooManyPixels 图片分辨率超出像素预算(内存三道闸之一)。
	ErrTooManyPixels = errors.New("upload: 图片分辨率超出限制")
	// ErrInvalidImage 声明格式合法但内容无法解析。
	ErrInvalidImage = errors.New("upload: 图片内容无法解析")
	// ErrUnknownPolicy EXIF 策略取值非法或尚未实现(如实验特性 spoof)。
	ErrUnknownPolicy = errors.New("upload: 未知的 EXIF 策略")
)

// EXIFPolicy EXIF 处理档位(设计定稿·EXIF 三档)。
type EXIFPolicy string

// EXIF 档位取值;spoof 为实验特性,尚未实现,取值时返回 ErrUnknownPolicy。
// 档位决策(全局默认/单次覆盖)由调用方(API 层)完成,本包只接受明确档位。
const (
	// PolicyStrip 剥离全部元数据。
	PolicyStrip EXIFPolicy = "strip"
	// PolicyPassthrough 保留原样,不动任何元数据(全局默认档)。
	PolicyPassthrough EXIFPolicy = "passthrough"
)

// Config Service 的全部依赖与参数。
type Config struct {
	// Conn 数据库连接(事务用)。
	Conn *sql.DB
	// Queries sqlc 查询集。
	Queries *db.Queries
	// Driver 目标渠道存储驱动。
	Driver storage.Driver
	// MaxPixels 像素预算(width × height 上限)。
	MaxPixels int64
}

// Options 一次上传的可选参数。
type Options struct {
	// FileName 原始文件名(仅存元数据,不参与任何逻辑)。
	FileName string
	// IdempotencyKey 客户端幂等键,可为空。
	IdempotencyKey string
	// EXIFPolicy 本次上传生效的 EXIF 档位,必填——由调用方解析
	// "单次指定 > 全局默认"后传入明确值,本包不做隐式回退。
	EXIFPolicy EXIFPolicy
}

// Service 上传编排服务,无可变状态、并发安全。
type Service struct {
	cfg Config
}

// Result 一次上传的结果。
type Result struct {
	// ImageID 对外短 ID。
	ImageID string
}

// New 创建上传服务。
// @param cfg 依赖与参数,字段均必填
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
	fileName, idemKey := opts.FileName, opts.IdempotencyKey
	if idemKey != "" {
		imageID, err := s.cfg.Queries.GetIdempotencyImage(ctx, idemKey)
		if err == nil {
			return Result{ImageID: imageID}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Result{}, fmt.Errorf("查询幂等键: %w", err)
		}
	}

	// 只信 magic bytes,不信扩展名与客户端声明(设计定稿·格式校验)。
	mime := http.DetectContentType(data)
	if _, ok := allowedMIME[mime]; !ok {
		return Result{}, ErrUnsupportedType
	}

	// 像素预算:只读图片头取尺寸,超限直接拒绝、不进完整解码器(内存三道闸)。
	meta, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	if int64(meta.Width)*int64(meta.Height) > s.cfg.MaxPixels {
		return Result{}, ErrTooManyPixels
	}

	if err := validatePolicy(opts.EXIFPolicy); err != nil {
		return Result{}, err
	}
	// 净化在前、哈希在后:哈希对象 = 实际存储字节(设计定稿·数据流)。
	if opts.EXIFPolicy == PolicyStrip && mime == "image/jpeg" {
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
	// 对象键嵌入 short_id 前缀,作为元数据库全损时反向重建的最后手段(设计定稿·命名)。
	key := shortID + "_" + hex.EncodeToString(sum[:4])

	if err = s.cfg.Driver.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		return Result{}, fmt.Errorf("写入渠道: %w", err)
	}

	// Put 成功而落库失败会产生孤儿对象,由对账任务回收(迭代 3),此处不回滚渠道写入。
	err = s.persist(ctx, persistParams{
		shortID:  shortID,
		fileName: fileName,
		idemKey:  idemKey,
		sha256:   hex.EncodeToString(sum[:]),
		phash:    int64(ph.GetHash()), //nolint:gosec // 64 位哈希按位存储,数值无语义,仅供汉明距离比较
		key:      key,
		mime:     mime,
		size:     int64(len(data)),
		width:    int64(meta.Width),
		height:   int64(meta.Height),
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
	return Result{ImageID: shortID}, nil
}

// validatePolicy 校验档位为已实现的明确取值。
// @Description: 空值、实验特性 spoof 与乱填值一律返回 ErrUnknownPolicy——
// 不做隐式回退,档位决策必须由调用方显式完成(不静默降级)。
// @param p 调用方传入的档位
// @return 取值非法时的错误
func validatePolicy(p EXIFPolicy) error {
	switch p {
	case PolicyStrip, PolicyPassthrough:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownPolicy, p)
	}
}

// persistParams 一次落库所需的全部字段。
type persistParams struct {
	// shortID 对外短 ID。
	shortID string
	// fileName 原始文件名。
	fileName string
	// idemKey 幂等键,可为空。
	idemKey string
	// sha256 净化后字节哈希(十六进制)。
	sha256 string
	// phash 感知哈希(按位存入 int64)。
	phash int64
	// key 渠道对象键。
	key string
	// mime 真实 MIME。
	mime string
	// size 存储字节大小。
	size int64
	// width/height 像素尺寸。
	width, height int64
}

// persist 在单事务内写入 blob、引用与幂等键。
// @return 事务执行失败时的错误
func (s *Service) persist(ctx context.Context, p persistParams) error {
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
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
		return fmt.Errorf("写入 blob 记录: %w", err)
	}
	if err = q.CreateImage(ctx, db.CreateImageParams{
		ID:       p.shortID,
		BlobID:   blobID,
		FileName: p.fileName,
	}); err != nil {
		return fmt.Errorf("写入引用记录: %w", err)
	}
	if p.idemKey != "" {
		if err = q.CreateIdempotencyKey(ctx, db.CreateIdempotencyKeyParams{
			Key:     p.idemKey,
			ImageID: p.shortID,
		}); err != nil {
			return fmt.Errorf("写入幂等键: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}
	return nil
}
