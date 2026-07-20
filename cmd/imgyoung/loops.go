// ImgYoung 后台周期循环:认证维护与渠道健康探测,均为兜底型,由 serve 编排启停。
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
)

// healthProbeTimeout 单轮健康探测的整体预算。
const healthProbeTimeout = 30 * time.Second

// tokenUsageFlushInterval Token 使用时刻批量回写间隔。
const tokenUsageFlushInterval = time.Minute

// sessionCleanupInterval 过期会话回收间隔。
const sessionCleanupInterval = time.Hour

// authMaintenanceLoop 认证维护循环:周期回写 Token 使用时刻、回收过期会话。
// @param ctx 控制循环生命周期
// @param authSvc 认证服务
// @param logger 结构化日志
func authMaintenanceLoop(ctx context.Context, authSvc *auth.Service, logger *slog.Logger) {
	usageTicker := time.NewTicker(tokenUsageFlushInterval)
	defer usageTicker.Stop()
	sessionTicker := time.NewTicker(sessionCleanupInterval)
	defer sessionTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			// 停机前把内存中的使用时刻冲干净,不丢最后一批;沿用原 ctx 的值但脱离其取消。
			authSvc.FlushTokenUsage(context.WithoutCancel(ctx))
			return
		case <-usageTicker.C:
			authSvc.FlushTokenUsage(ctx)
		case <-sessionTicker.C:
			if n, err := authSvc.DeleteExpiredSessions(ctx); err != nil {
				logger.Warn("回收过期会话失败", "err", err)
			} else if n > 0 {
				logger.Info("已回收过期会话", "count", n)
			}
		}
	}
}

// healthLoop 周期探测各渠道健康,状态变化写回 DB 并更新内存标记。
// @param ctx 控制循环生命周期
// @param interval 探测间隔读取口,逐轮取值热生效
func healthLoop(ctx context.Context, interval func() time.Duration, manager *channel.Manager, logger *slog.Logger) {
	timer := time.NewTimer(interval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
			changed, err := manager.CheckAll(probeCtx)
			cancel()
			if err != nil {
				logger.Warn("健康检查未完成", "err", err)
			}
			if len(changed) > 0 {
				logger.Info("渠道健康状态变化", "channels", changed)
			}
			timer.Reset(interval())
		}
	}
}
