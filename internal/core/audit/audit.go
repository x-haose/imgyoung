// Package audit 记录管理操作留痕。
// 机器发现、人做不可逆决定的配套:删除/合并/渠道变更/Token 事件均在此留痕,后台可查。
package audit

import (
	"context"
	"fmt"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// 动作标识常量(action 列取值),形如 "领域.动作"。
const (
	// ActionChannelCreate 渠道创建。
	ActionChannelCreate = "channel.create"
	// ActionChannelDelete 渠道删除。
	ActionChannelDelete = "channel.delete"
)

// Recorder 审计记录器,追加写 audit_log。
type Recorder struct {
	q *sqlc.Queries // sqlc 查询句柄
}

// New 创建审计记录器。
// @param q 查询句柄
// @return 就绪的记录器
func New(q *sqlc.Queries) *Recorder { return &Recorder{q: q} }

// Record 追加一条审计记录。
// @Description: 调用方决定失败是否阻断主操作;管理操作通常以 best-effort 记录,
// 审计写入失败只告警不回滚(留痕缺失优于因日志失败而拒绝合法管理操作)。
// @param ctx 控制超时与取消
// @param actor 操作者 user id
// @param action 动作标识(见本包常量)
// @param target 操作目标标识(渠道名/id 等),可空
// @param detail 附加说明,可空
// @return 写入失败时的错误
func (r *Recorder) Record(ctx context.Context, actor, action, target, detail string) error {
	if err := r.q.CreateAuditLog(ctx, sqlc.CreateAuditLogParams{
		Actor: actor, Action: action, Target: target, Detail: detail,
	}); err != nil {
		return fmt.Errorf("写入审计记录: %w", err)
	}
	return nil
}
