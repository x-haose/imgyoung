// ImgYoung 图床服务入口:以 uber/fx 装配配置、数据库、存储、任务与 HTTP 服务。
// fx 仅在本装配根(composition root)出现,业务包保持零 fx 依赖(设计定稿·二/7.4)。
package main

import (
	"log/slog"
	"net/http"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

// main 进程入口:构建 fx 应用并运行,直至收到 SIGINT/SIGTERM 优雅停机。
func main() {
	fx.New(
		fx.Provide(
			provideLogger,
			provideConfig,
			provideDB,
			provideQueries,
			provideMasterKey,
			provideToken,
			provideManager,
			provideRunner,
			provideUpload,
			provideMetrics,
			provideAudit,
			provideServer,
			provideSweeper,
			provideTracker,
			provideUsageReporter,
			provideHTTPServer,
		),
		// fx 只在出错时说话:略过逐条装配噪音,但绝不吞启动/停机错误。
		fx.WithLogger(func(logger *slog.Logger) fxevent.Logger {
			return fxErrorLogger{log: logger}
		}),
		// 强制实例化:HTTP 服务器拉起整条依赖链;后台循环单独注册。
		fx.Invoke(func(*http.Server) {}),
		fx.Invoke(registerBackgroundLoops),
	).Run()
}
