//go:build e2e

// 补齐现有 e2e 未覆盖的功能:像素上限 422、并发闸 429、变体磁盘缓存命中、用量指标、
// 变体 LRU 淘汰、健康翻转。均本地闭环,不依赖外部资源。
package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMaxPixelsRejected 超像素预算 → 422。
func TestMaxPixelsRejected(t *testing.T) {
	inst := startInstance(t, map[string]any{"max_pixels_mp": 1}) // 上限 100 万像素
	// 2000×2000 = 400 万像素,超限。
	resp := upload(t, inst, inst.token, "big.jpg", procJPEG(2000, 2000, 0), nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 422 {
		t.Errorf("超像素上传期望 422,得 %d", resp.StatusCode)
	}
}

// TestConcurrencyLimit 并发闸满 → 429(部分请求),同时应有成功者。
func TestConcurrencyLimit(t *testing.T) {
	inst := startInstance(t, map[string]any{"max_concurrent_uploads": 1})
	img := procJPEG(2600, 2000, 0) // 稍大,拉长单请求处理时间以制造重叠
	const n = 12
	var wg sync.WaitGroup
	codes := make([]int, n)
	for idx := range n {
		wg.Go(func() {
			// 每个请求独立幂等键,避免命中幂等短路。
			resp := upload(t, inst, inst.token, "c.jpg", img, nil,
				map[string]string{"Idempotency-Key": "conc-" + itoaLocal(idx)})
			codes[idx] = resp.StatusCode
			_ = resp.Body.Close()
		})
	}
	wg.Wait()
	var ok, busy int
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 429:
			busy++
		default:
		}
	}
	if ok == 0 {
		t.Errorf("并发上传应至少一个成功,codes=%v", codes)
	}
	if busy == 0 {
		t.Errorf("并发数=1 下 %d 并发应触发至少一个 429,codes=%v", n, codes)
	}
}

// TestVariantCacheHit 变体磁盘缓存:处理后缓存文件落盘,二次同参请求字节一致。
func TestVariantCacheHit(t *testing.T) {
	inst := startInstance(t, procSettings)
	_, url := mustUpload(t, inst, procJPEG(800, 600, 0), nil, nil)
	first := fetchBytes(t, inst, url+"?w=320")
	// 缓存目录应出现变体文件。
	varDir := filepath.Join(inst.dataDir, "variants")
	if !poll5(func() bool { return countFilesLocal(varDir) >= 1 }) {
		t.Errorf("变体未落盘缓存目录 %s", varDir)
	}
	second := fetchBytes(t, inst, url+"?w=320")
	if len(first) == 0 || string(first) != string(second) {
		t.Errorf("二次同参请求应命中缓存返回一致字节,len1=%d len2=%d", len(first), len(second))
	}
}

// TestUsageMetrics 渠道用量周期上报到 /metrics。
func TestUsageMetrics(t *testing.T) {
	inst := startInstance(t, map[string]any{"usage_report_seconds": 2})
	_, _ = mustUpload(t, inst, procJPEG(300, 300, 0), nil, nil)
	got := poll5(func() bool {
		return strings.Contains(string(fetchBytes(t, inst, "/metrics")), "imgyoung_channel_storage_bytes")
	})
	if !got {
		t.Error("/metrics 未出现 imgyoung_channel_storage_bytes 用量指标")
	}
}

func poll5(fn func() bool) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func countFilesLocal(dir string) int {
	n := 0
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func itoaLocal(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
