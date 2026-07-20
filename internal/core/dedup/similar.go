package dedup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/bits"
	"time"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// 相似裁决哨兵错误,调用方据此映射 HTTP 状态码。
var (
	ErrCandidateNotFound = errors.New("dedup: 候选不存在或已裁决")
	ErrCandidateStale    = errors.New("dedup: 候选涉及的物理图已不存在")
	ErrBadKeeper         = errors.New("dedup: 保留对象必须是候选对中的一方")
)

// ScanSimilarOnce 增量扫描 pHash 相似对,命中落候选表;返回本轮新增候选数。
// @Description: 每轮读全量在册 pHash(已排除待删),只把上轮水位之后的新 blob 与
// 其余全部比较——自部署量级内存暴力比对毫秒级完成,无需索引结构。距离 0 也入候选:
// pHash 相同不代表字节相同,自动合并仅限精确重复,其余一律人工裁决。
// 进程重启水位归零、全量重扫;已忽略的对经唯一约束吸收,不会重新出现。
// @return 新增候选数
// @return 扫描失败时的错误
func (m *Merger) ScanSimilarOnce(ctx context.Context) (int, error) {
	threshold := m.cfg.Settings().DedupPhashMaxDistance
	if threshold <= 0 {
		return 0, nil
	}
	rows, err := m.cfg.Queries.ListBlobPhashes(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("读取 pHash 全量: %w", err)
	}
	m.mu.Lock()
	watermark := m.lastScanned
	m.mu.Unlock()

	created := 0
	for i, blob := range rows {
		if blob.ID <= watermark {
			continue
		}
		for j := range i {
			dist := bits.OnesCount64(uint64(blob.Phash) ^ uint64(rows[j].Phash)) //nolint:gosec // 哈希按位比较,数值无语义
			if dist > threshold {
				continue
			}
			// OR IGNORE 吸收重扫已存在的对(含已忽略),按实际插入行数计数。
			n, err := m.cfg.Queries.CreateDedupCandidate(ctx, sqlc.CreateDedupCandidateParams{
				BlobA: rows[j].ID, BlobB: blob.ID, Distance: int64(dist),
			})
			if err != nil {
				return created, fmt.Errorf("写入相似候选: %w", err)
			}
			created += int(n)
		}
	}
	if len(rows) > 0 {
		m.mu.Lock()
		m.lastScanned = rows[len(rows)-1].ID
		m.mu.Unlock()
	}
	if created > 0 {
		m.cfg.Logger.Info("发现相似候选", "count", created)
	}
	return created, nil
}

// MergeCandidate 按人工裁决合并一个相似候选:保留 keep,另一方并入后打物理删标记。
// @Description: 与精确合并同一事务语义(改指向 + 并计数 + 标记待删);
// 完成后删除涉及被并 blob 的全部候选行——候选是工作队列,历史在 audit_log。
// 候选任一方已进入删除流程时拒绝,由调用方引导刷新列表。
// @param ctx 控制超时与取消
// @param candidateID 候选行 id
// @param keepBlobID 保留的物理图 id,必须是候选对中的一方
// @return ErrCandidateNotFound / ErrBadKeeper / ErrCandidateStale 或基础设施错误
func (m *Merger) MergeCandidate(ctx context.Context, candidateID, keepBlobID int64) error {
	cand, err := m.cfg.Queries.GetDedupCandidate(ctx, candidateID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrCandidateNotFound
	case err != nil:
		return fmt.Errorf("查询候选: %w", err)
	}
	if cand.Status != "pending" {
		return ErrCandidateNotFound
	}
	var loserID int64
	switch keepBlobID {
	case cand.BlobA:
		loserID = cand.BlobB
	case cand.BlobB:
		loserID = cand.BlobA
	default:
		return ErrBadKeeper
	}
	if _, err := m.aliveBlob(ctx, loserID); err != nil {
		return err
	}

	tx, err := m.cfg.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启合并事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := m.cfg.Queries.WithTx(tx)
	// 事务内复查保留对象存活:裁决与合并之间它自身引用可能全到期被减到 0 并标记物理删,
	// 此时并入会把引用改指向一个待删 blob。已入删除流程即视为候选失效,交前端刷新。
	keep, err := q.GetBlob(ctx, keepBlobID)
	if err != nil {
		return fmt.Errorf("复查保留对象: %w", err)
	}
	if keep.PendingDelete != 0 {
		return ErrCandidateStale
	}
	// 并入实际改指向的引用数,而非事务外快照——期间被并对象的引用可能已被到期回收,
	// 用快照会多计致保留对象引用计数永不归零(存储泄漏)。
	moved, err := q.RepointImages(ctx, sqlc.RepointImagesParams{BlobID: keepBlobID, BlobID_2: loserID})
	if err != nil {
		return fmt.Errorf("改指向保留对象: %w", err)
	}
	if err := q.AddBlobRef(ctx, sqlc.AddBlobRefParams{RefCount: moved, ID: keepBlobID}); err != nil {
		return fmt.Errorf("并入引用计数: %w", err)
	}
	grace := time.Duration(m.cfg.Settings().DeleteGraceMinutes) * time.Minute
	deleteAfter := sql.NullString{String: sqlc.TimeText(time.Now().Add(grace)), Valid: true}
	if err := q.MarkBlobMergedAway(ctx, sqlc.MarkBlobMergedAwayParams{DeleteAfter: deleteAfter, ID: loserID}); err != nil {
		return fmt.Errorf("标记被并对象待删: %w", err)
	}
	if err := q.DeleteDedupCandidatesByBlob(ctx, sqlc.DeleteDedupCandidatesByBlobParams{
		BlobA: loserID, BlobB: loserID,
	}); err != nil {
		return fmt.Errorf("清理被并对象候选: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交相似合并: %w", err)
	}
	m.cfg.Logger.Info("相似候选已合并", "candidate", candidateID, "keep", keepBlobID, "merged", loserID)
	return nil
}

// DismissCandidate 忽略一个相似候选,该对不再提示(行保留,唯一约束吸收重扫)。
// @param ctx 控制超时与取消
// @param candidateID 候选行 id
// @return ErrCandidateNotFound 或基础设施错误
func (m *Merger) DismissCandidate(ctx context.Context, candidateID int64) error {
	n, err := m.cfg.Queries.SetDedupCandidateStatus(ctx, sqlc.SetDedupCandidateStatusParams{
		Status: "dismissed", ID: candidateID,
	})
	if err != nil {
		return fmt.Errorf("忽略候选: %w", err)
	}
	if n == 0 {
		return ErrCandidateNotFound
	}
	return nil
}

// aliveBlob 读取仍在服务中的物理图;已进入删除流程视为候选失效。
// @param ctx 控制超时与取消
// @param blobID 物理图 id
// @return 物理图行
// @return ErrCandidateStale 或基础设施错误
func (m *Merger) aliveBlob(ctx context.Context, blobID int64) (sqlc.Blob, error) {
	blob, err := m.cfg.Queries.GetBlob(ctx, blobID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return sqlc.Blob{}, ErrCandidateStale
	case err != nil:
		return sqlc.Blob{}, fmt.Errorf("查询物理图: %w", err)
	}
	if blob.PendingDelete != 0 {
		return sqlc.Blob{}, ErrCandidateStale
	}
	return blob, nil
}
