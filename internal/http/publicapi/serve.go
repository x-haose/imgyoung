package publicapi

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// imageAccess 一次图片访问的入口属性。
type imageAccess struct {
	touch   bool // 是否记公开访问活跃度:对外入口计,后台入口不计;HEAD 与参数非法的请求不算访问
	mutable bool // URL 指向的内容可能变化:对象键别名可被同键重传改指,响应须每次向源站确认
}

// originalOpts 原图响应随之变化的维度。
type originalOpts struct {
	degraded   bool // 按需处理失败后的回退输出:不可缓存,也不走 304 与 302
	varyAccept bool // 同一 URL 的响应随 Accept 协商变化(支持 WebP 的浏览器拿到的是衍生图)
	mutable    bool // 见 imageAccess.mutable
}

// HandleServe 按短 ID 代理输出图片(对外公开入口,计公开访问活跃度)。
func (h *Handlers) HandleServe(w http.ResponseWriter, r *http.Request) {
	h.serveImageID(w, r, chi.URLParam(r, "id"), imageAccess{touch: true})
}

// HandleAdminServe 后台缩略图/预览入口:与公开入口同一处理管线,但不计公开访问活跃度。
// @Description: 管理员浏览若计数,会把冷图摘出待清理列表(TTL 续期时还顺延到期)、
// 抬高访问统计与热门榜,故后台展示一律走此会话鉴权通道;对外分享仍用 /i/。
func (h *Handlers) HandleAdminServe(w http.ResponseWriter, r *http.Request) {
	h.serveImageID(w, r, chi.URLParam(r, "id"), imageAccess{})
}

// serveImageID 按已解析出的短 ID 输出图片:查引用、校验处理参数、计活跃度、走衍生图或原图。
// @Description: 短 ID 入口与 S3 对象键入口只在"怎么定位这张图"与缓存语义上不同,定位之后的
// 活跃度、变体协商与容灾读取必须一致,故收敛于此。已到期、尚待清理扫描回收的引用按不存在处理。
// @param imageID 对外短 ID
// @param a 入口属性
func (h *Handlers) serveImageID(w http.ResponseWriter, r *http.Request, imageID string, a imageAccess) {
	row, err := h.cfg.Queries.GetImageBlob(r.Context(), sqlc.GetImageBlobParams{
		ID: imageID, Now: sqlc.TimeText(time.Now()),
	})
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.cfg.Logger.Error("查询引用记录失败", "id", imageID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	// 按需处理:解析档位参数,命中则走处理路径(即便 redirect 省流量模式也强制代理)。
	var req variantRequest
	if h.cfg.Variants != nil {
		if req, err = h.parseVariantRequest(r, row.Mime); err != nil {
			httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if a.touch && r.Method == http.MethodGet {
		// 真实公开访问:记活跃度(仅内存,批量落库)。
		h.cfg.Activity.Touch(imageID)
	}
	if req.process {
		h.serveVariant(w, r, row, req, a.mutable)
		return
	}
	h.serveOriginal(w, r, row, originalOpts{varyAccept: req.varyAccept, mutable: a.mutable})
}

// serveOriginal 代理或重定向原图字节:ETag/304 协商、302 省流量、按渠道优先级容灾读取;HEAD 只写响应头。
// @Description: 短 ID 的内容不可变,以 sha256 作强 ETag 并缓存一年;对象键别名可被改指,同样带 ETag
// 但要求每次确认。降级路径(处理失败回退)服务的是全尺寸原图而非目标衍生图,不能按 URL 缓存或
// 302 直链,否则"临时降级"会被当作规范内容永久绑定到带处理参数的 URL(缓存投毒),故跳过 304/302,改 no-store。
// @param w 响应写入器
// @param r 请求
// @param row 引用与物理图信息
// @param o 响应维度
func (h *Handlers) serveOriginal(w http.ResponseWriter, r *http.Request, row sqlc.GetImageBlobRow, o originalOpts) {
	etag := `"` + row.Sha256 + `"`
	if !o.degraded && r.Header.Get("If-None-Match") == etag {
		hd := w.Header()
		hd.Set("ETag", etag)
		hd.Set("Cache-Control", cacheControl(o.mutable))
		setVary(hd, o.varyAccept)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	candidates, ok := h.servingCandidates(w, r, row)
	if !ok {
		return
	}
	// 302 省流量模式:候选中允许直链且非加密的渠道直接重定向;无直链候选则回退代理。
	if !o.degraded && h.cfg.Settings().AccessMode == "redirect" &&
		redirectToDirectLink(w, r, candidates, row.StorageKey) {
		return
	}
	if r.Method == http.MethodHead {
		writeOriginalHeaders(w.Header(), row, o)
		w.WriteHeader(http.StatusOK)
		return
	}
	rc, err := h.cfg.Channels.OpenFirst(r.Context(), candidates, row.StorageKey)
	if err != nil {
		h.cfg.Logger.Error("全部候选渠道读取失败", "id", row.ID, "key", row.StorageKey, "err", err)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil {
			h.cfg.Logger.Debug("关闭渠道对象失败", "id", row.ID, "err", cerr)
		}
	}()
	writeOriginalHeaders(w.Header(), row, o)
	if _, err := io.Copy(w, rc); err != nil {
		// 多为客户端提前断开,记 debug 即可。
		h.cfg.Logger.Debug("输出图片中断", "id", row.ID, "err", err)
	}
}

// servingCandidates 取能服务该图的渠道候选;取不到时已写出错误响应。
// @param w 响应写入器
// @param r 请求
// @param row 引用与物理图信息
// @return 候选渠道,按优先级降序
// @return 是否取到;false 时调用方直接返回
func (h *Handlers) servingCandidates(w http.ResponseWriter, r *http.Request,
	row sqlc.GetImageBlobRow) ([]channel.Handle, bool) {
	candidates, err := h.cfg.Channels.CandidatesForBlob(r.Context(), row.BlobID)
	if err != nil {
		h.cfg.Logger.Error("查询渠道候选失败", "id", row.ID, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return nil, false
	}
	if len(candidates) == 0 {
		// 引用存在但当前无健康渠道持有就绪副本;一致性由对账任务兜底。
		h.cfg.Logger.Warn("无可用渠道服务该图", "id", row.ID)
		http.Error(w, "渠道暂不可用", http.StatusServiceUnavailable)
		return nil, false
	}
	return candidates, true
}

// redirectToDirectLink 向第一个允许直链、未加密且配置了公开地址的候选渠道发 302;无此候选报告 false。
// @Description: 重定向一律 no-store:目标随渠道健康、优先级与直链配置变化,被 CDN 或浏览器缓存后
// 渠道切换、删除与到期都要等缓存过期才生效,缓存命中的访问也不计活跃度。
// @param w 响应写入器
// @param r 请求
// @param candidates 候选渠道,按优先级降序
// @param storageKey 对象键
// @return 是否已重定向
func redirectToDirectLink(w http.ResponseWriter, r *http.Request, candidates []channel.Handle, storageKey string) bool {
	for _, cand := range candidates {
		if cand.AllowDirectLink && !cand.Encrypted && cand.PublicBaseURL != "" {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, cand.PublicBaseURL+"/"+storageKey, http.StatusFound)
			return true
		}
	}
	return false
}

// writeOriginalHeaders 设置原图响应头:降级响应 no-store;其余带强 ETag 与上传时刻的 Last-Modified,
// 缓存策略与 Vary 按响应维度定。
// @Description: nosniff 强制浏览器信任服务端类型判定。
// @param hd 响应头
// @param row 引用与物理图信息
// @param o 响应维度
func writeOriginalHeaders(hd http.Header, row sqlc.GetImageBlobRow, o originalOpts) {
	hd.Set("Content-Type", row.Mime)
	hd.Set("Content-Length", strconv.FormatInt(row.Size, 10))
	hd.Set("X-Content-Type-Options", "nosniff")
	if o.degraded {
		hd.Set("Cache-Control", "no-store")
		return
	}
	hd.Set("Cache-Control", cacheControl(o.mutable))
	hd.Set("ETag", `"`+row.Sha256+`"`)
	setVary(hd, o.varyAccept)
	if uploaded, err := sqlc.ParseTimeText(row.CreatedAt); err == nil {
		hd.Set("Last-Modified", uploaded.UTC().Format(http.TimeFormat))
	}
}

// 对象输出的错误哨兵,供 S3 协议面翻译成对应错误码。
var (
	// ErrObjectNotFound 引用不存在。
	ErrObjectNotFound = errors.New("publicapi: 对象不存在")
	// ErrObjectUnavailable 引用存在但当前没有渠道能读出对象。
	ErrObjectUnavailable = errors.New("publicapi: 对象暂不可读")
)

// ServeObject 按短 ID 向已验签的客户端输出原图:元数据头、If-None-Match 协商与流式字节。
// @Description: 客户端取回的是自己的对象,与公开浏览不同——不计活跃度、不走 302 直链、不做按需处理;
// HEAD 只写元数据头,不打开渠道对象流。错误只在写出任何响应之前返回,调用方据此写自己的错误体。
// @param w 响应写入器
// @param r 请求,GET 或 HEAD
// @param imageID 对外短 ID
// @return ErrObjectNotFound / ErrObjectUnavailable 或查询失败的错误
func (h *Handlers) ServeObject(w http.ResponseWriter, r *http.Request, imageID string) error {
	row, err := h.cfg.Queries.GetImageBlob(r.Context(), sqlc.GetImageBlobParams{
		ID: imageID, Now: sqlc.TimeText(time.Now()),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrObjectNotFound
	}
	if err != nil {
		return fmt.Errorf("查询引用记录: %w", err)
	}
	etag := `"` + row.Sha256 + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	// 对象键可被同键重传改指,缓存语义同 /o/ 别名
	opts := originalOpts{mutable: true}
	if r.Method == http.MethodHead {
		writeOriginalHeaders(w.Header(), row, opts)
		w.WriteHeader(http.StatusOK)
		return nil
	}
	candidates, err := h.cfg.Channels.CandidatesForBlob(r.Context(), row.BlobID)
	if err != nil {
		return fmt.Errorf("查询渠道候选: %w", err)
	}
	rc, err := h.cfg.Channels.OpenFirst(r.Context(), candidates, row.StorageKey)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrObjectUnavailable, err)
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil {
			h.cfg.Logger.Debug("关闭渠道对象失败", "id", row.ID, "err", cerr)
		}
	}()
	writeOriginalHeaders(w.Header(), row, opts)
	if _, err := io.Copy(w, rc); err != nil {
		h.cfg.Logger.Debug("输出对象中断", "id", row.ID, "err", err)
	}
	return nil
}
