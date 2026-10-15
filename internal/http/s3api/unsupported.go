package s3api

import (
	"net/http"

	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// allowedQueryKeys 已实现的操作接受的查询参数。x-id 是 AWS SDK 附在请求上的操作名提示,不改变语义;
// 其余查询参数(acl、tagging、uploads、partNumber、list-type 等)都在选择别的操作。
var allowedQueryKeys = map[string]bool{"x-id": true}

// RejectUnsupported 拒绝落在已实现路由上、实际却是别的操作的请求:带子资源查询参数的请求,以及 CopyObject。
// @Description: 须挂在验签之后。不拒绝的话,GET 对象?acl 会被当成取对象,PUT 对象?partNumber=1&uploadId=…
// 会把分段当成整个对象存下,CopyObject(PUT 加 x-amz-copy-source 头、正文为空)会被当成上传一个空文件。
// @param next 下一个处理器
// @return 中间件包裹后的处理器
func (h *Handlers) RejectUnsupported(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Copy-Source") != "" {
			h.HandleNotImplemented(w, r)
			return
		}
		for key := range r.URL.Query() {
			if !allowedQueryKeys[key] {
				h.HandleNotImplemented(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// HandleNotImplemented 以 S3 错误 NotImplemented(501)拒绝本服务没有实现的操作。
// @Description: 501 不在 S3 客户端的自动重试范围内,客户端直接报错,不会反复重试。
func (h *Handlers) HandleNotImplemented(w http.ResponseWriter, r *http.Request) {
	h.writeStatus(w, r, http.StatusNotImplemented, "NotImplemented", "不支持的 S3 操作")
}

// Reject 以 S3 的 XML 错误体写出上传防护闸的拒绝;满载与超频给出 S3 的限流错误码 SlowDown,客户端据此退避重试。
// @param w 响应写入器
// @param r 请求
// @param status HTTP 状态码
// @param code 错误码,文案取其中文文案
func (h *Handlers) Reject(w http.ResponseWriter, r *http.Request, status int, code errcode.Code) {
	var s3Code string
	switch status {
	case http.StatusTooManyRequests:
		s3Code = "SlowDown"
	case http.StatusUnauthorized:
		s3Code = "AccessDenied"
	default:
		s3Code = "InternalError"
	}
	h.writeStatus(w, r, status, s3Code, errcode.Message(code, nil))
}
