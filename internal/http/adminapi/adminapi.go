// Package adminapi 实现管理面 API:渠道等管理资源的 CRUD 与审计留痕。
// 业务编排在 core 各服务,本包只做 HTTP 翻译。
package adminapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/referer"
	"github.com/x-haose/imgyoung/internal/http/middleware"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// ChannelAdmin 渠道管理操作(由 channel.Manager 实现)。
type ChannelAdmin interface {
	// Create 创建渠道并重载,含存量副本播种与创建后深探测。
	Create(ctx context.Context, p channel.CreateParams) (int64, error)
	// Update 编辑渠道(配置整体替换;encrypted 不可变),含重载与编辑后深探测。
	Update(ctx context.Context, channelID int64, p channel.UpdateParams) error
	// Delete 删除渠道(持有唯一副本时拒绝)。
	Delete(ctx context.Context, channelID int64) error
	// List 返回全部渠道描述(不含配置与凭证)。
	List(ctx context.Context) ([]channel.Info, error)
	// ProbeChannel 对已有渠道执行深探测并把结果落库。
	ProbeChannel(ctx context.Context, channelID int64) error
	// TestConfig 用未落库的配置执行深探测(测试连接),错误文案可直接展示。
	TestConfig(ctx context.Context, driverName string, config map[string]string) error
	// ConfigForEdit 返回渠道配置的编辑视图(敏感值哨兵掩码,含模板表单字段还原)。
	ConfigForEdit(ctx context.Context, channelID int64) (channel.EditConfig, error)
	// RestoreSecrets 把配置中的哨兵值还原为指定渠道已存配置的同键原值。
	RestoreSecrets(ctx context.Context, channelID int64, config map[string]string) (map[string]string, error)
}

// ReplicationKicker 副本补齐管道的唤醒入口(由 replication.Runner 实现)。
type ReplicationKicker interface {
	// Reconcile 扫描全部未完成副本并重新入队(幂等,按参数去重)。
	Reconcile(ctx context.Context) error
}

// ImageAdmin 图片管理删除入口(由 lifecycle.Sweeper 实现)。
type ImageAdmin interface {
	// DeleteImage 立即删除一条引用,级联由清理任务完成。
	DeleteImage(ctx context.Context, imageID string) error
}

// Config Handlers 的全部依赖与参数。
type Config struct {
	Admin              ChannelAdmin      // 渠道管理操作
	Replicator         ReplicationKicker // 补齐管道唤醒口;nil 表示无补齐子系统(如部分测试装配)
	EncryptNewChannels bool              // 新建渠道未指定 encrypted 时的全局默认(创建时固化)
	Auth               *auth.Service     // 认证服务(登录/会话/用户/Token 管理)
	Images             ImageAdmin        // 图片删除入口
	Dedup              DedupAdmin        // 相似候选裁决执行口
	Queries            *sqlc.Queries     // 列表与统计查询
	Audit              *audit.Recorder   // 管理操作审计记录器
	Referer            *referer.Service  // 防盗链白名单管理(增删热生效)
	Runtime            RuntimeConfigView // 只读运行配置切面,GET /config 原样返回
	Full               FullConfigView    // 全量非敏感配置切面,GET /config/full 原样返回(管理员)
	Logger             *slog.Logger      // 结构化日志
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
// @param ctx 请求上下文
// @param action 动作标识(见 audit 包常量)
// @param target 操作目标标识
// @param detail 附加说明
func (h *Handlers) recordAudit(ctx context.Context, action, target, detail string) {
	actor := sqlc.DefaultUserID
	if info, ok := middleware.SessionFrom(ctx); ok {
		actor = info.Principal.UserID
	}
	if err := h.cfg.Audit.Record(ctx, actor, action, target, detail); err != nil {
		h.cfg.Logger.Warn("审计记录失败", "action", action, "target", target, "err", err)
	}
}

// 列表分页参数边界。
const (
	defaultPageSize = 20  // 未指定 page_size 时的默认值
	maxPageSize     = 100 // page_size 上限,防单请求拉全表
)

// pagination 解析 page / page_size 查询参数为 LIMIT / OFFSET。
// @param r 请求
// @return limit 与 offset
func pagination(r *http.Request) (limit, offset int64) {
	q := r.URL.Query()
	page, _ := strconv.ParseInt(q.Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	size, _ := strconv.ParseInt(q.Get("page_size"), 10, 64)
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return size, (page - 1) * size
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
