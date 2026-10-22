package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
	"github.com/x-haose/imgyoung/internal/infra/urlfetch"
)

// quotaStreakLimit 连续这么多张图都因存储配额不足而失败,就认为配额已满而中止任务;中间只要有一张成功就重新计数。
// 单张图超过剩余额度只算这张图失败,不拦住后面放得下的图。
const quotaStreakLimit = 3

// runStats 一次导入运行中跨条目共享的计数。
type runStats struct {
	quotaStreak atomic.Int32 // 连续因配额不足失败的条目数
}

// importItems 逐批转存任务里待转存的条目,处理完后任务进入 done;遇到配额已满、没有可用渠道或服务端故障即中止整个任务。
// @Description: 条目的结果在处理后立即写表,所以中断后重新执行只处理仍是 pending 的条目;已入库而没来得及写表的图片
// 靠每张图的幂等键避免重复。ctx 取消(用户取消或停机)时留下未处理的条目,不改任务状态。
// @param job 任务行
// @return 读取条目失败的错误
func (s *Service) importItems(ctx context.Context, job sqlc.ImportJob) error {
	var after int64
	var stats runStats
	for {
		items, err := s.cfg.Queries.ListPendingImportItems(ctx, sqlc.ListPendingImportItemsParams{
			JobID: job.ID, AfterID: after, PageLimit: pendingBatch,
		})
		if err != nil {
			return fmt.Errorf("读取待转存的条目: %w", err)
		}
		if len(items) == 0 {
			break
		}
		after = items[len(items)-1].ID
		code := s.importBatch(ctx, job, items, &stats)
		// 任务已被取消或停机:中止原因是被打断的副产物,不能把可能已重新开始的任务置为中止。
		if ctx.Err() != nil {
			return nil
		}
		if code != "" {
			s.abort(ctx, job.ID, StatusImporting, code)
			return nil
		}
	}
	_, err := s.cfg.Queries.TransitionImportJob(context.WithoutCancel(ctx), sqlc.TransitionImportJobParams{
		ID: job.ID, FromStatus: StatusImporting, ToStatus: StatusDone, FinishedAt: sqlc.TimeNull(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("置导入任务为已完成: %w", err)
	}
	return nil
}

// importBatch 并发转存一批条目。
// @param job 任务行
// @param items 待转存的条目
// @param st 本次运行的共享计数
// @return 需要中止整个任务时的原因码;否则为空串
func (s *Service) importBatch(ctx context.Context, job sqlc.ImportJob, items []sqlc.ListPendingImportItemsRow,
	st *runStats) string {
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu    sync.Mutex
		abort string
	)
	g := errgroup.Group{}
	g.SetLimit(importConcurrency)
	for _, item := range items {
		g.Go(func() error {
			if batchCtx.Err() != nil {
				return nil
			}
			if code := s.importOne(batchCtx, job, item, st); code != "" {
				mu.Lock()
				if abort == "" {
					abort = code
				}
				mu.Unlock()
				cancel()
			}
			return nil
		})
	}
	// 各 goroutine 只经 abort 报告结果,不返回错误。
	_ = g.Wait()
	return abort
}

// importOne 转存一个条目并把结果写表。
// @Description: 单个条目的失败(远端抓不到、不是可接受的图片)记在条目上;配额已满、没有可用渠道、服务端故障
// 继续下去只会让后面的条目同样失败,返回原因码要求中止任务。ctx 取消时条目保持 pending。
// @param ctx 批次的上下文,取消表示任务被取消、停机或批内已有条目要求中止
// @param job 任务行
// @param item 待转存的条目
// @param st 本次运行的共享计数
// @return 需要中止整个任务时的原因码;否则为空串
func (s *Service) importOne(ctx context.Context, job sqlc.ImportJob, item sqlc.ListPendingImportItemsRow,
	st *runStats) string {
	ictx, cancel := context.WithTimeout(ctx, itemTimeout)
	defer cancel()
	// 上次中断时图片可能已入库而结果没来得及写表:先按幂等键认领,不再抓取(旧地址可能已经失效)。
	existing, found, err := s.cfg.Upload.ExistingUpload(ctx, job.UserID, importKey(job.ID, item.ID))
	if err != nil {
		if ctx.Err() != nil {
			return ""
		}
		s.cfg.Logger.Error("查询条目的幂等键失败,任务中止", "item", item.ID, "err", err)
		return errcode.Internal.String()
	}
	if found {
		st.quotaStreak.Store(0)
		return s.markDone(ctx, item.ID, existing)
	}
	got, err := s.cfg.Images.FetchWith(ictx, item.Url, urlfetch.Options{
		Referer: refererFor(job, item.FirstSource), UserAgent: s.cfg.UserAgent,
	})
	if err != nil {
		if ctx.Err() != nil {
			return ""
		}
		code, detail := classifyFetch(err)
		return s.markFailed(ctx, item.ID, code, detail)
	}
	res, err := s.cfg.Upload.Process(ictx, got.Data, s.uploadOptions(job, item))
	if err != nil {
		if ctx.Err() != nil {
			return ""
		}
		return s.settleUploadError(ctx, item, st, err)
	}
	st.quotaStreak.Store(0)
	return s.markDone(ctx, item.ID, res.ImageID)
}

// importKey 一张图的幂等键。带上任务 ID:条目 ID 在任务被删除后可能被新任务的条目复用,
// 只用条目 ID 会让新条目认领到旧任务留下的图。
// @param jobID 任务 ID
// @param itemID 条目 ID
// @return 幂等键
func importKey(jobID string, itemID int64) string {
	return fmt.Sprintf("import-%s-%d", jobID, itemID)
}

// markDone 记下条目已导入。图片已入库,结果无论如何都要记下,所以脱离取消。
// @param itemID 条目 ID
// @param imageID 图片短 ID
// @return 写表失败时的 internal(中止任务);否则为空串
func (s *Service) markDone(ctx context.Context, itemID int64, imageID string) string {
	err := s.cfg.Queries.MarkImportItemDone(context.WithoutCancel(ctx), sqlc.MarkImportItemDoneParams{
		ID: itemID, ImageID: sql.NullString{String: imageID, Valid: true},
	})
	if err != nil {
		s.cfg.Logger.Error("记录导入结果失败", "item", itemID, "image", imageID, "err", err)
		return errcode.Internal.String()
	}
	return ""
}

// settleUploadError 把上传管线的错误落到条目或任务上。
// @param item 条目
// @param st 本次运行的共享计数
// @param err 上传管线返回的错误
// @return 需要中止整个任务时的原因码;否则为空串
func (s *Service) settleUploadError(ctx context.Context, item sqlc.ListPendingImportItemsRow, st *runStats,
	err error) string {
	switch {
	case errors.Is(err, upload.ErrUnsupportedType):
		return s.markFailed(ctx, item.ID, errcode.UploadUnsupportedType, truncateDetail(err.Error()))
	case errors.Is(err, upload.ErrTooManyPixels):
		return s.markFailed(ctx, item.ID, errcode.UploadTooManyPixels, truncateDetail(err.Error()))
	case errors.Is(err, upload.ErrBadDimensions):
		return s.markFailed(ctx, item.ID, errcode.UploadBadDimensions, truncateDetail(err.Error()))
	case errors.Is(err, upload.ErrInvalidImage):
		return s.markFailed(ctx, item.ID, errcode.UploadInvalidImage, truncateDetail(err.Error()))
	case errors.Is(err, upload.ErrQuotaExceeded):
		if st.quotaStreak.Add(1) >= quotaStreakLimit {
			return errcode.UploadQuotaExceeded.String()
		}
		return s.markFailed(ctx, item.ID, errcode.UploadQuotaExceeded, truncateDetail(err.Error()))
	case errors.Is(err, channel.ErrNoHealthyChannel), errors.Is(err, upload.ErrInsufficientReplicas):
		s.cfg.Logger.Error("渠道不可用,导入中止", "item", item.ID, "err", err)
		return errcode.UploadUnavailable.String()
	default:
		s.cfg.Logger.Error("导入处理失败,任务中止", "item", item.ID, "err", err)
		return errcode.Internal.String()
	}
}

// markFailed 记下条目失败的原因。
// @param itemID 条目 ID
// @param code 原因码
// @param detail 根因原文
// @return 写表失败时的 internal(中止任务);否则为空串
func (s *Service) markFailed(ctx context.Context, itemID int64, code errcode.Code, detail string) string {
	err := s.cfg.Queries.MarkImportItemFailed(context.WithoutCancel(ctx), sqlc.MarkImportItemFailedParams{
		ID: itemID, ErrorCode: code.String(), ErrorDetail: detail,
	})
	if err != nil {
		s.cfg.Logger.Error("记录条目失败原因失败", "item", itemID, "err", err)
		return errcode.Internal.String()
	}
	return ""
}

// uploadOptions 按任务参数构造一张图的上传选项。
// @param job 任务行
// @param item 条目
// @return 上传选项
func (s *Service) uploadOptions(job sqlc.ImportJob, item sqlc.ListPendingImportItemsRow) upload.Options {
	policy := imaging.EXIFPolicy(job.ExifPolicy)
	if policy == "" {
		policy = s.cfg.Settings().EXIFPolicy
	}
	opts := upload.Options{
		UserID:         job.UserID,
		FileName:       urlfetch.FileName(item.Url),
		IdempotencyKey: importKey(job.ID, item.ID),
		EXIFPolicy:     policy,
	}
	if job.TtlSeconds.Valid {
		opts.TTLSet = true
		opts.TTL = time.Duration(job.TtlSeconds.Int64) * time.Second
	}
	return opts
}

// refererFor 决定抓取一张图时发送的 Referer:任务指定的优先,否则取条目的第一个来源页(须是网页地址),都没有则不发。
// @param job 任务行
// @param firstSource 条目的第一个来源;没有为空串
// @return Referer;空串表示不发
func refererFor(job sqlc.ImportJob, firstSource string) string {
	if job.Referer != "" {
		return job.Referer
	}
	if strings.HasPrefix(firstSource, "http://") || strings.HasPrefix(firstSource, "https://") {
		return firstSource
	}
	return ""
}

// classifyFetch 把抓取失败归为错误码与根因原文。
// @param err 抓取返回的错误
// @return 错误码
// @return 根因原文
func classifyFetch(err error) (errcode.Code, string) {
	detail := truncateDetail(err.Error())
	switch {
	case errors.Is(err, urlfetch.ErrScheme):
		return errcode.URLScheme, detail
	case errors.Is(err, urlfetch.ErrBlockedAddress):
		return errcode.URLBlocked, detail
	case errors.Is(err, urlfetch.ErrTooManyRedirects):
		return errcode.URLTooManyRedirects, detail
	case errors.Is(err, urlfetch.ErrTooLarge):
		return errcode.URLTooLarge, detail
	case errors.Is(err, urlfetch.ErrStatus):
		return errcode.URLBadStatus, detail
	default:
		return errcode.URLFetchFailed, detail
	}
}
