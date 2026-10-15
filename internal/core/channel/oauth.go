package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
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

// findBackend 按名查找 rclone 后端。
// @param driver rclone 后端名
// @return 后端注册信息
// @return 后端不存在时的 ErrInvalidChannel
func findBackend(driver string) (*fs.RegInfo, error) {
	ri, err := fs.Find(driver)
	if err != nil {
		return nil, fmt.Errorf("%w: 未知的后端 %q", ErrInvalidChannel, driver)
	}
	return ri, nil
}

// backendParams 构造交给后端配置流程的参数映射:params 之上以后端选项默认值兜底(优先级最低)。
// @Description: 后端按选项结构读参时不自带默认值,缺省的选项会读成零值(如 onedrive 的
// access_scopes 读空,授权地址不带 scope);与构造驱动时的取值规则一致。后端写下的参数进入 params。
// @param ri 后端注册信息
// @param params 后端参数;后端写参时原地修改
// @return 参数映射
func backendParams(ri *fs.RegInfo, params configmap.Simple) *configmap.Map {
	m := configmap.New()
	m.AddGetter(params, configmap.PriorityNormal)
	m.AddGetter(optionDefaults(ri.Options), configmap.PriorityDefault)
	m.AddSetter(params)
	return m
}

// optionDefaults 以后端选项元数据提供参数默认值。
type optionDefaults fs.Options

// Get 读取选项默认值。
// @param key 参数名
// @return 默认值与该选项是否存在
func (d optionDefaults) Get(key string) (string, bool) {
	opt := fs.Options(d).Get(key)
	if opt == nil {
		return "", false
	}
	return opt.String(), true
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
	ri, err := findBackend(driver)
	if err != nil {
		return nil, err
	}
	values := configmap.Simple{}
	maps.Copy(values, params)
	m := backendParams(ri, values)
	opt, err := backendOAuthOptions(ctx, ri, m)
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

// OAuthStartParams 汇总发起授权所需的后端参数:模板已填字段按落位键合并,授权令牌除外。
// @Description: 编辑场景重新授权时,表单回显的哨兵(自建应用的 Client ID / Secret 等)还原为
// 该渠道的已存原值,整理规则与 Create / Update 相同。
// @param channelID 编辑场景的渠道主键;0 = 创建场景
// @param templateID 模板 id,须为需授权的模板
// @param inputs 已填字段值
// @return rclone 后端名
// @return 后端参数
// @return 模板不存在或无需授权、哨兵无法还原时的 ErrInvalidChannel,ErrChannelNotFound,或读取渠道失败的错误
func (m *Manager) OAuthStartParams(ctx context.Context, channelID int64, templateID string,
	inputs map[string]string) (string, map[string]string, error) {
	tpl := findTemplate(templateID)
	if tpl == nil {
		return "", nil, ErrUnknownTemplate
	}
	if !tpl.RequiresOAuth {
		return "", nil, ErrNotOAuthDriver
	}
	params, err := mergeTemplate(tpl, inputs, func(f TemplateField) (string, error) {
		if f.Type == FieldOAuth {
			return "", nil
		}
		return strings.TrimSpace(inputs[f.Key]), nil
	})
	if err != nil {
		return "", nil, err
	}
	var stored map[string]string
	if channelID > 0 {
		if stored, err = m.storedConfig(ctx, channelID); err != nil {
			return "", nil, err
		}
	}
	if params, err = prepareConfig(tpl.Driver, params, stored, true); err != nil {
		return "", nil, err
	}
	return tpl.Driver, params, nil
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

// ParsePastedToken 归一化用户粘贴的 rclone 授权结果。
// @Description: 兼容 rclone authorize 的两种输出——参数映射编码与裸令牌 JSON;
// 会过期的令牌须带刷新令牌,否则该渠道在访问令牌过期后无法续期。
// @param pasted 用户粘贴的原文
// @return 可直接写入渠道配置的令牌 JSON
// @return ErrInvalidToken 或会过期却缺少刷新令牌时的错误
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

// configWalkLimit 跟随后端配置状态机的最大步数;内置模板的后端十步内走完,超出视为状态机打转。
const configWalkLimit = 16

// backendOAuthOptions 取后端声明的 OAuth 选项。
// @Description: 后端的配置入口是一台状态机,授权环节未必在第一步(Google Drive 先校验
// client_id 再进入授权);本函数沿无需输入的跳转一路走到授权环节。途中若后端要求
// 交互输入(如缺 client_id 时的确认),无人可答,以明确错误返回而非猜答案。
// @param ctx 控制取配置阶段的取消
// @param ri 后端注册信息
// @param m 后端参数,影响端点补全与状态机走向
// @return 后端 OAuth 选项
// @return 后端不走 OAuth 或配置流程需要交互输入时的错误
func backendOAuthOptions(ctx context.Context, ri *fs.RegInfo, m configmap.Mapper) (*oauthutil.Options, error) {
	if ri.Config == nil {
		return nil, ErrNotOAuthDriver
	}
	in := fs.ConfigIn{}
	for range configWalkLimit {
		out, err := ri.Config(ctx, oauthConfigName, m, in)
		if err != nil {
			return nil, fmt.Errorf("读取后端 %q 的授权配置: %w", ri.Name, err)
		}
		opt, next, err := oauthStep(ri.Name, out)
		if err != nil {
			return nil, err
		}
		if opt != nil {
			return opt, nil
		}
		in = next
	}
	return nil, ErrNotOAuthDriver
}

// oauthStep 解读配置状态机的一步输出:到达授权环节返回选项,可继续则返回下一步输入。
// @Description: 流程结束(nil 输出或空状态)仍未出现授权环节,说明后端有交互式配置但不走 OAuth(如 s3)。
// @param driver rclone 后端名,仅用于错误文案
// @param out 本步输出
// @return 授权选项;尚未到达授权环节为 nil
// @return 下一步输入;已到达授权环节时无意义
// @return 后端配置失败、要求交互输入或不走 OAuth 时的错误
func oauthStep(driver string, out *fs.ConfigOut) (*oauthutil.Options, fs.ConfigIn, error) {
	if out == nil {
		return nil, fs.ConfigIn{}, ErrNotOAuthDriver
	}
	if out.Error != "" {
		return nil, fs.ConfigIn{}, fmt.Errorf("%w: 后端 %q 配置失败: %s", ErrInvalidChannel, driver, out.Error)
	}
	if out.OAuth != nil {
		opt, ok := out.OAuth.(*oauthutil.Options)
		if !ok || opt == nil || opt.OAuth2Config == nil {
			return nil, fs.ConfigIn{}, ErrNotOAuthDriver
		}
		return opt, fs.ConfigIn{}, nil
	}
	if out.Option != nil {
		return nil, fs.ConfigIn{}, fmt.Errorf("%w: 后端 %q 要求先提供 %s", ErrInvalidChannel, driver, out.Option.Name)
	}
	if out.State == "" {
		return nil, fs.ConfigIn{}, ErrNotOAuthDriver
	}
	return nil, fs.ConfigIn{State: out.State, Result: out.Result}, nil
}

// validateToken 校验令牌 JSON 并归一化。
// @Description: 到期时刻为零值的令牌永不过期(如 pCloud),不需要刷新令牌。
// @param raw 令牌 JSON 原文
// @return 归一化后的令牌 JSON
// @return ErrInvalidToken,或会过期却缺少刷新令牌时的 ErrInvalidChannel
func validateToken(raw string) (string, error) {
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return "", ErrInvalidToken
	}
	if tok.AccessToken == "" {
		return "", ErrInvalidToken
	}
	if tok.RefreshToken == "" && !tok.Expiry.IsZero() {
		return "", fmt.Errorf("%w: 缺少刷新令牌,访问令牌过期后无法续期", ErrInvalidChannel)
	}
	return marshalToken(&tok)
}

// marshalToken 按 rclone 的存储格式序列化令牌。
// @param tok 令牌
// @return 令牌 JSON
// @return 序列化失败时的错误
func marshalToken(tok *oauth2.Token) (string, error) {
	b, err := json.Marshal(tok)
	if err != nil {
		return "", fmt.Errorf("序列化令牌: %w", err)
	}
	return string(b), nil
}
