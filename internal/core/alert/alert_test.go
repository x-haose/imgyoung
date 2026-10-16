package alert_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/alert"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/reliability"
	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// receiver 收集 Webhook 投递的本地 HTTP 服务,可按配置拒收。
type receiver struct {
	mu     sync.Mutex
	got    []map[string]any // 收到的请求体,按到达顺序
	reject bool             // 为 true 时回 500,模拟接收方故障
}

// ServeHTTP 记录请求体并按配置回包。
func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.reject {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	rc.got = append(rc.got, body)
	w.WriteHeader(http.StatusNoContent)
}

// received 返回已收到的请求体副本。
func (rc *receiver) received() []map[string]any {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]map[string]any(nil), rc.got...)
}

// setReject 切换拒收。
func (rc *receiver) setReject(v bool) {
	rc.mu.Lock()
	rc.reject = v
	rc.mu.Unlock()
}

// alertEnv 告警测试装配:共用装配加告警服务,Webhook 地址与配额阈值可逐用例改。
type alertEnv struct {
	*testenv.Env
	svc     *alert.Service
	webhook string // 当前 Webhook 地址
	quota   int64  // 当前配额阈值(字节)
}

// newAlertEnv 装配不判定备份告警的告警服务。
func newAlertEnv(t *testing.T) *alertEnv {
	t.Helper()
	ae := &alertEnv{Env: testenv.New(t)}
	ae.svc = alert.New(alert.Config{
		Queries:    ae.Queries,
		QuotaBytes: func() int64 { return ae.quota },
		WebhookURL: func() string { return ae.webhook },
		Client:     &http.Client{Timeout: 5 * time.Second},
		Logger:     ae.Logger,
	})
	return ae
}

// evaluate 评估一轮并断言成功。
func (ae *alertEnv) evaluate(t *testing.T) {
	t.Helper()
	if err := ae.svc.EvaluateOnce(t.Context()); err != nil {
		t.Fatalf("EvaluateOnce: %v", err)
	}
}

// openAlerts 读出未恢复告警,按 kind/target 索引。
func (ae *alertEnv) openAlerts(t *testing.T) map[string]sqlc.Alert {
	t.Helper()
	rows, err := ae.Queries.ListOpenAlerts(t.Context())
	if err != nil {
		t.Fatalf("ListOpenAlerts: %v", err)
	}
	out := make(map[string]sqlc.Alert, len(rows))
	for _, a := range rows {
		out[a.Kind+"/"+a.Target] = a
	}
	return out
}

// setProbe 写回一条在役渠道的探测结果;reason 为空表示健康。
func (ae *alertEnv) setProbe(t *testing.T, id int64, reason channel.FailureReason, detail string) {
	t.Helper()
	params := sqlc.UpdateChannelProbeResultParams{Healthy: 1, ID: id}
	if reason != "" {
		params.Healthy = 0
		params.LastErrorStage = sql.NullString{String: string(channel.StageProbe), Valid: true}
		params.LastErrorReason = sql.NullString{String: string(reason), Valid: true}
		params.LastError = sql.NullString{String: detail, Valid: true}
	}
	if n, err := ae.Queries.UpdateChannelProbeResult(t.Context(), params); err != nil || n != 1 {
		t.Fatalf("写探测结果: 改动 %d 行, err=%v", n, err)
	}
}

// decodedParams 解出告警行的描述参数。
func decodedParams(t *testing.T, a sqlc.Alert) map[string]string {
	t.Helper()
	params, err := alert.DecodeParams(a.Params)
	if err != nil {
		t.Fatalf("解析告警参数: %v", err)
	}
	return params
}

// localID 内置 local 渠道的主键。
func (ae *alertEnv) localID(t *testing.T) int64 {
	t.Helper()
	return ae.Local(t).ID
}

// TestEvaluateOpensAndResolvesChannelUnhealthy 验证渠道探测不健康时开启告警、描述与参数取探测结论,
// 结论变化时更新同一条,恢复健康后同一告警被恢复而非再开一条。
func TestEvaluateOpensAndResolvesChannelUnhealthy(t *testing.T) {
	ae := newAlertEnv(t)
	id := ae.localID(t)

	ae.setProbe(t, id, channel.ReasonTimeout, "i/o timeout")
	ae.evaluate(t)
	got := ae.openAlerts(t)
	a, ok := got[alert.KindChannelUnhealthy+"/local"]
	if !ok || a.Message != "连接超时:检查端点地址与网络连通性(i/o timeout)" || len(got) != 1 {
		t.Fatalf("未恢复告警 = %+v, 期望仅 local 不健康且描述为探测结论", got)
	}
	if p := decodedParams(t, a); p["stage"] != "probe" || p["reason"] != "timeout" || p["detail"] != "i/o timeout" {
		t.Fatalf("告警参数 = %+v", p)
	}

	ae.setProbe(t, id, channel.ReasonAuth, "401 Unauthorized")
	ae.evaluate(t)
	again := ae.openAlerts(t)[alert.KindChannelUnhealthy+"/local"]
	if again.ID != a.ID || decodedParams(t, again)["reason"] != "auth" {
		t.Fatalf("结论变化应更新同一条告警, 得 %+v", again)
	}

	ae.setProbe(t, id, "", "")
	ae.evaluate(t)
	if left := ae.openAlerts(t); len(left) != 0 {
		t.Fatalf("恢复健康后仍有未恢复告警: %+v", left)
	}
	rows, err := ae.Queries.ListAlertsPage(t.Context(), sqlc.ListAlertsPageParams{Limit: 10})
	if err != nil || len(rows) != 1 || !rows[0].ResolvedAt.Valid {
		t.Fatalf("历史告警 = %+v (err=%v), 期望一条且已恢复", rows, err)
	}
}

// seedReplica 造一个 blob 并在渠道上记一行指定状态的副本。
func (ae *alertEnv) seedReplica(t *testing.T, channelID int64, state string, size int64) {
	t.Helper()
	blobID, err := ae.Queries.CreateBlob(t.Context(), sqlc.CreateBlobParams{
		Sha256: "h" + state, Phash: 1, StorageKey: "k_" + state, Mime: "image/png", Size: size, Width: 1, Height: 1,
	})
	if err != nil {
		t.Fatalf("建 blob: %v", err)
	}
	if err := ae.Queries.UpsertBlobChannel(t.Context(), sqlc.UpsertBlobChannelParams{
		BlobID: blobID, ChannelID: channelID, State: state,
	}); err != nil {
		t.Fatalf("写副本状态: %v", err)
	}
}

// TestEvaluateReplicationFailedAndQuota 验证补齐失败与超配额两类条件:failed 副本计数进描述,
// 用量超过阈值告警,阈值为 0 时不判定。
func TestEvaluateReplicationFailedAndQuota(t *testing.T) {
	ae := newAlertEnv(t)
	id := ae.localID(t)
	ae.seedReplica(t, id, channel.StateFailed, 1)
	ae.seedReplica(t, id, channel.StateDone, 1000)

	ae.evaluate(t)
	got := ae.openAlerts(t)
	if a, ok := got[alert.KindReplicationFailed+"/local"]; !ok || a.Message != "1 份副本补齐失败,渠道恢复健康后会重试" ||
		decodedParams(t, a)["failed"] != "1" {
		t.Fatalf("补齐失败告警 = %+v", got)
	}
	if _, ok := got[alert.KindChannelQuotaExceeded+"/local"]; ok {
		t.Fatal("阈值为 0 不应判定超配额")
	}

	ae.quota = 500
	ae.evaluate(t)
	a, ok := ae.openAlerts(t)[alert.KindChannelQuotaExceeded+"/local"]
	if p := decodedParams(t, a); !ok || p["bytes"] != "1000" || p["quota"] != "500" {
		t.Fatalf("用量 1000 超过阈值 500 应开启告警, 得 %+v", ae.openAlerts(t))
	}
}

// TestEvaluateDrainFailed 验证清理中渠道的排空失败结论成为告警,且不再被当作不健康告警;
// 探测结果不会写到清理中的渠道上。
func TestEvaluateDrainFailed(t *testing.T) {
	ae := newAlertEnv(t)
	id, _ := ae.AddLocalChannel(t, "victim", 50, false)
	if err := ae.Manager.Delete(t.Context(), id, true); err != nil {
		t.Fatalf("标记清理中: %v", err)
	}
	if n, err := ae.Queries.UpdateChannelProbeResult(t.Context(), sqlc.UpdateChannelProbeResultParams{
		Healthy: 1, ID: id,
	}); err != nil || n != 0 {
		t.Fatalf("探测结果不应写到清理中的渠道: 改动 %d 行, err=%v", n, err)
	}
	if n, err := ae.Queries.RecordChannelDrainFailure(t.Context(), sqlc.RecordChannelDrainFailureParams{
		LastErrorReason: sql.NullString{String: string(channel.ReasonForbidden), Valid: true},
		LastError:       sql.NullString{String: "403 Forbidden", Valid: true},
		ID:              id,
	}); err != nil || n != 1 {
		t.Fatalf("写排空失败: 改动 %d 行, err=%v", n, err)
	}
	ae.evaluate(t)
	got := ae.openAlerts(t)
	a, ok := got[alert.KindDrainFailed+"/victim"]
	if !ok || len(got) != 1 || decodedParams(t, a)["stage"] != "drain" {
		t.Fatalf("告警 = %+v, 期望仅一条排空失败", got)
	}
}

// TestEvaluateBackupFailed 验证最近一次备份失败时开启告警,备份成功后恢复:
// 让 local 不健康使备份无目标渠道而失败,再恢复健康。
func TestEvaluateBackupFailed(t *testing.T) {
	env := testenv.New(t)
	backup := reliability.New(reliability.Config{
		Conn: env.Conn, Channels: env.Manager, DataDir: env.DataDir,
		Keep: func() int { return 2 }, Interval: func() time.Duration { return time.Hour }, Logger: env.Logger,
	})
	ae := &alertEnv{Env: env}
	ae.svc = alert.New(alert.Config{
		Queries: env.Queries, QuotaBytes: func() int64 { return 0 }, Backup: backup,
		WebhookURL: func() string { return "" }, Client: &http.Client{Timeout: 5 * time.Second}, Logger: env.Logger,
	})
	id := ae.localID(t)
	ae.setProbe(t, id, channel.ReasonUnclassified, "探测失败")
	if err := env.Manager.Reload(t.Context()); err != nil {
		t.Fatalf("重载渠道: %v", err)
	}
	if _, err := backup.RunOnce(t.Context()); err == nil {
		t.Fatal("无健康渠道时备份应失败")
	}
	ae.evaluate(t)
	a, ok := ae.openAlerts(t)[alert.KindBackupFailed+"/"]
	if !ok || decodedParams(t, a)["code"] != "no_healthy_channel" {
		t.Fatalf("备份失败应开启告警且带错误码, 得 %+v", ae.openAlerts(t))
	}

	ae.setProbe(t, id, "", "")
	if err := env.Manager.Reload(t.Context()); err != nil {
		t.Fatalf("重载渠道: %v", err)
	}
	if _, err := backup.RunOnce(t.Context()); err != nil {
		t.Fatalf("恢复后备份应成功: %v", err)
	}
	ae.evaluate(t)
	if _, ok := ae.openAlerts(t)[alert.KindBackupFailed+"/"]; ok {
		t.Fatal("备份成功后告警应恢复")
	}
}

// TestDeliverWebhook 验证 Webhook 投递:开启与恢复各投一次、接收方拒收时留到下一轮重投、
// 地址为空时不投递但记为已投递。
func TestDeliverWebhook(t *testing.T) {
	rc := &receiver{}
	srv := httptest.NewServer(rc)
	t.Cleanup(srv.Close)
	ae := newAlertEnv(t)
	ae.webhook = srv.URL
	id := ae.localID(t)

	rc.setReject(true)
	ae.setProbe(t, id, channel.ReasonTimeout, "i/o timeout")
	ae.evaluate(t)
	if n := len(rc.received()); n != 0 {
		t.Fatalf("拒收时不应记为已投递, 收到 %d 条", n)
	}
	rc.setReject(false)
	ae.evaluate(t)
	got := rc.received()
	if len(got) != 1 {
		t.Fatalf("重投后应收到一条开启, 得 %+v", got)
	}
	params, isMap := got[0]["params"].(map[string]any)
	if got[0]["status"] != "open" || got[0]["kind"] != alert.KindChannelUnhealthy ||
		got[0]["target"] != "local" || !isMap || params["reason"] != "timeout" {
		t.Fatalf("重投后应收到一条开启, 得 %+v", got)
	}

	ae.setProbe(t, id, "", "")
	ae.evaluate(t)
	got = rc.received()
	if len(got) != 2 || got[1]["status"] != "resolved" || got[1]["resolved_at"] == "" {
		t.Fatalf("恢复后应收到一条恢复, 得 %+v", got)
	}
	ae.evaluate(t)
	if len(rc.received()) != 2 {
		t.Fatal("已投递的转折不应重复投递")
	}

	ae.webhook = ""
	ae.setProbe(t, id, channel.ReasonUnclassified, "再次失败")
	ae.evaluate(t)
	rows, err := ae.Queries.ListUnnotifiedAlerts(t.Context())
	if err != nil || len(rows) != 0 {
		t.Fatalf("地址为空时应直接记为已投递, 待投递 = %+v (err=%v)", rows, err)
	}
	if len(rc.received()) != 2 {
		t.Fatal("地址为空时不应外发")
	}
}

// TestDecodeParams 验证告警参数解码:JSON 对象还原为映射,其余取值(含 null)一律报错。
func TestDecodeParams(t *testing.T) {
	for _, raw := range []string{`{}`, `{"channel":"r2","reason":"auth"}`} {
		if params, err := alert.DecodeParams(raw); err != nil || params == nil {
			t.Errorf("DecodeParams(%s) = %v, %v; 期望非 nil 映射", raw, params, err)
		}
	}
	for _, raw := range []string{`null`, `[]`, `"x"`, `{"n":1}`, ``} {
		if _, err := alert.DecodeParams(raw); err == nil {
			t.Errorf("DecodeParams(%q) 应报错", raw)
		}
	}
}
