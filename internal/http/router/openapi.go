package router

import (
	"bytes"
	"net/http"
	"time"

	"github.com/x-haose/imgyoung/api"
)

// openAPIHandler 输出内嵌的接口文档;文档随二进制变化,不做长期缓存。
// @return 处理器
func openAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "openapi.yaml", time.Time{}, bytes.NewReader(api.OpenAPI))
	}
}
