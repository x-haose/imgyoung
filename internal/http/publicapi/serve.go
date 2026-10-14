package publicapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// HandleServe 按短 ID 代理输出图片(对外公开入口,计公开访问活跃度)。
func (h *Handlers) HandleServe(w http.ResponseWriter, r *http.Request) {
	h.serveImage(w, r, true)
}

// HandleAdminServe 后台缩略图/预览入口:与公开入口同一处理管线,但不计公开访问活跃度。
// @Description: 管理员浏览若计数,会把冷图摘出待清理列表(TTL 续期时还顺延到期)、
// 抬高访问统计与热门榜,故后台展示一律走此会话鉴权通道;对外分享仍用 /i/。
func (h *Handlers) HandleAdminServe(w http.ResponseWriter, r *http.Request) {
	h.serveImage(w, r, false)
}

// serveImage 按短 ID 输出图片:查引用、可选计活跃度、按参数走变体或原图路径。
// @param w 响应写入器
// @param r 请求
// @param touch 是否记公开访问活跃度(仅对外公开入口计,后台入口不计)
func (h *Handlers) serveImage(w http.ResponseWriter, r *http.Request, touch bool) {
	h.serveImageID(w, r, chi.URLParam(r, "id"), touch)
}

// serveImageID 按已解析出的短 ID 输出图片。
// @Description: 短 ID 入口与 S3 对象键入口只在"怎么定位这张图"上不同,定位之后的
// 活跃度、变体协商与容灾读取必须一致,故收敛于此。
// @param imageID 对外短 ID
// @param touch 是否记公开访问活跃度
func (h *Handlers) serveImageID(w http.ResponseWriter, r *http.Request, imageID string, touch bool) {
	row, err := h.cfg.Queries.GetImageBlob(r.Context(), imageID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.cfg.Logger.Error("查询引用记录失败", "id", imageID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	if touch {
		// 真实公开访问:记活跃度(仅内存,批量落库)。
		h.cfg.Activity.Touch(imageID)
	}

	// 按需处理:解析档位参数,命中则走处理路径(即便 redirect 省流量模式也强制代理)。
	if h.cfg.Variants != nil {
		req, perr := h.parseVariantRequest(r, row.Mime)
		if perr != nil {
			httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, perr.Error())
			return
		}
		if req.process {
			h.serveVariant(w, r, row, req)
			return
		}
	}
	h.serveOriginal(w, r, row, false)
}

// serveOriginal 代理或重定向原图字节:ETag/304 协商、302 省流量、按渠道优先级容灾读取。
// @Description: 非降级路径内容不可变,以 sha256 作强 ETag,If-None-Match 命中直接 304。
// 降级路径(处理失败回退)服务的是全尺寸原图而非目标衍生图,不能按 URL 强缓存或 302 直链,
// 否则"临时降级"会被当作规范内容永久绑定到带处理参数的 URL(缓存投毒),故跳过 304/302,改 no-store。
// @param w 响应写入器
// @param r 请求
// @param row 引用与物理图信息
// @param degraded 是否为按需处理失败后的回退输出
func (h *Handlers) serveOriginal(w http.ResponseWriter, r *http.Request, row sqlc.GetImageBlobRow, degraded bool) {
	etag := `"` + row.Sha256 + `"`
	if !degraded && r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", cacheImmutable)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	candidates, err := h.cfg.Channels.CandidatesForBlob(r.Context(), row.BlobID)
	if err != nil {
		h.cfg.Logger.Error("查询渠道候选失败", "id", row.ID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	if len(candidates) == 0 {
		// 引用存在但当前无健康渠道持有就绪副本;一致性由对账任务兜底。
		h.cfg.Logger.Warn("无可用渠道服务该图", "id", row.ID)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return
	}
	// 302 省流量模式:候选中允许直链且非加密的渠道直接重定向;无直链候选则回退代理。
	if !degraded && h.cfg.Settings().AccessMode == "redirect" &&
		redirectToDirectLink(w, r, candidates, row.StorageKey) {
		return
	}

	rc := h.openFirstReadable(r.Context(), candidates, row)
	if rc == nil {
		h.cfg.Logger.Error("全部候选渠道读取失败", "id", row.ID, "key", row.StorageKey)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil {
			h.cfg.Logger.Debug("关闭渠道对象失败", "id", row.ID, "err", cerr)
		}
	}()

	writeOriginalHeaders(w.Header(), row, degraded)
	if _, err := io.Copy(w, rc); err != nil {
		// 多为客户端提前断开,记 debug 即可。
		h.cfg.Logger.Debug("输出图片中断", "id", row.ID, "err", err)
	}
}

// redirectToDirectLink 向第一个允许直链、未加密且配置了公开地址的候选渠道发 302;无此候选报告 false。
// @param w 响应写入器
// @param r 请求
// @param candidates 候选渠道,按优先级降序
// @param storageKey 对象键
// @return 是否已重定向
func redirectToDirectLink(w http.ResponseWriter, r *http.Request, candidates []channel.Handle, storageKey string) bool {
	for _, cand := range candidates {
		if cand.AllowDirectLink && !cand.Encrypted && cand.PublicBaseURL != "" {
			http.Redirect(w, r, cand.PublicBaseURL+"/"+storageKey, http.StatusFound)
			return true
		}
	}
	return false
}

// openFirstReadable 按候选优先级逐个尝试打开对象,返回第一个成功的读取流;全部失败返回 nil。
// @param ctx 上下文
// @param candidates 候选渠道,按优先级降序
// @param row 引用与物理图信息
// @return 读取流,调用方负责关闭;nil 表示无候选可读
func (h *Handlers) openFirstReadable(ctx context.Context, candidates []channel.Handle,
	row sqlc.GetImageBlobRow) io.ReadCloser {
	for _, cand := range candidates {
		rc, err := cand.Driver.Open(ctx, row.StorageKey)
		if err == nil {
			return rc
		}
		h.cfg.Logger.Warn("渠道读取失败,切换下一候选", "id", row.ID, "channel", cand.Name, "err", err)
	}
	return nil
}

// writeOriginalHeaders 设置原图响应头:降级响应 no-store;正常响应内容不可变(URL 即版本)缓存一年并带强 ETag。
// @Description: nosniff 强制浏览器信任服务端类型判定。
// @param hd 响应头
// @param row 引用与物理图信息
// @param degraded 是否为降级输出
func writeOriginalHeaders(hd http.Header, row sqlc.GetImageBlobRow, degraded bool) {
	hd.Set("Content-Type", row.Mime)
	hd.Set("Content-Length", strconv.FormatInt(row.Size, 10))
	hd.Set("X-Content-Type-Options", "nosniff")
	if degraded {
		hd.Set("Cache-Control", "no-store")
		return
	}
	hd.Set("Cache-Control", cacheImmutable)
	hd.Set("ETag", `"`+row.Sha256+`"`)
}
