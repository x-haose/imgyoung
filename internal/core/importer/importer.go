// Package importer 批量导入:把一批图片地址转存进图床,地址来自用户直接给出的清单、Markdown 与 HTML 文本,
// 或由服务端抓取站点地图、订阅源与网页扫描得到。
// 业务表 import_jobs / import_items 是唯一真相,本包的调度循环只是按表里的状态推进:进程重启后,
// 状态为 scanning 与 importing 的任务从表里接着跑。
package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
)

// 任务的输入类型。
const (
	ModeImages = "images" // 图片地址清单,不扫描,直接导入
	ModeText   = "text"   // Markdown 或 HTML 文本,扫描后等待确认
	ModeSite   = "site"   // 站点地图、订阅源、网页或图片的地址,由服务端抓取并扫描,完成后等待确认
)

// 任务状态。
const (
	StatusScanning  = "scanning"  // 正在抓取并扫描
	StatusReady     = "ready"     // 扫描完成,等待确认
	StatusImporting = "importing" // 正在逐张转存
	StatusDone      = "done"      // 已处理完全部条目,其中可能有失败的
	StatusCancelled = "cancelled" // 已取消
	StatusAborted   = "aborted"   // 因配额已满、没有可用渠道或服务端故障而中止
)

// 条目状态。
const (
	StateCandidate = "candidate" // 扫描发现,等待确认
	StatePending   = "pending"   // 已确认,等待转存
	StateDone      = "done"      // 已导入
	StateFailed    = "failed"    // 转存失败
	StateSkipped   = "skipped"   // 确认时所在域名未被选中
)

const (
	// importConcurrency 一个任务同时转存的图片数。与上传并发闸相互独立,峰值内存按两者之和估算。
	importConcurrency = 4
	// scanConcurrency 扫描时同时抓取的页面数。
	scanConcurrency = 4
	// itemTimeout 转存一张图片(抓取、处理、写渠道、落库)的总时限。
	itemTimeout = 20 * time.Minute
	// pendingBatch 每批从表里取的待转存条目数。
	pendingBatch = 200
	// MaxDocumentBytes 站点地图、订阅源与网页的字节上限,压缩文件按解压后计;取站点地图协议的单文件上限。
	MaxDocumentBytes = 50 << 20
	// DocumentTimeout 抓取一个站点地图、订阅源或网页的总时限。
	DocumentTimeout = 2 * time.Minute
	// maxDetailRunes 条目失败原文保留的字符数上限。
	maxDetailRunes = 500
	// retryBackoff 读取任务表失败后的等待时间。
	retryBackoff = 5 * time.Second
)

// ErrNotFound 任务不存在,或不属于当前用户。
var ErrNotFound = errors.New("importer: 导入任务不存在")

// ErrNoSelection 所选域名下没有待导入的条目。
var ErrNoSelection = errors.New("importer: 所选域名下没有待导入的条目")

// ErrNothingToRetry 没有失败或未处理的条目可以继续。
var ErrNothingToRetry = errors.New("importer: 没有需要重试的条目")

// StateError 任务当前状态不允许这项操作。
type StateError struct {
	Status string // 任务当前状态
}

// Error 实现 error。
// @return 错误说明
func (e *StateError) Error() string {
	return "importer: 任务当前状态为 " + e.Status + ",不能执行此操作"
}

// InputError 创建任务的输入不合法。
type InputError struct {
	Detail string // 哪一项不合法
}

// Error 实现 error。
// @return 错误说明
func (e *InputError) Error() string { return "importer: 导入输入不合法: " + e.Detail }

// Fetcher 抓取远端内容(由 urlfetch.Fetcher 实现)。
type Fetcher interface {
	// FetchWith 抓取 rawURL 的内容,返回字节与远端声明的 Content-Type;失败返回 urlfetch 的哨兵错误或网络错误。
	FetchWith(ctx context.Context, rawURL string, opts urlfetch.Options) ([]byte, string, error)
}

// Config 导入服务依赖。
type Config struct {
	Conn      *sql.DB                // 写句柄,创建任务与状态转换用事务
	Queries   *sqlc.Queries          // 查询集
	Upload    *upload.Service        // 图片走与上传相同的处理管线
	Images    Fetcher                // 抓取图片;大小上限与单图上限同源
	Pages     Fetcher                // 抓取站点地图、订阅源与网页;上限见 MaxDocumentBytes
	Settings  func() config.Settings // 运营配置快照读取口,取全局默认的 EXIF 档位
	UserAgent string                 // 抓取时发送的 User-Agent
	Logger    *slog.Logger           // 结构化日志
}

// Service 批量导入服务。
type Service struct {
	cfg  Config
	wake chan struct{} // 有任务进入 scanning 或 importing 时唤醒调度循环;容量 1,合并重复唤醒

	mu      sync.Mutex
	running map[string]context.CancelFunc // 正在处理的任务 ID → 中止它的函数
}

// New 创建导入服务。
// @param cfg 依赖,字段均必填
// @return 就绪的导入服务
func New(cfg Config) *Service {
	return &Service{cfg: cfg, wake: make(chan struct{}, 1), running: map[string]context.CancelFunc{}}
}

// Run 调度循环:一次处理一个任务,按创建先后,直到 ctx 取消。
// @Description: 取表里最早的 scanning 或 importing 任务处理;没有时等唤醒。启动时表里遗留的这两种状态的任务
// 即上次中断的任务,由本循环接着跑:importing 从未处理的条目继续,scanning 丢弃已发现的候选后重新扫描。
// 读取任务表失败时记日志、等一段时间重试,不丢任务。
// @param ctx 控制循环生命周期;取消时中止在途的任务,条目状态留在表里
func (s *Service) Run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := s.cfg.Queries.NextRunnableImportJob(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			select {
			case <-ctx.Done():
			case <-s.wake:
			}
		case err != nil:
			if ctx.Err() == nil {
				s.cfg.Logger.Error("读取待处理的导入任务失败", "err", err)
				pause(ctx)
			}
		default:
			s.process(ctx, job)
		}
	}
}

// pause 等待 retryBackoff 或 ctx 取消。
// @param ctx 控制等待
func pause(ctx context.Context) {
	t := time.NewTimer(retryBackoff)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// kick 唤醒调度循环;已有唤醒在途时合并。
func (s *Service) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// process 处理一个任务直到它离开 scanning 或 importing,或 ctx 取消。
// @Description: 处理出错时把任务置为 aborted(internal),不让同一个任务被循环反复取到;置状态也失败时记日志并等待。
// @param ctx 调度循环的上下文
// @param job 待处理的任务行
func (s *Service) process(ctx context.Context, job sqlc.ImportJob) {
	jobCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.running[job.ID] = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.running, job.ID)
		s.mu.Unlock()
	}()

	var err error
	switch job.Status {
	case StatusScanning:
		err = s.scan(jobCtx, job)
	case StatusImporting:
		err = s.importItems(jobCtx, job)
	default:
		err = fmt.Errorf("任务状态 %q 不是可处理的状态", job.Status)
	}
	if err == nil || jobCtx.Err() != nil {
		return
	}
	s.cfg.Logger.Error("导入任务处理失败,任务中止", "job", job.ID, "status", job.Status, "err", err)
	s.abort(ctx, job.ID, job.Status, "internal")
}

// abort 把任务置为 aborted,记下原因;任务状态已被别处改走时不动。
// @param ctx 控制超时与取消
// @param id 任务 ID
// @param from 期望的当前状态
// @param code 原因码,取值同接口的 error_code
func (s *Service) abort(ctx context.Context, id, from, code string) {
	_, err := s.cfg.Queries.TransitionImportJob(context.WithoutCancel(ctx), sqlc.TransitionImportJobParams{
		ID: id, FromStatus: from, ToStatus: StatusAborted, ErrorCode: code,
		FinishedAt: sqlc.TimeNull(time.Now()),
	})
	if err != nil {
		s.cfg.Logger.Error("置导入任务为中止失败", "job", id, "err", err)
		pause(ctx)
	}
}

// interrupt 中止正在处理该任务的循环;任务不在处理中时无动作。
// @param id 任务 ID
func (s *Service) interrupt(id string) {
	s.mu.Lock()
	cancel := s.running[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// truncateDetail 把失败原文截到 maxDetailRunes 个字符。
// @param s 原文
// @return 截断后的原文
func truncateDetail(s string) string {
	r := []rune(s)
	if len(r) <= maxDetailRunes {
		return s
	}
	return string(r[:maxDetailRunes])
}
