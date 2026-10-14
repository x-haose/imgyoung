// Package httpx 提供 API 各包共享的响应写入原语与错误信封。
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
