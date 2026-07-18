// Package publicapi 实现对外数据面:图片上传与公开访问(代理/302/按需处理)。
// 业务编排在 core 各服务,本包只做 HTTP 翻译。
package publicapi

import (
	"context"
	"log/slog"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/core/variants"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// BlobChannels 按 blob 提供可服务的渠道候选(由 channel.Manager 实现)。
type BlobChannels interface {
	// CandidatesForBlob 返回健康且副本就绪的渠道,按优先级降序。
	CandidatesForBlob(ctx context.Context, blobID int64) ([]channel.Handle, error)
}

// AccessTracker 记录公开访问活跃度(由 lifecycle.Tracker 实现);仅内存,极轻量。
type AccessTracker interface {
	// Touch 记录一次对该引用的公开访问。
	Touch(imageID string)
}

// Config Handlers 的全部依赖与参数。
type Config struct {
	Queries           *sqlc.Queries      // sqlc 生成的类型安全查询集(访问路径用)
	Channels          BlobChannels       // 访问路径的渠道候选来源(容灾切换依据)
	RedirectMode      bool               // 302 省流量模式:候选渠道允许直链时重定向,否则回退代理
	Upload            *upload.Service    // 上传编排服务
	MaxUploadBytes    int64              // 单图字节上限(内存三道闸之一)
	MaxPixels         int64              // 像素预算(w×h 上限);仅用于超限错误文案,真校验在 upload.Service
	BaseURL           string             // 对外站点地址(无尾部斜杠);为空时上传响应按请求推导绝对地址
	DefaultEXIFPolicy imaging.EXIFPolicy // 全局默认 EXIF 档位;单次上传的表单字段可覆盖;档位决策在本层完成,传给 Service 的必须是明确档位
	Activity          AccessTracker      // 公开访问活跃度追踪器
	Variants          *variants.Service  // 按需处理服务;为 nil 表示不启用(仅代理原图)
	ProcessWidths     []int              // 允许的宽度档白名单,档外请求 400
	ProcessFormats    []string           // 允许的目标格式白名单,档外请求 400
	Logger            *slog.Logger       // 结构化日志
}

// Handlers 数据面处理器集,自身无可变状态、并发安全。
type Handlers struct {
	cfg Config
}

// New 创建数据面处理器集。
// @param cfg 依赖与参数,字段均必填
// @return 就绪的处理器集
func New(cfg Config) *Handlers {
	return &Handlers{cfg: cfg}
}
