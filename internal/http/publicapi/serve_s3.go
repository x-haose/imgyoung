package publicapi

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/x-haose/imgyoung/internal/infra/db/sqlc"
)

// HandleServeObjectKey 按 S3 对象键代理输出图片(对外公开入口,计公开访问活跃度)。
// @Description: 路径形如 /o/<用户 id>/<对象键>。带用户段是必需的——对象键由客户端自取,
// 只在用户内唯一,全局解析会让两个用户的同名键互相顶掉、把链接指到别人的图上。
// 用不可变的用户 id 而非登录名:登录名可改,改名后再有人占用旧名,此前分享出去的链接
// 就会改指新用户的图。解析出短 ID 后完全复用公开访问管线(变体/协商/ETag/防盗链)。
func (h *Handlers) HandleServeObjectKey(w http.ResponseWriter, r *http.Request) {
	userID, objectKey, ok := splitObjectPath(chi.URLParam(r, "*"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	imageID, err := h.cfg.Queries.GetImageIDByObjectKey(r.Context(), sqlc.GetImageIDByObjectKeyParams{
		UserID:    userID,
		ObjectKey: sql.NullString{String: objectKey, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.cfg.Logger.Error("按对象键查引用失败", "user_id", userID, "object_key", objectKey, "err", err)
		http.Error(w, "服务内部错误", http.StatusInternalServerError)
		return
	}
	h.serveImageID(w, r, imageID, imageAccess{touch: true, mutable: true})
}

// splitObjectPath 把 /o/ 之后的路径切成用户 id 与对象键。
// @Description: 对象键落库时是解码形态,故此处也解码一次——否则客户端用 %2B、%41 这类
// 过度转义写法拼出的链接会查不到。
// @param rest /o/ 之后的整段路径(转义形态)
// @return 用户 id
// @return 对象键(可含斜杠)
// @return 两段齐备且转义合法为 true
func splitObjectPath(rest string) (userID, objectKey string, ok bool) {
	userID, escapedKey, found := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	if !found || userID == "" || escapedKey == "" {
		return "", "", false
	}
	objectKey, err := url.PathUnescape(escapedKey)
	if err != nil || objectKey == "" {
		return "", "", false
	}
	return userID, objectKey, true
}
