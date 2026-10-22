// ImgYoung 生命周期编排:用 oklog/run 并发运行 HTTP、补齐子系统与后台循环,统一启停。
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/oklog/run"

	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/version"
)

// drainTimeout 停机第一段:停收新请求后等在途 HTTP 请求结束的上限,到时强制断开剩余连接。
const drainTimeout = 6 * time.Second

// stopTimeout 停机第二段:取消后台循环与在途补齐任务后等补齐子系统退出的上限。
// 两段合计 8 秒,落在 Docker 默认 10 秒的强杀时限之内;deploy/ 下的停止等待时间按此留余量。
const stopTimeout = 2 * time.Second

// serve 用 run.Group 统一编排 HTTP、补齐子系统与后台循环:并发运行,任一退出(收到
// SIGINT/SIGTERM、后台请求停机或某组件致命错误)即触发全组停机,待所有 actor 退出才返回。
// @Description: 各 actor 的 interrupt 都触发同一个只执行一次的按序停机(见 shutdown)。"末批 Token
// 使用时刻落库先于关库"仍成立:认证循环的 execute 在 ctx 取消后 flush 完才返回,Run 等全部 execute
// 退出才返回,而关库是本函数返回后的 defer。
// @return 非信号触发的致命错误;信号触发的正常停机返回 nil
func (a *app) serve() error {
	// 监听 fail-fast:绑定失败即返回,不进入运行组。
	ln, err := net.Listen("tcp", a.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("监听 %q: %w", a.httpSrv.Addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdown := sync.OnceFunc(func() { a.shutdown(cancel) })

	var g run.Group
	g.Add(run.SignalHandler(ctx, os.Interrupt, syscall.SIGTERM))
	a.addStopRequest(ctx, &g, shutdown)
	a.addLoops(ctx, &g, shutdown)

	// 补齐子系统(River):启动后阻塞至 ctx 取消;在途任务随 ctx 取消,由 shutdown 等其退出。
	// worker 启动成功即整个服务就绪;随后的恢复扫描是后台追赶,耗时随待补副本数增长,不计入启动。
	g.Add(func() error {
		if serr := a.runner.Start(ctx); serr != nil {
			return fmt.Errorf("启动补齐子系统: %w", serr)
		}
		if nerr := a.announceReady(ln.Addr()); nerr != nil {
			return nerr
		}
		if rerr := a.runner.Recover(ctx); rerr != nil {
			return fmt.Errorf("补齐子系统恢复扫描: %w", rerr)
		}
		<-ctx.Done()
		return nil
	}, func(error) { shutdown() })

	// HTTP:Serve 阻塞,shutdown 先对它做优雅 Shutdown;正常关停的 ErrServerClosed 不算错误。
	g.Add(func() error {
		if serr := a.httpSrv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务异常退出: %w", serr)
		}
		return nil
	}, func(error) { shutdown() })

	if rerr := g.Run(); rerr != nil && !errors.Is(rerr, run.ErrSignal) {
		return rerr
	}
	a.logger.Info("已优雅停机")
	return nil
}

// announceReady 打印启动信息并告知 systemd 服务已就绪:数据库已打开并迁移、端口已监听、补齐子系统已启动。
// @Description: 服务单元为 Type=notify 时,systemctl start / restart 等到这一刻才返回,进程在此之前退出则报启动失败。
// 不在 systemd 下运行(没有 NOTIFY_SOCKET)时只打印启动信息;通知随即从环境中移除,不传给子进程。
// @param addr 实际绑定的地址;监听端口配 0 时由系统分配,配置值无法告知调用方真实端口
// @return 通知写不进 systemd 的套接字时的错误
func (a *app) announceReady(addr net.Addr) error {
	build := version.Current()
	a.logger.Info("ImgYoung 已启动", "version", build.Version, "commit", build.Commit,
		"listen", addr.String(), "data_dir", a.boot.DataDir)
	if _, err := daemon.SdNotify(true, daemon.SdNotifyReady); err != nil {
		return fmt.Errorf("通知 systemd 服务已就绪: %w", err)
	}
	return nil
}

// shutdown 按序停机:先停收新请求并等在途请求结束,再取消后台循环与补齐任务、等补齐子系统退出。
// @Description: 在途请求还要写库、入队补齐任务,后台部分须在 HTTP 收敛之后才停。被取消的补齐任务
// 状态留在业务表,下次启动由对账重新入队,无需排空。关库在 serve 返回之后。
// @param cancel 取消后台循环与补齐子系统共用的生命周期上下文
func (a *app) shutdown(cancel context.CancelFunc) {
	drainCtx, drainCancel := context.WithTimeout(context.Background(), drainTimeout)
	defer drainCancel()
	if err := a.httpSrv.Shutdown(drainCtx); err != nil {
		a.logger.Warn("在途请求未在时限内结束,强制断开", "err", err)
		if cerr := a.httpSrv.Close(); cerr != nil {
			a.logger.Error("强制断开 HTTP 连接失败", "err", cerr)
		}
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), stopTimeout)
	defer stopCancel()
	if err := a.runner.Stop(stopCtx); err != nil {
		a.logger.Error("补齐子系统停机失败", "err", err)
	}
}

// addStopRequest 把后台请求的停机(恢复快照)作为 actor 加入运行组:actor 退出即触发全组停机,等同一次正常停机。
// @param ctx 运行组生命周期
// @param g 运行组
// @param shutdown 按序停机,只执行一次
func (a *app) addStopRequest(ctx context.Context, g *run.Group, shutdown func()) {
	g.Add(func() error {
		select {
		case <-a.stop:
			a.logger.Warn("收到停机请求,优雅停机后由进程管理器拉起")
		case <-ctx.Done():
		}
		return nil
	}, func(error) { shutdown() })
}

// addLoops 把全部后台循环(用量/查重/清理/活跃度/衍生图访问时间/健康/告警/备份/认证维护/批量导入)作为 actor 加入运行组。
// @Description: 各循环共用 ctx,execute 跑到 ctx 取消即返回;ctx 由按序停机在 HTTP 收敛之后取消。
// 间隔取自配置快照,后台改间隔即刻重排当前等待。均为兜底型循环。
// @param ctx 循环生命周期;取消即各自收敛退出
// @param g 运行组
// @param shutdown 按序停机,只执行一次
func (a *app) addLoops(ctx context.Context, g *run.Group, shutdown func()) {
	seconds := func(pick func(config.Settings) int) waitFunc {
		return a.waiter(func(s config.Settings) time.Duration {
			return time.Duration(pick(s)) * time.Second
		})
	}
	add := func(fn func()) {
		g.Add(func() error { fn(); return nil }, func(error) { shutdown() })
	}

	add(func() { authMaintenanceLoop(ctx, a.auth, a.logger) })
	add(func() { a.importer.Run(ctx) })
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
