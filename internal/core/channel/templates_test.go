package channel

import (
	"errors"
	"testing"
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
