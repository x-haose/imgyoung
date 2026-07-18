// Package httpx 提供 API 各包共享的响应写入原语与错误信封。
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// ErrorResponse API 错误的统一信封。
type ErrorResponse struct {
	Error string `json:"error"` // 面向用户的错误描述,不泄露内部细节
}

// WriteJSON 输出 JSON 响应。
// @Description: 编码失败几乎只有客户端提前断开一种来源,记 debug 不上抛。
// @param l 结构化日志
// @param w 响应写入器
// @param status HTTP 状态码
// @param v 待编码值
func WriteJSON(l *slog.Logger, w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		l.Debug("写 JSON 响应失败", "err", err)
	}
}

// WriteError 输出统一错误信封。
// @param l 结构化日志
// @param w 响应写入器
// @param status HTTP 状态码
// @param msg 面向用户的错误描述
func WriteError(l *slog.Logger, w http.ResponseWriter, status int, msg string) {
	WriteJSON(l, w, status, ErrorResponse{Error: msg})
}

// RequestBaseURL 从请求推导对外站点根地址(形如 https://img.example.com,无尾斜杠)。
// @Description: 未配置 base_url 时的兜底:反代场景优先信 X-Forwarded-Proto /
// X-Forwarded-Host(多级代理取首值),否则回退 Host 头与本连接的 TLS 判定。
// 推导结果即客户端本次所见域名;反代/CDN 对外域名与请求头不一致时仍须配置 base_url。
// 硬约束:推导值来自不可信请求头,仅可回显给当前请求方(如响应里拼本次上传的 URL);
// 禁止落库、禁止用于拼 CDN purge URL、禁止跨请求复用——这些场景一律以配置的 base_url 为准。
// @param r 当前请求
// @return 站点根地址
func RequestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := forwardedFirst(r.Header.Get("X-Forwarded-Proto")); v != "" {
		scheme = v
	}
	host := r.Host
	if v := forwardedFirst(r.Header.Get("X-Forwarded-Host")); v != "" {
		host = v
	}
	return scheme + "://" + host
}

// forwardedFirst 取 X-Forwarded-* 头的首个值(多级代理逗号追加,首值为最外层客户端侧)。
// @param v 原始头值
// @return 去空白后的首个值;头缺失时为空串
func forwardedFirst(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
