package adminapi

import (
	"net/http"

	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// RuntimeConfigView 面向后台 UI 的只读运行配置切面,启动时由装配根从进程配置摘取。
// 只含 UI 呈现所需的键;凭证/密钥类一律不进此结构。
type RuntimeConfigView struct {
	ColdCleanupDays    int    `json:"cold_cleanup_days"`    // 冷图待清理阈值(天);0 表示未启用
	DeleteGraceMinutes int    `json:"delete_grace_minutes"` // 引用归零到物理删的宽限(分钟);0 表示立即
	ProcessWidths      []int  `json:"process_widths"`       // 按需处理允许的宽度档白名单(像素);空表示不允许缩放
	DefaultTTLHours    int    `json:"default_ttl_hours"`    // 全局默认 TTL(小时);0 表示永不过期
	TTLRenewOnAccess   bool   `json:"ttl_renew_on_access"`  // 公开访问是否顺延带 TTL 图片的到期时刻
	MaxUploadMB        int64  `json:"max_upload_mb"`        // 单图上限(MB)
	BaseURL            string `json:"base_url"`             // 对外站点地址;空表示返回相对路径
	AccessMode         string `json:"access_mode"`          // 访问模式:proxy / redirect
	EXIFPolicy         string `json:"exif_policy"`          // 全局 EXIF 档位:passthrough / strip
}

// HandleConfig 返回生效运行配置的只读切面,供前端呈现阈值文案与档位。
// @Description: 挂在会话门内对全部登录用户开放——上传上限、删除宽限、缩略档位
// 对普通用户的操作反馈同样必要,且不含任何凭证/密钥类配置。
func (h *Handlers) HandleConfig(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, h.cfg.Runtime)
}
