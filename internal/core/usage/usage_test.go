package usage_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/x-haose/imgyoung/internal/core/usage"
	"github.com/x-haose/imgyoung/internal/infra/db/dbtest"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/observability"
)

// TestReportOnceSumsDoneReplicas 验证用量上报按已就绪副本的图片大小求和,写入指标集并经 /metrics 暴露。
func TestReportOnceSumsDoneReplicas(t *testing.T) {
	_, q := dbtest.New(t)
	ctx := t.Context()

	// 两个 blob(100 + 250 字节)在内置 local 渠道(id=1)就绪。
	for _, sz := range []int64{100, 250} {
		blobID, err := q.CreateBlob(ctx, sqlc.CreateBlobParams{
			Sha256: "h", Phash: 1, StorageKey: "k", Mime: "image/png", Size: sz, Width: 1, Height: 1,
		})
		if err != nil {
			t.Fatalf("建 blob: %v", err)
		}
		if err := q.UpsertBlobChannel(ctx, sqlc.UpsertBlobChannelParams{
			BlobID: blobID, ChannelID: 1, State: "done",
		}); err != nil {
			t.Fatalf("建副本: %v", err)
		}
	}

	metrics := observability.New()
	r := usage.New(usage.Config{
		Queries: q, Gauge: metrics, QuotaBytes: func() int64 { return 0 }, Logger: slog.New(slog.DiscardHandler),
	})
	if err := r.ReportOnce(ctx); err != nil {
		t.Fatalf("ReportOnce 返回错误: %v", err)
	}

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	const want = `imgyoung_channel_storage_bytes{channel="local"} 350`
	if body := rec.Body.String(); !strings.Contains(body, want) {
		t.Fatalf("/metrics 未见 %s:\n%s", want, body)
	}
}
