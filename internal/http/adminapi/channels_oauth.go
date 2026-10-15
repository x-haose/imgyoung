package adminapi

import (
	"errors"
	"html"
	"net/http"
	"net/url"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// OAuthCallbackPath 授权回调路径,须与各开放平台登记的回调地址逐字符一致。
const OAuthCallbackPath = "/api/admin/channel-oauth/callback"

// oauthStartRequest 发起授权的请求体。
type oauthStartRequest struct {
	Template  string            `json:"template"`   // 模板 id
	Inputs    map[string]string `json:"inputs"`     // 已填字段值:区域等影响端点的参数与自建应用凭证
	ChannelID int64             `json:"channel_id"` // 编辑场景的渠道主键,字段中的哨兵还原为该渠道已存值;0 = 创建场景
}

// oauthStartResponse 发起授权的响应体。
type oauthStartResponse struct {
	AuthURL string `json:"auth_url"` // 引导用户前往的授权地址
	State   string `json:"state"`    // 领取结果时使用的一次性标识
}

// oauthResultResponse 领取授权结果的响应体;授权失败时带上与错误信封同形的失败原因。
type oauthResultResponse struct {
	Pending              bool   `json:"pending"`         // 用户尚未完成授权
	Token                string `json:"token,omitempty"` // 授权令牌,填入渠道表单后提交
	*httpx.ErrorResponse        // 授权失败原因;未完成或成功时省略
}

// HandleChannelOAuthStart 发起网盘渠道授权,返回引导地址。
// @Description: 回调地址取自配置的对外站点地址而非请求头——它须在发起与回调两次请求
// 间保持一致,且要与开放平台登记值逐字符相同,按请求头推导会随反代与访问域名漂移。
func (h *Handlers) HandleChannelOAuthStart(w http.ResponseWriter, r *http.Request) {
	var req oauthStartRequest
	if !h.decodeChannelBody(w, r, &req) {
		return
	}
	base := h.cfg.Settings.Current().BaseURL
	if base == "" {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, errcode.OAuthBaseURLRequired)
		return
	}
	driver, params, err := h.cfg.Admin.OAuthStartParams(r.Context(), req.ChannelID, req.Template, req.Inputs)
	switch {
	case errors.Is(err, channel.ErrChannelNotFound):
		httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.ChannelNotFound)
		return
	case errors.Is(err, channel.ErrChannelDeleting):
		httpx.WriteError(h.cfg.Logger, w, http.StatusConflict, errcode.ChannelDeleting)
		return
	case errors.Is(err, channel.ErrInvalidChannel):
		httpx.WriteErrorFrom(h.cfg.Logger, w, http.StatusBadRequest, err)
		return
	case err != nil:
		h.cfg.Logger.Error("整理授权参数失败", "id", req.ChannelID, "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}

	redirectURI := base + OAuthCallbackPath
	flow, err := channel.NewOAuthFlow(r.Context(), driver, params, redirectURI)
	if err != nil {
		httpx.WriteErrorFrom(h.cfg.Logger, w, http.StatusBadRequest, err)
		return
	}
	state, err := h.cfg.OAuth.Start(driver, params, redirectURI)
	if err != nil {
		h.cfg.Logger.Error("登记授权会话失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, oauthStartResponse{
		AuthURL: flow.AuthCodeURL(state), State: state,
	})
}

// HandleChannelOAuthCallback 接收授权服务器回调,换取令牌后由前端领取。
// @Description: 本端点在浏览器新标签中打开,故返回极简 HTML 提示页而非 JSON;
// 令牌不写入页面,由前端另行凭 state 领取,避免落进浏览器历史与截图。同一 state 的回调再次打开时
// 不再换取(授权码只能用一次),按已记下的结果显示提示页。
func (h *Handlers) HandleChannelOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	text := oauthPageEnglish
	if httpx.PrefersChinese(r) {
		text = oauthPageChinese
	}
	pending, err := h.cfg.OAuth.Pending(state)
	if err != nil {
		writeOAuthPage(w, http.StatusBadRequest, text, text.expired)
		return
	}
	if pending.Completed {
		msg := text.done
		if pending.Failed {
			msg = text.failed
		}
		writeOAuthPage(w, http.StatusOK, text, msg)
		return
	}
	token, failure := h.exchangeOAuth(r, pending, q)
	if err := h.cfg.OAuth.Complete(state, token, failure); err != nil {
		writeOAuthPage(w, http.StatusBadRequest, text, text.expired)
		return
	}
	if failure != nil {
		writeOAuthPage(w, http.StatusOK, text, text.failed)
		return
	}
	writeOAuthPage(w, http.StatusOK, text, text.done)
}

// exchangeOAuth 处理授权服务器的回调:用户拒绝、授权流构造失败或换取令牌失败时给出失败原因。
// @param r 回调请求
// @param pending 发起授权时登记的信息
// @param q 回调查询参数
// @return token 换取到的令牌 JSON;失败时为空
// @return failure 失败原因;成功时为 nil
func (h *Handlers) exchangeOAuth(r *http.Request, pending channel.PendingAuth, q url.Values) (string, *errcode.Error) {
	if authErr := q.Get("error"); authErr != "" {
		return "", errcode.New(errcode.OAuthDenied, "detail", authErr)
	}
	flow, err := channel.NewOAuthFlow(r.Context(), pending.Driver, pending.Params, pending.RedirectURI)
	if err != nil {
		if e, ok := errcode.From(err); ok {
			return "", e
		}
		h.cfg.Logger.Error("构造网盘授权流失败", "driver", pending.Driver, "err", err)
		return "", errcode.New(errcode.Internal)
	}
	token, err := flow.Exchange(r.Context(), q.Get("code"))
	if err != nil {
		// 换取失败多为应用凭证或回调地址与开放平台登记值不一致,原因需回传给管理员。
		h.cfg.Logger.Warn("换取网盘授权令牌失败", "driver", pending.Driver, "err", err)
		return "", errcode.New(errcode.OAuthExchangeFailed)
	}
	return token, nil
}

// HandleChannelOAuthResult 领取授权结果;结果只可领取一次。
func (h *Handlers) HandleChannelOAuthResult(w http.ResponseWriter, r *http.Request) {
	token, failure, err := h.cfg.OAuth.Take(r.URL.Query().Get("state"))
	if err != nil {
		if errors.Is(err, channel.ErrOAuthSessionNotFound) {
			httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, errcode.OAuthSessionNotFound)
			return
		}
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, errcode.Internal)
		return
	}
	resp := oauthResultResponse{Pending: token == "" && failure == nil, Token: token}
	if failure != nil {
		body := httpx.ErrorBody(failure)
		resp.ErrorResponse = &body
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, resp)
}

// oauthPageText 授权回调提示页的一种语言版本。
type oauthPageText struct {
	lang    string // 页面语言,写进 html 的 lang 属性
	title   string // 页面标题
	expired string // 授权会话已失效
	failed  string // 授权未完成
	done    string // 授权成功
	closing string // 可关闭页面的提示
}

// 授权回调提示页的中英文版本;页面由浏览器从授权服务器跳回打开,按浏览器的 Accept-Language 选用。
var (
	oauthPageChinese = oauthPageText{
		lang: "zh-CN", title: "ImgYoung 授权",
		expired: "授权会话已失效,请回到后台重新发起。",
		failed:  "授权未完成,请回到后台查看。",
		done:    "授权成功,请回到后台继续。",
		closing: "可以关闭此页面了。",
	}
	oauthPageEnglish = oauthPageText{
		lang: "en", title: "ImgYoung authorization",
		expired: "The authorization session has expired. Go back to the admin console and start again.",
		failed:  "Authorization did not complete. Go back to the admin console for details.",
		done:    "Authorization complete. Go back to the admin console to continue.",
		closing: "You can close this page now.",
	}
)

// writeOAuthPage 输出回调提示页。
// @param w 响应写入器
// @param status HTTP 状态码
// @param text 页面所用语言的文案
// @param msg 提示文案,取自 text
func writeOAuthPage(w http.ResponseWriter, status int, text oauthPageText, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", text.lang)
	w.Header().Set("Vary", "Accept-Language")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><html lang="` + text.lang + `"><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + html.EscapeString(text.title) + `</title>` +
		`<div style="font:16px/1.7 system-ui,sans-serif;padding:15vh 8vw;text-align:center;color:#1a1a1a">` +
		html.EscapeString(msg) +
		`<p style="color:#888;font-size:14px">` + html.EscapeString(text.closing) + `</p></div>`))
}
