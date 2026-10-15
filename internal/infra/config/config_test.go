package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/secret"
)

// clearEnv 移除当前环境里全部 IMGYOUNG_ 变量(测试结束恢复),保证用例间隔离;设为空串另有语义,只能移除。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, envPrefix) {
			t.Setenv(k, "") // 登记测试结束时恢复原值
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("移除环境变量 %s: %v", k, err)
			}
		}
	}
}

// testDB 打开并迁移一个临时 SQLite 库。
func testDB(t *testing.T) (*sql.DB, *sqlc.Queries) {
	t.Helper()
	return dbtest.New(t)
}

// newStore 在指定库上装载配置存储(测试主密钥)。
func newStore(t *testing.T, conn *sql.DB, queries *sqlc.Queries, master *secret.MasterKey) *Store {
	t.Helper()
	store, err := LoadStore(context.Background(), conn, queries, master)
	if err != nil {
		t.Fatalf("LoadStore 返回错误: %v", err)
	}
	return store
}

// newMaster 生成测试用主密钥。
func newMaster(t *testing.T) *secret.MasterKey {
	t.Helper()
	key, err := secret.NewMasterKey()
	if err != nil {
		t.Fatalf("生成测试主密钥: %v", err)
	}
	return key
}

// rawPatch 把任意值编码为 Update 的 patch 项。
func rawPatch(t *testing.T, v any) json.RawMessage {
	t.Helper()
	blob, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("编码 patch 值: %v", err)
	}
	return blob
}

// mustBootstrap 装配引导配置,失败即终止测试。
func mustBootstrap(t *testing.T) Bootstrap {
	t.Helper()
	b, err := LoadBootstrap()
	if err != nil {
		t.Fatalf("LoadBootstrap 返回错误: %v", err)
	}
	return b
}

// TestLoadBootstrapDefaults 验证无环境变量时引导配置取文档约定的默认值:
// 反代只信回环,/metrics 只对回环与私网开放。
func TestLoadBootstrapDefaults(t *testing.T) {
	clearEnv(t)
	b := mustBootstrap(t)
	if b.Listen != ":8721" || b.DataDir != "data" {
		t.Errorf("引导默认值异常: %+v", b)
	}
	if b.MasterKey != "" {
		t.Errorf("主密钥期望默认为空: %+v", b)
	}
	if got := FormatPrefixes(b.TrustedProxies); got != "127.0.0.0/8, ::1/128" {
		t.Errorf("默认信任代理 = %q", got)
	}
	const wantMetrics = "127.0.0.0/8, ::1/128, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7"
	if got := FormatPrefixes(b.MetricsAllow); got != wantMetrics {
		t.Errorf("默认 /metrics 放行网段 = %q", got)
	}
}

// TestLoadBootstrapEnv 验证引导配置的环境变量覆盖,网段清单接受 CIDR 与裸地址。
func TestLoadBootstrapEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_LISTEN", "127.0.0.1:9000")
	t.Setenv("IMGYOUNG_DATA_DIR", "/var/lib/imgyoung")
	t.Setenv("IMGYOUNG_MASTER_KEY", "deadbeef")
	t.Setenv("IMGYOUNG_TRUSTED_PROXIES", " 172.16.0.0/12 , 203.0.113.9 ,,")
	t.Setenv("IMGYOUNG_METRICS_ALLOW", "10.1.2.3/8")
	b := mustBootstrap(t)
	if b.Listen != "127.0.0.1:9000" || b.DataDir != "/var/lib/imgyoung" ||
		b.MasterKey != "deadbeef" {
		t.Fatalf("引导配置未按环境变量覆盖: %+v", b)
	}
	if got := FormatPrefixes(b.TrustedProxies); got != "172.16.0.0/12, 203.0.113.9/32" {
		t.Errorf("信任代理 = %q, 期望裸地址转为 /32、空项忽略", got)
	}
	if got := FormatPrefixes(b.MetricsAllow); got != "10.0.0.0/8" {
		t.Errorf("/metrics 放行网段 = %q, 期望主机位清零", got)
	}
}

// TestLoadBootstrapExplicitEmpty 验证设为空串是显式置空而非取默认:网段清单为空(一概不信转发头、
// /metrics 对谁都不开放),主密钥为空(用文件);监听地址与数据目录不接受空值,错误指出变量名。
func TestLoadBootstrapExplicitEmpty(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_TRUSTED_PROXIES", "")
	t.Setenv("IMGYOUNG_METRICS_ALLOW", " ")
	t.Setenv("IMGYOUNG_MASTER_KEY", "")
	b := mustBootstrap(t)
	if len(b.TrustedProxies) != 0 || len(b.MetricsAllow) != 0 || b.MasterKey != "" {
		t.Fatalf("显式置空未生效: %+v", b)
	}
	for _, key := range []string{"IMGYOUNG_LISTEN", "IMGYOUNG_DATA_DIR"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")
			if _, err := LoadBootstrap(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s 置空的错误 = %v, 期望启动失败并指出变量名", key, err)
			}
		})
	}
}

// TestLoadBootstrapRejectsBadPrefix 验证网段清单写错一项即启动失败,错误指出变量名与条目。
func TestLoadBootstrapRejectsBadPrefix(t *testing.T) {
	clearEnv(t)
	t.Setenv("IMGYOUNG_METRICS_ALLOW", "10.0.0.0/8, not-an-ip")
	_, err := LoadBootstrap()
	if err == nil || !strings.Contains(err.Error(), "IMGYOUNG_METRICS_ALLOW") ||
		!strings.Contains(err.Error(), "not-an-ip") {
		t.Fatalf("错误 = %v, 期望指出变量名与非法条目", err)
	}
}

// TestDefaultsMatchSchema 漂移守卫:全新库的生效配置必须与 Go 侧 Defaults 逐字段一致,
// 保证建表列 DEFAULT 与 Defaults 两处不漂移。
func TestDefaultsMatchSchema(t *testing.T) {
	conn, queries := testDB(t)
	got := newStore(t, conn, queries, newMaster(t)).Current()
	if !reflect.DeepEqual(got, Defaults()) {
		t.Fatalf("首启配置与 Defaults 不一致:\n 表 = %+v\n Go = %+v", got, Defaults())
	}
}

// TestStoreUpdateHotAndPersist 验证修改即时可见、跨装载持久,且返回变更清单。
func TestStoreUpdateHotAndPersist(t *testing.T) {
	conn, queries := testDB(t)
	master := newMaster(t)
	store := newStore(t, conn, queries, master)

	changes, err := store.Update(context.Background(), map[string]json.RawMessage{
		"max_upload_mb": rawPatch(t, 100),
		"access_mode":   rawPatch(t, "redirect"),
		"base_url":      rawPatch(t, "https://img.example.com/"),
	})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %v, 期望 3 项", changes)
	}
	cur := store.Current()
	if cur.MaxUploadMB != 100 || cur.AccessMode != "redirect" {
		t.Fatalf("快照未热更新: %+v", cur)
	}
	if cur.BaseURL != "https://img.example.com" {
		t.Fatalf("BaseURL = %q, 期望归一去尾斜杠", cur.BaseURL)
	}

	if s := newStore(t, conn, queries, master).Current(); s.MaxUploadMB != 100 || s.AccessMode != "redirect" {
		t.Fatalf("修改未持久化: %+v", s)
	}
}

// TestStoreUpdateProcessWidths 验证数组类档位改动的编码/解码往返。
func TestStoreUpdateProcessWidths(t *testing.T) {
	conn, queries := testDB(t)
	master := newMaster(t)
	store := newStore(t, conn, queries, master)

	if _, err := store.Update(context.Background(), map[string]json.RawMessage{
		"process_widths":  rawPatch(t, []int{320, 640}),
		"process_formats": rawPatch(t, []string{"webp"}),
	}); err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	s := newStore(t, conn, queries, master).Current()
	if len(s.ProcessWidths) != 2 || s.ProcessWidths[1] != 640 {
		t.Fatalf("ProcessWidths = %v, 期望 [320 640]", s.ProcessWidths)
	}
	if len(s.ProcessFormats) != 1 || s.ProcessFormats[0] != "webp" {
		t.Fatalf("ProcessFormats = %v, 期望 [webp]", s.ProcessFormats)
	}
}

// TestStoreUpdateRejectsInvalid 验证未知键与非法值被拒绝且快照不变。
func TestStoreUpdateRejectsInvalid(t *testing.T) {
	conn, queries := testDB(t)
	store := newStore(t, conn, queries, newMaster(t))

	if _, err := store.Update(context.Background(),
		map[string]json.RawMessage{"listen": rawPatch(t, ":9999")}); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("未知键 err = %v, 期望 ErrUnknownKey", err)
	}
	for name, patch := range map[string]map[string]json.RawMessage{
		"取值越界":  {"access_mode": rawPatch(t, "cdn")},
		"类型不符":  {"max_upload_mb": rawPatch(t, "abc")},
		"档位清空":  {"process_formats": rawPatch(t, []string{})},
		"档位含非法": {"process_widths": rawPatch(t, []int{0})},
		"时长溢出":  {"default_ttl_hours": rawPatch(t, MaxDurationHours+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Update(context.Background(), patch); !errors.Is(err, ErrInvalidSetting) {
				t.Fatalf("err = %v, 期望 ErrInvalidSetting", err)
			}
		})
	}
	if s := store.Current(); s.AccessMode != "proxy" || s.MaxUploadMB != 50 {
		t.Fatalf("被拒修改不应影响快照: %+v", s)
	}
}

// TestStoreUpdateNoop 验证取值未变时不产生变更清单。
func TestStoreUpdateNoop(t *testing.T) {
	conn, queries := testDB(t)
	store := newStore(t, conn, queries, newMaster(t))
	changes, err := store.Update(context.Background(),
		map[string]json.RawMessage{"max_upload_mb": rawPatch(t, 50)})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %v, 期望空(取值未变)", changes)
	}
}

// TestStoreUpdateRejectionIsAtomic 验证含数组字段的非法 patch 整体拒绝、不就地改写快照。
// @Description: 守卫 applyPatch 的切片深拷贝——json 解码复用底层数组的坑,若回归则本用例
// 会看到 ProcessWidths 被非法 patch 部分改写。
func TestStoreUpdateRejectionIsAtomic(t *testing.T) {
	conn, queries := testDB(t)
	store := newStore(t, conn, queries, newMaster(t))
	before := append([]int(nil), store.Current().ProcessWidths...)

	// 同一 patch 里合法数组 + 非法枚举:必须整体拒绝,数组不得被改写。
	if _, err := store.Update(context.Background(), map[string]json.RawMessage{
		"process_widths": rawPatch(t, []int{50, 60}),
		"access_mode":    rawPatch(t, "garbage"),
	}); !errors.Is(err, ErrInvalidSetting) {
		t.Fatalf("err = %v, 期望 ErrInvalidSetting", err)
	}
	if got := store.Current().ProcessWidths; !reflect.DeepEqual(got, before) {
		t.Fatalf("被拒 patch 就地改写了快照数组: %v, 期望 %v", got, before)
	}
}

// TestStoreConcurrentUpdateAndRead 以 -race 守卫:并发改 process_widths 与无锁读快照切片无竞争。
func TestStoreConcurrentUpdateAndRead(t *testing.T) {
	conn, queries := testDB(t)
	store := newStore(t, conn, queries, newMaster(t))

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if _, err := store.Update(context.Background(), map[string]json.RawMessage{
				"process_widths": rawPatch(t, []int{100 + i, 200 + i}),
			}); err != nil {
				t.Errorf("并发 Update: %v", err)
			}
		})
	}
	for range 8 {
		wg.Go(func() {
			for range 50 {
				total := 0
				for _, w := range store.Current().ProcessWidths {
					total += w
				}
				_ = total
			}
		})
	}
	wg.Wait()
}

// TestStoreSecretRoundtrip 验证凭证键密文入库、装载解密、审计脱敏。
func TestStoreSecretRoundtrip(t *testing.T) {
	conn, queries := testDB(t)
	master := newMaster(t)
	store := newStore(t, conn, queries, master)

	changes, err := store.Update(context.Background(),
		map[string]json.RawMessage{cdnAPITokenKey: rawPatch(t, "cf-token-123")})
	if err != nil {
		t.Fatalf("Update 返回错误: %v", err)
	}
	if len(changes) != 1 || changes[0].New != maskedSet || changes[0].Old != maskedUnset {
		t.Fatalf("changes = %+v, 期望凭证值脱敏为 %s/%s", changes, maskedUnset, maskedSet)
	}

	row, err := queries.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("读取入库值: %v", err)
	}
	if row.CdnCloudflareApiToken == "" || strings.Contains(row.CdnCloudflareApiToken, "cf-token-123") {
		t.Fatalf("入库值 %q 应为非空密文", row.CdnCloudflareApiToken)
	}

	if s := newStore(t, conn, queries, master).Current(); s.CDNCloudflareAPIToken != "cf-token-123" {
		t.Fatalf("装载解密后 = %q, 期望明文 cf-token-123", s.CDNCloudflareAPIToken)
	}

	// Masked 抹掉凭证,供对外输出。
	if newStore(t, conn, queries, master).Current().Masked().CDNCloudflareAPIToken != "" {
		t.Fatal("Masked 未抹掉凭证明文")
	}
}
