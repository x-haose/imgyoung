package publicapi

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

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
	imageID := chi.URLParam(r, "id")

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
		params, want, ok, varyAccept := h.parseVariantRequest(r, row.Mime)
		if !ok {
			httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "非法的处理参数:宽度或格式不在允许档位")
			return
		}
		if want {
			h.serveVariant(w, r, row, params, varyAccept)
			return
		}
	}
	h.serveOriginal(w, r, row, false)
}

// serveOriginal 代理或重定向原图字节:ETag/304 协商、302 省流量、按渠道优先级容灾读取。
// @param w 响应写入器
// @param r 请求
// @param row 引用与物理图信息
func (h *Handlers) serveOriginal(w http.ResponseWriter, r *http.Request, row sqlc.GetImageBlobRow, degraded bool) {
	imageID := row.ID
	// 非降级路径:内容不可变,以 sha256 作强 ETag;If-None-Match 命中直接 304。
	// 降级路径(处理失败回退):服务的是全尺寸原图而非目标衍生图,绝不能按 URL 强缓存/302 直链,
	// 否则会把"临时降级"当"规范内容"永久绑定到带处理参数的 URL(缓存投毒),故跳过 304/302,改 no-store。
	if !degraded {
		etag := `"` + row.Sha256 + `"`
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", cacheImmutable)
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	candidates, err := h.cfg.Channels.CandidatesForBlob(r.Context(), row.BlobID)
	if err != nil {
		h.cfg.Logger.Error("查询渠道候选失败", "id", imageID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	if len(candidates) == 0 {
		// 引用存在但当前无健康渠道持有就绪副本;一致性由对账任务兜底。
		h.cfg.Logger.Warn("无可用渠道服务该图", "id", imageID)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return
	}

	// 302 省流量模式:候选中允许直链且非加密的渠道直接重定向;降级路径强制代理不 302。
	if !degraded && h.cfg.Settings().AccessMode == "redirect" {
		for _, cand := range candidates {
			if cand.AllowDirectLink && !cand.Encrypted && cand.PublicBaseURL != "" {
				http.Redirect(w, r, cand.PublicBaseURL+"/"+row.StorageKey, http.StatusFound)
				return
			}
		}
		// 无直链候选,回退代理。
	}

	// 访问容灾:按优先级逐个尝试,取第一个能打开的渠道。
	var rc io.ReadCloser
	for _, cand := range candidates {
		rc, err = cand.Driver.Open(r.Context(), row.StorageKey)
		if err == nil {
			break
		}
		rc = nil
		h.cfg.Logger.Warn("渠道读取失败,切换下一候选", "id", imageID, "channel", cand.Name, "err", err)
	}
	if rc == nil {
		h.cfg.Logger.Error("全部候选渠道读取失败", "id", imageID, "key", row.StorageKey)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil {
			h.cfg.Logger.Debug("关闭渠道对象失败", "id", imageID, "err", cerr)
		}
	}()

	hd := w.Header()
	hd.Set("Content-Type", row.Mime)
	hd.Set("Content-Length", strconv.FormatInt(row.Size, 10))
	hd.Set("X-Content-Type-Options", "nosniff")
	if degraded {
		// 降级响应不可被缓存,否则会污染带处理参数的 URL。
		hd.Set("Cache-Control", "no-store")
	} else {
		// 内容不可变(URL 即版本),缓存一年;nosniff 强制浏览器信任服务端类型判定。
		hd.Set("Cache-Control", cacheImmutable)
		hd.Set("ETag", `"`+row.Sha256+`"`)
	}
	if _, err := io.Copy(w, rc); err != nil {
		// 多为客户端提前断开,记 debug 即可。
		h.cfg.Logger.Debug("输出图片中断", "id", imageID, "err", err)
	}
}
