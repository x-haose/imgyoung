package router

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/api"
)

// openAPIETag 内嵌文档的强 ETag,按内容哈希计算一次;文档随二进制变化,再验证命中即 304。
var openAPIETag = sync.OnceValue(func() string {
	sum := sha256.Sum256(api.OpenAPI)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
})

// openAPIHandler 输出内嵌的接口文档;不做长期缓存,靠 ETag 再验证。
// @return 处理器
func openAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", openAPIETag())
		http.ServeContent(w, r, "openapi.yaml", time.Time{}, bytes.NewReader(api.OpenAPI))
	}
}
