// ImgYoung 生命周期编排:用 oklog/run 并发运行 HTTP、补齐子系统与后台循环,统一启停。
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/oklog/run"

	"github.com/x-haose/imgyoung/internal/infra/config"
)

// shutdownTimeout 优雅停机的整体预算:HTTP 收敛 + 后台循环退出 + 补齐子系统排空。
const shutdownTimeout = 30 * time.Second

// serve 用 run.Group 统一编排 HTTP、补齐子系统与后台循环:并发运行,任一退出(收到
// SIGINT/SIGTERM 或某组件致命错误)即触发全组停机,待所有 actor 退出才返回。
// @Description: 优雅停机由各 actor 的 interrupt 完成(HTTP 走 Shutdown、River 走 Stop、
// 循环走 ctx 取消)。"末批 Token 使用时刻落库先于关库"仍成立:认证循环的 execute 在
// ctx 取消后 flush 完才返回,Run 等全部 execute 退出才返回,而关库是本函数返回后的 defer。
// @return 非信号触发的致命错误;信号触发的正常停机返回 nil
func (a *app) serve() error {
	// 监听 fail-fast:绑定失败即返回,不进入运行组。
	ln, err := net.Listen("tcp", a.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("监听 %q: %w", a.httpSrv.Addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var g run.Group
	g.Add(run.SignalHandler(ctx, os.Interrupt, syscall.SIGTERM))
	a.addStopRequest(ctx, &g, cancel)
	a.addLoops(ctx, &g, cancel)

	// 补齐子系统(River):启动后阻塞至停机,interrupt 排空在途任务。
	g.Add(func() error {
		if serr := a.runner.Start(ctx); serr != nil {
			return fmt.Errorf("启动补齐子系统: %w", serr)
		}
		<-ctx.Done()
		return nil
	}, func(error) {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer stopCancel()
		if serr := a.runner.Stop(stopCtx); serr != nil {
			a.logger.Error("补齐子系统停机失败", "err", serr)
		}
		cancel()
	})

	// HTTP:Serve 阻塞,interrupt 走优雅 Shutdown;正常关停的 ErrServerClosed 不算错误。
	g.Add(func() error {
		if serr := a.httpSrv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务异常退出: %w", serr)
		}
		return nil
	}, func(error) {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer stopCancel()
		if serr := a.httpSrv.Shutdown(stopCtx); serr != nil {
			a.logger.Error("HTTP 优雅停机失败", "err", serr)
		}
	})

	// 打印实际绑定地址:监听端口配 0 时由系统分配,配置值无法告知调用方真实端口。
	a.logger.Info("ImgYoung 已启动", "listen", ln.Addr().String(), "data_dir", a.boot.DataDir)
	if rerr := g.Run(); rerr != nil && !errors.Is(rerr, run.ErrSignal) {
		return rerr
	}
	a.logger.Info("已优雅停机")
	return nil
}

// addStopRequest 把后台请求的停机(恢复快照)作为 actor 加入运行组:actor 退出即触发全组停机,等同一次正常停机。
// @param ctx 运行组生命周期
// @param g 运行组
// @param cancel 触发全组停机的取消函数
func (a *app) addStopRequest(ctx context.Context, g *run.Group, cancel context.CancelFunc) {
	g.Add(func() error {
		select {
		case <-a.stop:
			a.logger.Warn("收到停机请求,优雅停机后由进程管理器拉起")
		case <-ctx.Done():
		}
		return nil
	}, func(error) { cancel() })
}

// addLoops 把全部后台周期循环(用量/查重/清理/活跃度/衍生图访问时间/健康/告警/备份/认证维护)作为 actor 加入运行组。
// @Description: 各循环共用 ctx,execute 跑到 ctx 取消即返回,interrupt 取消共享 ctx(幂等);
// 间隔取自配置快照,后台改间隔即刻重排当前等待。均为兜底型循环。
// @param ctx 循环生命周期;取消即各自收敛退出
// @param g 运行组
// @param cancel 触发全组停机的取消函数
func (a *app) addLoops(ctx context.Context, g *run.Group, cancel context.CancelFunc) {
	seconds := func(pick func(config.Settings) int) waitFunc {
		return a.waiter(func(s config.Settings) time.Duration {
			return time.Duration(pick(s)) * time.Second
		})
	}
	add := func(fn func()) {
		g.Add(func() error { fn(); return nil }, func(error) { cancel() })
	}

	add(func() { authMaintenanceLoop(ctx, a.auth, a.logger) })
	add(func() { a.reporter.Run(ctx, seconds(func(s config.Settings) int { return s.UsageReportSeconds })) })
	add(func() { a.merger.Run(ctx, seconds(func(s config.Settings) int { return s.DedupScanSeconds })) })
	add(func() { a.sweeper.Run(ctx, seconds(func(s config.Settings) int { return s.CleanupSweepSeconds })) })
	add(func() { a.tracker.Run(ctx, seconds(func(s config.Settings) int { return s.ActivityFlushSeconds })) })
	add(func() { a.vstore.Run(ctx, seconds(func(s config.Settings) int { return s.ActivityFlushSeconds })) })
	add(func() {
		healthLoop(ctx, seconds(func(s config.Settings) int { return s.HealthCheckSeconds }), a.manager, a.logger)
	})
	// 告警评估与健康探测同节奏:探测结果是告警的主要输入。
	add(func() { a.alerts.Run(ctx, seconds(func(s config.Settings) int { return s.HealthCheckSeconds })) })
	add(func() {
		a.backup.Run(ctx, a.waiter(func(s config.Settings) time.Duration {
			return time.Duration(s.BackupIntervalHours) * time.Hour
		}))
	})
}
