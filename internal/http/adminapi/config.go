package adminapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/core/auth"
	"github.com/x-haose/imgyoung/internal/core/replication"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/config"
)

// RuntimeConfigView 面向后台 UI 的只读运行配置切面,逐请求从当前快照摘取。
// 只含 UI 呈现所需的键;凭证/密钥类一律不进此结构。
type RuntimeConfigView struct {
	ColdCleanupDays    int    `json:"cold_cleanup_days"`    // 冷图待清理阈值(天);0 表示未启用
	DeleteGraceMinutes int    `json:"delete_grace_minutes"` // 引用归零到物理删的宽限(分钟);0 表示立即
	ProcessWidths      []int  `json:"process_widths"`       // 按需处理允许的宽度档白名单(像素);空表示不允许缩放
	DefaultTTLHours    int    `json:"default_ttl_hours"`    // 全局默认 TTL(小时);0 表示永不过期
	TTLRenewOnAccess   bool   `json:"ttl_renew_on_access"`  // 公开访问是否顺延带 TTL 图片的到期时刻
	MaxUploadMB        int64  `json:"max_upload_mb"`        // 单图上限(MB)
	BaseURL            string `json:"base_url"`             // 对外站点地址;空则按请求域名推导绝对地址
	AccessMode         string `json:"access_mode"`          // 访问模式:proxy / redirect
	EXIFPolicy         string `json:"exif_policy"`          // 全局 EXIF 档位:passthrough / strip
}

// HandleConfig 返回生效运行配置的只读切面,供前端呈现阈值文案与档位。
// @Description: 挂在会话门内对全部登录用户开放——上传上限、删除宽限、缩略档位
// 对普通用户的操作反馈同样必要,且不含任何凭证/密钥类配置。
func (h *Handlers) HandleConfig(w http.ResponseWriter, _ *http.Request) {
	set := h.cfg.Settings.Current()
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, RuntimeConfigView{
		ColdCleanupDays:    set.ColdCleanupDays,
		DeleteGraceMinutes: set.DeleteGraceMinutes,
		ProcessWidths:      set.ProcessWidths,
		DefaultTTLHours:    set.DefaultTTLHours,
		TTLRenewOnAccess:   set.TTLRenewOnAccess,
		MaxUploadMB:        set.MaxUploadMB,
		BaseURL:            set.BaseURL,
		AccessMode:         set.AccessMode,
		EXIFPolicy:         string(set.EXIFPolicy),
	})
}

// BootstrapView 引导配置的只读展示切面;密钥与密码类引导项绝不进任何响应。
type BootstrapView struct {
	Listen         string `json:"listen"`          // HTTP 监听地址
	DataDir        string `json:"data_dir"`        // 数据目录
	TrustedProxies string `json:"trusted_proxies"` // 信任其转发头的反向代理网段,逗号分隔
	MetricsAllow   string `json:"metrics_allow"`   // 允许抓取 /metrics 的网段,逗号分隔
}

// DerivedView 派生/编译期常量的展示切面,帮助自助排障但不可编辑。
type DerivedView struct {
	CDNPurgeEnabled          bool `json:"cdn_purge_enabled"`          // 删除级联的 CDN purge 是否生效(zone、token、base_url 三者齐备)
	CDNAPITokenSet           bool `json:"cdn_api_token_set"`          // Cloudflare API Token 是否已配置(值本身脱敏)
	LoginBurst               int  `json:"login_burst"`                // 每个 IP / 用户名允许的突发登录尝试次数
	LoginRefillSeconds       int  `json:"login_refill_seconds"`       // 突发额度用尽后每恢复一次尝试的间隔(秒)
	ReconcileIntervalSeconds int  `json:"reconcile_interval_seconds"` // 副本对账间隔(秒)
}

// FullConfigView 设置页「运行配置」的完整切面:引导项只读展示,
// 运营项(settings 键即字段名)可经 PUT /config 修改热生效,凭证值脱敏。
type FullConfigView struct {
	Bootstrap BootstrapView   `json:"bootstrap"` // 引导配置(环境变量,改需重启)
	Settings  config.Settings `json:"settings"`  // 运营配置当前生效值(凭证字段已抹空)
	Derived   DerivedView     `json:"derived"`   // 派生状态与编译期常量
}

// fullConfigView 组装当前生效配置的完整切面。
// @return 完整切面
func (h *Handlers) fullConfigView() FullConfigView {
	set := h.cfg.Settings.Current()
	return FullConfigView{
		Bootstrap: BootstrapView{
			Listen: h.cfg.Boot.Listen, DataDir: h.cfg.Boot.DataDir,
			TrustedProxies: config.FormatPrefixes(h.cfg.Boot.TrustedProxies),
			MetricsAllow:   config.FormatPrefixes(h.cfg.Boot.MetricsAllow),
		},
		Settings: set.Masked(),
		Derived: DerivedView{
			CDNPurgeEnabled:          set.CDNPurgeEnabled(),
			CDNAPITokenSet:           set.CDNCloudflareAPIToken != "",
			LoginBurst:               auth.LoginBurst,
			LoginRefillSeconds:       int(auth.LoginRefillEvery / time.Second),
			ReconcileIntervalSeconds: int(replication.ReconcileInterval / time.Second),
		},
	}
}

// HandleConfigFull 返回全量生效配置(仅管理员路由),设置页展示与编辑回显用。
func (h *Handlers) HandleConfigFull(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, h.fullConfigView())
}

// HandleConfigUpdate 应用一组运营配置修改:校验 → 落库 → 热生效 → 审计,返回更新后的完整切面。
// @Description: 请求体为 键 → 新值 的 JSON 对象,只动出现的键;凭证键(cdn_cloudflare_api_token)
// 传空串表示清除、不传表示不动。未知键与非法取值整体拒绝(400),不做部分生效。
func (h *Handlers) HandleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil || len(patch) == 0 {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "请求体需为非空的 键→值 JSON 对象")
		return
	}
	changes, err := h.cfg.Settings.Update(r.Context(), patch)
	switch {
	case errors.Is(err, config.ErrUnknownKey), errors.Is(err, config.ErrInvalidSetting):
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		h.cfg.Logger.Error("配置修改失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "配置修改失败")
		return
	}
	if len(changes) > 0 {
		h.recordAudit(r.Context(), audit.ActionConfigUpdate, "settings", changeDetail(changes))
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, h.fullConfigView())
}

// changeDetail 把变更清单拼成审计详情文本(凭证值已由存储层脱敏)。
// @param changes 变更清单
// @return "键: 旧值 → 新值" 的分号拼接
func changeDetail(changes []config.Change) string {
	parts := make([]string, 0, len(changes))
	for _, ch := range changes {
		parts = append(parts, fmt.Sprintf("%s: %s → %s", ch.Key, ch.Old, ch.New))
	}
	return strings.Join(parts, "; ")
}
