// Package reliability 元数据库快照备份:
// 定时 VACUUM INTO 产出一致性快照,上传到最高优先级健康渠道并保留最近 N 份。
// 元数据比图片本身珍贵——渠道里是哈希名对象,映射丢失即全部不可反查。
package reliability

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/store/driver"
)

// Dir 备份对象在渠道内的目录前缀;对账 GC 必须排除该前缀。
const Dir = "_imgyoung/backup"

// snapshotTmpName 快照临时文件名(数据目录下,成功上传后即删)。
const snapshotTmpName = "backup-snapshot.tmp"

// snapshotTimeLayout 快照对象名里的时间戳格式,UTC;字典序即时间序。
const snapshotTimeLayout = "20060102-150405.000"

// Config 备份服务依赖。
type Config struct {
	Conn     *sql.DB              // 数据库读句柄(执行 VACUUM INTO)
	Channels *channel.Manager     // 渠道管理器(选择上传目标)
	DataDir  string               // 数据目录(快照临时文件所在)
	Keep     func() int           // 备份保留份数读取口,逐轮取值热生效;取值必须 > 0
	Interval func() time.Duration // 备份间隔读取口,启动时据此判断上一份快照是否仍新鲜;取值必须 > 0
	Logger   *slog.Logger         // 结构化日志
}

// Service 备份服务。
type Service struct {
	cfg         Config                 // 依赖
	lastFailure atomic.Pointer[string] // 最近一次备份的失败原因;nil 表示最近一次成功或尚未运行
}

// New 创建备份服务。
// @param cfg 依赖,字段均必填
// @return 就绪的备份服务
func New(cfg Config) *Service {
	return &Service{cfg: cfg}
}

// RunOnce 执行一次备份:快照 → 上传主渠道 → 清理超额旧份;结果记入 LastFailure。
// @Description: 快照经渠道驱动写入,加密渠道自动得到密文备份;恢复经 Stage 暂存、重启时 ApplyStaged 换入。
// @return 新快照
// @return 快照、上传或清理失败时的错误
func (s *Service) RunOnce(ctx context.Context) (Snapshot, error) {
	snap, err := s.backup(ctx)
	s.recordResult(err)
	return snap, err
}

// backup 执行一次备份的全部步骤。
// @param ctx 控制超时与取消
// @return 新快照
// @return 快照、上传或清理失败时的错误
func (s *Service) backup(ctx context.Context) (Snapshot, error) {
	primary, err := s.cfg.Channels.Primary()
	if err != nil {
		return Snapshot{}, fmt.Errorf("选择备份目标渠道: %w", err)
	}

	tmp := filepath.Join(s.cfg.DataDir, snapshotTmpName)
	// 残留的上次临时文件会让 VACUUM INTO 失败,先清。
	_ = os.Remove(tmp)
	if _, err := s.cfg.Conn.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return Snapshot{}, fmt.Errorf("生成数据库快照: %w", err)
	}
	defer func() { _ = os.Remove(tmp) }()

	f, err := os.Open(tmp)
	if err != nil {
		return Snapshot{}, fmt.Errorf("打开快照文件: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return Snapshot{}, fmt.Errorf("读取快照大小: %w", err)
	}
	taken := time.Now()
	key := snapshotKey(taken)
	err = primary.Driver.Put(ctx, key, f, info.Size())
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("上传快照到渠道 %q: %w", primary.Name, err)
	}
	s.cfg.Logger.Info("元数据库快照已备份", "channel", primary.Name, "key", key, "bytes", info.Size())
	if err := s.prune(ctx, primary); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Name: strings.TrimPrefix(key, Dir+"/"), Size: info.Size(), TakenAt: taken.UTC()}, nil
}

// Run 周期执行备份,直至 ctx 取消;启动时上一份快照已超过一个间隔才先备份一次。
// @Description: 每次启动都备份会让频繁重启的实例用启动快照挤掉按保留份数轮换的历史
// (保留 7 份时一天内重启 7 次就把一周的快照全换成同一天的);上一份仍在间隔内即跳过。
// @param wait 周期闸,到点放行、ctx 取消即收敛
func (s *Service) Run(ctx context.Context, wait func(context.Context) bool) {
	due, err := s.startupDue(ctx)
	if err != nil {
		s.cfg.Logger.Warn("无法判断上一份快照时刻,按需要备份处理", "err", err)
		due = true
	}
	if !due {
		s.cfg.Logger.Info("上一份元数据库快照仍在备份间隔内,启动时不重复备份")
	} else if _, err := s.RunOnce(ctx); err != nil {
		s.cfg.Logger.Warn("启动备份未完成", "err", err)
	}
	for wait(ctx) {
		if _, err := s.RunOnce(ctx); err != nil {
			s.cfg.Logger.Warn("定时备份未完成", "err", err)
		}
	}
}

// LastFailure 返回最近一次备份的失败原因,供告警评估读取。
// @return message 失败原因;最近一次成功或尚未运行时为空串
// @return failed 最近一次是否失败
func (s *Service) LastFailure() (message string, failed bool) {
	if p := s.lastFailure.Load(); p != nil {
		return *p, true
	}
	return "", false
}

// recordResult 记下一次备份的结果,供 LastFailure 读取。
// @param err 本次备份的错误;nil 表示成功
func (s *Service) recordResult(err error) {
	if err == nil {
		s.lastFailure.Store(nil)
		return
	}
	msg := err.Error()
	s.lastFailure.Store(&msg)
}

// startupDue 判断启动时是否需要立即备份:主渠道上最新快照距今已超过一个备份间隔。
// @return 需要备份为 true;主渠道上没有任何快照也为 true
// @return 选渠道或列举失败时的错误
func (s *Service) startupDue(ctx context.Context) (bool, error) {
	primary, err := s.cfg.Channels.Primary()
	if err != nil {
		return false, fmt.Errorf("选择备份目标渠道: %w", err)
	}
	objs, err := primary.Driver.List(ctx, Dir)
	if err != nil {
		return false, fmt.Errorf("列举备份目录: %w", err)
	}
	var newest time.Time
	for _, o := range objs {
		if ts, ok := snapshotTime(o.Key); ok && ts.After(newest) {
			newest = ts
		}
	}
	return newest.IsZero() || time.Since(newest) >= s.cfg.Interval(), nil
}

// snapshotKey 生成快照在渠道内的对象键,时间戳为 UTC。
// @param t 快照时刻
// @return 形如 _imgyoung/backup/imgyoung-20060102-150405.000.db 的键
func snapshotKey(t time.Time) string {
	return fmt.Sprintf("%s/imgyoung-%s.db", Dir, t.UTC().Format(snapshotTimeLayout))
}

// snapshotTime 从快照对象键解析出快照时刻。
// @param key 渠道内对象键
// @return 快照时刻(UTC)与是否为本服务生成的快照
func snapshotTime(key string) (time.Time, bool) {
	stamp, ok := strings.CutPrefix(key, Dir+"/imgyoung-")
	if !ok {
		return time.Time{}, false
	}
	stamp, ok = strings.CutSuffix(stamp, ".db")
	if !ok {
		return time.Time{}, false
	}
	ts, err := time.ParseInLocation(snapshotTimeLayout, stamp, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// prune 清理超出保留份数的旧备份(文件名含时间戳,字典序即时间序)。
// @param h 备份所在渠道
// @return 列举或删除失败时的错误
func (s *Service) prune(ctx context.Context, h channel.Handle) error {
	objs, err := h.Driver.List(ctx, Dir)
	if err != nil {
		return fmt.Errorf("列举备份目录: %w", err)
	}
	keep := s.cfg.Keep()
	if len(objs) <= keep {
		return nil
	}
	slices.SortFunc(objs, func(a, b driver.ObjectInfo) int { return strings.Compare(b.Key, a.Key) })
	for _, o := range objs[keep:] {
		if err := h.Driver.Delete(ctx, o.Key); err != nil {
			return fmt.Errorf("清理旧备份 %q: %w", o.Key, err)
		}
		s.cfg.Logger.Info("已清理过期备份", "key", o.Key)
	}
	return nil
}
