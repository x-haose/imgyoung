package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/id"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/imgrefs"
)

// Options 创建任务的公共参数。
type Options struct {
	UserID     string             // 任务归属的用户,导入的图片归属于他;必填
	TTL        time.Duration      // 导入图片的有效期,仅 TTLSet 为 true 时生效;0 表示永久
	TTLSet     bool               // 是否显式指定了有效期;false 时按用户默认与全局默认
	EXIFPolicy imaging.EXIFPolicy // 导入图片的 EXIF 档位;空表示全局默认
	Referer    string             // 抓取图片时发送的 Referer,须是 http 或 https 绝对地址;空表示取条目的第一个来源页
}

// Document 一份待扫描的文本。
type Document struct {
	Name string // 文档名,通常是文件名;作为条目的来源,也决定按 HTML 还是 Markdown 解析
	Text string // 文档全文
}

// CreateImages 创建图片地址清单任务:条目直接待转存,任务进入 importing。
// @param opts 公共参数
// @param urls 图片地址,每个都须是 http 或 https 绝对地址;规范化后相同的只导入一次
// @return 新任务 ID
// @return *InputError 输入不合法;其他为基础设施错误
func (s *Service) CreateImages(ctx context.Context, opts Options, urls []string) (string, error) {
	if len(urls) == 0 {
		return "", &InputError{Detail: "urls 不能为空"}
	}
	normalized := make([]string, len(urls))
	for i, raw := range urls {
		u, ok := imgrefs.Normalize(raw, nil)
		if !ok {
			return "", &InputError{Detail: fmt.Sprintf("urls[%d] 不是 http 或 https 的绝对地址", i)}
		}
		normalized[i] = u
	}
	params, err := newJob(opts, ModeImages, StatusImporting, nil)
	if err != nil {
		return "", err
	}
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.CreateImportJob(ctx, params); err != nil {
			return fmt.Errorf("写入导入任务: %w", err)
		}
		for _, u := range normalized {
			if _, err := upsertItem(ctx, q, params.ID, u, StatePending); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	s.kick()
	return params.ID, nil
}

// CreateText 创建文本扫描任务:在本次调用内扫描全部文档,任务进入 ready 等待确认。
// @param opts 公共参数
// @param docs 待扫描的文档,至少一份
// @param baseURL 解析相对路径的基准地址,空表示没有;给出时须是 http 或 https 绝对地址
// @return 新任务 ID
// @return *InputError 输入不合法;其他为基础设施错误
func (s *Service) CreateText(ctx context.Context, opts Options, docs []Document, baseURL string) (string, error) {
	if len(docs) == 0 {
		return "", &InputError{Detail: "documents 不能为空"}
	}
	base, err := parseBase(baseURL)
	if err != nil {
		return "", err
	}
	params, err := newJob(opts, ModeText, StatusScanning, nil)
	if err != nil {
		return "", err
	}
	scanned, err := s.scanDocuments(ctx, docs, base)
	if err != nil {
		return "", err
	}
	report := textReport(scanned)
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("编码扫描报告: %w", err)
	}
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.CreateImportJob(ctx, params); err != nil {
			return fmt.Errorf("写入导入任务: %w", err)
		}
		return storeScanned(ctx, q, params.ID, scanned, report.Pages, string(reportJSON))
	})
	if err != nil {
		return "", err
	}
	return params.ID, nil
}

// storeScanned 把文本扫描到的候选写入任务,并让任务进入 ready。
// @param q 事务内的查询集
// @param jobID 任务 ID
// @param docs 各文档的扫描结果
// @param pages 文档数
// @param report 扫描报告的 JSON
// @return 写入失败的错误
func storeScanned(ctx context.Context, q *sqlc.Queries, jobID string, docs []scannedDocument, pages int64,
	report string) error {
	for _, d := range docs {
		if err := addRefs(ctx, q, jobID, d.name, d.res.Refs); err != nil {
			return err
		}
	}
	ok, err := finishScan(ctx, q, jobID, pages, pages, report)
	if err == nil && !ok {
		err = errors.New("文本任务创建后不在 scanning 状态")
	}
	return err
}

// CreateSite 创建站点扫描任务:任务进入 scanning,由调度循环抓取并扫描。
// @param opts 公共参数
// @param sites 站点地图、订阅源、网页或图片的地址,至少一个,每个都须是 http 或 https 绝对地址
// @return 新任务 ID
// @return *InputError 输入不合法;其他为基础设施错误
func (s *Service) CreateSite(ctx context.Context, opts Options, sites []string) (string, error) {
	if len(sites) == 0 {
		return "", &InputError{Detail: "sites 不能为空"}
	}
	seeds := make([]string, len(sites))
	for i, raw := range sites {
		u, ok := imgrefs.Normalize(raw, nil)
		if !ok {
			return "", &InputError{Detail: fmt.Sprintf("sites[%d] 不是 http 或 https 的绝对地址", i)}
		}
		seeds[i] = u
	}
	params, err := newJob(opts, ModeSite, StatusScanning, seeds)
	if err != nil {
		return "", err
	}
	if err := s.cfg.Queries.CreateImportJob(ctx, params); err != nil {
		return "", fmt.Errorf("写入导入任务: %w", err)
	}
	s.kick()
	return params.ID, nil
}

// newJob 校验公共参数并构造任务行。
// @param opts 公共参数
// @param mode 输入类型
// @param status 初始状态
// @param seeds 起始地址,可为 nil
// @return 任务行参数
// @return *InputError 参数不合法
func newJob(opts Options, mode, status string, seeds []string) (sqlc.CreateImportJobParams, error) {
	if opts.EXIFPolicy != "" {
		if err := imaging.ValidatePolicy(opts.EXIFPolicy); err != nil {
			return sqlc.CreateImportJobParams{}, &InputError{Detail: "exif_policy 取值非法"}
		}
	}
	referer := ""
	if opts.Referer != "" {
		r, ok := imgrefs.Normalize(opts.Referer, nil)
		if !ok {
			return sqlc.CreateImportJobParams{}, &InputError{Detail: "referer 不是 http 或 https 的绝对地址"}
		}
		referer = r
	}
	if seeds == nil {
		seeds = []string{}
	}
	seedsJSON, err := json.Marshal(seeds)
	if err != nil {
		return sqlc.CreateImportJobParams{}, fmt.Errorf("编码起始地址: %w", err)
	}
	p := sqlc.CreateImportJobParams{
		ID: id.New(), UserID: opts.UserID, Mode: mode, Status: status, Seeds: string(seedsJSON),
		ExifPolicy: string(opts.EXIFPolicy), Referer: referer,
	}
	if opts.TTLSet {
		p.TtlSeconds = sql.NullInt64{Int64: int64(opts.TTL / time.Second), Valid: true}
	}
	return p, nil
}

// parseBase 解析基准地址。
// @param raw 基准地址,空表示没有
// @return 基准地址;空串时为 nil
// @return *InputError 不是 http 或 https 绝对地址
func parseBase(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	normalized, ok := imgrefs.Normalize(raw, nil)
	if !ok {
		return nil, &InputError{Detail: "base_url 不是 http 或 https 的绝对地址"}
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("解析规范化后的基准地址: %w", err)
	}
	return u, nil
}

// inTx 在写句柄的一个事务里执行 fn;fn 返回错误即回滚。
// @param fn 事务内的操作,收到绑定到事务的查询集
// @return fn 的错误,或开启、提交事务失败的错误
func (s *Service) inTx(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	tx, err := s.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	// 提交成功后 Rollback 返回 ErrTxDone,属预期,显式忽略。
	defer func() { _ = tx.Rollback() }()
	if err := fn(s.cfg.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务: %w", err)
	}
	return nil
}

// owned 读取任务并确认归属;别人的任务按不存在处理,不泄露存在性。
// @param q 查询集,可绑定到事务
// @param userID 操作者用户 id
// @param jobID 任务 ID
// @return 任务行
// @return ErrNotFound 或查询失败的错误
func owned(ctx context.Context, q *sqlc.Queries, userID, jobID string) (sqlc.ImportJob, error) {
	job, err := q.GetImportJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.UserID != userID) {
		return sqlc.ImportJob{}, ErrNotFound
	}
	if err != nil {
		return sqlc.ImportJob{}, fmt.Errorf("读取导入任务: %w", err)
	}
	return job, nil
}

// Start 确认并开始导入:所选域名下的候选条目转为待转存,其余记为跳过,任务进入 importing。
// @param userID 操作者用户 id
// @param jobID 任务 ID
// @param hosts 要导入的域名
// @return ErrNotFound、*StateError(任务不在 ready)、ErrNoSelection(所选域名下没有候选条目);其他为基础设施错误
func (s *Service) Start(ctx context.Context, userID, jobID string, hosts []string) error {
	if len(hosts) > maxStartHosts {
		return &InputError{Detail: fmt.Sprintf("hosts 超过 %d 个", maxStartHosts)}
	}
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		job, err := owned(ctx, q, userID, jobID)
		if err != nil {
			return err
		}
		if job.Status != StatusReady {
			return &StateError{Status: job.Status}
		}
		n, err := q.ConfirmImportItems(ctx, sqlc.ConfirmImportItemsParams{JobID: jobID, Hosts: uniqueHosts(hosts)})
		if err != nil {
			return fmt.Errorf("确认待导入的条目: %w", err)
		}
		if n == 0 {
			return ErrNoSelection
		}
		if err := q.SkipImportCandidates(ctx, jobID); err != nil {
			return fmt.Errorf("跳过未选中的条目: %w", err)
		}
		return transition(ctx, q, job, StatusReady, StatusImporting)
	})
	if err != nil {
		return err
	}
	s.kick()
	return nil
}

// transition 在任务仍处于 from 状态时把它改为 to;状态已被改走即报 StateError。
// @param job 已读取的任务行,状态改走时据此报告
// @param from 期望的当前状态
// @param to 目标状态
// @return *StateError 或写入失败的错误
func transition(ctx context.Context, q *sqlc.Queries, job sqlc.ImportJob, from, to string) error {
	n, err := q.TransitionImportJob(ctx, sqlc.TransitionImportJobParams{
		ID: job.ID, FromStatus: from, ToStatus: to,
	})
	if err != nil {
		return fmt.Errorf("改变任务状态: %w", err)
	}
	if n == 0 {
		return &StateError{Status: job.Status}
	}
	return nil
}

// Cancel 取消 scanning、ready 或 importing 的任务;正在转存的那张处理完即停,已导入的保留。
// @param userID 操作者用户 id
// @param jobID 任务 ID
// @return ErrNotFound、*StateError(任务已经结束);其他为基础设施错误
func (s *Service) Cancel(ctx context.Context, userID, jobID string) error {
	if _, err := owned(ctx, s.cfg.Queries, userID, jobID); err != nil {
		return err
	}
	n, err := s.cfg.Queries.CancelImportJob(ctx, sqlc.CancelImportJobParams{
		ID: jobID, FinishedAt: sqlc.TimeNull(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("取消导入任务: %w", err)
	}
	if n == 0 {
		return s.stateError(ctx, jobID)
	}
	s.interrupt(jobID)
	return nil
}

// Retry 把 done、aborted 或 cancelled 的任务里失败的条目重新置为待转存,连同未处理的一起继续,任务回到 importing。
// @param userID 操作者用户 id
// @param jobID 任务 ID
// @return ErrNotFound、*StateError、ErrNothingToRetry(没有失败或未处理的条目);其他为基础设施错误
func (s *Service) Retry(ctx context.Context, userID, jobID string) error {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		job, err := owned(ctx, q, userID, jobID)
		if err != nil {
			return err
		}
		if job.Status != StatusDone && job.Status != StatusAborted && job.Status != StatusCancelled {
			return &StateError{Status: job.Status}
		}
		if job.Mode == ModeSite && !job.ScanReport.Valid && job.Status != StatusDone {
			// 扫描没有完成就中止或取消:没有候选可续,从头重新扫描。
			return transition(ctx, q, job, job.Status, StatusScanning)
		}
		if err := requeue(ctx, q, jobID); err != nil {
			return err
		}
		n, err := q.ResumeImportJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("继续导入任务: %w", err)
		}
		if n == 0 {
			return &StateError{Status: job.Status}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.kick()
	return nil
}

// requeue 把失败的条目重新置为待转存。
// @param q 事务内的查询集
// @param jobID 任务 ID
// @return ErrNothingToRetry(重置后仍没有待转存的条目);其他为基础设施错误
func requeue(ctx context.Context, q *sqlc.Queries, jobID string) error {
	requeued, err := q.RequeueFailedImportItems(ctx, jobID)
	if err != nil {
		return fmt.Errorf("重置失败的条目: %w", err)
	}
	pending, err := q.CountPendingImportItems(ctx, jobID)
	if err != nil {
		return fmt.Errorf("统计待转存的条目: %w", err)
	}
	if requeued == 0 && pending == 0 {
		return ErrNothingToRetry
	}
	return nil
}

// Delete 删除任务及其条目、来源;已导入的图片不受影响。scanning 与 importing 的任务须先取消。
// @param userID 操作者用户 id
// @param jobID 任务 ID
// @return ErrNotFound、*StateError(任务仍在进行);其他为基础设施错误
func (s *Service) Delete(ctx context.Context, userID, jobID string) error {
	if _, err := owned(ctx, s.cfg.Queries, userID, jobID); err != nil {
		return err
	}
	n, err := s.cfg.Queries.DeleteImportJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("删除导入任务: %w", err)
	}
	if n == 0 {
		return s.stateError(ctx, jobID)
	}
	return nil
}

// stateError 重新读取任务,按它此刻的状态构造 StateError;任务已不存在即 ErrNotFound。
// @param jobID 任务 ID
// @return ErrNotFound、*StateError 或读取失败的错误
func (s *Service) stateError(ctx context.Context, jobID string) error {
	job, err := s.cfg.Queries.GetImportJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取导入任务: %w", err)
	}
	return &StateError{Status: job.Status}
}

// uniqueHosts 把域名转成小写、去掉首尾空白并去重,与条目里 host 的形态一致。
// @param hosts 域名
// @return 规范化后的域名
func uniqueHosts(hosts []string) []string {
	seen := make(map[string]struct{}, len(hosts))
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if _, dup := seen[h]; !dup {
			seen[h] = struct{}{}
			out = append(out, h)
		}
	}
	return out
}
