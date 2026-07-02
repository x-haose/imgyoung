package config

import (
	"os"
	"path/filepath"
	"testing"
)

// clearEnv 清空本包关心的全部环境变量,保证用例间隔离。
// @Description: t.Setenv 置空即可——Load 将空值视为未设置。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		envConfig, envListen, envDataDir, envToken, envMaxUploadMB,
		envBaseURL, envMaxPixelsMP, envRefererWhitelist, envAllowEmptyReferer,
	} {
		t.Setenv(k, "")
	}
}

// writeYAML 在临时目录写一个配置文件并返回其路径。
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "imgyoung.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试配置文件: %v", err)
	}
	return path
}

// TestLoadDefaults 验证无环境变量、无配置文件时返回文档约定的默认值。
func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, 期望 :8080", cfg.Listen)
	}
	if cfg.DataDir != "data" {
		t.Errorf("DataDir = %q, 期望 data", cfg.DataDir)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, 期望空(由 main 生成随机值)", cfg.Token)
	}
	if cfg.MaxUploadBytes != 50<<20 {
		t.Errorf("MaxUploadBytes = %d, 期望 %d", cfg.MaxUploadBytes, 50<<20)
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q, 期望空", cfg.BaseURL)
	}
	if cfg.MaxPixels != 50_000_000 {
		t.Errorf("MaxPixels = %d, 期望 50MP", cfg.MaxPixels)
	}
	if len(cfg.RefererWhitelist) != 0 {
		t.Errorf("RefererWhitelist = %v, 期望空(不启用防盗链)", cfg.RefererWhitelist)
	}
	if !cfg.AllowEmptyReferer {
		t.Error("AllowEmptyReferer = false, 期望默认 true(设计定稿:空 Referer 默认放行)")
	}
	if cfg.EXIFPolicy != "strip" {
		t.Errorf("EXIFPolicy = %q, 期望默认 strip", cfg.EXIFPolicy)
	}
}

// TestLoadEXIFPolicy 验证 EXIF 档位配置:合法值生效,非法值(含未实现的 spoof)快速失败。
func TestLoadEXIFPolicy(t *testing.T) {
	clearEnv(t)
	t.Setenv(envEXIFPolicy, "passthrough")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.EXIFPolicy != "passthrough" {
		t.Errorf("EXIFPolicy = %q, 期望 passthrough", cfg.EXIFPolicy)
	}

	for _, v := range []string{"spoof", "bogus"} {
		t.Run(v, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(envEXIFPolicy, v)
			if _, err := Load(); err == nil {
				t.Fatalf("exif_policy=%q 应返回错误", v)
			}
		})
	}
}

// TestLoadRefererAndBaseURL 验证防盗链与站点地址相关配置的读取。
func TestLoadRefererAndBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv(envBaseURL, "https://img.example.com/")
	t.Setenv(envMaxPixelsMP, "2")
	t.Setenv(envRefererWhitelist, "*.example.com, blog.foo.dev")
	t.Setenv(envAllowEmptyReferer, "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.BaseURL != "https://img.example.com" {
		t.Errorf("BaseURL = %q, 期望去除尾部斜杠", cfg.BaseURL)
	}
	if cfg.MaxPixels != 2_000_000 {
		t.Errorf("MaxPixels = %d, 期望 2MP", cfg.MaxPixels)
	}
	want := []string{"*.example.com", "blog.foo.dev"}
	if len(cfg.RefererWhitelist) != 2 || cfg.RefererWhitelist[0] != want[0] || cfg.RefererWhitelist[1] != want[1] {
		t.Errorf("RefererWhitelist = %v, 期望 %v(逗号分隔并去空白)", cfg.RefererWhitelist, want)
	}
	if cfg.AllowEmptyReferer {
		t.Error("AllowEmptyReferer = true, 期望 false")
	}
}

// TestLoadEnvOverrides 验证环境变量能覆盖全部默认值。
func TestLoadEnvOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv(envListen, "127.0.0.1:9000")
	t.Setenv(envDataDir, "/var/lib/imgyoung")
	t.Setenv(envToken, "secret")
	t.Setenv(envMaxUploadMB, "8")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.Listen != "127.0.0.1:9000" || cfg.DataDir != "/var/lib/imgyoung" ||
		cfg.Token != "secret" || cfg.MaxUploadBytes != 8<<20 {
		t.Fatalf("配置未按环境变量覆盖: %+v", cfg)
	}
}

// TestLoadYAMLFile 验证 IMGYOUNG_CONFIG 指向的 yaml 文件生效。
func TestLoadYAMLFile(t *testing.T) {
	clearEnv(t)
	path := writeYAML(t, "listen: \":7000\"\ndata_dir: /srv/img\ntoken: from-file\nmax_upload_mb: 12\n")
	t.Setenv(envConfig, path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.Listen != ":7000" || cfg.DataDir != "/srv/img" ||
		cfg.Token != "from-file" || cfg.MaxUploadBytes != 12<<20 {
		t.Fatalf("配置未按 yaml 文件加载: %+v", cfg)
	}
}

// TestLoadEnvBeatsFile 验证优先级:环境变量 > 配置文件 > 默认值。
func TestLoadEnvBeatsFile(t *testing.T) {
	clearEnv(t)
	path := writeYAML(t, "listen: \":7000\"\nmax_upload_mb: 12\n")
	t.Setenv(envConfig, path)
	t.Setenv(envListen, ":9999")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.Listen != ":9999" {
		t.Errorf("Listen = %q, 期望环境变量覆盖为 :9999", cfg.Listen)
	}
	if cfg.MaxUploadBytes != 12<<20 {
		t.Errorf("MaxUploadBytes = %d, 期望保留文件值 %d", cfg.MaxUploadBytes, 12<<20)
	}
	if cfg.DataDir != "data" {
		t.Errorf("DataDir = %q, 期望保留默认值 data", cfg.DataDir)
	}
}

// TestLoadDefaultConfigFile 验证工作目录下的 imgyoung.yml 会被自动发现。
func TestLoadDefaultConfigFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, defaultConfigFile), []byte("listen: \":6000\"\n"), 0o600); err != nil {
		t.Fatalf("写默认配置文件: %v", err)
	}
	t.Chdir(dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.Listen != ":6000" {
		t.Fatalf("Listen = %q, 期望自动发现 imgyoung.yml 中的 :6000", cfg.Listen)
	}
}

// TestLoadInvalidMaxUpload 验证非法的上传上限快速失败(设计定稿·输入验证)。
func TestLoadInvalidMaxUpload(t *testing.T) {
	for name, v := range map[string]string{"非数字": "abc", "零": "0", "负数": "-3"} {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(envMaxUploadMB, v)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q 应返回错误", envMaxUploadMB, v)
			}
		})
	}
}

// TestLoadMissingConfigFile 验证显式指定但不存在的配置文件快速失败。
func TestLoadMissingConfigFile(t *testing.T) {
	clearEnv(t)
	t.Setenv(envConfig, "/no/such/file.yml")
	if _, err := Load(); err == nil {
		t.Fatal("指向不存在的配置文件应返回错误")
	}
}

// TestGenerateToken 验证随机 Token 的长度与唯一性。
func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken 返回错误: %v", err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken 返回错误: %v", err)
	}
	if len(a) != tokenHexLen {
		t.Fatalf("Token 长度 = %d, 期望 %d", len(a), tokenHexLen)
	}
	if a == b {
		t.Fatalf("两次生成的 Token 相同: %q", a)
	}
}
