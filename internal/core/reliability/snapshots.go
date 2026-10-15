package reliability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/store/driver"
)

// ErrSnapshotNotFound 指定名字的快照不在任何健康渠道上。
var ErrSnapshotNotFound = errors.New("reliability: 快照不存在")

// ErrSnapshotName 快照名不是本服务生成的形态。
var ErrSnapshotName = errors.New("reliability: 快照名非法")

// DBFileName 元数据库在数据目录下的文件名。
const DBFileName = "imgyoung.db"

// restoreFileName 待换入的快照在数据目录下的文件名;启动时开库前换入。
const restoreFileName = DBFileName + ".restore"

// Snapshot 一份元数据库快照及持有它的渠道。
type Snapshot struct {
	Name     string    `json:"name"`     // 对象文件名,形如 imgyoung-20060102-150405.000.db
	Size     int64     `json:"size"`     // 字节数;取持有它的渠道里优先级最高者的
	TakenAt  time.Time `json:"taken_at"` // 快照时刻,UTC,毫秒精度
	Channels []string  `json:"channels"` // 持有该快照的渠道名,按优先级降序
}

// List 列出各健康渠道上快照的并集,最新在前。
// @param ctx 控制超时与取消
// @return 快照列表,可为空
// @return 没有健康渠道(包裹 channel.ErrNoHealthyChannel)或任一渠道列举失败时的错误
func (s *Service) List(ctx context.Context) ([]Snapshot, error) {
	targets := s.cfg.Channels.Healthy()
	if len(targets) == 0 {
		return nil, fmt.Errorf("选择备份渠道: %w", channel.ErrNoHealthyChannel)
	}
	byName := map[string]*Snapshot{}
	for _, h := range targets { // 已按优先级降序,渠道名随之有序
		snaps, err := listStored(ctx, h.Driver)
		if err != nil {
			return nil, fmt.Errorf("渠道 %q: %w", h.Name, err)
		}
		for _, o := range snaps {
			name := strings.TrimPrefix(o.key, Dir+"/")
			snap, ok := byName[name]
			if !ok {
				snap = &Snapshot{Name: name, Size: o.size, TakenAt: o.takenAt}
				byName[name] = snap
			}
			snap.Channels = append(snap.Channels, h.Name)
		}
	}
	out := make([]Snapshot, 0, len(byName))
	for _, snap := range byName {
		out = append(out, *snap)
	}
	slices.SortFunc(out, func(a, b Snapshot) int { return b.TakenAt.Compare(a.TakenAt) })
	return out, nil
}

// Open 打开一份快照供下载:按优先级依次尝试各健康渠道,没有该快照或读取失败的渠道换下一个。
// @param ctx 控制超时与取消
// @param name 快照名(List 返回的 Name)
// @return 快照字节流,调用方负责关闭
// @return 字节数
// @return ErrSnapshotName;没有健康渠道时包裹 channel.ErrNoHealthyChannel;各渠道都没有该快照时包裹
// ErrSnapshotNotFound;有渠道读取失败且没有渠道读出时为列出各渠道原因的错误
func (s *Service) Open(ctx context.Context, name string) (io.ReadCloser, int64, error) {
	key, err := snapshotKeyOf(name)
	if err != nil {
		return nil, 0, err
	}
	targets := s.cfg.Channels.Healthy()
	if len(targets) == 0 {
		return nil, 0, fmt.Errorf("选择备份渠道: %w", channel.ErrNoHealthyChannel)
	}
	var failures []string
	for _, h := range targets {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		rc, size, err := openStored(ctx, h.Driver, key)
		if err == nil {
			return rc, size, nil
		}
		if !errors.Is(err, driver.ErrNotFound) {
			s.cfg.Logger.Warn("渠道读取快照失败,换下一个渠道", "channel", h.Name, "snapshot", name, "err", err)
			failures = append(failures, fmt.Sprintf("渠道 %q: %v", h.Name, err))
		}
	}
	if len(failures) == 0 {
		return nil, 0, fmt.Errorf("%w: %s", ErrSnapshotNotFound, name)
	}
	return nil, 0, fmt.Errorf("读取快照 %s: %s", name, strings.Join(failures, "; "))
}

// openStored 在单个渠道上打开快照对象。
// @param d 渠道驱动
// @param key 快照对象键
// @return 字节流,调用方负责关闭
// @return 字节数
// @return 对象缺失时包裹 driver.ErrNotFound,其余为读取失败的错误
func openStored(ctx context.Context, d driver.Driver, key string) (io.ReadCloser, int64, error) {
	info, err := d.Stat(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	rc, err := d.Open(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	return rc, info.Size, nil
}

// Stage 把一份快照取回数据目录,等下次启动换入为元数据库。
// @Description: 运行中的 SQLite 文件不可能安全替换——全部句柄都指着它;恢复分两步:
// 本方法只落暂存文件,进程重启时由 ApplyStaged 在开库前换入。暂存文件先写临时名再改名,半截文件不会被当作快照。
// @param ctx 控制超时与取消
// @param name 快照名(List 返回的 Name)
// @return 暂存文件路径
// @return ErrSnapshotName / ErrSnapshotNotFound、渠道读取或落盘失败时的错误
func (s *Service) Stage(ctx context.Context, name string) (string, error) {
	rc, _, err := s.Open(ctx, name)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	target := filepath.Join(s.cfg.DataDir, restoreFileName)
	tmp, err := os.CreateTemp(s.cfg.DataDir, ".restore-*")
	if err != nil {
		return "", fmt.Errorf("创建暂存文件: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, rc); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("写入暂存文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("关闭暂存文件: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("落位暂存文件: %w", err)
	}
	s.cfg.Logger.Warn("快照已暂存,进程重启时换入为元数据库", "snapshot", name, "path", target)
	return target, nil
}

// ApplyStaged 在开库前检查数据目录里的暂存快照,存在则换入:现库连同 WAL 边车改名留存,暂存文件改名为库文件。
// @Description: 换入只做改名,任一步失败都不碰剩余文件,留给操作者处理;留存的旧库名带时间戳,不会被下次覆盖。
// @param dataDir 数据目录
// @param logger 结构化日志
// @return 是否换入了快照
// @return 改名失败时的错误
func ApplyStaged(dataDir string, logger *slog.Logger) (bool, error) {
	staged := filepath.Join(dataDir, restoreFileName)
	if _, err := os.Stat(staged); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("检查暂存快照: %w", err)
	}
	live := filepath.Join(dataDir, DBFileName)
	keep := live + ".pre-restore-" + time.Now().UTC().Format(snapshotTimeLayout)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(live+suffix, keep+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("留存现库 %s: %w", live+suffix, err)
		}
	}
	if err := os.Rename(staged, live); err != nil {
		return false, fmt.Errorf("换入暂存快照: %w", err)
	}
	logger.Warn("已用暂存快照换入元数据库,原库留存", "kept", keep)
	return true, nil
}

// snapshotKeyOf 把快照名还原为渠道内对象键,并校验其形态。
// @param name 快照名
// @return 对象键
// @return 名字不是本服务生成的形态时为 ErrSnapshotName
func snapshotKeyOf(name string) (string, error) {
	key := Dir + "/" + name
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", ErrSnapshotName
	}
	if _, ok := snapshotTime(key); !ok {
		return "", ErrSnapshotName
	}
	return key, nil
}
