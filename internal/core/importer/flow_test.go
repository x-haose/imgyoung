package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/x-haose/imgyoung/internal/core/testenv"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
)

// stubFetcher 抓取器替身:真实抓取器按设计拒绝一切内网地址,而测试只能监听环回。按地址回放预置内容,
// 没有预置的地址按远端 404 处理;block 里的地址一直阻塞到 ctx 取消。
type stubFetcher struct {
	mu      sync.Mutex
	replies map[string][]byte // 地址 → 内容
	errs    map[string]error  // 地址 → 抓取错误,优先于 replies
	block   map[string]bool   // 命中即阻塞到 ctx 取消
	final   map[string]string // 地址 → 重定向后的最终地址;没有则等于请求地址
	calls   []stubCall        // 按到达顺序记录的请求
	started chan string       // 阻塞的抓取开始时通知其地址
}

// stubCall 一次抓取请求。
type stubCall struct {
	url, referer, agent string
}

// newStub 构造空的替身。
func newStub() *stubFetcher {
	return &stubFetcher{
		replies: map[string][]byte{}, errs: map[string]error{}, block: map[string]bool{}, final: map[string]string{},
		started: make(chan string, 16),
	}
}

// FetchWith 实现 Fetcher。
func (f *stubFetcher) FetchWith(ctx context.Context, rawURL string, opts urlfetch.Options) (urlfetch.Fetched, error) {
	f.mu.Lock()
	f.calls = append(f.calls, stubCall{url: rawURL, referer: opts.Referer, agent: opts.UserAgent})
	data, hasData := f.replies[rawURL]
	err := f.errs[rawURL]
	blocked := f.block[rawURL]
	finalURL := f.final[rawURL]
	f.mu.Unlock()
	if finalURL == "" {
		finalURL = rawURL
	}
	if blocked {
		f.started <- rawURL
		<-ctx.Done()
		return urlfetch.Fetched{}, ctx.Err()
	}
	switch {
	case err != nil:
		return urlfetch.Fetched{}, err
	case !hasData:
		return urlfetch.Fetched{}, fmt.Errorf("%w: HTTP 404", urlfetch.ErrStatus)
	}
	return urlfetch.Fetched{Data: data, FinalURL: finalURL}, nil
}

// reply 预置一个地址的内容,并清除它的错误。
func (f *stubFetcher) reply(rawURL string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[rawURL] = data
	delete(f.errs, rawURL)
}

// callsTo 返回对某个地址的全部请求。
func (f *stubFetcher) callsTo(rawURL string) []stubCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []stubCall
	for _, c := range f.calls {
		if c.url == rawURL {
			out = append(out, c)
		}
	}
	return out
}

// harness 一套测试装配:真实 SQLite、真实上传管线,抓取器是替身。
type harness struct {
	t      *testing.T
	env    *testenv.Env
	svc    *Service
	up     *upload.Service
	images *stubFetcher
	pages  *stubFetcher
}

// newHarness 组装测试环境。
func newHarness(t *testing.T) *harness {
	t.Helper()
	env := testenv.New(t)
	settings := testenv.Settings(func(s *config.Settings) { s.EXIFPolicy = imaging.PolicyPassthrough })
	up := upload.New(upload.Config{
		Conn: env.Conn, Queries: env.Queries, Channels: env.Manager, Settings: settings, Logger: env.Logger,
	})
	h := &harness{t: t, env: env, up: up, images: newStub(), pages: newStub()}
	h.svc = New(Config{
		Conn: env.Conn, Queries: env.Queries, Upload: up, Images: h.images, Pages: h.pages,
		Settings: settings, UserAgent: "test-agent", Logger: env.Logger,
	})
	return h
}

// pngBytes 生成一张 2x2 的真实 PNG。
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}

// job 读取任务行。
func (h *harness) job(id string) sqlc.ImportJob {
	h.t.Helper()
	row, err := h.env.Queries.GetImportJob(h.t.Context(), id)
	if err != nil {
		h.t.Fatalf("读取任务 %s: %v", id, err)
	}
	return row
}

// counts 读取任务各状态的条目数。
func (h *harness) counts(id string) map[string]int64 {
	h.t.Helper()
	rows, err := h.env.Queries.CountImportItemsByState(h.t.Context(), id)
	if err != nil {
		h.t.Fatalf("统计条目: %v", err)
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.State] = r.ItemCount
	}
	return out
}

// runImport 同步处理一个 importing 的任务直到离开该状态。
func (h *harness) runImport(id string) {
	h.t.Helper()
	job := h.job(id)
	if job.Status != StatusImporting {
		h.t.Fatalf("任务状态 = %q, 期望 importing", job.Status)
	}
	if err := h.svc.importItems(h.t.Context(), job); err != nil {
		h.t.Fatalf("导入失败: %v", err)
	}
}

// runScan 同步扫描一个 scanning 的任务。
func (h *harness) runScan(id string) {
	h.t.Helper()
	if err := h.svc.scan(h.t.Context(), h.job(id)); err != nil {
		h.t.Fatalf("扫描失败: %v", err)
	}
}

// imageCount 图片总数。
func (h *harness) imageCount() int64 {
	h.t.Helper()
	var n int64
	if err := h.env.Conn.QueryRowContext(h.t.Context(), `SELECT COUNT(*) FROM images`).Scan(&n); err != nil {
		h.t.Fatalf("统计图片: %v", err)
	}
	return n
}

// wantStatus 断言任务状态。
func (h *harness) wantStatus(id, want string) {
	h.t.Helper()
	if got := h.job(id).Status; got != want {
		h.t.Fatalf("任务状态 = %q, 期望 %q", got, want)
	}
}

// exec 直接执行一条写库语句。
func (h *harness) exec(query string, args ...any) {
	h.t.Helper()
	if _, err := h.env.Conn.ExecContext(h.t.Context(), query, args...); err != nil {
		h.t.Fatalf("执行 %q: %v", query, err)
	}
}

// me 默认用户作为操作者。
func me() Actor { return Actor{UserID: sqlc.DefaultUserID} }

// opts 默认用户的创建参数。
func opts() Options { return Options{UserID: sqlc.DefaultUserID} }

// TestImagesFlow 图片地址清单:规范化后重复的只导一次,图片走上传管线入库,任务完成,对照表可查。
func TestImagesFlow(t *testing.T) {
	h := newHarness(t)
	h.images.reply("https://img.example.com/a.png", pngBytes(t))
	h.images.reply("https://img.example.com/b.png", pngBytes(t))

	id, err := h.svc.CreateImages(t.Context(), opts(), []string{
		"https://img.example.com/a.png", "https://IMG.example.com/a.png#frag", "https://img.example.com/b.png",
	})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.wantStatus(id, StatusImporting)
	if c := h.counts(id); c[StatePending] != 2 {
		t.Fatalf("待转存 = %d, 期望 2(重复地址只算一个)", c[StatePending])
	}

	h.runImport(id)

	h.wantStatus(id, StatusDone)
	if c := h.counts(id); c[StateDone] != 2 || h.imageCount() != 2 {
		t.Fatalf("已导入 = %d, 图片数 = %d, 期望都是 2", c[StateDone], h.imageCount())
	}
	if !h.job(id).FinishedAt.Valid {
		t.Error("完成的任务应有结束时刻")
	}
	rows, err := h.env.Queries.ListImportMappingItems(t.Context(), id)
	if err != nil || len(rows) != 2 || !rows[0].ImageID.Valid {
		t.Fatalf("对照表 = %v, err=%v", rows, err)
	}
	for _, c := range h.images.callsTo("https://img.example.com/a.png") {
		if c.referer != "" || c.agent != "test-agent" {
			t.Errorf("图片清单不带 Referer、带 User-Agent,得 %+v", c)
		}
	}
	if err := h.svc.Retry(t.Context(), me(), id); !errors.Is(err, ErrNothingToRetry) {
		t.Fatalf("没有失败条目时重试 = %v, 期望 ErrNothingToRetry", err)
	}
}

// TestTextFlowConfirmsHosts 文本扫描后任务等待确认;只导入所选域名,其余记为跳过;状态不对与所选域名下没有条目都被拒绝。
func TestTextFlowConfirmsHosts(t *testing.T) {
	h := newHarness(t)
	h.images.reply("https://img.old.com/a.png", pngBytes(t))
	docs := []Document{{Name: "post.md", Text: "![a](https://img.old.com/a.png)\n\n" +
		"![b](https://cdn.other.com/b.png)\n\n![c](/rel/c.png)"}}

	id, err := h.svc.CreateText(t.Context(), opts(), docs, "https://blog.example.com/")
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.wantStatus(id, StatusReady)
	if c := h.counts(id); c[StateCandidate] != 3 {
		t.Fatalf("候选 = %d, 期望 3", c[StateCandidate])
	}
	report, err := DecodeReport(h.job(id).ScanReport)
	if err != nil || report == nil || report.Pages != 1 || len(report.Unfetchable) != 0 {
		t.Fatalf("扫描报告 = %+v, err=%v", report, err)
	}

	err = h.svc.Start(t.Context(), me(), id, []string{"no.such.host"})
	if !errors.Is(err, ErrNoSelection) {
		t.Fatalf("所选域名下没有条目 = %v, 期望 ErrNoSelection", err)
	}
	h.wantStatus(id, StatusReady)
	if err := h.svc.Start(t.Context(), me(), id, []string{"IMG.old.com"}); err != nil {
		t.Fatalf("确认: %v", err)
	}
	c := h.counts(id)
	if c[StatePending] != 1 || c[StateSkipped] != 2 || c[StateCandidate] != 0 {
		t.Fatalf("确认后各状态 = %v", c)
	}
	var stateErr *StateError
	if err := h.svc.Start(t.Context(), me(), id, []string{"img.old.com"}); !errors.As(err, &stateErr) {
		t.Fatalf("重复确认 = %v, 期望 StateError", err)
	}

	h.runImport(id)
	h.wantStatus(id, StatusDone)
	if got := h.imageCount(); got != 1 {
		t.Fatalf("图片数 = %d, 期望 1", got)
	}
	if calls := h.images.callsTo("https://img.old.com/a.png"); len(calls) != 1 || calls[0].referer != "" {
		t.Fatalf("来源是文档名,不是网页,不应发 Referer: %+v", calls)
	}
}

// TestFailuresAndRetry 单个条目失败不影响其余:失败原因落在条目上,任务仍完成;重试只重跑失败的。
func TestFailuresAndRetry(t *testing.T) {
	h := newHarness(t)
	const (
		okURL   = "https://img.example.com/ok.png"
		missURL = "https://img.example.com/miss.png"
		htmlURL = "https://img.example.com/page.png"
	)
	h.images.reply(okURL, pngBytes(t))
	h.images.reply(htmlURL, []byte("<html>not an image</html>"))

	id, err := h.svc.CreateImages(t.Context(), opts(), []string{okURL, missURL, htmlURL})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.runImport(id)

	h.wantStatus(id, StatusDone)
	if c := h.counts(id); c[StateDone] != 1 || c[StateFailed] != 2 {
		t.Fatalf("各状态 = %v, 期望 done=1 failed=2", c)
	}
	codes := map[string]string{}
	rows, err := h.env.Queries.ListImportItems(t.Context(), sqlc.ListImportItemsParams{
		JobID: id, State: StateFailed, PageLimit: 10,
	})
	if err != nil {
		t.Fatalf("读取失败条目: %v", err)
	}
	for _, r := range rows {
		codes[r.Url] = r.ErrorCode
	}
	if codes[missURL] != "url_bad_status" || codes[htmlURL] != "upload_unsupported_type" {
		t.Fatalf("失败原因 = %v", codes)
	}

	h.images.reply(missURL, pngBytes(t))
	if err := h.svc.Retry(t.Context(), me(), id); err != nil {
		t.Fatalf("重试: %v", err)
	}
	if job := h.job(id); job.Status != StatusImporting || job.FinishedAt.Valid {
		t.Fatalf("重试后任务 = %+v", job)
	}
	h.runImport(id)
	if c := h.counts(id); c[StateDone] != 2 || c[StateFailed] != 1 {
		t.Fatalf("重试后各状态 = %v, 期望 done=2 failed=1", c)
	}
	if calls := h.images.callsTo(okURL); len(calls) != 1 {
		t.Fatalf("已导入的条目不应被重抓: %d 次", len(calls))
	}
}

// TestQuotaAbortAndResume 存储配额已满时,连续多张图因配额失败就中止整个任务而不是逐条失败下去;解决后重试继续。
func TestQuotaAbortAndResume(t *testing.T) {
	h := newHarness(t)
	var urls []string
	for _, name := range []string{"a", "b", "c", "d"} {
		u := "https://img.example.com/" + name + ".png"
		h.images.reply(u, pngBytes(t))
		urls = append(urls, u)
	}
	h.exec(`UPDATE users SET quota_bytes = 1 WHERE id = ?`, sqlc.DefaultUserID)
	id, err := h.svc.CreateImages(t.Context(), opts(), urls)
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}

	h.runImport(id)

	job := h.job(id)
	if job.Status != StatusAborted || job.ErrorCode != "upload_quota_exceeded" || !job.FinishedAt.Valid {
		t.Fatalf("任务 = %+v, 期望 aborted / upload_quota_exceeded", job)
	}
	if c := h.counts(id); c[StateDone] != 0 || c[StateFailed]+c[StatePending] != 4 {
		t.Fatalf("各状态 = %v, 期望没有导入成功的", c)
	}

	h.exec(`UPDATE users SET quota_bytes = NULL WHERE id = ?`, sqlc.DefaultUserID)
	if err := h.svc.Retry(t.Context(), me(), id); err != nil {
		t.Fatalf("重试: %v", err)
	}
	if job := h.job(id); job.ErrorCode != "" || job.Status != StatusImporting {
		t.Fatalf("重试后任务 = %+v", job)
	}
	h.runImport(id)
	h.wantStatus(id, StatusDone)
	if c := h.counts(id); c[StateDone] != 4 {
		t.Fatalf("各状态 = %v, 期望 done=4", c)
	}
}

// noisePNG 生成一张不可压缩的 PNG,体积远大于 2x2 的小图。
func noisePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = byte(i*7919 + i/13)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码噪声 PNG: %v", err)
	}
	return buf.Bytes()
}

// TestOversizedImageDoesNotBlockJob 一张图超过剩余额度只算这张失败,放得下的图照常导入,任务不中止。
func TestOversizedImageDoesNotBlockJob(t *testing.T) {
	h := newHarness(t)
	const bigURL, smallURL = "https://img.example.com/big.png", "https://img.example.com/small.png"
	h.images.reply(bigURL, noisePNG(t))
	h.images.reply(smallURL, pngBytes(t))
	h.exec(`UPDATE users SET quota_bytes = 1000 WHERE id = ?`, sqlc.DefaultUserID)
	id, err := h.svc.CreateImages(t.Context(), opts(), []string{bigURL, smallURL})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}

	h.runImport(id)

	h.wantStatus(id, StatusDone)
	if c := h.counts(id); c[StateDone] != 1 || c[StateFailed] != 1 {
		t.Fatalf("各状态 = %v, 期望 done=1 failed=1", c)
	}
	rows, err := h.env.Queries.ListImportItems(t.Context(), sqlc.ListImportItemsParams{
		JobID: id, State: StateFailed, PageLimit: 10,
	})
	if err != nil || len(rows) != 1 || rows[0].Url != bigURL || rows[0].ErrorCode != "upload_quota_exceeded" {
		t.Fatalf("失败条目 = %+v, err=%v, 期望是超额的大图", rows, err)
	}
}

// TestResumeDoesNotDuplicate 图片已入库而条目还没来得及记结果(进程在中间退出)时,重新处理沿用同一张图,不产生第二张;
// 此时旧地址已经失效也不影响,因为不再抓取。
func TestResumeDoesNotDuplicate(t *testing.T) {
	h := newHarness(t)
	const target = "https://img.example.com/a.png"
	id, err := h.svc.CreateImages(t.Context(), opts(), []string{target})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	items, err := h.env.Queries.ListPendingImportItems(t.Context(),
		sqlc.ListPendingImportItemsParams{JobID: id, PageLimit: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("待转存条目 = %v, err=%v", items, err)
	}
	pre, err := h.up.Process(t.Context(), pngBytes(t), upload.Options{
		UserID: sqlc.DefaultUserID, FileName: "a.png", EXIFPolicy: imaging.PolicyPassthrough,
		IdempotencyKey: importKey(id, items[0].ID),
	})
	if err != nil {
		t.Fatalf("预先入库: %v", err)
	}

	h.runImport(id)

	rows, err := h.env.Queries.ListImportMappingItems(t.Context(), id)
	if err != nil || len(rows) != 1 || rows[0].ImageID.String != pre.ImageID {
		t.Fatalf("对照表 = %v, 期望指向已入库的 %s, err=%v", rows, pre.ImageID, err)
	}
	if got := h.imageCount(); got != 1 {
		t.Fatalf("图片数 = %d, 期望 1", got)
	}
	if calls := h.images.callsTo(target); len(calls) != 0 {
		t.Fatalf("已入库的条目不应再抓取远端: %d 次", len(calls))
	}
}

// TestItemIDReuseDoesNotCrossJobs 删除任务后条目 ID 会被新任务的条目复用,新条目不能认领到旧任务留下的图。
func TestItemIDReuseDoesNotCrossJobs(t *testing.T) {
	h := newHarness(t)
	h.images.reply("https://img.example.com/a.png", pngBytes(t))
	h.images.reply("https://img.example.com/b.png", pngBytes(t))

	first, err := h.svc.CreateImages(t.Context(), opts(), []string{"https://img.example.com/a.png"})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.runImport(first)
	if err := h.svc.Delete(t.Context(), me(), first); err != nil {
		t.Fatalf("删除任务: %v", err)
	}

	second, err := h.svc.CreateImages(t.Context(), opts(), []string{"https://img.example.com/b.png"})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.runImport(second)

	if got := h.imageCount(); got != 2 {
		t.Fatalf("图片数 = %d, 期望 2:第二个任务的条目要有自己的图", got)
	}
	if calls := h.images.callsTo("https://img.example.com/b.png"); len(calls) != 1 {
		t.Fatalf("第二个任务的地址应被抓取一次,得 %d 次", len(calls))
	}
}

// TestCancelStopsImport 取消正在转存的任务:调度中的处理被中止,任务是 cancelled 而不是 done,未处理的条目保持 pending。
func TestCancelStopsImport(t *testing.T) {
	h := newHarness(t)
	const slow = "https://img.example.com/slow.png"
	h.images.block[slow] = true
	id, err := h.svc.CreateImages(t.Context(), opts(), []string{slow})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		h.svc.process(t.Context(), h.job(id))
	}()
	<-h.images.started

	if err := h.svc.Cancel(t.Context(), me(), id); err != nil {
		t.Fatalf("取消: %v", err)
	}
	<-finished

	h.wantStatus(id, StatusCancelled)
	if c := h.counts(id); c[StatePending] != 1 || c[StateFailed] != 0 {
		t.Fatalf("各状态 = %v, 期望条目仍是 pending", c)
	}
	var stateErr *StateError
	err = h.svc.Cancel(t.Context(), me(), id)
	if !errors.As(err, &stateErr) || stateErr.Status != StatusCancelled {
		t.Fatalf("重复取消 = %v, 期望 StateError(cancelled)", err)
	}
}

// TestOwnership 别人的任务对普通用户一律按不存在处理,四个操作都不例外。
func TestOwnership(t *testing.T) {
	h := newHarness(t)
	h.exec(`INSERT INTO users (id, name, role) VALUES ('u2', 'other', 'user')`)
	id, err := h.svc.CreateImages(t.Context(), opts(), []string{"https://img.example.com/a.png"})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	other := Actor{UserID: "u2"}
	checks := map[string]error{
		"Start":  h.svc.Start(t.Context(), other, id, []string{"img.example.com"}),
		"Cancel": h.svc.Cancel(t.Context(), other, id),
		"Retry":  h.svc.Retry(t.Context(), other, id),
		"Delete": h.svc.Delete(t.Context(), other, id),
	}
	for name, err := range checks {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s = %v, 期望 ErrNotFound", name, err)
		}
	}
	h.wantStatus(id, StatusImporting)
}

// TestAdminAccess 管理员可以取消、删除任何人的任务;确认与重试只有创建者能做,管理员得到 ErrNotOwner。
func TestAdminAccess(t *testing.T) {
	h := newHarness(t)
	h.exec(`INSERT INTO users (id, name, role) VALUES ('u2', 'other', 'user')`)
	admin := Actor{UserID: sqlc.DefaultUserID, Admin: true}
	docs := []Document{{Name: "a.md", Text: "![a](https://img.old.com/a.png)"}}
	ready, err := h.svc.CreateText(t.Context(), Options{UserID: "u2"}, docs, "")
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}

	if err := h.svc.Start(t.Context(), admin, ready, []string{"img.old.com"}); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("管理员确认别人的任务 = %v, 期望 ErrNotOwner", err)
	}
	if err := h.svc.Retry(t.Context(), admin, ready); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("管理员重试别人的任务 = %v, 期望 ErrNotOwner", err)
	}
	h.wantStatus(ready, StatusReady)
	if err := h.svc.Cancel(t.Context(), admin, ready); err != nil {
		t.Fatalf("管理员取消别人的任务: %v", err)
	}
	if err := h.svc.Delete(t.Context(), admin, ready); err != nil {
		t.Fatalf("管理员删除别人的任务: %v", err)
	}
}

// TestDeleteRules 进行中的任务不能删;删除任务连同条目一起消失;图片被删后条目里的图片 ID 置空,对照表不再列出它。
func TestDeleteRules(t *testing.T) {
	h := newHarness(t)
	h.images.reply("https://img.example.com/a.png", pngBytes(t))
	id, err := h.svc.CreateImages(t.Context(), opts(), []string{"https://img.example.com/a.png"})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	var stateErr *StateError
	if err := h.svc.Delete(t.Context(), me(), id); !errors.As(err, &stateErr) {
		t.Fatalf("删除进行中的任务 = %v, 期望 StateError", err)
	}
	h.runImport(id)

	h.exec(`DELETE FROM idempotency_keys`)
	h.exec(`DELETE FROM images`)
	rows, err := h.env.Queries.ListImportMappingItems(t.Context(), id)
	if err != nil || len(rows) != 0 {
		t.Fatalf("图片被删后对照表 = %v, err=%v, 期望为空", rows, err)
	}

	if err := h.svc.Delete(t.Context(), me(), id); err != nil {
		t.Fatalf("删除已完成的任务: %v", err)
	}
	var n int64
	row := h.env.Conn.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM import_items`)
	if err := row.Scan(&n); err != nil || n != 0 {
		t.Fatalf("条目数 = %d, err=%v, 期望随任务删除", n, err)
	}
}

// TestNextRunnableIsFIFO 调度按创建先后取任务。
func TestNextRunnableIsFIFO(t *testing.T) {
	h := newHarness(t)
	first, err := h.svc.CreateSite(t.Context(), opts(), []string{"https://a.example.com/sitemap.xml"})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	if _, err := h.svc.CreateSite(t.Context(), opts(), []string{"https://b.example.com/sitemap.xml"}); err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	next, err := h.env.Queries.NextRunnableImportJob(t.Context())
	if err != nil || next.ID != first {
		t.Fatalf("下一个任务 = %s, err=%v, 期望先创建的 %s", next.ID, err, first)
	}
}

// siteSitemapIndex 站点地图索引:一个同域名的子站点地图,一个别的域名的。
const siteSitemapIndex = `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
<sitemap><loc>https://blog.example.com/sitemap-posts.xml</loc></sitemap>
<sitemap><loc>https://other.example.com/sitemap.xml</loc></sitemap></sitemapindex>`

// siteSitemapPosts 文章站点地图:两篇同域名文章、一篇别的域名、一个写不成地址的条目,并随第一篇列出一张图。
const siteSitemapPosts = `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
<url><loc>https://blog.example.com/p1</loc>
<image:image><image:loc>https://cdn.old.com/s.png</image:loc></image:image></url>
<url><loc>https://blog.example.com/p2</loc></url>
<url><loc>https://other.example.com/x</loc></url>
<url><loc>relative/path</loc></url></urlset>`

// TestSiteScanAndImport 站点扫描:展开索引与站点地图,同域名限定,抓取失败与无法抓取的引用进报告,图片按域名分组等待确认;
// 重新扫描结果不变;导入时用来源页作 Referer。
func TestSiteScanAndImport(t *testing.T) {
	h := newHarness(t)
	h.pages.reply("https://blog.example.com/sitemap_index.xml", []byte(siteSitemapIndex))
	h.pages.reply("https://blog.example.com/sitemap-posts.xml", []byte(siteSitemapPosts))
	h.pages.reply("https://blog.example.com/p1",
		[]byte(`<html><body><img src="https://cdn.old.com/a.png"><img src="/local/b.png"></body></html>`))
	h.images.reply("https://cdn.old.com/a.png", pngBytes(t))
	h.images.reply("https://cdn.old.com/s.png", pngBytes(t))

	id, err := h.svc.CreateSite(t.Context(), opts(), []string{"https://blog.example.com/sitemap_index.xml"})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.wantStatus(id, StatusScanning)
	h.runScan(id)
	h.wantStatus(id, StatusReady)
	first := h.job(id)

	// 模拟扫描中途重启:任务仍是 scanning,重新扫描先清掉上次的候选与进度,结果应一致。
	h.exec(`UPDATE import_jobs SET status = 'scanning', scan_report = NULL WHERE id = ?`, id)
	h.runScan(id)
	h.wantStatus(id, StatusReady)
	if c := h.counts(id); c[StateCandidate] != 3 {
		t.Fatalf("候选 = %d, 期望 3(cdn.old.com 两张、blog.example.com 一张)", c[StateCandidate])
	}
	again := h.job(id)
	if again.ScanReport != first.ScanReport || again.ScanPagesFound != 2 || again.ScanPagesDone != 2 {
		t.Fatalf("重新扫描后 found=%d done=%d 报告=%s, 期望与第一次一致",
			again.ScanPagesFound, again.ScanPagesDone, again.ScanReport.String)
	}

	report, err := DecodeReport(h.job(id).ScanReport)
	if err != nil || report == nil {
		t.Fatalf("扫描报告: %+v, err=%v", report, err)
	}
	if report.Pages != 2 || report.OffsitePages != 2 || report.PagesWithoutImages != 0 {
		t.Errorf("报告计数 = %+v", report)
	}
	if len(report.FailedPages) != 1 || report.FailedPages[0].Source != "https://blog.example.com/p2" ||
		report.FailedPages[0].Code != "url_bad_status" {
		t.Errorf("失败页面 = %+v", report.FailedPages)
	}
	if len(report.Unfetchable) != 1 || report.Unfetchable[0].Reference != "relative/path" {
		t.Errorf("无法抓取的引用 = %+v", report.Unfetchable)
	}

	hosts, err := h.env.Queries.ListImportHosts(t.Context(), id)
	if err != nil || len(hosts) != 2 || hosts[0].Host != "cdn.old.com" || hosts[0].ItemCount != 2 {
		t.Fatalf("域名分组 = %+v, err=%v", hosts, err)
	}

	if err := h.svc.Start(t.Context(), me(), id, []string{"cdn.old.com"}); err != nil {
		t.Fatalf("确认: %v", err)
	}
	h.runImport(id)
	h.wantStatus(id, StatusDone)
	for _, u := range []string{"https://cdn.old.com/a.png", "https://cdn.old.com/s.png"} {
		calls := h.images.callsTo(u)
		if len(calls) != 1 || calls[0].referer != "https://blog.example.com/p1" {
			t.Errorf("%s 的请求 = %+v, 期望带来源页 Referer", u, calls)
		}
	}
	refs, err := h.env.Queries.ListImportMappingReferences(t.Context(), id)
	if err != nil || len(refs) != 2 {
		t.Fatalf("引用位置 = %+v, err=%v, 期望 2 条", refs, err)
	}
}

// TestSiteSeedKinds 种子地址的类型按内容判定:图片直接收下、不记来源;网页直接扫描;内容无法解析记入报告。
func TestSiteSeedKinds(t *testing.T) {
	h := newHarness(t)
	h.pages.reply("https://cdn.old.com/direct.png", pngBytes(t))
	h.pages.reply("https://blog.example.com/page", []byte(`<img src="/a.png">`))
	h.pages.reply("https://blog.example.com/broken.xml", []byte(`<urlset><url><loc>https://blog.exa`))

	id, err := h.svc.CreateSite(t.Context(), opts(), []string{
		"https://cdn.old.com/direct.png", "https://blog.example.com/page", "https://blog.example.com/broken.xml",
	})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.runScan(id)

	h.wantStatus(id, StatusReady)
	if c := h.counts(id); c[StateCandidate] != 2 {
		t.Fatalf("候选 = %v, 期望直接图片与网页里的图片共 2 个", c)
	}
	report, err := DecodeReport(h.job(id).ScanReport)
	if err != nil || report == nil || len(report.FailedPages) != 1 ||
		report.FailedPages[0].Code != "import_listing_invalid" {
		t.Fatalf("扫描报告 = %+v, err=%v, 期望截断的站点地图记为 import_listing_invalid", report, err)
	}
	rows, err := h.env.Queries.ListImportItems(t.Context(), sqlc.ListImportItemsParams{JobID: id, PageLimit: 10})
	if err != nil {
		t.Fatalf("读取条目: %v", err)
	}
	for _, r := range rows {
		if r.Url == "https://cdn.old.com/direct.png" && r.SourceCount != 0 {
			t.Errorf("直接给出的图片不记来源,得 %d 个", r.SourceCount)
		}
	}
}

// TestSiteScanFollowsRedirects 站点地图与页面经重定向后,所列页面的域名与页面里的相对路径都按最终地址判断。
func TestSiteScanFollowsRedirects(t *testing.T) {
	h := newHarness(t)
	const (
		typed = "http://blog.example.com/sitemap.xml"
		final = "https://WWW.Blog.example.com:443/sitemap.xml" // 重定向目标带大小写与默认端口
		page  = "https://www.blog.example.com/blog/post"
	)
	h.pages.final[typed] = final
	h.pages.reply(typed, []byte(`<urlset><url><loc>`+page+`</loc></url></urlset>`))
	h.pages.final[page] = page + "/"
	h.pages.reply(page, []byte(`<img src="a.png">`))

	id, err := h.svc.CreateSite(t.Context(), opts(), []string{typed})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.runScan(id)

	report, err := DecodeReport(h.job(id).ScanReport)
	if err != nil || report == nil || report.OffsitePages != 0 || report.Pages != 1 {
		t.Fatalf("扫描报告 = %+v, err=%v, 期望所列页面不被当作站外", report, err)
	}
	rows, err := h.env.Queries.ListImportItems(t.Context(), sqlc.ListImportItemsParams{JobID: id, PageLimit: 10})
	if err != nil || len(rows) != 1 || rows[0].Url != "https://www.blog.example.com/blog/post/a.png" {
		t.Fatalf("候选 = %+v, err=%v, 期望相对路径按重定向后的地址解析", rows, err)
	}
}

// TestSiteRetryRestartsInterruptedScan 扫描没完成就中止或取消的站点任务,重试从头重新扫描,而不是报没有可重试的条目。
func TestSiteRetryRestartsInterruptedScan(t *testing.T) {
	h := newHarness(t)
	id, err := h.svc.CreateSite(t.Context(), opts(), []string{"https://blog.example.com/page"})
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.exec(`UPDATE import_jobs SET status = 'aborted', error_code = 'internal' WHERE id = ?`, id)

	if err := h.svc.Retry(t.Context(), me(), id); err != nil {
		t.Fatalf("重试: %v", err)
	}
	if job := h.job(id); job.Status != StatusScanning || job.ErrorCode != "" || job.FinishedAt.Valid {
		t.Fatalf("重试后任务 = %+v, 期望回到 scanning", job)
	}

	// 扫描完成过的站点任务(有报告)中止在导入阶段,重试不回到扫描。
	h.exec(`UPDATE import_jobs SET status = 'aborted', scan_report = '{}' WHERE id = ?`, id)
	if err := h.svc.Retry(t.Context(), me(), id); !errors.Is(err, ErrNothingToRetry) {
		t.Fatalf("没有失败条目时重试 = %v, 期望 ErrNothingToRetry", err)
	}
}

// TestTextDocumentLimits 文本文档过大按输入不合法拒绝;解析超时同样拒绝,请求不会被病态输入无限拖住。
func TestTextDocumentLimits(t *testing.T) {
	h := newHarness(t)
	var inputErr *InputError
	huge := Document{Name: "huge.md", Text: strings.Repeat("a", MaxTextDocumentBytes+1)}
	if _, err := h.svc.CreateText(t.Context(), opts(), []Document{huge}, ""); !errors.As(err, &inputErr) {
		t.Fatalf("过大的文档 = %v, 期望 InputError", err)
	}

	h.svc.parseDeadline = 50 * time.Millisecond
	slow := Document{Name: "slow.md", Text: strings.Repeat("[a](", 40000)}
	if _, err := h.svc.CreateText(t.Context(), opts(), []Document{slow}, ""); !errors.As(err, &inputErr) {
		t.Fatalf("解析超时 = %v, 期望 InputError", err)
	}
}

// TestStartHostsBounds 确认时域名重复只算一个;域名数超过上限按输入不合法拒绝,不会撞上数据库的绑定变量上限。
func TestStartHostsBounds(t *testing.T) {
	h := newHarness(t)
	docs := []Document{{Name: "a.md", Text: "![a](https://img.old.com/a.png)"}}
	id, err := h.svc.CreateText(t.Context(), opts(), docs, "")
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	var inputErr *InputError
	tooMany := make([]string, maxStartHosts+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("h%d.example.com", i)
	}
	if err := h.svc.Start(t.Context(), me(), id, tooMany); !errors.As(err, &inputErr) {
		t.Fatalf("域名过多 = %v, 期望 InputError", err)
	}
	h.wantStatus(id, StatusReady)
	if err := h.svc.Start(t.Context(), me(), id, []string{"img.old.com", "IMG.old.com"}); err != nil {
		t.Fatalf("重复域名确认: %v", err)
	}
}

// TestChunkedCreation 条目数超过一批时分批写入,任务最后才变得可见,条目一个不少。
func TestChunkedCreation(t *testing.T) {
	h := newHarness(t)
	n := writeChunk*2 + 7
	urls := make([]string, n)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://img.example.com/%d.png", i)
	}
	id, err := h.svc.CreateImages(t.Context(), opts(), urls)
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	h.wantStatus(id, StatusImporting)
	if c := h.counts(id); c[StatePending] != int64(n) {
		t.Fatalf("待转存 = %d, 期望 %d", c[StatePending], n)
	}
}

// TestRecoverRemovesUnfinishedCreation 创建中途退出遗留的 creating 任务对列表、详情都不可见,启动清理会连同条目一起删掉。
func TestRecoverRemovesUnfinishedCreation(t *testing.T) {
	h := newHarness(t)
	h.exec(`INSERT INTO import_jobs (id, user_id, mode, status) VALUES ('half', 'default', 'images', 'creating')`)
	h.exec(`INSERT INTO import_items (job_id, url, host, state)
VALUES ('half', 'https://a.example.com/a.png', 'a.example.com', 'pending')`)

	rows, err := h.env.Queries.ListImportJobs(t.Context(), sqlc.ListImportJobsParams{PageLimit: 10})
	if err != nil || len(rows) != 0 {
		t.Fatalf("列表 = %v, err=%v, 期望看不到 creating 任务", rows, err)
	}
	if err := h.svc.Cancel(t.Context(), me(), "half"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("取消 creating 任务 = %v, 期望 ErrNotFound", err)
	}
	if next, err := h.env.Queries.NextRunnableImportJob(t.Context()); err == nil {
		t.Fatalf("调度不应取到 creating 任务: %s", next.ID)
	}

	if err := h.svc.Recover(t.Context()); err != nil {
		t.Fatalf("启动清理: %v", err)
	}
	var jobs, items int64
	row := h.env.Conn.QueryRowContext(t.Context(),
		`SELECT (SELECT COUNT(*) FROM import_jobs), (SELECT COUNT(*) FROM import_items)`)
	if err := row.Scan(&jobs, &items); err != nil || jobs != 0 || items != 0 {
		t.Fatalf("清理后任务 %d 个、条目 %d 个, err=%v, 期望都被删除", jobs, items, err)
	}
}

// TestDeleteLargeJobInChunks 条目数超过一批的任务也能删干净:任务行与全部条目都不留。
func TestDeleteLargeJobInChunks(t *testing.T) {
	h := newHarness(t)
	n := deleteChunk*2 + 5
	urls := make([]string, n)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://img.example.com/%d.png", i)
	}
	id, err := h.svc.CreateImages(t.Context(), opts(), urls)
	if err != nil {
		t.Fatalf("创建任务: %v", err)
	}
	if err := h.svc.Cancel(t.Context(), me(), id); err != nil {
		t.Fatalf("取消: %v", err)
	}
	if err := h.svc.Delete(t.Context(), me(), id); err != nil {
		t.Fatalf("删除: %v", err)
	}
	var jobs, items int64
	row := h.env.Conn.QueryRowContext(t.Context(),
		`SELECT (SELECT COUNT(*) FROM import_jobs), (SELECT COUNT(*) FROM import_items)`)
	if err := row.Scan(&jobs, &items); err != nil || jobs != 0 || items != 0 {
		t.Fatalf("删除后任务 %d 个、条目 %d 个, err=%v, 期望都被删除", jobs, items, err)
	}
}

// TestParseSlotsBusy 解析名额被占满时,新的文本任务排队有时限,超时返回 ErrBusy 而不是一直挂着。
func TestParseSlotsBusy(t *testing.T) {
	h := newHarness(t)
	h.svc.parseDeadline = 50 * time.Millisecond
	for range parseSlots {
		h.svc.parse <- struct{}{}
	}
	docs := []Document{{Name: "a.md", Text: "![a](https://img.old.com/a.png)"}}
	if _, err := h.svc.CreateText(t.Context(), opts(), docs, ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("名额占满时 = %v, 期望 ErrBusy", err)
	}
	for range parseSlots {
		<-h.svc.parse
	}
	if _, err := h.svc.CreateText(t.Context(), opts(), docs, ""); err != nil {
		t.Fatalf("名额释放后应能创建: %v", err)
	}
}
