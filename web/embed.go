// Package web 承载管理后台前端的构建产物(go:embed),随单二进制分发。
// 前端源码与产物同目录:src/ 为 React + TanStack Router/Query + shadcn/ui 工程,dist/ 为 Vite 构建输出。
package web

import (
	"embed"
	"io/fs"
)

// distFS 内嵌的前端构建产物;dist 仅含 .gitkeep 时后台页面为空,API 不受影响。
//
//go:embed all:dist
var distFS embed.FS

// Dist 返回以 dist 为根的产物文件系统。
// @return 前端产物文件系统
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist 目录名与 embed 指令同源,不一致属编译期程序错误。
		panic(err)
	}
	return sub
}
