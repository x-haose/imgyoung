package adminapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/core/audit"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
	"github.com/x-haose/imgyoung/internal/infra/signedurl"
)

// maxSignedTTLSeconds 签名链接有效期的上限,秒。
const maxSignedTTLSeconds = int64(signedurl.MaxTTL / time.Second)

// signedURLRequest POST /images/{id}/signed-url 的请求体。
type signedURLRequest struct {
	TTLSeconds int64 `json:"ttl_seconds"` // 链接有效时长,秒,1 到 signedurl.MaxTTL 折算的秒数
}

// signedURLResponse 签名链接。
type signedURLResponse struct {
	URL       string `json:"url"`        // 带签名的绝对链接
	ExpiresAt string `json:"expires_at"` // 链接到期时刻,UTC 文本
}

// HandleImageSignedURL 为一张私有图签发有时效的访问链接;普通用户只可为自己的图签发(他人图一律 404)。
// @Description: 签发不改任何状态,链接到期自然失效;签名只覆盖图片 ID 与到期时刻,已发出的链接无法单独收回,
// 要在到期前让它失效只能删除图片。
func (h *Handlers) HandleImageSignedURL(w http.ResponseWriter, r *http.Request) {
	p, ok := h.sessionPrincipal(r)
	if !ok {
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	imageID := chi.URLParam(r, "id")
	var req signedURLRequest
	r.Body = http.MaxBytesReader(w, r.Body, userBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.TTLSeconds < 1 || req.TTLSeconds > maxSignedTTLSeconds {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.SignedURLInvalid,
			"max", strconv.FormatInt(maxSignedTTLSeconds, 10))
		return
	}
	if _, ok := h.imageVisible(w, r, p, imageID); !ok {
		return
	}
	signing, err := h.cfg.Queries.GetImageSigning(r.Context(), imageID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ImageNotFound)
		return
	case err != nil:
		h.cfg.Logger.Error("查询私有开关失败", "id", imageID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	case signing.Private == 0:
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.ImageNotPrivate)
		return
	}
	expires := time.Now().Add(time.Duration(req.TTLSeconds) * time.Second)
	base := httpx.ImageURL(r, h.cfg.Settings.Current().BaseURL, imageID)
	h.recordAudit(r.Context(), audit.ActionImageSignedURL, imageID,
		"ttl_seconds="+strconv.FormatInt(req.TTLSeconds, 10))
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, signedURLResponse{
		URL: base + "?" + h.cfg.Signer.Query(imageID, signing.SignEpoch, expires), ExpiresAt: sqlc.TimeText(expires),
	})
}
