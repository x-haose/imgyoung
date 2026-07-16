// Package adminapi 实现管理面 API:渠道等管理资源的 CRUD 与审计留痕。
// 业务编排在 core 各服务,本包只做 HTTP 翻译。
package adminapi

import (
	"context"
	"log/slog"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// ChannelAdmin 渠道管理操作(由 channel.Manager 实现)。
type ChannelAdmin interface {
	// Create 创建渠道并重载。
	Create(ctx context.Context, p channel.CreateParams) (int64, error)
	// Delete 删除渠道(持有唯一副本时拒绝)。
	Delete(ctx context.Context, channelID int64) error
	// List 返回全部渠道描述(不含配置与凭证)。
	List(ctx context.Context) ([]channel.Info, error)
}

// Config Handlers 的全部依赖与参数。
type Config struct {
	Admin              ChannelAdmin    // 渠道管理操作
	EncryptNewChannels bool            // 新建渠道未指定 encrypted 时的全局默认(创建时固化)
	Audit              *audit.Recorder // 管理操作审计记录器
	Logger             *slog.Logger    // 结构化日志
}

// Handlers 管理面处理器集,自身无可变状态、并发安全。
type Handlers struct {
	cfg Config
}

// New 创建管理面处理器集。
// @param cfg 依赖与参数,字段均必填
// @return 就绪的处理器集
func New(cfg Config) *Handlers {
	return &Handlers{cfg: cfg}
}

// recordAudit best-effort 记录管理操作;审计写入失败只告警,不阻断主操作
// (留痕缺失优于因日志失败而拒绝合法管理操作)。单用户期操作者恒为默认用户。
// @param ctx 请求上下文
// @param action 动作标识(见 audit 包常量)
// @param target 操作目标标识
// @param detail 附加说明
func (h *Handlers) recordAudit(ctx context.Context, action, target, detail string) {
	if err := h.cfg.Audit.Record(ctx, sqlc.DefaultUserID, action, target, detail); err != nil {
		h.cfg.Logger.Warn("审计记录失败", "action", action, "target", target, "err", err)
	}
}
