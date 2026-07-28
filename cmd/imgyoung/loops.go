// ImgYoung 后台周期循环:认证维护与渠道健康探测,均为兜底型,由 serve 编排启停。
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/config"
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

// healthLoop 周期探测各渠道健康,状态变化写回 DB 并更新内存标记;
// 同轮顺带续期网盘渠道临期的授权令牌。
// @param ctx 控制循环生命周期
// @param wait 周期闸,到点放行、ctx 取消即收敛
func healthLoop(ctx context.Context, wait waitFunc, manager *channel.Manager, logger *slog.Logger) {
	for wait(ctx) {
		probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
		changed, err := manager.CheckAll(probeCtx)
		cancel()
		if err != nil {
			logger.Warn("健康检查未完成", "err", err)
		}
		if len(changed) > 0 {
			logger.Info("渠道健康状态变化", "channels", changed)
		}

		refreshCtx, cancelRefresh := context.WithTimeout(ctx, healthProbeTimeout)
		refreshed, err := manager.RefreshOAuthTokens(refreshCtx)
		cancelRefresh()
		if err != nil {
			logger.Warn("授权令牌续期未完成", "err", err)
		}
		if len(refreshed) > 0 {
			logger.Info("网盘授权令牌已续期", "channels", refreshed)
		}
	}
}

// waitFunc 周期任务的等待闸:睡到下一轮返回 true,ctx 取消返回 false。
type waitFunc = func(context.Context) bool

// waiter 造一个按配置间隔放行的周期闸。
// @Description: 等待期间改了本轮间隔即刻按新值重排,不必等旧周期走完——否则把间隔从
// 数小时改成数秒要等到旧周期到点才生效,与"保存即生效"不符。与本闸无关的配置变更不打扰计时。
// @param pick 从配置快照取本闸的间隔
// @return 等待闸
func (a *app) waiter(pick func(config.Settings) time.Duration) waitFunc {
	return func(ctx context.Context) bool {
		d := pick(a.store.Current())
		timer := time.NewTimer(d)
		defer timer.Stop()
		for {
			changed := a.store.Changed()
			select {
			case <-ctx.Done():
				return false
			case <-timer.C:
				return true
			case <-changed:
				next := pick(a.store.Current())
				if next == d {
					continue
				}
				d = next
				timer.Stop()
				timer.Reset(d)
			}
		}
	}
}
