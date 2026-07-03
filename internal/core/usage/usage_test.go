package usage_test

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/usage"
	"github.com/x-haose/imgyoung/internal/infra/db"
)

// fakeGauge 记录各渠道被设置的用量值。
type fakeGauge struct {
	vals map[string]float64
}

// SetChannelStorage 记录设置值。
func (f *fakeGauge) SetChannelStorage(channel string, bytes float64) {
	f.vals[channel] = bytes
}

// TestReportOnceSumsDoneReplicas 验证用量上报按已就绪副本的图片大小求和,写入量表。
func TestReportOnceSumsDoneReplicas(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(conn); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	q := db.New(conn)
	ctx := t.Context()

	// 两个 blob(100 + 250 字节)在内置 local 渠道(id=1)就绪。
	for _, sz := range []int64{100, 250} {
		blobID, err := q.CreateBlob(ctx, db.CreateBlobParams{
			Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: sz, Width: 1, Height: 1,
		})
		if err != nil {
			t.Fatalf("建 blob: %v", err)
		}
		if err := q.UpsertBlobChannel(ctx, db.UpsertBlobChannelParams{BlobID: blobID, ChannelID: 1, State: "done"}); err != nil {
			t.Fatalf("建副本: %v", err)
		}
	}

	g := &fakeGauge{vals: map[string]float64{}}
	r := usage.New(usage.Config{Queries: q, Gauge: g, QuotaBytes: 0, Logger: slog.New(slog.DiscardHandler)})
	if err := r.ReportOnce(ctx); err != nil {
		t.Fatalf("ReportOnce 返回错误: %v", err)
	}

	if got := g.vals["local"]; got != 350 {
		t.Fatalf("local 渠道用量 = %v, 期望 350(100+250)", got)
	}
}
