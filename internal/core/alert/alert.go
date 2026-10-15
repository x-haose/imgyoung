// Package alert 告警:评估循环按现状判定哪些告警条件成立,与 alerts 表里未恢复的告警对账——
// 新成立的开启、不再成立的恢复,并在开启与恢复两个转折点向 Webhook 投递。
// 条件来源只读现状(渠道表、副本水位、用量、备份结果),各子系统不埋钩子。
package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// 告警类型标识。
const (
	KindChannelUnhealthy     = "channel_unhealthy"      // 渠道探测不健康
	KindReplicationFailed    = "replication_failed"     // 渠道上有补齐失败的副本
	KindChannelQuotaExceeded = "channel_quota_exceeded" // 渠道用量超过配额阈值
	KindDrainFailed          = "drain_failed"           // 清理中渠道的对象删除失败,排空仍在自动重试
	KindBackupFailed         = "backup_failed"          // 最近一次元数据库备份失败
)

// Condition 当前成立的一条告警条件。
type Condition struct {
	Kind    string // 告警类型
	Target  string // 告警对象,渠道名等;无对象为空串
	Message string // 人话描述,随每轮评估更新
}

// key 同一告警的身份:类型加对象。
type key struct {
	kind   string
	target string
}

// BackupStatus 备份结果的读取口(由 reliability.Service 实现)。
type BackupStatus interface {
	// LastFailure 返回最近一次备份的失败原因;最近一次成功或尚未运行时 failed 为 false。
	LastFailure() (message string, failed bool)
}

// Config 告警服务依赖。
type Config struct {
	Queries    *sqlc.Queries // 查询集:读现状、写告警表
	QuotaBytes func() int64  // 单渠道用量告警阈值读取口(字节),逐轮取值;<=0 表示不判定
	Backup     BackupStatus  // 备份结果读取口;nil 表示不判定备份告警
	WebhookURL func() string // 告警 Webhook 地址读取口,逐轮取值;空表示不外发
	Client     *http.Client  // 投递 Webhook 的 HTTP 客户端,须带超时
	Logger     *slog.Logger  // 结构化日志
}

// Service 告警服务,无可变状态、并发安全。
type Service struct {
	cfg Config // 依赖
}

// New 创建告警服务。
// @param cfg 依赖,Backup 可为 nil
// @return 就绪的服务
func New(cfg Config) *Service { return &Service{cfg: cfg} }

// EvaluateOnce 评估一轮:收集成立的条件,与未恢复告警对账,然后投递待通知的开启与恢复。
// @Description: 条件来源任一读取失败整轮放弃——半套现状做对账会把读不到的那类告警误判为已恢复。
// @param ctx 控制超时与取消
// @return 读取现状或写告警表失败时的错误
func (s *Service) EvaluateOnce(ctx context.Context) error {
	conditions, err := s.conditions(ctx)
	if err != nil {
		return err
	}
	if err := s.reconcile(ctx, conditions); err != nil {
		return err
	}
	return s.deliver(ctx)
}

// Run 周期评估直至 ctx 取消;启动即先评估一次。
// @param ctx 控制循环生命周期
// @param wait 周期闸,到点放行、ctx 取消即收敛
func (s *Service) Run(ctx context.Context, wait func(context.Context) bool) {
	if err := s.EvaluateOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.cfg.Logger.Warn("启动告警评估未完成", "err", err)
	}
	for wait(ctx) {
		if err := s.EvaluateOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.cfg.Logger.Warn("告警评估未完成", "err", err)
		}
	}
}

// reconcile 把成立的条件与未恢复告警对账:缺的开启、多的恢复、描述有变的更新。
// @param ctx 控制超时与取消
// @param conditions 本轮成立的条件
// @return 写告警表失败时的错误
func (s *Service) reconcile(ctx context.Context, conditions []Condition) error {
	open, err := s.cfg.Queries.ListOpenAlerts(ctx)
	if err != nil {
		return fmt.Errorf("读取未恢复告警: %w", err)
	}
	openByKey := make(map[key]sqlc.Alert, len(open))
	for _, a := range open {
		openByKey[key{a.Kind, a.Target}] = a
	}
	for _, c := range conditions {
		k := key{c.Kind, c.Target}
		existing, ok := openByKey[k]
		delete(openByKey, k)
		if !ok {
			err = s.open(ctx, c)
		} else {
			err = s.refresh(ctx, existing, c)
		}
		if err != nil {
			return err
		}
	}
	for _, stale := range openByKey {
		if err := s.cfg.Queries.ResolveAlert(ctx, stale.ID); err != nil {
			return fmt.Errorf("恢复告警 %d: %w", stale.ID, err)
		}
		s.cfg.Logger.Info("告警恢复", "kind", stale.Kind, "target", stale.Target)
	}
	return nil
}

// open 开启一条新告警。
// @param ctx 控制超时与取消
// @param c 成立的条件
// @return 写告警表失败时的错误
func (s *Service) open(ctx context.Context, c Condition) error {
	if _, err := s.cfg.Queries.OpenAlert(ctx, sqlc.OpenAlertParams{
		Kind: c.Kind, Target: c.Target, Message: c.Message,
	}); err != nil {
		return fmt.Errorf("开启告警 %s/%s: %w", c.Kind, c.Target, err)
	}
	s.cfg.Logger.Warn("告警开启", "kind", c.Kind, "target", c.Target, "message", c.Message)
	return nil
}

// refresh 条件仍成立时更新已开启告警的描述;描述未变则不写。
// @param ctx 控制超时与取消
// @param existing 已开启的告警行
// @param c 本轮条件
// @return 写告警表失败时的错误
func (s *Service) refresh(ctx context.Context, existing sqlc.Alert, c Condition) error {
	if existing.Message == c.Message {
		return nil
	}
	if err := s.cfg.Queries.UpdateAlertMessage(ctx, sqlc.UpdateAlertMessageParams{
		Message: c.Message, ID: existing.ID,
	}); err != nil {
		return fmt.Errorf("更新告警 %d 描述: %w", existing.ID, err)
	}
	return nil
}
