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
			inputs: map[string]string{
				"access_key_id": "AK", "secret_access_key": "SK", "region": "us-east-1", "bucket": "my-bkt",
			},
			wantCfg: map[string]string{
				"provider": "AWS", "access_key_id": "AK", "secret_access_key": "SK",
				"region": "us-east-1", "path": "my-bkt",
			},
		},
		"R2 拼 account 端点、region=auto、无 region 字段": {
			template: "r2",
			inputs: map[string]string{
				"access_key_id": "AK", "secret_access_key": "SK", "account_id": "acc123", "bucket": "imgs",
			},
			wantCfg: map[string]string{
				"provider": "Cloudflare", "region": "auto",
				"endpoint": "https://acc123.r2.cloudflarestorage.com", "path": "imgs",
			},
		},
		"COS 由 region 拼 endpoint、region 不进 config": {
			template: "cos",
			inputs: map[string]string{
				"access_key_id": "SID", "secret_access_key": "SKEY", "region": "ap-shanghai", "bucket": "b-125",
			},
			wantCfg: map[string]string{
				"provider": "TencentCOS", "endpoint": "cos.ap-shanghai.myqcloud.com", "path": "b-125",
			},
		},
		"OSS region 含前缀直接拼 endpoint": {
			template: "oss",
			inputs: map[string]string{
				"access_key_id": "AK", "secret_access_key": "SK", "region": "oss-cn-hangzhou", "bucket": "b",
			},
			wantCfg: map[string]string{"provider": "Alibaba", "endpoint": "oss-cn-hangzhou.aliyuncs.com", "path": "b"},
		},
		"B2 region 既进 config 又拼 endpoint": {
			template: "b2",
			inputs: map[string]string{
				"access_key_id": "kid", "secret_access_key": "akey", "region": "us-west-004", "bucket": "b",
			},
			wantCfg: map[string]string{
				"provider": "Other", "region": "us-west-004",
				"endpoint": "https://s3.us-west-004.backblazeb2.com", "path": "b",
			},
		},
		"MinIO 用户直填 endpoint": {
			template: "minio",
			inputs: map[string]string{
				"access_key_id": "AK", "secret_access_key": "SK", "endpoint": "https://minio.local", "bucket": "b",
			},
			wantCfg: map[string]string{"provider": "Minio", "endpoint": "https://minio.local", "path": "b"},
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
	_, _, err := BuildTemplateConfig("aws-s3", map[string]string{"access_key_id": "AK"})
	if !errors.Is(err, ErrInvalidChannel) {
		t.Errorf("缺 secret/region/bucket 应报错, got %v", err)
	}
	if _, _, err := BuildTemplateConfig("no-such", nil); !errors.Is(err, ErrUnknownTemplate) {
		t.Errorf("未知模板应报 ErrUnknownTemplate, got %v", err)
	}
}

// TestTemplateInputsFromConfig 表单字段反解与 BuildTemplateConfig 互逆:
// 直落键取回原值、拼接字段从 endpoint 反提、敏感字段哨兵掩码。
func TestTemplateInputsFromConfig(t *testing.T) {
	secret, err := secretKeysOf("s3")
	if err != nil {
		t.Fatalf("取 s3 敏感键: %v", err)
	}
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
				"access_key_id": SecretPlaceholder, "secret_access_key": SecretPlaceholder,
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
				"access_key_id": SecretPlaceholder, "secret_access_key": SecretPlaceholder,
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
				"access_key_id": SecretPlaceholder, "secret_access_key": SecretPlaceholder,
				"endpoint": "https://minio.local", "bucket": "b",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			inputs, ok := TemplateInputsFromConfig(tc.template, tc.config, secret)
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
	if _, ok := TemplateInputsFromConfig("no-such", nil, secret); ok {
		t.Error("未知模板应返回 ok=false")
	}
	// 端点与模式不匹配(如手工改过配置)时拼接字段缺省而非误配。
	inputs, ok := TemplateInputsFromConfig("r2", map[string]string{"endpoint": "https://other.example.com"}, secret)
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

// TestWebDAVTemplatePasswordEncoding 验证 WebDAV 模板的密码入库形态:新填的明文按 rclone 要求编码,
// 编辑时提交的哨兵还原为已存的编码值、不再二次编码。
// @Description: 漏编码时后端会把明文当作已编码值解出乱码;把哨兵当明文编码则原密码被替换成乱码。
// 两者都表现为凭证正确却认证失败。
func TestWebDAVTemplatePasswordEncoding(t *testing.T) {
	const plain = "明文密码"
	inputs := map[string]string{
		"url": "https://dav.jianguoyun.com/dav/", "vendor": "other",
		"user": "u@example.com", "pass": plain, "path": "imgyoung",
	}
	drv, submitted, err := BuildTemplateConfig("webdav", inputs)
	if err != nil {
		t.Fatalf("BuildTemplateConfig 报错: %v", err)
	}
	created, err := prepareConfig(drv, submitted, nil, true)
	if err != nil {
		t.Fatalf("整理创建配置: %v", err)
	}
	if got, err := obscure.Reveal(created["pass"]); err != nil || got != plain {
		t.Fatalf("新密码未按明文编码: %q(err=%v)", got, err)
	}
	if created["user"] != "u@example.com" {
		t.Errorf("user 被误编码: %q", created["user"])
	}

	inputs["pass"] = SecretPlaceholder
	inputs["path"] = "imgyoung-2"
	_, submitted, err = BuildTemplateConfig("webdav", inputs)
	if err != nil {
		t.Fatalf("BuildTemplateConfig 报错: %v", err)
	}
	edited, err := prepareConfig(drv, submitted, created, true)
	if err != nil {
		t.Fatalf("整理编辑配置: %v", err)
	}
	if edited["pass"] != created["pass"] {
		t.Fatalf("哨兵未还原为已存编码值: %q, 期望 %q", edited["pass"], created["pass"])
	}
	if edited["path"] != "imgyoung-2" {
		t.Errorf("非敏感字段未替换: %q", edited["path"])
	}
}

// TestDriveTemplatesDeclareOAuth 验证各网盘模板的授权标记与后端实际要求一致:
// 能构造出授权流的后端才允许标记 RequiresOAuth,反之亦然。
func TestDriveTemplatesDeclareOAuth(t *testing.T) {
	for _, tpl := range Templates() {
		t.Run(tpl.ID, func(t *testing.T) {
			_, err := NewOAuthFlow(t.Context(), tpl.Driver, map[string]string{ConfigKeyClientID: "probe"}, "")
			if want := err == nil; tpl.RequiresOAuth != want {
				t.Errorf("模板 %q 标记 RequiresOAuth=%v, 后端 %q 实际 %v(%v)",
					tpl.ID, tpl.RequiresOAuth, tpl.Driver, want, err)
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
	secret, err := secretKeysOf("drive")
	if err != nil {
		t.Fatalf("取 drive 敏感键: %v", err)
	}
	inputs, ok := TemplateInputsFromConfig("googledrive", map[string]string{
		ConfigKeyToken: secretToken, "path": "imgyoung",
	}, secret)
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

// TestTemplateCredentialFieldsAreSecret 验证模板的密码与授权字段都落在后端声明的敏感键上。
// @Description: 回读掩码以后端选项元数据为准;凭证字段若落在元数据不认的键上,编辑回读会把明文下发。
func TestTemplateCredentialFieldsAreSecret(t *testing.T) {
	for _, tpl := range Templates() {
		secret, err := secretKeysOf(tpl.Driver)
		if err != nil {
			t.Fatalf("模板 %q 的后端 %q: %v", tpl.ID, tpl.Driver, err)
		}
		for _, f := range tpl.Fields {
			if (f.Type == FieldPassword || f.Type == FieldOAuth) && !secret[f.configKey] {
				t.Errorf("模板 %q 的凭证字段 %q 落在非敏感键 %q 上", tpl.ID, f.Key, f.configKey)
			}
		}
	}
}

// TestMaskSecretsByBackendMetadata 验证回读掩码以后端选项元数据为准:WebDAV 的密码与账号名掩码,
// 服务地址与约定键 path / public_base_url 照常回显。
func TestMaskSecretsByBackendMetadata(t *testing.T) {
	secret, err := secretKeysOf("webdav")
	if err != nil {
		t.Fatalf("取 webdav 敏感键: %v", err)
	}
	masked := maskSecrets(map[string]string{
		"url": "https://dav.example.com/", "user": "u", "pass": "obscured-value",
		configKeyPath: "imgyoung", configKeyPublicBaseURL: "https://cdn.example.com",
	}, secret)
	for _, k := range []string{"pass", "user"} {
		if masked[k] != SecretPlaceholder {
			t.Errorf("%s = %q, 期望哨兵掩码", k, masked[k])
		}
	}
	for _, k := range []string{"url", configKeyPath, configKeyPublicBaseURL} {
		if masked[k] == SecretPlaceholder {
			t.Errorf("%s 被误掩码", k)
		}
	}
}

// TestEveryTemplateHasCategory 验证每个模板都归入已知分类。
// @Description: 分类驱动前端分组,漏标的模板会整组消失而非降级显示。
func TestEveryTemplateHasCategory(t *testing.T) {
	known := map[string]bool{CategoryObjectStorage: true, CategoryDrive: true}
	for _, tpl := range Templates() {
		if !known[tpl.Category] {
			t.Errorf("模板 %q 的分类 %q 不在已知分类内", tpl.ID, tpl.Category)
		}
	}
}
