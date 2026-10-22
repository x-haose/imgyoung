package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// Actor 发起操作的用户。
type Actor struct {
	UserID string // 用户 id
	Admin  bool   // 是否管理员:管理员能看到并取消、删除所有人的任务
}

// ErrNotOwner 任务属于别的用户,这项操作只有创建者能做。
var ErrNotOwner = errors.New("importer: 只有创建任务的用户能执行此操作")

// access 读取任务并确认操作者可以访问它;不可访问的任务按不存在处理,不泄露存在性。
// @Description: 创建者可以访问自己的任务。管理员可以访问所有人的任务,但 ownerOnly 的操作(确认、重试)
// 只有创建者能做,管理员得到 ErrNotOwner。还在分批写入的 creating 任务对任何人都不存在。
// @param q 查询集,可绑定到事务
// @param actor 操作者
// @param jobID 任务 ID
// @param ownerOnly 操作是否只有创建者能做
// @return 任务行
// @return ErrNotFound、ErrNotOwner 或查询失败的错误
func access(ctx context.Context, q *sqlc.Queries, actor Actor, jobID string, ownerOnly bool) (sqlc.ImportJob, error) {
	job, err := q.GetImportJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status == StatusCreating) {
		return sqlc.ImportJob{}, ErrNotFound
	}
	if err != nil {
		return sqlc.ImportJob{}, fmt.Errorf("读取导入任务: %w", err)
	}
	switch {
	case job.UserID == actor.UserID:
		return job, nil
	case !actor.Admin:
		return sqlc.ImportJob{}, ErrNotFound
	case ownerOnly:
		return sqlc.ImportJob{}, ErrNotOwner
	default:
		return job, nil
	}
}

// Start 确认并开始导入:所选域名下的候选条目转为待转存,其余记为跳过,任务进入 importing。
// @param actor 操作者
// @param jobID 任务 ID
// @param hosts 要导入的域名
// @return ErrNotFound、ErrNotOwner、*StateError(任务不在 ready)、ErrNoSelection(所选域名下没有候选条目);其他为基础设施错误
func (s *Service) Start(ctx context.Context, actor Actor, jobID string, hosts []string) error {
	if len(hosts) > maxStartHosts {
		return &InputError{Detail: fmt.Sprintf("hosts 超过 %d 个", maxStartHosts)}
	}
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		job, err := access(ctx, q, actor, jobID, true)
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

// Cancel 取消 scanning、ready 或 importing 的任务;正在转存的图片被中断、条目保持 pending,已导入的保留。管理员可以取消任何人的任务。
// @param actor 操作者
// @param jobID 任务 ID
// @return ErrNotFound、*StateError(任务已经结束);其他为基础设施错误
func (s *Service) Cancel(ctx context.Context, actor Actor, jobID string) error {
	if _, err := access(ctx, s.cfg.Queries, actor, jobID, false); err != nil {
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
// @param actor 操作者
// @param jobID 任务 ID
// @return ErrNotFound、ErrNotOwner、*StateError、ErrNothingToRetry(没有失败或未处理的条目);其他为基础设施错误
func (s *Service) Retry(ctx context.Context, actor Actor, jobID string) error {
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		job, err := access(ctx, q, actor, jobID, true)
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

// Delete 删除任务及其条目、来源;已导入的图片不受影响。scanning 与 importing 的任务须先取消。管理员可以删除任何人的任务。
// @param actor 操作者
// @param jobID 任务 ID
// @return ErrNotFound、*StateError(任务仍在进行);其他为基础设施错误
func (s *Service) Delete(ctx context.Context, actor Actor, jobID string) error {
	if _, err := access(ctx, s.cfg.Queries, actor, jobID, false); err != nil {
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
