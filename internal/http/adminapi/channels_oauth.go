package adminapi

import (
	"errors"
	"html"
	"net/http"

	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/http/httpx"
)

// OAuthCallbackPath 授权回调路径,须与各开放平台登记的回调地址逐字符一致。
const OAuthCallbackPath = "/api/admin/channel-oauth/callback"

// oauthStartRequest 发起授权的请求体。
type oauthStartRequest struct {
	Template     string            `json:"template"`      // 模板 id
	Inputs       map[string]string `json:"inputs"`        // 已填字段值,提供区域等影响端点的参数
	ClientID     string            `json:"client_id"`     // 自建应用标识
	ClientSecret string            `json:"client_secret"` // 自建应用密钥
}

// oauthStartResponse 发起授权的响应体。
type oauthStartResponse struct {
	AuthURL string `json:"auth_url"` // 引导用户前往的授权地址
	State   string `json:"state"`    // 领取结果时使用的一次性标识
}

// oauthResultResponse 领取授权结果的响应体。
type oauthResultResponse struct {
	Pending bool   `json:"pending"`         // 用户尚未完成授权
	Token   string `json:"token,omitempty"` // 授权令牌,填入渠道表单后提交
	Error   string `json:"error,omitempty"` // 授权失败原因
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
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest,
			"一键授权需先在设置中填写对外站点地址;或改用粘贴令牌方式")
		return
	}
	tpl, ok := channel.TemplateByID(req.Template)
	if !ok || !tpl.RequiresOAuth {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, "该渠道类型无需授权")
		return
	}

	params := oauthParams(req)
	redirectURI := base + OAuthCallbackPath
	flow, err := channel.NewOAuthFlow(r.Context(), tpl.Driver, params, redirectURI)
	if err != nil {
		httpx.WriteError(h.cfg.Logger, w, http.StatusBadRequest, templateErrMsg(err))
		return
	}
	state, err := h.cfg.OAuth.Start(tpl.Driver, params, redirectURI)
	if err != nil {
		h.cfg.Logger.Error("登记授权会话失败", "err", err)
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, oauthStartResponse{
		AuthURL: flow.AuthCodeURL(state), State: state,
	})
}

// HandleChannelOAuthCallback 接收授权服务器回调,换取令牌后由前端领取。
// @Description: 本端点在浏览器新标签中打开,故返回极简 HTML 提示页而非 JSON;
// 令牌不写入页面,由前端另行凭 state 领取,避免落进浏览器历史与截图。
func (h *Handlers) HandleChannelOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	pending, err := h.cfg.OAuth.Pending(state)
	if err != nil {
		writeOAuthPage(w, http.StatusBadRequest, "授权会话已失效,请回到后台重新发起。")
		return
	}
	if authErr := q.Get("error"); authErr != "" {
		_ = h.cfg.OAuth.Complete(state, "", "授权被拒绝或取消:"+authErr)
		writeOAuthPage(w, http.StatusOK, "授权未完成,请回到后台查看。")
		return
	}

	flow, err := channel.NewOAuthFlow(r.Context(), pending.Driver, pending.Params, pending.RedirectURI)
	if err != nil {
		_ = h.cfg.OAuth.Complete(state, "", templateErrMsg(err))
		writeOAuthPage(w, http.StatusOK, "授权未完成,请回到后台查看。")
		return
	}
	token, err := flow.Exchange(r.Context(), q.Get("code"))
	if err != nil {
		// 换取失败多为应用凭证或回调地址与开放平台登记值不一致,原因需回传给管理员。
		h.cfg.Logger.Warn("换取网盘授权令牌失败", "driver", pending.Driver, "err", err)
		_ = h.cfg.OAuth.Complete(state, "", "换取令牌失败,请核对应用凭证与回调地址")
		writeOAuthPage(w, http.StatusOK, "授权未完成,请回到后台查看。")
		return
	}
	if err := h.cfg.OAuth.Complete(state, token, ""); err != nil {
		writeOAuthPage(w, http.StatusBadRequest, "授权会话已失效,请回到后台重新发起。")
		return
	}
	writeOAuthPage(w, http.StatusOK, "授权成功,请回到后台继续。")
}

// HandleChannelOAuthResult 领取授权结果;结果只可领取一次。
func (h *Handlers) HandleChannelOAuthResult(w http.ResponseWriter, r *http.Request) {
	token, failure, err := h.cfg.OAuth.Take(r.URL.Query().Get("state"))
	if err != nil {
		if errors.Is(err, channel.ErrOAuthSessionNotFound) {
			httpx.WriteError(h.cfg.Logger, w, http.StatusNotFound, "授权会话不存在或已过期")
			return
		}
		httpx.WriteError(h.cfg.Logger, w, http.StatusInternalServerError, "服务内部错误")
		return
	}
	httpx.WriteJSON(h.cfg.Logger, w, http.StatusOK, oauthResultResponse{
		Pending: token == "" && failure == "", Token: token, Error: failure,
	})
}

// oauthParams 汇总授权所需的后端参数:模板已填字段加自建应用凭证。
// @param req 发起授权的请求
// @return 后端参数
func oauthParams(req oauthStartRequest) map[string]string {
	params := make(map[string]string, len(req.Inputs)+2)
	for k, v := range req.Inputs {
		if k != channel.ConfigKeyToken && v != "" {
			params[k] = v
		}
	}
	if req.ClientID != "" {
		params[channel.ConfigKeyClientID] = req.ClientID
		params[channel.ConfigKeyClientSecret] = req.ClientSecret
	}
	return params
}

// writeOAuthPage 输出回调提示页。
// @param w 响应写入器
// @param status HTTP 状态码
// @param msg 提示文案
func writeOAuthPage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>ImgYoung 授权</title>` +
		`<div style="font:16px/1.7 system-ui,sans-serif;padding:15vh 8vw;text-align:center;color:#1a1a1a">` +
		html.EscapeString(msg) + `<p style="color:#888;font-size:14px">可以关闭此页面了。</p></div>`))
}
