// ImgYoung 图床服务入口:手工装配各组件并启动;生命周期编排见 serve。
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/x-haose/imgyoung/internal/infra/version"
)

// main 进程入口:装配并启动服务,阻塞至优雅停机;失败退出码 1。
// @Description: 运行参数只有 -version:打印构建信息后退出。其余配置一律走环境变量与后台。
func main() {
	showVersion := flag.Bool("version", false, "打印版本信息并退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.Current())
		return
	}
	// os.Exit 放在此处而非 runServer 内:确保 runServer 的 defer cleanup 先跑完再退出。
	if err := runServer(newLogger()); err != nil {
		os.Exit(1)
	}
}

// runServer 装配并运行服务直至停机;装配失败或运行致命错误时返回错误。
// @param logger 结构化日志
// @return 启动或运行错误
func runServer(logger *slog.Logger) error {
	a, cleanup, err := build(logger)
	if err != nil {
		logger.Error("启动失败", "err", err)
		return err
	}
	defer cleanup()
	if err := a.serve(); err != nil {
		logger.Error("运行失败", "err", err)
		return err
	}
	return nil
}
