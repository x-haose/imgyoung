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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// evictBatch 单轮淘汰的最多条数;分批循环直到降到容量以下,避免一次性拉全表。
const evictBatch = 32

// touchChunkSize 单条 TouchVariants 语句携带的 id 数上限;SQLite 绑定变量上限远高于此值,
// 分片让批量大小与该上限解耦。
const touchChunkSize = 500

// flushStopTimeout 停机前最后一次访问时间落库的超时预算。
const flushStopTimeout = 5 * time.Second

// Config 缓存存储依赖。
type Config struct {
	Dir      string        // variant 文件根目录
	Capacity func() int64  // 磁盘缓存上限读取口(字节),逐轮淘汰取值热生效;超过按 last_access 淘汰
	Queries  *sqlc.Queries // variants 表查询集
	Logger   *slog.Logger  // 结构化日志
}

// Store 衍生图磁盘缓存:文件按缓存键分片存放,DB 表为索引与 LRU 依据。
// @Description: 命中只在内存记下变体 id,访问时间由 Flush 周期批量落库——逐命中 UPDATE
// 会让读热路径与 SQLite 单写者争锁。
type Store struct {
	cfg     Config
	evictMu sync.Mutex         // 串行化 LRU 淘汰的"读总量→选最旧→删"临界区,避免并发 Put 触发的重复/过度淘汰
	touchMu sync.Mutex         // 保护 touched
	touched map[int64]struct{} // 自上次 Flush 以来命中过的变体 id
}

// Entry 一次缓存命中的内容。
type Entry struct {
	Data   []byte // 衍生图字节
	Format string // 目标格式(供定 Content-Type)
}

// New 构造缓存存储。
// @param cfg 依赖配置
// @return 存储
func New(cfg Config) *Store { return &Store{cfg: cfg, touched: map[int64]struct{}{}} }

// Key 计算某物理图在给定处理参数下的缓存键(兼本地文件名)。
// @Description: 覆盖影响输出字节的全部维度(blob 身份+宽度+格式+质量+编码调参);
// 任一配置变更即产生新键,旧变体经 LRU 自然淘汰,不会读到与新配置不符的陈旧结果。
// @param blobID 源物理图主键
// @param width 目标宽度像素
// @param format 目标格式
// @param quality 质量档
// @param tuning 编码调参(webp 为 method,jpeg 与 png 取 0)
// @return 64 位十六进制缓存键
func Key(blobID int64, width int, format string, quality, tuning int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s|%d|%d", blobID, width, format, quality, tuning)))
	return hex.EncodeToString(sum[:])
}

// Get 按缓存键取衍生图字节;命中记入待刷新集合,访问时间由 Flush 批量落库。
// @Description: 索引存在但文件缺失(手工删/坏盘)视为未命中,交由上层重建、不报错。
// @param ctx 上下文
// @param blobID 源物理图主键
// @param key 缓存键
// @return 命中内容
// @return 是否命中
// @return 错误
func (s *Store) Get(ctx context.Context, blobID int64, key string) (Entry, bool, error) {
	v, err := s.cfg.Queries.GetVariant(ctx, sqlc.GetVariantParams{BlobID: blobID, ParamsHash: key})
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("查缓存索引: %w", err)
	}
	data, err := os.ReadFile(s.pathFor(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 索引在但文件丢失:清掉残留索引行,免得其字节一直虚占容量、催发无谓淘汰。
			if derr := s.cfg.Queries.DeleteVariant(ctx, v.ID); derr != nil {
				s.cfg.Logger.Warn("清理残留 variant 索引失败", "id", v.ID, "err", derr)
			}
			return Entry{}, false, nil
		}
		return Entry{}, false, fmt.Errorf("读缓存文件: %w", err)
	}
	s.touchMu.Lock()
	s.touched[v.ID] = struct{}{}
	s.touchMu.Unlock()
	return Entry{Data: data, Format: v.Format}, true, nil
}

// Flush 把累积的命中批量落库为访问时间;失败的分片放回,留待下次重试。
// @Description: 访问时间只决定 LRU 淘汰顺序,一批丢失只降淘汰精度,不影响正确性。
// @param ctx 上下文
// @return 落库失败时的错误
func (s *Store) Flush(ctx context.Context) error {
	s.touchMu.Lock()
	if len(s.touched) == 0 {
		s.touchMu.Unlock()
		return nil
	}
	ids := slices.Collect(maps.Keys(s.touched))
	s.touched = map[int64]struct{}{}
	s.touchMu.Unlock()

	for start := 0; start < len(ids); start += touchChunkSize {
		end := min(start+touchChunkSize, len(ids))
		if err := s.cfg.Queries.TouchVariants(ctx, ids[start:end]); err != nil {
			s.requeue(ids[start:])
			return fmt.Errorf("刷新变体访问时间: %w", err)
		}
	}
	return nil
}

// requeue 把尚未落库的变体 id 放回待刷新集合。
// @param ids 待放回的 id
func (s *Store) requeue(ids []int64) {
	s.touchMu.Lock()
	for _, id := range ids {
		s.touched[id] = struct{}{}
	}
	s.touchMu.Unlock()
}

// Run 周期 Flush 直至 ctx 取消;停机前做最后一次 Flush 不丢累积。
// @param ctx 控制循环生命周期
// @param wait 周期闸,到点放行、ctx 取消即收敛
func (s *Store) Run(ctx context.Context, wait func(context.Context) bool) {
	for wait(ctx) {
		if err := s.Flush(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.cfg.Logger.Warn("变体访问时间 flush 未完成", "err", err)
		}
	}
	flushCtx, cancel := context.WithTimeout(context.Background(), flushStopTimeout)
	defer cancel()
	if err := s.Flush(flushCtx); err != nil {
		s.cfg.Logger.Warn("停机变体访问时间 flush 未完成", "err", err)
	}
}

// Put 落盘衍生图并写索引,随后按需触发容量淘汰。
// @Description: 先写文件后写索引;索引写失败回删文件避免孤儿。同键重复写幂等(upsert)。
// @param ctx 上下文
// @param blobID 源物理图主键
// @param key 缓存键
// @param width 目标宽度像素,0 表示不缩放;入索引供 CDN purge 还原 URL 参数
// @param format 目标格式
// @param data 衍生图字节
// @return 错误
func (s *Store) Put(ctx context.Context, blobID int64, key string, width int, format string, data []byte) error {
	path := s.pathFor(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("建缓存目录: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("写缓存文件: %w", err)
	}
	if err := s.cfg.Queries.UpsertVariant(ctx, sqlc.UpsertVariantParams{
		BlobID: blobID, ParamsHash: key, Width: int64(width), Format: format, Size: int64(len(data)),
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
// 各删一批而过度淘汰。先把内存里的命中落成访问时间,淘汰顺序才反映真实冷热。
// DeleteVariant 持续失败时(如磁盘满/DB 锁),用"整批零进展即止"守卫防死循环。
// @param ctx 上下文
func (s *Store) evict(ctx context.Context) {
	s.evictMu.Lock()
	defer s.evictMu.Unlock()

	if err := s.Flush(ctx); err != nil {
		s.cfg.Logger.Warn("淘汰前刷新访问时间失败,按已落库顺序淘汰", "err", err)
	}
	total, err := s.cfg.Queries.SumVariantsSize(ctx)
	if err != nil {
		s.cfg.Logger.Warn("统计缓存用量失败,跳过本次淘汰", "err", err)
		return
	}
	capacity := s.cfg.Capacity()
	for total > capacity {
		batch, lerr := s.cfg.Queries.ListVariantsLRU(ctx, evictBatch)
		if lerr != nil {
			s.cfg.Logger.Warn("列举待淘汰变体失败", "err", lerr)
			return
		}
		if len(batch) == 0 {
			return
		}
		freed, deleted := s.reclaim(ctx, batch)
		if deleted == 0 {
			// 整批都删不掉(索引删除持续失败):再循环只会拿到同一批、无限空转,让位下次触发。
			s.cfg.Logger.Warn("淘汰整批零进展,暂止本轮以防空转", "total", total, "capacity", capacity)
			return
		}
		total -= freed
	}
}

// reclaim 删除一批淘汰候选的文件与索引;索引删不掉的项不计入。
// @param ctx 上下文
// @param batch 待淘汰变体,由旧到新
// @return freed 释放的字节数
// @return deleted 索引已删除的条数
func (s *Store) reclaim(ctx context.Context, batch []sqlc.ListVariantsLRURow) (freed int64, deleted int) {
	for _, v := range batch {
		if rmErr := os.Remove(s.pathFor(v.ParamsHash)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			s.cfg.Logger.Warn("删淘汰缓存文件失败", "key", v.ParamsHash, "err", rmErr)
		}
		if delErr := s.cfg.Queries.DeleteVariant(ctx, v.ID); delErr != nil {
			s.cfg.Logger.Warn("删淘汰缓存索引失败", "id", v.ID, "err", delErr)
			continue
		}
		freed += v.Size
		deleted++
	}
	return freed, deleted
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
