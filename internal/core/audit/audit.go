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
	// ActionChannelUpdate 渠道配置编辑。
	ActionChannelUpdate = "channel.update"
	// ActionChannelDelete 渠道删除。
	ActionChannelDelete = "channel.delete"
	// ActionChannelExport 渠道 rclone 配置导出(含凭证与解密密码)。
	ActionChannelExport = "channel.export"
	// ActionTokenIssue Token 签发。
	ActionTokenIssue = "token.issue"
	// ActionTokenRevoke Token 吊销。
	ActionTokenRevoke = "token.revoke"
	// ActionTokenRotate Token 轮转(同名新签发 + 旧吊销)。
	ActionTokenRotate = "token.rotate"
	// ActionTokenDelete 已失效 Token 记录删除。
	ActionTokenDelete = "token.delete"
	// ActionUserCreate 用户创建。
	ActionUserCreate = "user.create"
	// ActionUserUpdate 用户资料/密码变更。
	ActionUserUpdate = "user.update"
	// ActionUserDelete 用户删除。
	ActionUserDelete = "user.delete"
	// ActionImageDelete 图片引用删除。
	ActionImageDelete = "image.delete"
	// ActionImageUpdate 图片有效期/清理豁免修改。
	ActionImageUpdate = "image.update"
	// ActionImageRecover 按渠道里的对象找回图片记录。
	ActionImageRecover = "image.recover"
	// ActionDataExport 元数据导出。
	ActionDataExport = "data.export"
	// ActionBackupCreate 手动触发元数据库备份。
	ActionBackupCreate = "backup.create"
	// ActionBackupRestore 暂存快照待重启换入。
	ActionBackupRestore = "backup.restore"
	// ActionDedupMerge 相似候选人工合并。
	ActionDedupMerge = "dedup.merge"
	// ActionDedupDismiss 相似候选忽略。
	ActionDedupDismiss = "dedup.dismiss"
	// ActionRefererAdd 防盗链白名单规则添加。
	ActionRefererAdd = "referer.add"
	// ActionRefererRemove 防盗链白名单规则删除。
	ActionRefererRemove = "referer.remove"
	// ActionConfigUpdate 运营配置修改(热生效)。
	ActionConfigUpdate = "config.update"
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
