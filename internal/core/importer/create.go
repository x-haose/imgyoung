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

// writeChunk 创建任务时每个事务写入的条目数。一个事务占着唯一的写连接,大批量一次写完会让上传等其他写入
// 排队到超时;分批提交让它们在批与批之间插队。
const writeChunk = 2000

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
	err = s.buildJob(ctx, params, len(normalized), func(q *sqlc.Queries, i int) error {
		_, err := upsertItem(ctx, q, params.ID, normalized[i], StatePending)
		return err
	}, func(q *sqlc.Queries) error {
		job := sqlc.ImportJob{ID: params.ID, Status: StatusCreating}
		return transition(ctx, q, job, StatusCreating, StatusImporting)
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
	params, err := newJob(opts, ModeText, StatusReady, nil)
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
	var flat []sourcedRef
	for _, d := range scanned {
		for _, r := range d.res.Refs {
			flat = append(flat, sourcedRef{source: d.name, ref: r})
		}
	}
	err = s.buildJob(ctx, params, len(flat), func(q *sqlc.Queries, i int) error {
		return addRef(ctx, q, params.ID, flat[i].source, flat[i].ref)
	}, func(q *sqlc.Queries) error {
		ok, err := finishScan(ctx, q, params.ID, report.Pages, report.Pages, string(reportJSON))
		if err == nil && !ok {
			err = errors.New("文本任务创建后不在 creating 状态")
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return params.ID, nil
}

// sourcedRef 一处带来源的图片引用。
type sourcedRef struct {
	source string      // 引用所在的文档名
	ref    imgrefs.Ref // 引用
}

// buildJob 分批写入一个任务的条目,最后让任务对外可见。
// @Description: 任务行先以 creating 状态建立,接口与调度都看不见它;条目每 writeChunk 个一个事务提交;
// 全部写完后由 publish 在一个小事务里把它切到可见的状态。中途失败即删掉这个任务,进程在中途退出遗留的
// creating 任务由 Recover 在启动时清掉,所以不会留下半个任务。
// @param params 任务行参数,Status 是最终要进入的状态
// @param n 条目数
// @param write 写入第 i 个条目,在事务内执行
// @param publish 让任务进入最终状态,在事务内执行
// @return 写入失败的错误
func (s *Service) buildJob(ctx context.Context, params sqlc.CreateImportJobParams, n int,
	write func(q *sqlc.Queries, i int) error, publish func(q *sqlc.Queries) error) error {
	creating := params
	creating.Status = StatusCreating
	if err := s.cfg.Queries.CreateImportJob(ctx, creating); err != nil {
		return fmt.Errorf("写入导入任务: %w", err)
	}
	err := s.writeChunks(ctx, n, write)
	if err == nil {
		err = s.inTx(ctx, publish)
	}
	if err != nil {
		s.discard(ctx, params.ID)
	}
	return err
}

// writeChunks 把 n 个条目分成每批 writeChunk 个,每批一个事务。
// @param n 条目数
// @param write 写入第 i 个条目
// @return 任一批失败的错误
func (s *Service) writeChunks(ctx context.Context, n int, write func(q *sqlc.Queries, i int) error) error {
	for lo := 0; lo < n; lo += writeChunk {
		hi := min(lo+writeChunk, n)
		err := s.inTx(ctx, func(q *sqlc.Queries) error {
			for i := lo; i < hi; i++ {
				if err := write(q, i); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// discard 删掉没有写完的 creating 任务及其条目。删除脱离请求的取消:请求断开正是常见的失败原因。
// 删不掉只记日志,遗留的行由 Recover 在下次启动时清掉。
// @param jobID 任务 ID
func (s *Service) discard(ctx context.Context, jobID string) {
	if _, err := s.cfg.Queries.DeleteCreatingImportJob(context.WithoutCancel(ctx), jobID); err != nil {
		s.cfg.Logger.Error("清理没有写完的导入任务失败,留待下次启动清理", "job", jobID, "err", err)
	}
}

// Recover 启动时清掉上次进程在创建中途退出遗留的 creating 任务。
// @Description: 须在对外提供服务之前调用:此时不会有进行中的创建,否则会误删正在写入的任务。
// @return 删除失败的错误
func (s *Service) Recover(ctx context.Context) error {
	n, err := s.cfg.Queries.DeleteStaleCreatingImportJobs(ctx)
	if err != nil {
		return fmt.Errorf("清理没有写完的导入任务: %w", err)
	}
	if n > 0 {
		s.cfg.Logger.Warn("已清理上次中断时没有写完的导入任务", "count", n)
	}
	return nil
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
