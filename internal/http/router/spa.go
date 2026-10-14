package router

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// spaCSP 后台页面的内容安全策略:全部资源自带(embed),仅样式放行内联
// (弹层定位与进度条以 style 属性写入所需);禁嵌入、禁外联。
const spaCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'self'; object-src 'none'"

// spaServer 内嵌后台页面的处理器:命中静态产物按内容哈希强缓存,其余路径回落 index.html(history 路由)。
type spaServer struct {
	dist  fs.FS        // 前端产物文件系统
	files http.Handler // 静态产物文件服务
	index []byte       // index.html 内容;nil 表示产物缺失,页面路径回 404
	log   *slog.Logger // 结构化日志
}

// spaHandler 构造内嵌 SPA 处理器;index 不缓存,新版本发布即生效。
// @param dist 前端产物文件系统
// @param l 结构化日志
// @return 挂在路由 NotFound 位置的处理器
func spaHandler(dist fs.FS, l *slog.Logger) http.HandlerFunc {
	s := &spaServer{dist: dist, files: http.FileServerFS(dist), log: l}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		l.Warn("前端产物缺少 index.html,后台页面不可用(API 不受影响)", "err", err)
	} else {
		s.index = index
	}
	return s.ServeHTTP
}

// ServeHTTP 分流:API 与图片路径未匹配保持纯 404,静态产物直出,其余路径回落 index.html。
// @param w 响应写入器
// @param r 请求
func (s *spaServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/i/") {
		http.NotFound(w, r)
		return
	}
	if s.serveFile(w, r) {
		return
	}
	if s.index == nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Security-Policy", spaCSP)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(s.index))
}

// serveFile 路径命中产物中的普通文件时输出它并报告 true;根路径、index.html、目录与不存在的路径报告 false。
// @Description: Vite 产物 assets/ 下的文件名带内容哈希,可长期强缓存。
// @param w 响应写入器
// @param r 请求
// @return 是否已输出
func (s *spaServer) serveFile(w http.ResponseWriter, r *http.Request) bool {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || name == "index.html" {
		return false
	}
	info, err := fs.Stat(s.dist, name)
	if err != nil || info.IsDir() {
		return false
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	s.files.ServeHTTP(w, r)
	return true
}
