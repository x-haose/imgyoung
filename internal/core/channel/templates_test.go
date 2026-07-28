package channel

import (
	"errors"
	"testing"

	"github.com/rclone/rclone/fs/config/obscure"
)

// TestBuildTemplateConfigMerge 各模板的预设合并、字段落位与 endpoint 拼接正确。
func TestBuildTemplateConfigMerge(t *testing.T) {
	cases := map[string]struct {
		template string
		inputs   map[string]string
		wantCfg  map[string]string // 期望的关键键值(子集断言)
	}{
		"AWS 用 region、endpoint 留空": {
			template: "aws-s3",
			inputs:   map[string]string{"access_key_id": "AK", "secret_access_key": "SK", "region": "us-east-1", "bucket": "my-bkt"},
			wantCfg:  map[string]string{"provider": "AWS", "access_key_id": "AK", "secret_access_key": "SK", "region": "us-east-1", "path": "my-bkt"},
		},
		"R2 拼 account 端点、region=auto、无 region 字段": {
			template: "r2",
			inputs:   map[string]string{"access_key_id": "AK", "secret_access_key": "SK", "account_id": "acc123", "bucket": "imgs"},
			wantCfg:  map[string]string{"provider": "Cloudflare", "region": "auto", "endpoint": "https://acc123.r2.cloudflarestorage.com", "path": "imgs"},
		},
		"COS 由 region 拼 endpoint、region 不进 config": {
			template: "cos",
			inputs:   map[string]string{"access_key_id": "SID", "secret_access_key": "SKEY", "region": "ap-shanghai", "bucket": "b-125"},
			wantCfg:  map[string]string{"provider": "TencentCOS", "endpoint": "cos.ap-shanghai.myqcloud.com", "path": "b-125"},
		},
		"OSS region 含前缀直接拼 endpoint": {
			template: "oss",
			inputs:   map[string]string{"access_key_id": "AK", "secret_access_key": "SK", "region": "oss-cn-hangzhou", "bucket": "b"},
			wantCfg:  map[string]string{"provider": "Alibaba", "endpoint": "oss-cn-hangzhou.aliyuncs.com", "path": "b"},
		},
		"B2 region 既进 config 又拼 endpoint": {
			template: "b2",
			inputs:   map[string]string{"access_key_id": "kid", "secret_access_key": "akey", "region": "us-west-004", "bucket": "b"},
			wantCfg:  map[string]string{"provider": "Other", "region": "us-west-004", "endpoint": "https://s3.us-west-004.backblazeb2.com", "path": "b"},
		},
		"MinIO 用户直填 endpoint": {
			template: "minio",
			inputs:   map[string]string{"access_key_id": "AK", "secret_access_key": "SK", "endpoint": "https://minio.local", "bucket": "b"},
			wantCfg:  map[string]string{"provider": "Minio", "endpoint": "https://minio.local", "path": "b"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			drv, cfg, err := BuildTemplateConfig(tc.template, tc.inputs)
			if err != nil {
				t.Fatalf("合并失败: %v", err)
			}
			if drv != "s3" {
				t.Errorf("driver = %q, 期望 s3", drv)
			}
			for k, want := range tc.wantCfg {
				if cfg[k] != want {
					t.Errorf("config[%q] = %q, 期望 %q", k, cfg[k], want)
				}
			}
			// COS/OSS 的 region 不应作为独立 config 键(它拼进了 endpoint)。
			if tc.template == "cos" || tc.template == "oss" {
				if _, ok := cfg["region"]; ok {
					t.Errorf("%s 的 region 不应进 config,应仅拼入 endpoint", tc.template)
				}
			}
		})
	}
}

// TestBuildTemplateConfigRequired 缺必填字段报错、未知模板报错。
func TestBuildTemplateConfigRequired(t *testing.T) {
	if _, _, err := BuildTemplateConfig("aws-s3", map[string]string{"access_key_id": "AK"}); !errors.Is(err, ErrInvalidChannel) {
		t.Errorf("缺 secret/region/bucket 应报错, got %v", err)
	}
	if _, _, err := BuildTemplateConfig("no-such", nil); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("未知模板应报 ErrUnknownTemplate, got %v", err)
	}
}

// TestTemplateInputsFromConfig 表单字段反解与 BuildTemplateConfig 互逆:
// 直落键取回原值、拼接字段从 endpoint 反提、密码字段哨兵掩码。
func TestTemplateInputsFromConfig(t *testing.T) {
	cases := map[string]struct {
		template   string
		config     map[string]string
		wantInputs map[string]string
	}{
		"R2 从端点反提 account_id": {
			template: "r2",
			config: map[string]string{
				"provider": "Cloudflare", "region": "auto",
				"access_key_id": "AK", "secret_access_key": "SK",
				"endpoint": "https://acc123.r2.cloudflarestorage.com", "path": "imgs",
				"public_base_url": "https://cdn.example.com",
			},
			wantInputs: map[string]string{
				"access_key_id": "AK", "secret_access_key": SecretPlaceholder,
				"account_id": "acc123", "bucket": "imgs", "public_base_url": "https://cdn.example.com",
			},
		},
		"COS 从端点反提 region": {
			template: "cos",
			config: map[string]string{
				"provider": "TencentCOS", "access_key_id": "SID", "secret_access_key": "SKEY",
				"endpoint": "cos.ap-shanghai.myqcloud.com", "path": "b-125",
			},
			wantInputs: map[string]string{
				"access_key_id": "SID", "secret_access_key": SecretPlaceholder,
				"region": "ap-shanghai", "bucket": "b-125",
			},
		},
		"MinIO 直落键原样取回": {
			template: "minio",
			config: map[string]string{
				"provider": "Minio", "access_key_id": "AK", "secret_access_key": "SK",
				"endpoint": "https://minio.local", "path": "b",
			},
			wantInputs: map[string]string{
				"access_key_id": "AK", "secret_access_key": SecretPlaceholder,
				"endpoint": "https://minio.local", "bucket": "b",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			inputs, ok := TemplateInputsFromConfig(tc.template, tc.config)
			if !ok {
				t.Fatalf("模板 %q 应存在", tc.template)
			}
			for k, want := range tc.wantInputs {
				if inputs[k] != want {
					t.Errorf("inputs[%q] = %q, 期望 %q", k, inputs[k], want)
				}
			}
		})
	}
	if _, ok := TemplateInputsFromConfig("no-such", nil); ok {
		t.Error("未知模板应返回 ok=false")
	}
	// 端点与模式不匹配(如手工改过配置)时拼接字段缺省而非误配。
	inputs, ok := TemplateInputsFromConfig("r2", map[string]string{"endpoint": "https://other.example.com"})
	if !ok {
		t.Fatal("r2 模板应存在")
	}
	if v, exists := inputs["account_id"]; exists {
		t.Errorf("模式不匹配仍反提出 account_id=%q", v)
	}
}

// TestTemplatesDownloadShape 下发的模板不泄露预设/拼接细节(仅暴露渲染所需字段)。
func TestTemplatesDownloadShape(t *testing.T) {
	for _, tpl := range Templates() {
		if tpl.ID == "" || tpl.Driver == "" || len(tpl.Fields) == 0 {
			t.Errorf("模板 %q 结构不完整", tpl.ID)
		}
		for _, f := range tpl.Fields {
			if f.Key == "" || f.Label == "" || f.Type == "" {
				t.Errorf("模板 %q 字段不完整: %+v", tpl.ID, f)
			}
			if f.Type == FieldSelect && len(f.Options) == 0 {
				t.Errorf("模板 %q 的下拉字段 %q 无选项", tpl.ID, f.Key)
			}
		}
	}
}

// TestWebDAVTemplateObscuresPassword 验证 WebDAV 密码按 rclone 要求编码后入库。
// @Description: 漏编码时后端会把明文当作已编码值解出乱码,表现为凭证正确却认证失败。
func TestWebDAVTemplateObscuresPassword(t *testing.T) {
	// nolint:gosec // G101:测试桩值,非真实凭证。
	const plain = "明文密码"
	drv, cfg, err := BuildTemplateConfig("webdav", map[string]string{
		"url": "https://dav.jianguoyun.com/dav/", "vendor": "other",
		"user": "u@example.com", "pass": plain, "path": "imgyoung",
	})
	if err != nil {
		t.Fatalf("BuildTemplateConfig 报错: %v", err)
	}
	if drv != "webdav" {
		t.Errorf("driver = %q, 期望 webdav", drv)
	}
	if cfg["pass"] == "明文密码" {
		t.Fatal("密码未编码即入库")
	}
	got, err := obscure.Reveal(cfg["pass"])
	if err != nil {
		t.Fatalf("编码结果无法还原: %v", err)
	}
	if got != "明文密码" {
		t.Errorf("还原得 %q, 期望原密码", got)
	}
	// 非密码字段不应被编码。
	if cfg["user"] != "u@example.com" {
		t.Errorf("user 被误编码: %q", cfg["user"])
	}
}

// TestDriveTemplatesDeclareOAuth 验证各网盘模板的授权标记与后端实际要求一致。
func TestDriveTemplatesDeclareOAuth(t *testing.T) {
	for _, tpl := range Templates() {
		t.Run(tpl.ID, func(t *testing.T) {
			if want := NeedsOAuth(tpl.Driver); tpl.RequiresOAuth != want {
				t.Errorf("模板 %q 标记 RequiresOAuth=%v, 后端 %q 实际 %v",
					tpl.ID, tpl.RequiresOAuth, tpl.Driver, want)
			}
		})
	}
}

// TestOAuthTemplatesCarryTokenField 验证需授权的模板都留有令牌落位字段。
func TestOAuthTemplatesCarryTokenField(t *testing.T) {
	for _, tpl := range Templates() {
		if !tpl.RequiresOAuth {
			continue
		}
		var found bool
		for _, f := range tpl.Fields {
			if f.Type == FieldOAuth && f.configKey == ConfigKeyToken {
				found = true
			}
		}
		if !found {
			t.Errorf("模板 %q 需授权却无令牌字段,授权结果无处落位", tpl.ID)
		}
	}
}

// TestOAuthTokenNeverLeavesInPlaintext 验证编辑回读时授权令牌以哨兵掩码,不回传明文。
// @Description: 令牌等同凭证,明文下发前端会经浏览器历史、截图、日志外泄。
func TestOAuthTokenNeverLeavesInPlaintext(t *testing.T) {
	const secretToken = `{"access_token":"AT","refresh_token":"RT"}`
	inputs, ok := TemplateInputsFromConfig("googledrive", map[string]string{
		ConfigKeyToken: secretToken, "path": "imgyoung",
	})
	if !ok {
		t.Fatal("模板不存在")
	}
	if inputs[ConfigKeyToken] == secretToken {
		t.Fatal("授权令牌明文回传")
	}
	if inputs[ConfigKeyToken] != SecretPlaceholder {
		t.Errorf("令牌字段 = %q, 期望哨兵掩码", inputs[ConfigKeyToken])
	}
	// 非凭证字段仍应原样回读,否则编辑表单预填不出来。
	if inputs["path"] != "imgyoung" {
		t.Errorf("path = %q, 期望原样回读", inputs["path"])
	}
}
