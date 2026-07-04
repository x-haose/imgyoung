// Package variants 管理按需处理衍生图的本地磁盘缓存:字节落盘 + variants 表索引 + 容量上限 LRU 淘汰。
package variants

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// evictBatch 单轮淘汰的最多条数;分批循环直到降到容量以下,避免一次性拉全表。
const evictBatch = 32

// Config 缓存存储依赖。
type Config struct {
	Dir      string        // variant 文件根目录
	Capacity int64         // 磁盘缓存上限(字节),超过按 last_access 淘汰
	Queries  *sqlc.Queries // variants 表查询集
	Logger   *slog.Logger  // 结构化日志
}

// Store 衍生图磁盘缓存:文件按缓存键分片存放,DB 表为索引与 LRU 依据。
type Store struct {
	cfg     Config
	evictMu sync.Mutex // 串行化 LRU 淘汰的"读总量→选最旧→删"临界区,避免并发 Put 触发的重复/过度淘汰
}

// New 构造缓存存储。
// @param cfg 依赖配置
// @return 存储
func New(cfg Config) *Store { return &Store{cfg: cfg} }

// Key 计算某物理图在给定处理参数下的缓存键(兼本地文件名)。
// @Description: 覆盖影响输出字节的全部维度(blob 身份+宽度+格式+质量+编码调参);
// 任一配置变更即产生新键,旧变体经 LRU 自然淘汰,不会读到与新配置不符的陈旧结果。
// @param blobID 源物理图主键
// @param width 目标宽度像素
// @param format 目标格式
// @param quality 质量档
// @param tuning 编码调参(webp method / avif speed / jpeg 取 0)
// @return 64 位十六进制缓存键
func Key(blobID int64, width int, format string, quality, tuning int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%d|%d", blobID, width, format, quality, tuning)))
	return hex.EncodeToString(sum[:])
}

// Get 按缓存键取衍生图字节;命中则刷新访问时间。
// @Description: 索引存在但文件缺失(手工删/坏盘)视为未命中,交由上层重建、不报错。
// @param ctx 上下文
// @param blobID 源物理图主键
// @param key 缓存键
// @return 衍生图字节
// @return 目标格式(供定 Content-Type)
// @return 是否命中
// @return 错误
func (s *Store) Get(ctx context.Context, blobID int64, key string) ([]byte, string, bool, error) {
	v, err := s.cfg.Queries.GetVariant(ctx, sqlc.GetVariantParams{BlobID: blobID, ParamsHash: key})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("查缓存索引: %w", err)
	}
	data, err := os.ReadFile(s.pathFor(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 索引在但文件丢失:清掉残留索引行,免得其字节一直虚占容量、催发无谓淘汰。
			if derr := s.cfg.Queries.DeleteVariant(ctx, v.ID); derr != nil {
				s.cfg.Logger.Warn("清理残留 variant 索引失败", "id", v.ID, "err", derr)
			}
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("读缓存文件: %w", err)
	}
	if terr := s.cfg.Queries.TouchVariant(ctx, v.ID); terr != nil {
		s.cfg.Logger.Warn("刷新 variant 访问时间失败", "id", v.ID, "err", terr)
	}
	return data, v.Format, true, nil
}

// Put 落盘衍生图并写索引,随后按需触发容量淘汰。
// @Description: 先写文件后写索引;索引写失败回删文件避免孤儿。同键重复写幂等(upsert)。
// @param ctx 上下文
// @param blobID 源物理图主键
// @param key 缓存键
// @param format 目标格式
// @param data 衍生图字节
// @return 错误
func (s *Store) Put(ctx context.Context, blobID int64, key, format string, data []byte) error {
	path := s.pathFor(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("建缓存目录: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写缓存文件: %w", err)
	}
	if err := s.cfg.Queries.UpsertVariant(ctx, sqlc.UpsertVariantParams{
		BlobID: blobID, ParamsHash: key, Format: format, Size: int64(len(data)),
	}); err != nil {
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			s.cfg.Logger.Warn("回删孤儿缓存文件失败", "key", key, "err", rmErr)
		}
		return fmt.Errorf("写缓存索引: %w", err)
	}
	s.evict(ctx)
	return nil
}

// DeleteByBlob 删除某物理图的全部衍生图(文件 + 索引),供删除级联调用。
// @param ctx 上下文
// @param blobID 源物理图主键
// @return 错误
func (s *Store) DeleteByBlob(ctx context.Context, blobID int64) error {
	rows, err := s.cfg.Queries.ListVariantsByBlob(ctx, blobID)
	if err != nil {
		return fmt.Errorf("列举物理图变体: %w", err)
	}
	for _, r := range rows {
		if rmErr := os.Remove(s.pathFor(r.ParamsHash)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			s.cfg.Logger.Warn("删变体文件失败", "key", r.ParamsHash, "err", rmErr)
		}
	}
	if err := s.cfg.Queries.DeleteVariantsByBlob(ctx, blobID); err != nil {
		return fmt.Errorf("删变体索引: %w", err)
	}
	return nil
}

// evict 在总量超上限时按 last_access 由旧到新淘汰,直到降回容量以下。
// @Description: evictMu 串行化本临界区——Put 由多 worker 并发调用,无锁会让并发 evict 各读到超容量、
// 各删一批而过度淘汰。DeleteVariant 持续失败时(如磁盘满/DB 锁),用"整批零进展即止"守卫防死循环。
// @param ctx 上下文
func (s *Store) evict(ctx context.Context) {
	s.evictMu.Lock()
	defer s.evictMu.Unlock()

	total, err := s.cfg.Queries.SumVariantsSize(ctx)
	if err != nil {
		s.cfg.Logger.Warn("统计缓存用量失败,跳过本次淘汰", "err", err)
		return
	}
	for total > s.cfg.Capacity {
		batch, lerr := s.cfg.Queries.ListVariantsLRU(ctx, evictBatch)
		if lerr != nil {
			s.cfg.Logger.Warn("列举待淘汰变体失败", "err", lerr)
			return
		}
		if len(batch) == 0 {
			return
		}
		progressed := false
		for _, v := range batch {
			if rmErr := os.Remove(s.pathFor(v.ParamsHash)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				s.cfg.Logger.Warn("删淘汰缓存文件失败", "key", v.ParamsHash, "err", rmErr)
			}
			if delErr := s.cfg.Queries.DeleteVariant(ctx, v.ID); delErr != nil {
				s.cfg.Logger.Warn("删淘汰缓存索引失败", "id", v.ID, "err", delErr)
				continue
			}
			total -= v.Size
			progressed = true
		}
		if !progressed {
			// 整批都删不掉(索引删除持续失败):再循环只会拿到同一批、无限空转,让位下次触发。
			s.cfg.Logger.Warn("淘汰整批零进展,暂止本轮以防空转", "total", total, "capacity", s.cfg.Capacity)
			return
		}
	}
}

// pathFor 返回缓存键对应的本地文件路径;按键前两位分片,避免单目录文件过多。
// @param key 缓存键(64 位十六进制)
// @return 本地文件绝对/相对路径
func (s *Store) pathFor(key string) string {
	return filepath.Join(s.cfg.Dir, key[:2], key)
}

// writeFileAtomic 以临时文件写入后 rename,避免读到半截文件。
// @param path 目标路径
// @param data 待写字节
// @return 错误
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
