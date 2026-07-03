// Package backup 元数据库快照备份(设计定稿·可靠性与灾备):
// 定时 VACUUM INTO 产出一致性快照,上传到最高优先级健康渠道并保留最近 N 份。
// 元数据比图片本身珍贵——渠道里是哈希名对象,映射丢失即全部不可反查。
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/x-haose/imgyoung/internal/core/channel"
)

// Dir 备份对象在渠道内的目录前缀;对账 GC 必须排除该前缀。
const Dir = "_imgyoung/backup"

// snapshotTmpName 快照临时文件名(数据目录下,成功上传后即删)。
const snapshotTmpName = "backup-snapshot.tmp"

// Config 备份服务依赖。
type Config struct {
	// Conn 数据库连接(执行 VACUUM INTO)。
	Conn *sql.DB
	// Channels 渠道管理器(选择上传目标)。
	Channels *channel.Manager
	// DataDir 数据目录(快照临时文件所在)。
	DataDir string
	// Keep 保留的备份份数,必须 > 0。
	Keep int
	// Logger 结构化日志。
	Logger *slog.Logger
}

// Service 备份服务。
type Service struct {
	// cfg 依赖。
	cfg Config
}

// New 创建备份服务。
// @param cfg 依赖,字段均必填
// @return 就绪的备份服务
func New(cfg Config) *Service {
	return &Service{cfg: cfg}
}

// RunOnce 执行一次备份:快照 → 上传主渠道 → 清理超额旧份。
// @Description: 快照经渠道驱动写入,加密渠道自动得到密文备份;
// 恢复 = 从渠道取回快照文件替换 imgyoung.db(README·灾备)。
// @return 快照、上传或清理失败时的错误
func (s *Service) RunOnce(ctx context.Context) error {
	primary, err := s.cfg.Channels.Primary()
	if err != nil {
		return fmt.Errorf("选择备份目标渠道: %w", err)
	}

	tmp := filepath.Join(s.cfg.DataDir, snapshotTmpName)
	// 残留的上次临时文件会让 VACUUM INTO 失败,先清。
	_ = os.Remove(tmp)
	if _, err := s.cfg.Conn.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return fmt.Errorf("生成数据库快照: %w", err)
	}
	defer func() { _ = os.Remove(tmp) }()

	f, err := os.Open(tmp)
	if err != nil {
		return fmt.Errorf("打开快照文件: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("读取快照大小: %w", err)
	}
	key := fmt.Sprintf("%s/imgyoung-%s.db", Dir, time.Now().UTC().Format("20060102-150405.000"))
	err = primary.Driver.Put(ctx, key, f, info.Size())
	if cerr := f.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("上传快照到渠道 %q: %w", primary.Name, err)
	}
	s.cfg.Logger.Info("元数据库快照已备份", "channel", primary.Name, "key", key, "bytes", info.Size())

	return s.prune(ctx, primary)
}

// Run 按 interval 周期执行备份,直至 ctx 取消;启动即先备份一次。
// @param interval 备份间隔
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if err := s.RunOnce(ctx); err != nil {
		s.cfg.Logger.Warn("启动备份未完成", "err", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.RunOnce(ctx); err != nil {
				s.cfg.Logger.Warn("定时备份未完成", "err", err)
			}
		}
	}
}

// prune 清理超出保留份数的旧备份(文件名含时间戳,字典序即时间序)。
// @param h 备份所在渠道
// @return 列举或删除失败时的错误
func (s *Service) prune(ctx context.Context, h channel.Handle) error {
	objs, err := h.Driver.List(ctx, Dir)
	if err != nil {
		return fmt.Errorf("列举备份目录: %w", err)
	}
	if len(objs) <= s.cfg.Keep {
		return nil
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key > objs[j].Key })
	for _, o := range objs[s.cfg.Keep:] {
		if err := h.Driver.Delete(ctx, o.Key); err != nil {
			return fmt.Errorf("清理旧备份 %q: %w", o.Key, err)
		}
		s.cfg.Logger.Info("已清理过期备份", "key", o.Key)
	}
	return nil
}
