//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// countObjects 统计渠道存储目录下的对象文件数(排除 _imgyoung 备份等元数据前缀)。
func countObjects(dir string) int {
	n := 0
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.Contains(p, "_imgyoung") {
			n++
		}
		return nil
	})
	return n
}

func dirSizeBytes(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// TestDedupMergeSharedBlob 精确查重:两张字节相同图各自落一份对象 → 合并后两引用仍可取回,
// 冗余对象被物理回收(2→1),证明合并为同一物理副本。
func TestDedupMergeSharedBlob(t *testing.T) {
	inst := startInstance(t, map[string]any{
		"dedup_scan_seconds":    2,
		"cleanup_sweep_seconds": 2,
		"delete_grace_minutes":  0,
	})
	src := procJPEG(256, 256, 0)
	id1, u1 := mustUpload(t, inst, src, nil, map[string]string{"Idempotency-Key": "dd-a"})
	id2, u2 := mustUpload(t, inst, src, nil, map[string]string{"Idempotency-Key": "dd-b"})
	if id1 == id2 {
		t.Fatalf("两次上传应得不同引用 id,得同一 %q", id1)
	}
	if n := countObjects(blobsDirOf(inst)); n < 2 {
		t.Fatalf("合并前应有 2 份对象,得 %d", n)
	}
	// 等 dedup 合并 + sweeper 物理回收冗余对象。
	merged := waitFor(15*time.Second, func() bool { return countObjects(blobsDirOf(inst)) == 1 })
	// 两引用都仍可取回(指向 keeper)。
	r1 := fetch(t, inst, u1, "")
	c1 := r1.StatusCode
	_ = r1.Body.Close()
	r2 := fetch(t, inst, u2, "")
	c2 := r2.StatusCode
	_ = r2.Body.Close()
	if c1 != 200 || c2 != 200 {
		t.Errorf("合并后两引用应仍可取回,得 %d %d", c1, c2)
	}
	if !merged {
		t.Errorf("冗余对象未回收,对象数=%d(期望 1)", countObjects(blobsDirOf(inst)))
	}
}

// TestMultiRefKeepsBlob 多引用共享 blob:合并后 refcount=2,删一个引用(ttl)另一个仍可取回、对象保留。
func TestMultiRefKeepsBlob(t *testing.T) {
	inst := startInstance(t, map[string]any{
		"dedup_scan_seconds":    2,
		"cleanup_sweep_seconds": 2,
		"delete_grace_minutes":  0,
	})
	src := procJPEG(200, 200, 0)
	_, uKeep := mustUpload(t, inst, src, nil, map[string]string{"Idempotency-Key": "mr-keep"})
	_, uTTL := mustUpload(t, inst, src, map[string]string{"ttl": "3"}, map[string]string{"Idempotency-Key": "mr-ttl"})
	// 等合并成同一 blob(对象数降为 1)。
	if !waitFor(15*time.Second, func() bool { return countObjects(blobsDirOf(inst)) == 1 }) {
		t.Fatalf("合并未完成,对象数=%d", countObjects(blobsDirOf(inst)))
	}
	// 等带 ttl 的引用到期回收。
	gone := waitFor(15*time.Second, func() bool {
		r := fetch(t, inst, uTTL, "")
		c := r.StatusCode
		_ = r.Body.Close()
		return c == 404
	})
	if !gone {
		t.Error("ttl 引用未到期回收")
	}
	// 另一永久引用仍可取回,且共享的物理对象仍在(refcount>1 时删一引用不删 blob)。
	r := fetch(t, inst, uKeep, "")
	c := r.StatusCode
	_ = r.Body.Close()
	if c != 200 {
		t.Errorf("保留引用应仍可取回,得 %d", c)
	}
	if n := countObjects(blobsDirOf(inst)); n != 1 {
		t.Errorf("共享 blob 应保留(refcount>1),对象数=%d(期望 1)", n)
	}
}

// TestHealthFlip 健康翻转:渠道后端不可达 → 标记不健康;恢复 → 标记健康。
func TestHealthFlip(t *testing.T) {
	inst := startInstance(t, nil) // 健康检查间隔已由 startInstance 默认压到 1 秒
	dir := filepath.Join(inst.dataDir, "flip")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	jb := fmt.Sprintf(`{"name":"flip","driver":"local","priority":50,"config":{"path":%q}}`, dir)
	createChannel(t, inst, jb)
	if !waitFor(10*time.Second, func() bool { return channelHealthy(t, inst, "flip") }) {
		t.Fatal("渠道初始应健康")
	}
	// 让后端不可达:把目录换成同名普通文件,探测 Stat 报非 NotFound 错 → 不健康。
	_ = os.RemoveAll(dir)
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !waitFor(10*time.Second, func() bool { return !channelHealthy(t, inst, "flip") }) {
		t.Error("后端不可达后应标记不健康")
	}
	// 恢复:删文件、建回目录 → 探测恢复 → 健康。
	_ = os.Remove(dir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if !waitFor(10*time.Second, func() bool { return channelHealthy(t, inst, "flip") }) {
		t.Error("后端恢复后应重新标记健康")
	}
}

// TestVariantLRUEviction 变体缓存容量上限:生成远超上限的变体后,缓存目录总量被 LRU 压回上限附近。
func TestVariantLRUEviction(t *testing.T) {
	widths := []int{200, 300, 400, 500, 600, 700, 800, 900, 1000, 1100, 1200, 1280}
	formats := []string{"webp", "jpeg", "png"}
	inst := startInstance(t, map[string]any{
		"variants_cache_mb": 1, // 1MB 上限
		"process_widths":    widths,
		"process_formats":   formats,
	})
	_, url := mustUpload(t, inst, procJPEG(1400, 1400, 0), nil, nil)
	// 生成大量不同变体(12 宽 × 3 格式 = 36 个),总量远超 1MB。
	for _, w := range widths {
		for _, f := range formats {
			r := fetch(t, inst, fmt.Sprintf("%s?w=%d&format=%s", url, w, f), "")
			_ = r.Body.Close()
		}
	}
	varDir := filepath.Join(inst.dataDir, "variants")
	// 等淘汰稳定后,缓存总量应被压回 1MB 上限附近(留一个在途文件的余量)。
	time.Sleep(1 * time.Second)
	size := dirSizeBytes(varDir)
	const cap1MB = 1 << 20
	if size > cap1MB*3/2 {
		t.Errorf("LRU 未生效:缓存 %d 字节 远超 1MB 上限", size)
	}
	if size == 0 {
		t.Error("缓存目录为空,变体未落盘")
	}
}

func blobsDirOf(inst *instance) string { return filepath.Join(inst.dataDir, "blobs") }

func waitFor(d time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
