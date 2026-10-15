//go:build e2e

package e2e

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	verrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"

	"github.com/x-haose/imgyoung/api"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// contract 由内嵌的 OpenAPI 文档构造的校验器,TestMain 装载一次,全部用例共用(校验器并发安全)。
var contract validator.Validator

// loadContract 解析内嵌文档并校验文档自身合法,失败即整套用例不可信。
// @return 文档无法解析或不合法时的错误
func loadContract() error {
	doc, err := libopenapi.NewDocument(api.OpenAPI)
	if err != nil {
		return fmt.Errorf("解析 OpenAPI 文档: %w", err)
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		return fmt.Errorf("构造契约校验器: %w", errors.Join(errs...))
	}
	if ok, verrs := v.ValidateDocument(); !ok {
		return fmt.Errorf("OpenAPI 文档不合法:\n%s", describeViolations(verrs))
	}
	contract = v
	return nil
}

// contractCovers 判定路径是否在文档校验范围内:API、健康检查与短 ID 图片入口。
// @Description: /o/ 的对象键可含斜杠,与路径模板无法逐段匹配;S3 协议面是 XML 协议、SPA 是页面,均不在文档内。
// @param path 请求路径
// @return 在范围内为 true
func contractCovers(path string) bool {
	return path == "/healthz" || strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/i/")
}

// contractTransport 在真实传输之上按 OpenAPI 文档校验每条响应:状态码须在文档内,
// 响应体须符合其 schema;不符即记为用例失败。文档之外的路径回 404 视为正常(未知路径本就该 404)。
type contractTransport struct {
	t *testing.T // 违约时记失败的用例
}

// RoundTrip 发出请求并校验响应;校验器读完响应体后会原样放回,调用方照常读取。
// @param req 请求
// @return 响应与传输错误
func (c contractTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil || !contractCovers(req.URL.Path) {
		return resp, err
	}
	ok, violations := contract.ValidateHttpResponse(req, resp)
	if ok || (resp.StatusCode == http.StatusNotFound && onlyPathMissing(violations)) {
		return resp, nil
	}
	c.t.Errorf("响应不符合接口文档:%s %s → %d\n%s", req.Method, req.URL.Path, resp.StatusCode,
		describeViolations(violations))
	return resp, nil
}

// onlyPathMissing 判定违约是否只有「路径不在文档内」一种;路径在文档内而方法缺失不算,那是文档漏写。
// @param violations 校验器报告的违约
// @return 全部为路径缺失时为 true
func onlyPathMissing(violations []*verrors.ValidationError) bool {
	for _, v := range violations {
		if v.ValidationType != helpers.PathValidation || v.ValidationSubType != helpers.ValidationMissing {
			return false
		}
	}
	return len(violations) > 0
}

// describeViolations 把违约清单整理成可读的多行文本。
// @param violations 校验器报告的违约
// @return 每条一行,schema 违约附带位置
func describeViolations(violations []*verrors.ValidationError) string {
	var b strings.Builder
	for _, v := range violations {
		fmt.Fprintf(&b, "- %s: %s\n", v.Message, v.Reason)
		for _, sv := range v.SchemaValidationErrors {
			fmt.Fprintf(&b, "    %s (%s @ %s)\n", sv.Reason, sv.FieldPath, sv.KeywordLocation)
		}
	}
	return b.String()
}

// newClient 返回按接口文档校验响应的 HTTP 客户端;用例里的一切请求都经它发出。
// @return 客户端
func newClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: contractTransport{t: t}}
}

// newSessionClient 返回带 cookie jar 的校验客户端,供登录后持有会话。
// @return 客户端
func newSessionClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar: %v", err)
	}
	client := newClient(t)
	client.Jar = jar
	return client
}

// TestErrorCodesMatchContract 验证服务端登记的错误码与接口文档的 ErrorCode 枚举逐一对应。
// @Description: 管理后台的错误文案词典按枚举生成类型:服务端多出的错误码在界面上查不到文案,
// 文档多出的则是不会出现的死条目。
func TestErrorCodesMatchContract(t *testing.T) {
	doc, err := libopenapi.NewDocument(api.OpenAPI)
	if err != nil {
		t.Fatalf("解析 OpenAPI 文档: %v", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("构造文档模型: %v", err)
	}
	proxy, ok := model.Model.Components.Schemas.Get("ErrorCode")
	if !ok {
		t.Fatal("文档里没有 ErrorCode")
	}
	documented := map[string]bool{}
	for _, n := range proxy.Schema().Enum {
		documented[n.Value] = true
	}
	for _, c := range errcode.Codes() {
		if !documented[c.String()] {
			t.Errorf("服务端错误码 %s 不在文档的 ErrorCode 枚举里", c)
		}
		delete(documented, c.String())
	}
	for c := range documented {
		t.Errorf("文档的 ErrorCode 枚举里的 %s 服务端没有登记", c)
	}
}
