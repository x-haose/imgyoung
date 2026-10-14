package channel_test

import (
	"errors"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/channel"
)

// minioTestInputs minio 模板的一组完整表单输入(端点指向必然拒连的本地端口,
// 探测快速失败但不阻塞创建/编辑)。
func minioTestInputs() map[string]string {
	return map[string]string{
		"access_key_id":     "AKTEST",
		"secret_access_key": "SECRET-ORIGINAL-0123",
		"endpoint":          "http://127.0.0.1:1",
		"bucket":            "bkt",
	}
}

// createMinioChannel 以 minio 模板创建一条 s3 渠道(深探测失败不影响创建)。
// @return 渠道 ID
func createMinioChannel(t *testing.T, env *testEnv) int64 {
	t.Helper()
	drv, cfg, err := channel.BuildTemplateConfig("minio", minioTestInputs())
	if err != nil {
		t.Fatalf("合并模板配置: %v", err)
	}
	id, err := env.mgr.Create(t.Context(), channel.CreateParams{
		Name: "minio-edit", Driver: drv, Config: cfg, Template: "minio", Priority: 10,
	})
	if err != nil {
		t.Fatalf("创建模板渠道: %v", err)
	}
	return id
}

// TestManagerConfigForEdit 验证配置回读:模板 id 持久化、敏感值哨兵掩码、
// 非敏感值明文预填,模板表单字段可还原。
func TestManagerConfigForEdit(t *testing.T) {
	env := newTestEnv(t)
	id := createMinioChannel(t, env)

	ec, err := env.mgr.ConfigForEdit(t.Context(), id)
	if err != nil {
		t.Fatalf("ConfigForEdit 返回错误: %v", err)
	}
	if ec.Template != "minio" || ec.Driver != "s3" {
		t.Fatalf("模板/后端未还原: %+v", ec)
	}
	if ec.Config["secret_access_key"] != channel.SecretPlaceholder {
		t.Fatalf("敏感配置值未掩码: %q", ec.Config["secret_access_key"])
	}
	if ec.Config["endpoint"] != "http://127.0.0.1:1" || ec.Config["path"] != "bkt" {
		t.Fatalf("非敏感配置未明文回读: %+v", ec.Config)
	}
	if ec.Inputs["secret_access_key"] != channel.SecretPlaceholder {
		t.Fatalf("密码字段未掩码: %+v", ec.Inputs)
	}
	if ec.Inputs["access_key_id"] != "AKTEST" || ec.Inputs["bucket"] != "bkt" ||
		ec.Inputs["endpoint"] != "http://127.0.0.1:1" {
		t.Fatalf("模板表单字段未还原: %+v", ec.Inputs)
	}

	if _, err := env.mgr.ConfigForEdit(t.Context(), 99999); !errors.Is(err, channel.ErrChannelNotFound) {
		t.Fatalf("回读不存在渠道错误 = %v, 期望 ErrChannelNotFound", err)
	}
}

// TestManagerUpdateKeepsSecretSentinel 验证编辑保留凭证:提交哨兵值时沿用已存
// 原值,非敏感字段照常替换;哨兵无原值可还原时明确拒绝。
func TestManagerUpdateKeepsSecretSentinel(t *testing.T) {
	env := newTestEnv(t)
	id := createMinioChannel(t, env)

	drv, cfg, err := channel.BuildTemplateConfig("minio", map[string]string{
		"access_key_id":     "AKTEST",
		"secret_access_key": channel.SecretPlaceholder,
		"endpoint":          "http://127.0.0.1:2",
		"bucket":            "bkt2",
	})
	if err != nil {
		t.Fatalf("合并模板配置: %v", err)
	}
	if err := env.mgr.Update(t.Context(), id, channel.UpdateParams{
		Name: "minio-edit", Driver: drv, Config: cfg, Template: "minio", Priority: 20,
	}); err != nil {
		t.Fatalf("哨兵编辑返回错误: %v", err)
	}
	restored, err := env.mgr.RestoreSecrets(t.Context(), id, map[string]string{
		"secret_access_key": channel.SecretPlaceholder,
	})
	if err != nil {
		t.Fatalf("RestoreSecrets 返回错误: %v", err)
	}
	if restored["secret_access_key"] != "SECRET-ORIGINAL-0123" {
		t.Fatalf("原凭证未保留: %q", restored["secret_access_key"])
	}
	ec, err := env.mgr.ConfigForEdit(t.Context(), id)
	if err != nil {
		t.Fatalf("回读编辑结果: %v", err)
	}
	if ec.Config["endpoint"] != "http://127.0.0.1:2" || ec.Config["path"] != "bkt2" {
		t.Fatalf("非敏感字段未替换: %+v", ec.Config)
	}

	// 哨兵值指向不存在的原值:只能重新填写,不能静默落一个哨兵字面量。
	bad := map[string]string{"access_key_id": "AKTEST", "secret_access_key": channel.SecretPlaceholder,
		"endpoint": "http://127.0.0.1:2", "path": "bkt2", "session_token": channel.SecretPlaceholder}
	if err := env.mgr.Update(t.Context(), id, channel.UpdateParams{
		Name: "minio-edit", Driver: "s3", Config: bad, Template: "",
	}); !errors.Is(err, channel.ErrInvalidChannel) {
		t.Fatalf("无原值哨兵错误 = %v, 期望 ErrInvalidChannel", err)
	}

	if _, err := env.mgr.RestoreSecrets(t.Context(), 99999, nil); !errors.Is(err, channel.ErrChannelNotFound) {
		t.Fatalf("还原不存在渠道错误 = %v, 期望 ErrChannelNotFound", err)
	}
}
