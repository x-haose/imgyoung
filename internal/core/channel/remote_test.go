package channel

import (
	"testing"

	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/fspath"
)

// parseBack 用 rclone 自己的解析器做往返校验,返回解析出的参数表。
func parseBack(t *testing.T, remote string) fspath.Parsed {
	t.Helper()
	parsed, err := fspath.Parse(remote)
	if err != nil {
		t.Fatalf("rclone 无法解析构造出的连接字符串 %q: %v", remote, err)
	}
	return parsed
}

// TestBuildRemotePlain 验证普通参数的连接字符串构造与往返解析。
func TestBuildRemotePlain(t *testing.T) {
	remote := BuildRemote("s3", map[string]string{
		"provider":          "Cloudflare",
		"access_key_id":     "AKID",
		"secret_access_key": "SK",
	}, "bucket/imgs")

	parsed := parseBack(t, remote)
	if parsed.Name != ":s3" {
		t.Fatalf("backend = %q, 期望 :s3", parsed.Name)
	}
	if parsed.Path != "bucket/imgs" {
		t.Fatalf("path = %q, 期望 bucket/imgs", parsed.Path)
	}
	if parsed.Config["provider"] != "Cloudflare" || parsed.Config["secret_access_key"] != "SK" {
		t.Fatalf("参数往返不一致: %v", parsed.Config)
	}
}

// TestBuildRemoteQuotesSpecialValues 验证含逗号/冒号/引号的值被正确引用转义。
func TestBuildRemoteQuotesSpecialValues(t *testing.T) {
	tricky := map[string]string{
		"a": "has,comma",
		"b": "has:colon",
		"c": "has'quote",
		"d": `has"double`,
		"e": "plain",
	}
	parsed := parseBack(t, BuildRemote("webdav", tricky, "dir"))
	for k, want := range tricky {
		if got := parsed.Config[k]; got != want {
			t.Errorf("参数 %s 往返 = %q, 期望 %q", k, got, want)
		}
	}
}

// TestBuildRemoteNoParams 验证无参数后端(如 local)的形式。
func TestBuildRemoteNoParams(t *testing.T) {
	parsed := parseBack(t, BuildRemote("local", nil, "/data/blobs"))
	if parsed.Name != ":local" || parsed.Path != "/data/blobs" {
		t.Fatalf("往返 = %+v, 期望 :local + /data/blobs", parsed)
	}
}

// TestApplyDriverDefaults 验证 s3 默认跳过桶检查,且不覆盖用户显式值、不波及其他后端。
func TestApplyDriverDefaults(t *testing.T) {
	s3 := map[string]string{"access_key_id": "AK"}
	applyDriverDefaults("s3", s3)
	if s3["no_check_bucket"] != "true" {
		t.Errorf("s3 默认 no_check_bucket = %q, 期望 true", s3["no_check_bucket"])
	}

	override := map[string]string{"no_check_bucket": "false"}
	applyDriverDefaults("s3", override)
	if override["no_check_bucket"] != "false" {
		t.Errorf("用户显式值被覆盖 = %q, 期望保留 false", override["no_check_bucket"])
	}

	local := map[string]string{}
	applyDriverDefaults("local", local)
	if _, ok := local["no_check_bucket"]; ok {
		t.Error("非 s3 后端不应被注入 no_check_bucket")
	}
}

// TestWrapCrypt 验证 crypt 包裹:内层连接字符串完整嵌套、密码经 obscure 可还原。
func TestWrapCrypt(t *testing.T) {
	inner := BuildRemote("s3", map[string]string{"access_key_id": "AK,ID"}, "bucket/x")
	wrapped, err := WrapCrypt(inner, "file-pass", "salt-pass")
	if err != nil {
		t.Fatalf("WrapCrypt 返回错误: %v", err)
	}

	parsed := parseBack(t, wrapped)
	if parsed.Name != ":crypt" {
		t.Fatalf("backend = %q, 期望 :crypt", parsed.Name)
	}
	if got := parsed.Config["remote"]; got != inner {
		t.Fatalf("内层 remote 往返 = %q, 期望 %q", got, inner)
	}
	pw, err := obscure.Reveal(parsed.Config["password"])
	if err != nil || pw != "file-pass" {
		t.Fatalf("password 还原 = %q(err=%v), 期望 file-pass", pw, err)
	}
	pw2, err := obscure.Reveal(parsed.Config["password2"])
	if err != nil || pw2 != "salt-pass" {
		t.Fatalf("password2 还原 = %q(err=%v), 期望 salt-pass", pw2, err)
	}
	if parsed.Config["filename_encryption"] != "standard" {
		t.Fatalf("filename_encryption = %q, 期望 standard", parsed.Config["filename_encryption"])
	}
}
