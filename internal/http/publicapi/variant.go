package publicapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/imaging"
)

// cacheImmutable 衍生图与原图内容均不可变(URL 即版本),缓存一年。
const cacheImmutable = "public, max-age=31536000, immutable"

// parseVariantRequest 解析按需处理参数并做档位白名单校验。
// @Description: 宽度取 ?w=(须在档位白名单)、格式取 ?format=(显式或 auto→Accept 协商);
// 无处理诉求(无宽度且协商无更优格式,或目标即原格式)则不处理、走原图。
// @param r 请求
// @param mime 原图 MIME,用于"保持原格式"判定
// @return 处理参数
// @return 是否需要处理
// @return 参数是否合法(false 表示应回 400)
func (h *Handlers) parseVariantRequest(r *http.Request, mime string) (params imaging.ProcessParams, want, ok, varyAccept bool) {
	// 源不可重编码(gif 动图等):一律发原图,绝不因显式 format/协商被解码首帧转成静图、丢动画。
	orig := originalProcessableFormat(mime)
	if orig == "" {
		return imaging.ProcessParams{}, false, true, false
	}

	q := r.URL.Query()
	widthStr := q.Get("w")
	formatParam := q.Get("format")

	var width int
	if widthStr != "" {
		n, err := strconv.Atoi(widthStr)
		if err != nil || !h.widthAllowed(n) {
			return imaging.ProcessParams{}, false, false, false
		}
		width = n
	}

	// 目标格式:显式档位 > Accept 协商 > 原格式。协商路径(format 空/auto)响应随 Accept 变,需 Vary。
	negotiated := formatParam == "" || formatParam == "auto"
	format := formatParam
	switch {
	case negotiated:
		format = negotiateFormat(r.Header.Get("Accept"), h.formatAllowed)
		if format == "" {
			format = orig // 无更优格式,保持原格式(已确保可重编码)
		}
	case !h.formatAllowed(format):
		return imaging.ProcessParams{}, false, false, false
	}

	// 目标即原格式且不缩放:处理无收益,走原图。
	if width == 0 && format == orig {
		return imaging.ProcessParams{}, false, true, negotiated
	}
	return imaging.ProcessParams{Width: width, Format: format}, true, true, negotiated
}

// negotiateFormat 按 Accept 头选最优目标格式:浏览器支持且档位允许 webp 则返回 webp,否则空串。
// @Description: 图片 Accept 结构简单,用子串包含判定即可,无需解析 q 值。
// @param accept 请求 Accept 头
// @param allowed 判定格式是否在白名单
// @return 目标格式或空串
func negotiateFormat(accept string, allowed func(string) bool) string {
	if allowed(imaging.FormatWebP) && strings.Contains(accept, "image/webp") {
		return imaging.FormatWebP
	}
	return ""
}

// originalProcessableFormat 返回原 MIME 对应的可重编码格式;不可重编码(gif 动画等)返回空串。
// @param mime 原图 MIME
// @return 可重编码格式或空串
func originalProcessableFormat(mime string) string {
	switch mime {
	case "image/jpeg":
		return imaging.FormatJPEG
	case "image/webp":
		return imaging.FormatWebP
	case "image/png":
		return imaging.FormatPNG
	default:
		return ""
	}
}

// widthAllowed 判定宽度是否在档位白名单内。
// @param w 目标宽度
// @return 是否允许
func (h *Handlers) widthAllowed(w int) bool {
	for _, aw := range h.cfg.ProcessWidths {
		if aw == w {
			return true
		}
	}
	return false
}

// formatAllowed 判定格式是否在白名单内。
// @param f 目标格式
// @return 是否允许
func (h *Handlers) formatAllowed(f string) bool {
	for _, af := range h.cfg.ProcessFormats {
		if af == f {
			return true
		}
	}
	return false
}

// serveVariant 服务按需处理结果:ETag/304 协商、经处理服务取或生成、失败降级回退原图。
// @param w 响应写入器
// @param r 请求
// @param row 引用与物理图信息
// @param params 处理参数
// @param varyAccept 目标格式是否由 Accept 协商决定(是则响应须 Vary: Accept)
func (h *Handlers) serveVariant(w http.ResponseWriter, r *http.Request, row sqlc.GetImageBlobRow, params imaging.ProcessParams, varyAccept bool) {
	// 衍生图内容由(原图 + 宽度 + 格式)唯一决定,据此构造强 ETag。
	etag := `"` + row.Sha256 + "-" + strconv.Itoa(params.Width) + "-" + params.Format + `"`
	if match := r.Header.Get("If-None-Match"); match == etag {
		hd := w.Header()
		hd.Set("ETag", etag)
		setVary(hd, varyAccept)
		hd.Set("Cache-Control", cacheImmutable)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	srcFn := func(ctx context.Context) ([]byte, error) { return h.fetchBlobBytes(ctx, row) }
	data, mime, err := h.cfg.Variants.GetOrProcess(r.Context(), row.BlobID, params, srcFn)
	if err != nil {
		// 处理繁忙或失败:降级回退原图,保证可用性;degraded=true 确保不把全尺寸原图当衍生图缓存。
		h.cfg.Logger.Warn("按需处理失败,降级回退原图", "id", row.ID, "err", err)
		h.serveOriginal(w, r, row, true)
		return
	}

	hd := w.Header()
	hd.Set("Content-Type", mime)
	hd.Set("Content-Length", strconv.Itoa(len(data)))
	hd.Set("Cache-Control", cacheImmutable)
	hd.Set("ETag", etag)
	setVary(hd, varyAccept)
	hd.Set("X-Content-Type-Options", "nosniff")
	// 与原图路径一致用流式拷贝输出二进制图像;nosniff + 显式 Content-Type 已排除嗅探风险。
	if _, werr := io.Copy(w, bytes.NewReader(data)); werr != nil {
		h.cfg.Logger.Debug("输出衍生图中断", "id", row.ID, "err", werr)
	}
}

// setVary 仅在响应确实随 Accept 变化时设 Vary: Accept,避免显式 format 请求污染 CDN 缓存键。
// @param h 响应头
// @param varyAccept 是否随 Accept 变化
func setVary(h http.Header, varyAccept bool) {
	if varyAccept {
		h.Set("Vary", "Accept")
	}
}

// fetchBlobBytes 按渠道优先级容灾读取原图全部字节,供处理引擎解码。
// @param ctx 上下文
// @param row 引用与物理图信息
// @return 原图字节
// @return 错误
func (h *Handlers) fetchBlobBytes(ctx context.Context, row sqlc.GetImageBlobRow) ([]byte, error) {
	candidates, err := h.cfg.Channels.CandidatesForBlob(ctx, row.BlobID)
	if err != nil {
		return nil, fmt.Errorf("查询渠道候选: %w", err)
	}
	for _, cand := range candidates {
		rc, oerr := cand.Driver.Open(ctx, row.StorageKey)
		if oerr != nil {
			h.cfg.Logger.Warn("处理取源:渠道读取失败,切换下一候选", "id", row.ID, "channel", cand.Name, "err", oerr)
			continue
		}
		// 多读 1 字节以区分"恰好足额"与"超限被截断":截断的残缺字节喂解码器会误判为坏图。
		data, rerr := io.ReadAll(io.LimitReader(rc, h.cfg.MaxUploadBytes+1))
		_ = rc.Close()
		if rerr != nil {
			return nil, fmt.Errorf("读取源图字节: %w", rerr)
		}
		if int64(len(data)) > h.cfg.MaxUploadBytes {
			return nil, fmt.Errorf("源图字节超上限 %d", h.cfg.MaxUploadBytes)
		}
		return data, nil
	}
	return nil, errors.New("无可用渠道提供源图")
}
