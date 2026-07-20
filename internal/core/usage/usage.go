// Package usage 渠道用量监控与配额告警:
// 周期统计各渠道逻辑存储用量,暴露为指标;超过配额阈值时告警,防按量付费渠道账单爆炸。
package usage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// StorageGauge 渠道存储用量量表设置口(由 observability.Metrics 实现);
// 用接口而非具体类型,保持 usage 可脱离指标实现单测。
type StorageGauge interface {
	// SetChannelStorage 设置某渠道存储用量(字节)。
	SetChannelStorage(channel string, bytes float64)
}

// Config Reporter 依赖与参数。
type Config struct {
	Queries    *sqlc.Queries // sqlc 查询集
	Gauge      StorageGauge  // 存储用量量表设置口
	QuotaBytes func() int64  // 单渠道用量告警阈值读取口(字节),逐轮取值热生效;<=0 表示不告警,仅上报指标
	Logger     *slog.Logger  // 结构化日志
}

// Reporter 用量上报器,无可变状态、并发安全。
type Reporter struct {
	cfg Config // 依赖
}

// New 创建用量上报器。
// @param cfg 依赖,字段均必填(QuotaBytes 可为 0 表示不告警)
// @return 就绪的上报器
func New(cfg Config) *Reporter { return &Reporter{cfg: cfg} }

// ReportOnce 统计各渠道用量,更新指标,并对超阈值渠道告警。
// @return 查询失败时的错误
func (r *Reporter) ReportOnce(ctx context.Context) error {
	rows, err := r.cfg.Queries.ChannelStorageUsage(ctx)
	if err != nil {
		return fmt.Errorf("统计渠道用量: %w", err)
	}
	quota := r.cfg.QuotaBytes()
	for _, row := range rows {
		r.cfg.Gauge.SetChannelStorage(row.Name, float64(row.Bytes))
		if quota > 0 && row.Bytes > quota {
			r.cfg.Logger.Warn("渠道存储用量超过配额阈值,请留意账单/容量",
				"channel", row.Name, "bytes", row.Bytes, "quota_bytes", quota)
		}
	}
	return nil
}

// Run 周期上报,直至 ctx 取消;启动即先报一次。
// @param interval 上报间隔读取口,逐轮取值热生效
func (r *Reporter) Run(ctx context.Context, interval func() time.Duration) {
	if err := r.ReportOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.cfg.Logger.Warn("启动用量上报未完成", "err", err)
	}
	timer := time.NewTimer(interval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := r.ReportOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.cfg.Logger.Warn("定期用量上报未完成", "err", err)
			}
			timer.Reset(interval())
		}
	}
}
