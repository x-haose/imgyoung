package publicapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// defaultSignedTTL 私有图上传响应里签名链接的默认有效时长;可用 signed_ttl 指定。
const defaultSignedTTL = 24 * time.Hour

// privacy 一次上传的私有选项。
type privacy struct {
	private   bool          // 是否作为私有图保存
	signedTTL time.Duration // 响应里签名链接的有效时长,仅 private 为 true 时使用
}

// parsePrivacy 解析上传请求里的 private 与 signed_ttl。
// @Description: 两项都可省略;private 省略为 false,signed_ttl 省略为 defaultSignedTTL。
// 非私有上传不使用 signed_ttl,但取值仍须合法,免得拼写错误被悄悄吞掉。
// @param private private 字段原文,空串表示省略
// @param signedTTL signed_ttl 字段原文,空串表示省略
// @return 解析结果
// @return 取值不合法为 false
func parsePrivacy(private, signedTTL string) (privacy, bool) {
	p := privacy{signedTTL: defaultSignedTTL}
	if private != "" {
		v, err := strconv.ParseBool(private)
		if err != nil {
			return privacy{}, false
		}
		p.private = v
	}
	if signedTTL != "" {
		n, err := strconv.ParseInt(signedTTL, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != signedTTL || n < 1 || n > httpx.MaxTTLSeconds {
			return privacy{}, false
		}
		p.signedTTL = time.Duration(n) * time.Second
	}
	return p, true
}

// writePrivacyInvalid 写 private 或 signed_ttl 取值不合法的响应。
func (h *Handlers) writePrivacyInvalid(w http.ResponseWriter) {
	httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.UploadPrivateInvalid,
		"max", strconv.FormatInt(httpx.MaxTTLSeconds, 10))
}

// uploadedBody 组装上传成功的响应体;私有图的 url 是带签名的链接,客户端直接粘贴即可使用。
// @param set 配置快照
// @param imageID 对外短 ID
// @param signedTTL 签名链接的有效时长
// @return 响应体
// @return 查询图片是否私有失败时的错误
func (h *Handlers) uploadedBody(r *http.Request, set config.Settings, imageID string,
	signedTTL time.Duration) (uploadResponse, error) {
	body := uploadResponse{ID: imageID, URL: httpx.ImageURL(r, set.BaseURL, imageID)}
	private, err := h.cfg.Queries.GetImagePrivate(r.Context(), imageID)
	if err != nil {
		return uploadResponse{}, fmt.Errorf("查询私有开关: %w", err)
	}
	if private == 0 {
		return body, nil
	}
	expires := time.Now().Add(signedTTL)
	body.URL += "?" + h.cfg.Signer.Query(imageID, expires)
	body.Private = true
	body.ExpiresAt = sqlc.TimeText(expires)
	return body, nil
}
