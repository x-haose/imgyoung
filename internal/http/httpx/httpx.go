// Package httpx 提供 API 各包共享的响应写入原语与错误信封。
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// ErrorResponse API 错误的统一信封;也嵌在批量结果等响应里描述单项失败。
type ErrorResponse struct {
	Code   errcode.Code      `json:"code"`             // 错误码,管理后台按它与参数渲染当前语言的文案
	Error  string            `json:"error"`            // 中文文案,供不经管理后台的调用方直接展示;不泄露内部细节
	Params map[string]string `json:"params,omitempty"` // 文案参数,键即文案里的占位符名
}

// ErrorBody 把带错误码的错误转成信封。
// @param e 带错误码的错误
// @return 信封
func ErrorBody(e *errcode.Error) ErrorResponse {
	return ErrorResponse{Code: e.Code, Error: e.Error(), Params: e.Params}
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
// @param code 错误码
// @param kv 文案参数,键值交替
func WriteError(l *slog.Logger, w http.ResponseWriter, status int, code errcode.Code, kv ...string) {
	WriteJSON(l, w, status, ErrorBody(errcode.New(code, kv...)))
}

// WriteErrorFrom 按错误链上的错误码输出统一错误信封。
// @Description: 调用方已按哨兵判定该错误面向用户、应带错误码;链上找不到错误码是代码缺陷,
// 记 error 日志并按服务内部错误输出。
// @param l 结构化日志
// @param w 响应写入器
// @param status 找到错误码时的 HTTP 状态码
// @param err 错误
func WriteErrorFrom(l *slog.Logger, w http.ResponseWriter, status int, err error) {
	e, ok := errcode.From(err)
	if !ok {
		l.Error("面向用户的错误缺少错误码", "err", err)
		WriteError(l, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	WriteJSON(l, w, status, ErrorBody(e))
}

// PrefersChinese 按 Accept-Language 判定客户端要中文还是英文:取 q 值最高的语言标签,zh 开头为中文。
// @Description: 服务端只出中英两种文案,以中文为准:没有该头、头里没有可用标签或最高者为 * 时按中文;
// q 值相同取先出现的。q 值写错的项忽略。
// @param r 当前请求
// @return 要中文为 true
func PrefersChinese(r *http.Request) bool {
	best, bestQ := "", 0.0
	for part := range strings.SplitSeq(r.Header.Get("Accept-Language"), ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				continue
			}
			q = parsed
		}
		if tag != "" && q > bestQ {
			best, bestQ = tag, q
		}
	}
	return best == "" || best == "*" || strings.HasPrefix(strings.ToLower(best), "zh")
}
