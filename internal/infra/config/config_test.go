package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearEnv 置空当前环境里全部 IMGYOUNG_ 变量,保证用例间隔离,
// 并顺带覆盖开发者本机可能已设置的同前缀变量。
// @Description: t.Setenv 置空即可——Load 将空值视为未设置,退出时自动还原。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, envPrefix) {
			t.Setenv(k, "")
		}
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
	if cfg.MaxConcurrentUploads != 8 {
		t.Errorf("MaxConcurrentUploads = %d, 期望默认 8", cfg.MaxConcurrentUploads)
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
	if cfg.EXIFPolicy != "passthrough" {
		t.Errorf("EXIFPolicy = %q, 期望默认 passthrough(默认不动用户原图)", cfg.EXIFPolicy)
	}
	if cfg.MinSuccess != 1 || cfg.AccessMode != "proxy" || cfg.EncryptNewChannels || cfg.HealthCheckSeconds != 60 {
		t.Errorf("多渠道相关默认值异常: min_success=%d access_mode=%q encrypt_new=%v health=%d",
			cfg.MinSuccess, cfg.AccessMode, cfg.EncryptNewChannels, cfg.HealthCheckSeconds)
	}
	if cfg.BackupIntervalHours != 24 || cfg.BackupKeep != 7 {
		t.Errorf("备份默认值异常: interval_hours=%d keep=%d, 期望 24 / 7",
			cfg.BackupIntervalHours, cfg.BackupKeep)
	}
}

// TestLoadInvalidAccessMode 验证非法访问模式快速失败。
func TestLoadInvalidAccessMode(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_ACCESS_MODE", "cdn")
	if _, err := Load(); err == nil {
		t.Fatal("access_mode=cdn 应返回错误")
	}
}

// TestLoadEXIFPolicy 验证 EXIF 档位配置:合法值生效,非法值(含未实现的 spoof)快速失败。
func TestLoadEXIFPolicy(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_EXIF_POLICY", "strip")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if cfg.EXIFPolicy != "strip" {
		t.Errorf("EXIFPolicy = %q, 期望 strip", cfg.EXIFPolicy)
	}

	for _, v := range []string{"spoof", "bogus"} {
		t.Run(v, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("IMGYOUNG_EXIF_POLICY", v)
			if _, err := Load(); err == nil {
				t.Fatalf("exif_policy=%q 应返回错误", v)
			}
		})
	}
}

// TestLoadRefererAndBaseURL 验证防盗链与站点地址相关配置的读取。
func TestLoadRefererAndBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_BASE_URL", "https://img.example.com/")
	t.Setenv("IMGYOUNG_MAX_PIXELS_MP", "2")
	t.Setenv("IMGYOUNG_REFERER_WHITELIST", "*.example.com, blog.foo.dev")
	t.Setenv("IMGYOUNG_ALLOW_EMPTY_REFERER", "false")

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
	t.Setenv("IMGYOUNG_LISTEN", "127.0.0.1:9000")
	t.Setenv("IMGYOUNG_DATA_DIR", "/var/lib/imgyoung")
	t.Setenv("IMGYOUNG_TOKEN", "secret")
	t.Setenv("IMGYOUNG_MAX_UPLOAD_MB", "8")

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
	t.Setenv("IMGYOUNG_LISTEN", ":9999")

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

// TestLoadDefaultConfigFile 验证工作目录下的 configs/config.yml 会被自动发现。
func TestLoadDefaultConfigFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	confPath := filepath.Join(dir, defaultConfigFile)
	if err := os.MkdirAll(filepath.Dir(confPath), 0o750); err != nil {
		t.Fatalf("创建配置目录: %v", err)
	}
	if err := os.WriteFile(confPath, []byte("listen: \":6000\"\n"), 0o600); err != nil {
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
			t.Setenv("IMGYOUNG_MAX_UPLOAD_MB", v)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q 应返回错误", "IMGYOUNG_MAX_UPLOAD_MB", v)
			}
		})
	}
}

// TestLoadInvalidMaxConcurrentUploads 验证非法的并发上限快速失败。
func TestLoadInvalidMaxConcurrentUploads(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_MAX_CONCURRENT_UPLOADS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("max_concurrent_uploads=0 应返回错误")
	}
}

// TestLoadInvalidBackup 验证非正的备份间隔与保留份数快速失败。
func TestLoadInvalidBackup(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_BACKUP_KEEP", "0")
	if _, err := Load(); err == nil {
		t.Fatal("backup_keep=0 应返回错误")
	}
	clearEnv(t)
	t.Setenv("IMGYOUNG_BACKUP_INTERVAL_HOURS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("backup_interval_hours=0 应返回错误")
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
