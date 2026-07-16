package router

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

// spaCSP 后台页面的内容安全策略:全部资源自带(embed),仅样式放行内联
// (Ant Design 运行时注入样式所需);禁嵌入、禁外联。
const spaCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'self'; object-src 'none'"

// spaHandler 服务内嵌 SPA:命中静态文件按内容哈希强缓存,其余路径回落
// index.html(history 路由);index 不缓存,新版本发布即生效。
// @param dist 前端产物文件系统
// @param l 结构化日志
// @return 挂在路由 NotFound 位置的处理器
func spaHandler(dist fs.FS, l *slog.Logger) http.HandlerFunc {
	fileServer := http.FileServerFS(dist)
	index, indexErr := fs.ReadFile(dist, "index.html")
	if indexErr != nil {
		l.Warn("前端产物缺少 index.html,后台页面不可用(API 不受影响)", "err", indexErr)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// API 与图片路径的未匹配请求保持纯 404,不回落页面。
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/i/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				// Vite 产物文件名带内容哈希,可长期强缓存。
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		if indexErr != nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Security-Policy", spaCSP)
		if _, err := w.Write(index); err != nil {
			l.Debug("写 SPA 页面失败", "err", err)
		}
	}
}
