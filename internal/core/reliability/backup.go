// Package reliability 元数据库快照备份:
// 定时 VACUUM INTO 产出一致性快照,同一份快照写到每个健康渠道,各渠道各自保留最近 N 份;
// 下载与恢复按优先级取第一个读得出该快照的渠道。
// 元数据比图片本身珍贵——渠道里是哈希名对象,映射丢失即全部不可反查。
package reliability

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/store/driver"
)

// Dir 备份对象在渠道内的目录前缀;对账 GC 必须排除该前缀。
const Dir = "_imgyoung/backup"

// snapshotTmpName 快照临时文件名(数据目录下,一轮备份写完各渠道即删)。
const snapshotTmpName = "backup-snapshot.tmp"

// snapshotTimeLayout 快照对象名里的时间戳格式,UTC;字典序即时间序。
const snapshotTimeLayout = "20060102-150405.000"

// Config 备份服务依赖。
type Config struct {
	Conn     *sql.DB              // 数据库读句柄(执行 VACUUM INTO)
	Channels *channel.Manager     // 渠道管理器;快照写到它的全部健康渠道
	DataDir  string               // 数据目录(快照临时文件所在)
	Keep     func() int           // 备份保留份数读取口,逐轮取值热生效,各渠道各自按它清理;取值必须 > 0
	Interval func() time.Duration // 备份间隔读取口,启动时据此判断各渠道上的快照是否仍新鲜;取值必须 > 0
	Logger   *slog.Logger         // 结构化日志
}

// Service 备份服务。
type Service struct {
	cfg         Config                        // 依赖
	mu          sync.Mutex                    // 串行化备份:各轮共用同一个快照临时文件,定时与手动备份不能交叠
	lastFailure atomic.Pointer[errcode.Error] // 最近一次备份的失败原因;nil 表示最近一次成功或尚未运行
}

// New 创建备份服务。
// @param cfg 依赖,字段均必填
// @return 就绪的备份服务
func New(cfg Config) *Service {
	return &Service{cfg: cfg}
}

// RunOnce 执行一次备份:快照一次 → 写入每个健康渠道并各自清理超额旧份;结果记入 LastFailure。
// @Description: 快照经各渠道驱动写入,加密渠道自动得到密文备份。单个渠道失败不影响其余渠道继续写,
// 但本轮记为失败,写成的渠道保留这份快照。写入期间占用各渠道的系统对象(见 channel.Manager.HoldSystemWrites),
// 期间被标为清理中的渠道跳过不写。恢复经 Stage 暂存、重启时 ApplyStaged 换入。
// @return 新快照,Channels 为写入的全部渠道;失败时为零值
// @return 带错误码的错误:没有健康渠道时(包裹 channel.ErrNoHealthyChannel)、生成快照失败,或任一渠道
// 上传、清理失败时列出各失败渠道及根因
func (s *Service) RunOnce(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.backup(ctx)
	s.recordResult(err)
	return snap, err
}

// backup 执行一次备份的全部步骤。
// @param ctx 控制超时与取消
// @return 新快照
// @return 选渠道、生成快照失败或有渠道未写成时的错误
func (s *Service) backup(ctx context.Context) (Snapshot, error) {
	targets := s.cfg.Channels.Healthy()
	if len(targets) == 0 {
		return Snapshot{}, errNoTarget()
	}
	f, size, err := s.vacuum(ctx)
	if err != nil {
		return Snapshot{}, errcode.Wrap(err, errcode.BackupSnapshotFailed, "detail", channel.Explain(err).Detail)
	}
	defer discard(f)

	taken := time.Now().UTC().Truncate(time.Millisecond)
	key := snapshotKey(taken)
	written := make([]string, 0, len(targets))
	var failures []string
	for _, h := range targets {
		stored, err := s.storeHeld(ctx, h, key, io.NewSectionReader(f, 0, size), size)
		if err != nil {
			failures = append(failures, h.Name+": "+s.cfg.Channels.ExplainFor(h.ID, err).Detail)
			s.cfg.Logger.Warn("快照写入渠道失败", "channel", h.Name, "key", key, "err", err)
			continue
		}
		if stored {
			written = append(written, h.Name)
		}
	}
	name := strings.TrimPrefix(key, Dir+"/")
	attempted := len(written) + len(failures)
	switch {
	case len(failures) > 0:
		return Snapshot{}, errcode.New(errcode.BackupStoreFailed, "snapshot", name,
			"failed", strconv.Itoa(len(failures)), "total", strconv.Itoa(attempted),
			"detail", strings.Join(failures, "; "))
	case attempted == 0:
		return Snapshot{}, errNoTarget()
	}
	return Snapshot{Name: name, Size: size, TakenAt: taken, Channels: written}, nil
}

// errNoTarget 没有可写快照的渠道。
// @return 包裹 channel.ErrNoHealthyChannel 的带错误码错误
func errNoTarget() error {
	return errcode.Wrap(fmt.Errorf("选择备份目标渠道: %w", channel.ErrNoHealthyChannel), errcode.NoHealthyChannel)
}

// storeHeld 占用渠道的系统对象后写入快照;渠道此刻已标为清理中则不写。
// @param h 目标渠道
// @param key 快照对象键
// @param r 快照内容
// @param size 字节数
// @return stored 是否写入
// @return err 上传或清理失败时的错误
func (s *Service) storeHeld(ctx context.Context, h channel.Handle, key string, r io.Reader, size int64) (stored bool,
	err error) {
	release, ok := s.cfg.Channels.HoldSystemWrites(h.ID)
	if !ok {
		return false, nil
	}
	defer release()
	return true, s.store(ctx, h, key, r, size)
}

// vacuum 用 VACUUM INTO 在数据目录生成一致性快照文件并打开它。
// @param ctx 控制超时与取消
// @return 快照文件,调用方用毕经 discard 关闭并删除
// @return 字节数
// @return 生成、打开或读取大小失败时的错误
func (s *Service) vacuum(ctx context.Context) (*os.File, int64, error) {
	tmp := filepath.Join(s.cfg.DataDir, snapshotTmpName)
	// 残留的上次临时文件会让 VACUUM INTO 失败,先清;删不掉时由 VACUUM INTO 报错。
	_ = os.Remove(tmp)
	if _, err := s.cfg.Conn.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return nil, 0, fmt.Errorf("生成数据库快照: %w", err)
	}
	info, err := os.Stat(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return nil, 0, fmt.Errorf("读取快照大小: %w", err)
	}
	f, err := os.Open(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return nil, 0, fmt.Errorf("打开快照文件: %w", err)
	}
	return f, info.Size(), nil
}

// discard 关闭并删除快照临时文件。
// @Description: 只读文件的关闭错误无可处理;删不掉的残留在下一轮备份前再删一次,仍删不掉则那一轮的 VACUUM INTO 报错。
// @param f 快照文件
func discard(f *os.File) {
	_ = f.Close()
	_ = os.Remove(f.Name())
}

// store 把快照写入一个渠道,再清理该渠道上超出保留份数的旧快照。
// @param ctx 控制超时与取消
// @param h 目标渠道
// @param key 快照对象键
// @param r 快照内容
// @param size 字节数
// @return 上传或清理失败时的错误
func (s *Service) store(ctx context.Context, h channel.Handle, key string, r io.Reader, size int64) error {
	if err := h.Driver.Put(ctx, key, r, size); err != nil {
		return fmt.Errorf("上传快照: %w", err)
	}
	s.cfg.Logger.Info("元数据库快照已备份", "channel", h.Name, "key", key, "bytes", size)
	return s.prune(ctx, h)
}

// Run 周期执行备份,直至 ctx 取消;启动时有健康渠道缺少备份间隔内的快照才先备份一次。
// @Description: 每次启动都备份会让频繁重启的实例用启动快照挤掉按保留份数轮换的历史
// (保留 7 份时一天内重启 7 次就把一周的快照全换成同一天的);各渠道上都有间隔内的快照即跳过。
// @param wait 周期闸,到点放行、ctx 取消即收敛
func (s *Service) Run(ctx context.Context, wait func(context.Context) bool) {
	due, err := s.startupDue(ctx)
	if err != nil {
		s.cfg.Logger.Warn("无法判断各渠道上一份快照的时刻,按需要备份处理", "err", err)
		due = true
	}
	if !due {
		s.cfg.Logger.Info("各健康渠道上都有备份间隔内的元数据库快照,启动时不重复备份")
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
// @return 失败原因;最近一次成功或尚未运行时为 nil
func (s *Service) LastFailure() *errcode.Error {
	return s.lastFailure.Load()
}

// recordResult 记下一次备份的结果,供 LastFailure 读取。
// @param err 本次备份的错误;nil 表示成功。backup 的失败都带错误码,不带的是代码缺陷,按服务内部错误记
func (s *Service) recordResult(err error) {
	if err == nil {
		s.lastFailure.Store(nil)
		return
	}
	e, ok := errcode.From(err)
	if !ok {
		e = errcode.Wrap(err, errcode.Internal)
	}
	s.lastFailure.Store(e)
}

// startupDue 判断启动时是否需要立即备份:任一健康渠道上没有备份间隔内的快照。
// @return 需要备份为 true
// @return 没有健康渠道(包裹 channel.ErrNoHealthyChannel)或任一渠道列举失败时的错误
func (s *Service) startupDue(ctx context.Context) (bool, error) {
	targets := s.cfg.Channels.Healthy()
	if len(targets) == 0 {
		return false, fmt.Errorf("选择备份目标渠道: %w", channel.ErrNoHealthyChannel)
	}
	interval := s.cfg.Interval()
	for _, h := range targets {
		snaps, err := listStored(ctx, h.Driver)
		if err != nil {
			return false, fmt.Errorf("渠道 %q: %w", h.Name, err)
		}
		if !slices.ContainsFunc(snaps, func(o stored) bool { return time.Since(o.takenAt) < interval }) {
			return true, nil
		}
	}
	return false, nil
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

// stored 渠道上的一份快照对象。
type stored struct {
	key     string    // 渠道内对象键
	size    int64     // 字节数
	takenAt time.Time // 快照时刻,UTC,取自对象名
}

// listStored 列出渠道上本服务生成的快照对象;按对象名识别,备份目录里的其它对象不计。
// @param d 渠道驱动
// @return 快照对象,顺序不定
// @return 列举失败时的错误
func listStored(ctx context.Context, d driver.Driver) ([]stored, error) {
	objs, err := d.List(ctx, Dir)
	if err != nil {
		return nil, fmt.Errorf("列举备份目录: %w", err)
	}
	out := make([]stored, 0, len(objs))
	for _, o := range objs {
		if ts, ok := snapshotTime(o.Key); ok {
			out = append(out, stored{key: o.Key, size: o.Size, takenAt: ts})
		}
	}
	return out, nil
}

// prune 清理渠道上超出保留份数的旧快照。
// @param h 快照所在渠道
// @return 列举或删除失败时的错误
func (s *Service) prune(ctx context.Context, h channel.Handle) error {
	snaps, err := listStored(ctx, h.Driver)
	if err != nil {
		return err
	}
	keep := s.cfg.Keep()
	if len(snaps) <= keep {
		return nil
	}
	slices.SortFunc(snaps, func(a, b stored) int { return b.takenAt.Compare(a.takenAt) })
	for _, o := range snaps[keep:] {
		if err := h.Driver.Delete(ctx, o.key); err != nil {
			return fmt.Errorf("清理旧快照 %q: %w", o.key, err)
		}
		s.cfg.Logger.Info("已清理过期备份", "channel", h.Name, "key", o.key)
	}
	return nil
}
