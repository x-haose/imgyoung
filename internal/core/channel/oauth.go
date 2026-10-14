package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/lib/oauthutil"
	"golang.org/x/oauth2"
)

// rclone 认得的 OAuth 配置键。
const (
	ConfigKeyToken        = "token"         // 令牌 JSON
	ConfigKeyClientID     = "client_id"     // 应用标识
	ConfigKeyClientSecret = "client_secret" // 应用密钥
)

// oauthConfigName 取配置时传给 rclone 的远端名;不落配置文件,仅用于其日志。
const oauthConfigName = "imgyoung"

// toConfigMap 把渠道参数转成 rclone 的参数映射。
// @param params 渠道参数
// @return 参数映射
func toConfigMap(params map[string]string) configmap.Simple {
	m := configmap.Simple{}
	for k, v := range params {
		m.Set(k, v)
	}
	return m
}

// ErrNotOAuthDriver 指定后端无需 OAuth 授权。
var ErrNotOAuthDriver = fmt.Errorf("%w: 该后端无需 OAuth 授权", ErrInvalidChannel)

// ErrInvalidToken 令牌不是合法的 rclone 授权结果。
var ErrInvalidToken = fmt.Errorf("%w: 令牌格式非法", ErrInvalidChannel)

// OAuthFlow 一个后端的 OAuth 授权码流。
type OAuthFlow struct {
	conf *oauth2.Config          // 已合并凭证与回调地址的标准配置
	opts []oauth2.AuthCodeOption // 后端要求的额外授权参数
}

// NewOAuthFlow 为指定后端构造授权码流。
// @Description: 端点、scopes 与内置应用标识取自后端注册信息;params 中的
// client_id / client_secret / auth_url / token_url 按 rclone 语义覆盖内置值,
// 影响端点的后端参数(onedrive 的 region、pcloud 的 hostname 等)由后端据此补全。
// @param ctx 控制取配置阶段的取消
// @param driver rclone 后端名
// @param params 后端参数
// @param redirectURI 授权回调地址;空则沿用后端内置地址(指向本机 53682 端口)
// @return 授权流
// @return 后端不存在、无需授权或取配置失败时的错误
func NewOAuthFlow(ctx context.Context, driver string, params map[string]string,
	redirectURI string) (*OAuthFlow, error) {
	m := toConfigMap(params)
	opt, err := backendOAuthOptions(ctx, driver, m)
	if err != nil {
		return nil, err
	}
	merged, _ := oauthutil.OverrideCredentials(oauthConfigName, m, opt.OAuth2Config)
	conf := merged.MakeOauth2Config()
	// 内置应用标识只登记了本机回调地址,自建应用才需覆盖成本站地址。
	if redirectURI != "" {
		conf.RedirectURL = redirectURI
	}

	// 不带离线授权则拿不到刷新令牌,进程重启后无法续期。
	codeOpts := opt.OAuth2Opts
	if !opt.NoOffline {
		codeOpts = append(codeOpts, oauth2.AccessTypeOffline)
	}
	return &OAuthFlow{conf: conf, opts: codeOpts}, nil
}

// AuthCodeURL 生成引导用户前往的授权地址。
// @param state 防跨站请求伪造的一次性串
// @return 授权地址
func (f *OAuthFlow) AuthCodeURL(state string) string {
	return f.conf.AuthCodeURL(state, f.opts...)
}

// Exchange 以授权码换取令牌。
// @param ctx 控制换取请求的超时与取消
// @param code 授权服务器回调带回的授权码
// @return 可直接写入渠道配置的令牌 JSON
// @return 换取失败时的错误
func (f *OAuthFlow) Exchange(ctx context.Context, code string) (string, error) {
	tok, err := f.conf.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("换取令牌: %w", err)
	}
	return marshalToken(tok)
}

// Refresh 用刷新令牌换取新的访问令牌。
// @Description: 服务商判定旧令牌仍有效时会原样返回,调用方据此决定是否落库。
// @param ctx 控制刷新请求的超时与取消
// @param tok 已存令牌
// @return 新令牌
// @return 刷新失败(多为授权被撤销或刷新令牌过期)时的错误
func (f *OAuthFlow) Refresh(ctx context.Context, tok *oauth2.Token) (*oauth2.Token, error) {
	fresh, err := f.conf.TokenSource(ctx, tok).Token()
	if err != nil {
		return nil, fmt.Errorf("刷新令牌: %w", err)
	}
	return fresh, nil
}

// ParsePastedToken 归一化用户粘贴的 rclone 授权结果。
// @Description: 兼容 rclone authorize 的两种输出——参数映射编码与裸令牌 JSON;
// 校验刷新令牌存在,缺失则该渠道在访问令牌过期后无法续期。
// @param pasted 用户粘贴的原文
// @return 可直接写入渠道配置的令牌 JSON
// @return ErrInvalidToken 或缺少刷新令牌时的错误
func ParsePastedToken(pasted string) (string, error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", ErrInvalidToken
	}
	// rclone authorize 新格式输出的是整个参数映射,令牌在其 token 键下。
	if m := (configmap.Simple{}); m.Decode(pasted) == nil {
		if raw, ok := m.Get(ConfigKeyToken); ok && raw != "" {
			return validateToken(raw)
		}
	}
	return validateToken(pasted)
}

// configWalkLimit 跟随后端配置状态机寻找授权环节的最大步数;正常后端两三步内即到达。
const configWalkLimit = 16

// backendOAuthOptions 取后端声明的 OAuth 选项。
// @Description: 后端的配置入口是一台状态机,授权环节未必在第一步(Google Drive 先校验
// client_id 再进入授权);本函数沿无需输入的跳转一路走到授权环节。途中若后端要求
// 交互输入(如缺 client_id 时的确认),无人可答,以明确错误返回而非猜答案。
// @param ctx 控制取配置阶段的取消
// @param driver rclone 后端名
// @param m 后端参数,影响端点补全与状态机走向
// @return 后端 OAuth 选项
// @return 后端不存在、不走 OAuth 或配置流程需要交互输入时的错误
func backendOAuthOptions(ctx context.Context, driver string, m configmap.Simple) (*oauthutil.Options, error) {
	ri, err := fs.Find(driver)
	if err != nil {
		return nil, fmt.Errorf("%w: 未知的后端 %q", ErrInvalidChannel, driver)
	}
	if ri.Config == nil {
		return nil, ErrNotOAuthDriver
	}
	in := fs.ConfigIn{}
	for range configWalkLimit {
		out, err := ri.Config(ctx, oauthConfigName, m, in)
		if err != nil {
			return nil, fmt.Errorf("读取后端 %q 的授权配置: %w", driver, err)
		}
		// 流程结束(nil 或回到起始状态)仍未出现授权环节:后端有交互式配置但不走 OAuth(如 s3)。
		if out == nil {
			return nil, ErrNotOAuthDriver
		}
		if out.Error != "" {
			return nil, fmt.Errorf("%w: 后端 %q 配置失败: %s", ErrInvalidChannel, driver, out.Error)
		}
		if out.OAuth != nil {
			opt, ok := out.OAuth.(*oauthutil.Options)
			if !ok || opt == nil || opt.OAuth2Config == nil {
				return nil, ErrNotOAuthDriver
			}
			return opt, nil
		}
		if out.Option != nil {
			return nil, fmt.Errorf("%w: 后端 %q 要求先提供 %s", ErrInvalidChannel, driver, out.Option.Name)
		}
		if out.State == "" {
			return nil, ErrNotOAuthDriver
		}
		in = fs.ConfigIn{State: out.State, Result: out.Result}
	}
	return nil, ErrNotOAuthDriver
}

// validateToken 校验令牌 JSON 并归一化。
// @param raw 令牌 JSON 原文
// @return 归一化后的令牌 JSON
// @return ErrInvalidToken
func validateToken(raw string) (string, error) {
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return "", ErrInvalidToken
	}
	if tok.AccessToken == "" {
		return "", ErrInvalidToken
	}
	if tok.RefreshToken == "" {
		return "", fmt.Errorf("%w: 缺少刷新令牌,访问令牌过期后无法续期", ErrInvalidChannel)
	}
	return marshalToken(&tok)
}

// marshalToken 按 rclone 的存储格式序列化令牌。
// @param tok 令牌
// @return 令牌 JSON
// @return 序列化失败时的错误
func marshalToken(tok *oauth2.Token) (string, error) {
	// nolint:gosec // G117:序列化令牌正是本函数的职责,产物存入渠道配置时由 AES-GCM 加密。
	b, err := json.Marshal(tok)
	if err != nil {
		return "", fmt.Errorf("序列化令牌: %w", err)
	}
	return string(b), nil
}
