// Package adminapi 实现管理面 API:渠道等管理资源的 CRUD 与审计留痕。
// 业务编排在 core 各服务,本包只做 HTTP 翻译。
package adminapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/lifecycle"
	"github.com/x-haose/imgyoung/internal/core/recovery"
	"github.com/x-haose/imgyoung/internal/core/referer"
	"github.com/x-haose/imgyoung/internal/core/reliability"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// ChannelAdmin 渠道管理操作(由 channel.Manager 实现)。
type ChannelAdmin interface {
	// Create 创建渠道并重载,含存量副本播种与创建后深探测。
	Create(ctx context.Context, p channel.CreateParams) (int64, error)
	// Update 编辑渠道(配置整体替换;encrypted 不可变),含重载与编辑后深探测。
	Update(ctx context.Context, channelID int64, p channel.UpdateParams) error
	// Delete 删除渠道(持有唯一副本时拒绝);purge 为 true 只打清理中标记,对象由排空任务删除。
	Delete(ctx context.Context, channelID int64, purge bool) error
	// List 返回全部渠道描述(不含配置与凭证)。
	List(ctx context.Context) ([]channel.Info, error)
	// ProbeChannel 对已有渠道执行深探测并把结果落库。
	ProbeChannel(ctx context.Context, channelID int64) error
	// TestConfig 用未落库的配置执行深探测(测试连接),返回失败结论与须交还表单的刷新后令牌。
	TestConfig(ctx context.Context, p channel.TestParams) (channel.TestResult, error)
	// ConfigForEdit 返回渠道配置的编辑视图(敏感值哨兵掩码,含模板表单字段还原)。
	ConfigForEdit(ctx context.Context, channelID int64) (channel.EditConfig, error)
	// RcloneConfig 生成渠道的 rclone 配置文件(含凭证;加密渠道含 crypt 远端与解密密码),文件头注释用 lang。
	RcloneConfig(ctx context.Context, channelID int64, lang channel.Language) (channel.RcloneConfigFile, error)
	// OAuthStartParams 汇总发起授权所需的后端名与参数;编辑场景还原字段中的哨兵。
	OAuthStartParams(ctx context.Context, channelID int64, templateID string,
		inputs map[string]string) (string, map[string]string, error)
}

// ReplicationKicker 副本补齐管道的唤醒入口(由 replication.Runner 实现)。
type ReplicationKicker interface {
	// Reconcile 扫描全部未完成副本并重新入队(幂等,按参数去重)。
	Reconcile(ctx context.Context) error
	// EnqueueChannelDrain 为清理中的渠道入队排空任务(幂等,按参数去重)。
	EnqueueChannelDrain(ctx context.Context, channelID int64) error
}

// ImageAdmin 图片的修改与删除入口(由 lifecycle.Sweeper 实现)。
type ImageAdmin interface {
	// UpdateImage 在单事务内修改一条引用的有效期与清理豁免;返回 lifecycle.ErrImageNotFound / ErrImageExpired。
	UpdateImage(ctx context.Context, imageID string, patch lifecycle.ImagePatch) error
	// DeleteImage 立即删除一条引用,级联由清理任务完成。
	DeleteImage(ctx context.Context, imageID string) error
}

// BackupAdmin 元数据库备份的管理入口(由 reliability.Service 实现)。
type BackupAdmin interface {
	// List 列出各健康渠道上快照的并集,最新在前,每份带持有它的渠道。
	List(ctx context.Context) ([]reliability.Snapshot, error)
	// Backup 立即备份一次,写到每个健康渠道并返回新快照;任一渠道未写成即返回错误。备份归备份服务执行,
	// 不随请求断开而中止,停机时中止;本方法等到备份结束。
	Backup() (reliability.Snapshot, error)
	// Open 按优先级从持有快照的渠道打开它供下载,返回字节流与大小。
	Open(ctx context.Context, name string) (io.ReadCloser, int64, error)
	// Restore 把快照取回、核对完整后暂存到数据目录,等进程重启换入;由备份服务执行,ctx(发起恢复的请求)
	// 在暂存完成前结束即中止并撤回,停机时中止。
	Restore(ctx context.Context, name string) (string, error)
}

// RecoveryAdmin 按渠道里的对象找回图片记录的管理入口(由 recovery.Service 实现)。
type RecoveryAdmin interface {
	// Recover 找回健康渠道上缺少记录的图片,归属 userID;ctx 取消即中止,已登记的保留。
	Recover(ctx context.Context, userID string) (recovery.Result, error)
}

// SettingsStore 运营配置存储口(由 config.Store 实现):读取当前快照、应用修改热生效。
type SettingsStore interface {
	// Current 返回当前生效配置快照(切片字段只读)。
	Current() config.Settings
	// Update 应用一组按键的修改并热生效,返回实际变化清单(凭证值已脱敏)。
	Update(ctx context.Context, patch map[string]json.RawMessage) ([]config.Change, error)
}

// Config Handlers 的全部依赖与参数。
type Config struct {
	Admin      ChannelAdmin           // 渠道管理操作
	OAuth      *channel.OAuthSessions // 网盘渠道进行中的授权会话
	Replicator ReplicationKicker      // 补齐管道唤醒口;nil 表示无补齐子系统(如部分测试装配)
	Auth       *auth.Service          // 认证服务(登录/会话/用户/Token 管理)
	Images     ImageAdmin             // 图片修改与删除入口
	Dedup      DedupAdmin             // 相似候选裁决执行口
	Queries    *sqlc.Queries          // 列表与统计查询
	Audit      *audit.Recorder        // 管理操作审计记录器
	Referer    *referer.Service       // 防盗链白名单管理(增删热生效)
	Settings   SettingsStore          // 运营配置存储:读取快照供各请求取值,PUT /config 经它落库热生效
	Boot       config.Bootstrap       // 引导配置(只读展示用;密钥与密码不进任何响应)
	Backups    BackupAdmin            // 备份管理入口
	Recovery   RecoveryAdmin          // 图片记录找回入口
	Imports    ImportAdmin            // 批量导入的命令入口
	Shutdown   func()                 // 触发整个进程优雅停机;恢复快照后调用,由进程管理器拉起时换入
	Logger     *slog.Logger           // 结构化日志
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
// (留痕缺失优于因日志失败而拒绝合法管理操作)。操作者取自请求上下文的会话主体。
// 写入脱离请求的取消:操作已经完成,客户端此时断开不该丢掉这条留痕。
// @param ctx 请求上下文
// @param action 动作标识(见 audit 包常量)
// @param target 操作目标标识
// @param detail 附加说明
func (h *Handlers) recordAudit(ctx context.Context, action, target, detail string) {
	actor := sqlc.DefaultUserID
	if info, ok := middleware.SessionFrom(ctx); ok {
		actor = info.Principal.UserID
	}
	if err := h.cfg.Audit.Record(context.WithoutCancel(ctx), actor, action, target, detail); err != nil {
		h.cfg.Logger.Warn("审计记录失败", "action", action, "target", target, "err", err)
	}
}

// listResponse 列表统一信封,total 供前端分页。
type listResponse struct {
	Items any   `json:"items"` // 当前页数据
	Total int64 `json:"total"` // 过滤后的总条数
}

// sessionPrincipal 取出请求会话主体;路由保证存在,缺失属装配错误。
// @param r 请求
// @return 会话主体与是否存在
func (h *Handlers) sessionPrincipal(r *http.Request) (auth.Principal, bool) {
	info, ok := middleware.SessionFrom(r.Context())
	if !ok {
		h.cfg.Logger.Error("管理请求缺少会话主体,检查路由装配", "path", r.URL.Path)
	}
	return info.Principal, ok
}
