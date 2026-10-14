// Package api 持有对外 HTTP 接口的 OpenAPI 文档,内嵌进二进制:运行中的实例在 /api/openapi.yaml
// 提供与自身版本一致的副本,端到端测试据此校验每条响应。
package api

import _ "embed"

// OpenAPI 接口文档正文,OpenAPI 3.1,YAML 编码。
//
//go:embed openapi.yaml
var OpenAPI []byte
