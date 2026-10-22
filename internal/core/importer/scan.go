package importer

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imgrefs"
	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
)

// scannedDocument 一份文档扫描的结果。
type scannedDocument struct {
	name string         // 文档名,没有时按序号补一个
	res  imgrefs.Result // 扫描到的引用
}

// scanDocuments 扫描全部文本文档。
// @Description: 单份文档有字节上限;解析放进有并发名额与时限的执行单元里——解析函数不能被取消,超时后它仍会算完
// 才归还名额,所以恶意输入最多占用 parseSlots 个核,而不是随请求数增长。
// @param docs 文档
// @param base 解析相对路径的基准地址,可为 nil
// @return 各文档的扫描结果
// @return *InputError(文档过大或解析超时)、ctx 取消的错误
func (s *Service) scanDocuments(ctx context.Context, docs []Document, base *url.URL) ([]scannedDocument, error) {
	out := make([]scannedDocument, len(docs))
	for i, d := range docs {
		if len(d.Text) > MaxTextDocumentBytes {
			return nil, &InputError{Detail: fmt.Sprintf("documents[%d] 超过 %d MiB", i, MaxTextDocumentBytes>>20)}
		}
		doc, err := s.scanWithDeadline(ctx, i, d, base)
		if err != nil {
			return nil, err
		}
		out[i] = doc
	}
	return out, nil
}

// scanWithDeadline 在并发名额内扫描一份文档,超时即放弃等待。
// @param i 文档序号
// @param d 文档
// @param base 解析相对路径的基准地址,可为 nil
// @return 扫描结果
// @return *InputError(解析超时)、ErrBusy(等名额超时)、ctx 取消的错误
func (s *Service) scanWithDeadline(ctx context.Context, i int, d Document, base *url.URL) (scannedDocument, error) {
	wait := time.NewTimer(s.parseDeadline)
	select {
	case s.parse <- struct{}{}:
		wait.Stop()
	case <-wait.C:
		return scannedDocument{}, ErrBusy
	case <-ctx.Done():
		wait.Stop()
		return scannedDocument{}, ctx.Err()
	}
	done := make(chan scannedDocument, 1)
	go func() {
		defer func() { <-s.parse }()
		done <- scanDocument(i, d, base)
	}()
	timer := time.NewTimer(s.parseDeadline)
	defer timer.Stop()
	select {
	case res := <-done:
		return res, nil
	case <-timer.C:
		return scannedDocument{}, &InputError{Detail: fmt.Sprintf("documents[%d] 解析超时,内容可能含有大量嵌套结构", i)}
	case <-ctx.Done():
		return scannedDocument{}, ctx.Err()
	}
}

// scanDocument 扫描一份文本:HTML 文档按 HTML 解析,其余按 Markdown 解析(内嵌的 HTML 同样会被扫描)。
// @param i 文档在请求里的序号,从 0 起,文档名为空时据此补名
// @param d 文档
// @param base 解析相对路径的基准地址,可为 nil
// @return 扫描结果
func scanDocument(i int, d Document, base *url.URL) scannedDocument {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		name = fmt.Sprintf("文本 %d", i+1)
	}
	src := []byte(d.Text)
	if isHTMLDocument(name, d.Text) {
		return scannedDocument{name: name, res: imgrefs.FromHTML(src, base)}
	}
	return scannedDocument{name: name, res: imgrefs.FromMarkdown(src, base)}
}

// isHTMLDocument 判断一份文本是否按 HTML 解析:文件名以 .html 或 .htm 结尾,或内容以 <!doctype html、<html 开头。
// @param name 文档名
// @param text 文档全文
// @return 按 HTML 解析为 true
func isHTMLDocument(name, text string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") {
		return true
	}
	head := strings.ToLower(strings.TrimLeft(text, "\xef\xbb\xbf \t\r\n"))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

// textReport 汇总文本扫描的结果。
// @param docs 各文档的扫描结果
// @return 扫描报告
func textReport(docs []scannedDocument) ScanReport {
	r := ScanReport{Pages: int64(len(docs)), FailedPages: []PageFailure{}, Unfetchable: []Unfetchable{}}
	for _, d := range docs {
		if len(d.res.Refs) == 0 && len(d.res.Unfetchable) == 0 {
			r.PagesWithoutImages++
		}
		for _, raw := range d.res.Unfetchable {
			r.addUnfetchable(Unfetchable{Source: d.name, Reference: raw})
		}
	}
	return r
}

// upsertItem 登记一个图片地址条目;同一任务里已有该地址时沿用原条目。
// @param q 查询集,可绑定到事务
// @param jobID 任务 ID
// @param rawURL 规范化后的图片地址
// @param state 新条目的状态
// @return 条目 ID
func upsertItem(ctx context.Context, q *sqlc.Queries, jobID, rawURL, state string) (int64, error) {
	itemID, err := q.UpsertImportItem(ctx, sqlc.UpsertImportItemParams{
		JobID: jobID, Url: rawURL, Host: imgrefs.Host(rawURL), State: state,
	})
	if err != nil {
		return 0, fmt.Errorf("写入图片地址 %q: %w", rawURL, err)
	}
	return itemID, nil
}

// addRef 把一处引用登记为待确认的条目,并记下来源与原文。
// @param q 查询集,可绑定到事务
// @param jobID 任务 ID
// @param source 引用所在的页面地址或文档名;空串表示不记来源
// @param r 可抓取的引用
// @return 写入失败时的错误
func addRef(ctx context.Context, q *sqlc.Queries, jobID, source string, r imgrefs.Ref) error {
	itemID, err := upsertItem(ctx, q, jobID, r.URL, StateCandidate)
	if err != nil || source == "" {
		return err
	}
	if err := q.AddImportItemSource(ctx, sqlc.AddImportItemSourceParams{
		ItemID: itemID, Source: source, Raw: r.Raw,
	}); err != nil {
		return fmt.Errorf("写入引用来源: %w", err)
	}
	return nil
}

// addRefs 把一个来源里扫描到的引用登记为待确认的条目。
// @param q 查询集,可绑定到事务
// @param jobID 任务 ID
// @param source 引用所在的页面地址或文档名;空串表示不记来源
// @param refs 可抓取的引用
// @return 写入失败时的错误
func addRefs(ctx context.Context, q *sqlc.Queries, jobID, source string, refs []imgrefs.Ref) error {
	for _, r := range refs {
		if err := addRef(ctx, q, jobID, source, r); err != nil {
			return err
		}
	}
	return nil
}

// finishScan 记下扫描结果并让任务进入 ready;任务已不在 scanning(比如被取消)时不动。
// @param q 查询集,可绑定到事务
// @param jobID 任务 ID
// @param found 发现的页面数
// @param done 处理的页面数
// @param report 扫描报告的 JSON
// @return 任务进入了 ready 为 true
func finishScan(ctx context.Context, q *sqlc.Queries, jobID string, found, done int64, report string) (bool, error) {
	n, err := q.FinishImportScan(ctx, sqlc.FinishImportScanParams{
		ID: jobID, PagesFound: found, PagesDone: done, ScanReport: sql.NullString{String: report, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("记录扫描结果: %w", err)
	}
	return n > 0, nil
}

// siteScan 一次站点扫描的状态。
type siteScan struct {
	s      *Service
	job    sqlc.ImportJob
	mu     sync.Mutex
	report ScanReport
	found  int64               // 已发现的页面数
	done   int64               // 已处理的页面数
	failed map[string]struct{} // 已记入 FailedPages 的地址
}

// scan 扫描一个 site 任务:展开站点地图、订阅源得到页面,逐页抓取并扫描出图片,候选条目写入表里。
// @Description: 重新扫描前清掉上次遗留的候选与进度,所以中断后从头来过是安全的。单个页面抓取失败只记入报告,
// 不影响其余页面;数据库故障与 ctx 取消终止扫描。
// @param job 任务行
// @return 终止扫描的错误
func (s *Service) scan(ctx context.Context, job sqlc.ImportJob) error {
	seeds, err := DecodeSeeds(job.Seeds)
	if err != nil {
		return err
	}
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.ClearImportCandidates(ctx, job.ID); err != nil {
			return fmt.Errorf("清除上次的候选: %w", err)
		}
		return q.ResetImportScan(ctx, job.ID)
	})
	if err != nil {
		return err
	}
	sc := &siteScan{
		s: s, job: job, failed: map[string]struct{}{},
		report: ScanReport{FailedPages: []PageFailure{}, Unfetchable: []Unfetchable{}},
	}
	pages, err := sc.discover(ctx, seeds)
	if err != nil {
		return err
	}
	if err := sc.fetchPages(ctx, pages); err != nil {
		return err
	}
	return sc.finish(ctx)
}

// finish 写入扫描报告,任务进入 ready。
// @return 写入失败时的错误
func (sc *siteScan) finish(ctx context.Context) error {
	sc.mu.Lock()
	sc.report.Pages = sc.found
	raw, err := json.Marshal(sc.report)
	found, done := sc.found, sc.done
	sc.mu.Unlock()
	if err != nil {
		return fmt.Errorf("编码扫描报告: %w", err)
	}
	_, err = finishScan(ctx, sc.s.cfg.Queries, sc.job.ID, found, done, string(raw))
	return err
}

// listingTask 待抓取的站点地图、订阅源或种子地址。
type listingTask struct {
	url  string // 规范化后的地址
	seed bool   // 是用户给出的地址:网页可直接扫描,图片可直接收下;子站点地图返回网页则是内容有误
}

// discover 展开种子地址:站点地图与订阅源列出的页面收集起来,索引的子站点地图继续展开,图片与网页种子直接处理。
// @param seeds 规范化后的种子地址
// @return 待抓取的页面地址,按发现顺序,已去重
// @return 数据库故障或 ctx 取消的错误
func (sc *siteScan) discover(ctx context.Context, seeds []string) ([]string, error) {
	queue := make([]listingTask, 0, len(seeds))
	for _, seed := range seeds {
		queue = append(queue, listingTask{url: seed, seed: true})
	}
	visited := map[string]struct{}{}
	seenPage := map[string]struct{}{}
	var pages []string
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task := queue[0]
		queue = queue[1:]
		if _, dup := visited[task.url]; dup {
			continue
		}
		visited[task.url] = struct{}{}
		listing, err := sc.expand(ctx, task)
		if err != nil {
			return nil, err
		}
		for _, child := range listing.children {
			queue = append(queue, listingTask{url: child})
		}
		for _, p := range listing.pages {
			if _, dup := seenPage[p]; !dup {
				seenPage[p] = struct{}{}
				pages = append(pages, p)
			}
		}
	}
	sc.mu.Lock()
	sc.found += int64(len(pages))
	sc.mu.Unlock()
	return pages, sc.progress(ctx)
}

// expanded 一个种子或子站点地图展开的结果。
type expanded struct {
	pages    []string // 列出的页面地址,已规范化且限定在所属域名下
	children []string // 列出的子站点地图地址
}

// expand 抓取并解析一个种子或子站点地图。抓取失败、内容无法解析记入报告,返回空结果。
// @param task 待展开的任务
// @return 列出的页面与子站点地图
// @return 数据库故障或 ctx 取消的错误
func (sc *siteScan) expand(ctx context.Context, task listingTask) (expanded, error) {
	doc, err := sc.fetchDocument(ctx, task.url)
	if err != nil {
		return expanded{}, sc.recordFailure(ctx, task.url, err)
	}
	if strings.HasPrefix(http.DetectContentType(doc.Data), "image/") {
		return expanded{}, sc.storeDirectImage(ctx, task.url)
	}
	kind := imgrefs.Sniff(doc.Data)
	if kind == imgrefs.KindHTML {
		if !task.seed {
			return expanded{}, sc.recordFailure(ctx, task.url, imgrefs.ErrMalformed)
		}
		return expanded{}, sc.storePage(ctx, task.url, doc)
	}
	listing, err := imgrefs.ParseListing(doc.Data, kind)
	if err != nil {
		return expanded{}, sc.recordFailure(ctx, task.url, err)
	}
	return sc.collectListing(ctx, task.url, doc.FinalURL, listing)
}

// collectListing 把一份站点地图、索引或订阅源列出的内容归类:同域名下的页面与子站点地图留下,
// 其他域名的计数跳过,写不成地址的记为无法抓取,站点地图列出的图片直接登记为候选。
// @param listURL 这份列表自己的地址,作来源记录
// @param finalURL 抓取列表时重定向后的最终地址;所列页面须与它同域名
// @param l 解析结果
// @return 留下的页面与子站点地图
// @return 数据库故障的错误
func (sc *siteScan) collectListing(ctx context.Context, listURL, finalURL string,
	l imgrefs.Listing) (expanded, error) {
	host := imgrefs.Host(finalURL)
	if normalized, ok := imgrefs.Normalize(finalURL, nil); ok {
		host = imgrefs.Host(normalized)
	}
	var out expanded
	keep := func(raws []string) []string {
		var kept []string
		for _, raw := range raws {
			abs, ok := imgrefs.Normalize(raw, nil)
			switch {
			case !ok:
				sc.addUnfetchable(listURL, raw)
			case imgrefs.Host(abs) != host:
				sc.mu.Lock()
				sc.report.OffsitePages++
				sc.mu.Unlock()
			default:
				kept = append(kept, abs)
			}
		}
		return kept
	}
	out.pages = keep(l.Pages)
	out.children = keep(l.Children)
	return out, sc.storeListedImages(ctx, listURL, l.Images)
}

// storeListedImages 把站点地图随页面列出的图片登记为候选,来源是它所在的页面。
// @param listURL 站点地图自己的地址,图片没有所在页面时作为来源
// @param images 列出的图片
// @return 写入失败时的错误
func (sc *siteScan) storeListedImages(ctx context.Context, listURL string, images []imgrefs.ListedImage) error {
	return sc.s.inTx(ctx, func(q *sqlc.Queries) error {
		for _, li := range images {
			abs, ok := imgrefs.Normalize(li.Image, nil)
			if !ok {
				sc.addUnfetchable(listURL, li.Image)
				continue
			}
			source := listURL
			if page, ok := imgrefs.Normalize(li.Page, nil); ok {
				source = page
			}
			if err := addRefs(ctx, q, sc.job.ID, source, []imgrefs.Ref{{Raw: li.Image, URL: abs}}); err != nil {
				return err
			}
		}
		return nil
	})
}

// storeDirectImage 把用户给出的图片地址直接登记为候选,没有来源。
// @param imageURL 规范化后的图片地址
// @return 写入失败时的错误
func (sc *siteScan) storeDirectImage(ctx context.Context, imageURL string) error {
	return sc.s.inTx(ctx, func(q *sqlc.Queries) error {
		return addRefs(ctx, q, sc.job.ID, "", []imgrefs.Ref{{Raw: imageURL, URL: imageURL}})
	})
}

// storePage 扫描一个网页并登记其中的图片引用,页面计入已发现与已处理。
// @param pageURL 页面地址,作来源记录
// @param doc 抓回的页面;相对路径按它重定向后的最终地址解析
// @return 写入失败时的错误
func (sc *siteScan) storePage(ctx context.Context, pageURL string, doc urlfetch.Fetched) error {
	base, err := url.Parse(doc.FinalURL)
	if err != nil {
		return fmt.Errorf("解析页面地址 %q: %w", doc.FinalURL, err)
	}
	res := imgrefs.FromHTML(doc.Data, base)
	sc.mu.Lock()
	sc.found++
	sc.mu.Unlock()
	return sc.recordPage(ctx, pageURL, res)
}

// recordPage 在一个事务里登记一个页面的扫描结果并更新进度。
// @param pageURL 页面地址
// @param res 扫描结果
// @return 写入失败时的错误
func (sc *siteScan) recordPage(ctx context.Context, pageURL string, res imgrefs.Result) error {
	err := sc.s.inTx(ctx, func(q *sqlc.Queries) error {
		return addRefs(ctx, q, sc.job.ID, pageURL, res.Refs)
	})
	if err != nil {
		return err
	}
	sc.mu.Lock()
	sc.done++
	if len(res.Refs) == 0 && len(res.Unfetchable) == 0 {
		sc.report.PagesWithoutImages++
	}
	for _, raw := range res.Unfetchable {
		sc.report.addUnfetchable(Unfetchable{Source: pageURL, Reference: raw})
	}
	sc.mu.Unlock()
	return sc.progress(ctx)
}

// fetchPages 并发抓取页面,抽出图片引用写入候选。单个页面失败记入报告,其余照常。
// @param pages 页面地址
// @return 数据库故障或 ctx 取消的错误
func (sc *siteScan) fetchPages(ctx context.Context, pages []string) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(scanConcurrency)
	for _, page := range pages {
		g.Go(func() error {
			doc, err := sc.fetchDocument(gctx, page)
			if err != nil {
				if gctx.Err() != nil {
					return gctx.Err()
				}
				sc.mu.Lock()
				sc.done++
				sc.mu.Unlock()
				return sc.recordFailure(gctx, page, err)
			}
			base, err := url.Parse(doc.FinalURL)
			if err != nil {
				return fmt.Errorf("解析页面地址 %q: %w", doc.FinalURL, err)
			}
			return sc.recordPage(gctx, page, imgrefs.FromHTML(doc.Data, base))
		})
	}
	return g.Wait()
}

// fetchDocument 抓取一个站点地图、订阅源或网页;gzip 压缩的按解压后计大小。
// @param target 规范化后的地址
// @return 抓取结果,内容已解压
// @return 抓取或解压失败的错误
func (sc *siteScan) fetchDocument(ctx context.Context, target string) (urlfetch.Fetched, error) {
	got, err := sc.s.cfg.Pages.FetchWith(ctx, target, urlfetch.Options{UserAgent: sc.s.cfg.UserAgent})
	if err != nil {
		return urlfetch.Fetched{}, err
	}
	got.Data, err = gunzip(got.Data)
	return got, err
}

// gunzip 内容是 gzip 时解压,否则原样返回;解压后超过 MaxDocumentBytes 视为超限。
// @param data 抓回的内容
// @return 解压后的内容
// @return urlfetch.ErrTooLarge,或包裹 imgrefs.ErrMalformed 的解压错误
func gunzip(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", imgrefs.ErrMalformed, err)
	}
	out, err := io.ReadAll(io.LimitReader(zr, MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", imgrefs.ErrMalformed, err)
	}
	if len(out) > MaxDocumentBytes {
		return nil, urlfetch.ErrTooLarge
	}
	return out, nil
}

// recordFailure 把一个地址的抓取或解析失败记入报告。ctx 已取消时原样返回取消错误,让扫描终止。
// @param source 失败的地址
// @param cause 失败原因
// @return ctx 取消的错误;其余为 nil
func (sc *siteScan) recordFailure(ctx context.Context, source string, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	code, detail := classifyDocument(cause)
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if _, dup := sc.failed[source]; dup {
		return nil
	}
	sc.failed[source] = struct{}{}
	sc.report.addFailure(PageFailure{Source: source, Code: code.String(), Detail: detail})
	return nil
}

// addUnfetchable 记一处无法抓取的引用。
// @param source 引用所在的地址
// @param raw 引用原文
func (sc *siteScan) addUnfetchable(source, raw string) {
	sc.mu.Lock()
	sc.report.addUnfetchable(Unfetchable{Source: source, Reference: raw})
	sc.mu.Unlock()
}

// progress 把已发现与已处理的页面数写入任务行。
// @return 写入失败时的错误
func (sc *siteScan) progress(ctx context.Context) error {
	sc.mu.Lock()
	found, done := sc.found, sc.done
	sc.mu.Unlock()
	err := sc.s.cfg.Queries.SetImportScanProgress(ctx, sqlc.SetImportScanProgressParams{
		ID: sc.job.ID, PagesFound: found, PagesDone: done,
	})
	if err != nil {
		return fmt.Errorf("记录扫描进度: %w", err)
	}
	return nil
}

// classifyDocument 把抓取或解析站点地图、订阅源、网页的失败归为错误码与根因原文。
// @param err 失败原因
// @return 错误码
// @return 根因原文
func classifyDocument(err error) (errcode.Code, string) {
	if errors.Is(err, imgrefs.ErrMalformed) {
		return errcode.ImportListingInvalid, truncateDetail(err.Error())
	}
	return classifyFetch(err)
}
