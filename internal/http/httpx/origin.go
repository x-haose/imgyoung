package httpx

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
)

// Origin 请求的来源判定:客户端地址与客户端到站点的第一跳是否 HTTPS。
// @Description: 由 ProxyHeaders 中间件按信任的反向代理解析后放入上下文;未经中间件的请求
// 按连接本身判定。登录限速键、Secure cookie 与对外地址推导都只认它,不各自看请求头。
type Origin struct {
	IP        netip.Addr // 客户端地址;经信任代理转发时取自 X-Forwarded-For,否则为连接对端;无法解析时为零值
	Secure    bool       // 第一跳是否 HTTPS;经信任代理转发时取自 X-Forwarded-Proto,否则看本连接 TLS
	Forwarded bool       // 转发头是否可信(连接对端是信任的代理);为 true 时 X-Forwarded-Host 可用于推导对外域名
}

// originKey 上下文键的私有类型,防跨包碰撞。
type originKey struct{}

// WithOrigin 把来源判定写入上下文。
// @param ctx 请求上下文
// @param o 来源判定
// @return 携带来源判定的上下文
func WithOrigin(ctx context.Context, o Origin) context.Context {
	return context.WithValue(ctx, originKey{}, o)
}

// OriginFrom 取出请求的来源判定;上下文中没有时按连接本身推导(对端地址、本连接 TLS)。
// @param r 当前请求
// @return 来源判定
func OriginFrom(r *http.Request) Origin {
	if o, ok := r.Context().Value(originKey{}).(Origin); ok {
		return o
	}
	return Origin{IP: PeerAddr(r), Secure: r.TLS != nil}
}

// PeerAddr 解析连接对端地址;IPv4 映射到 IPv6 的形式还原为 IPv4。
// @param r 当前请求
// @return 对端地址;RemoteAddr 不是 ip:port 形态(如单元测试或 Unix socket)时为零值
func PeerAddr(r *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap()
}

// ClientIP 返回客户端地址文本,作登录限速等按来源计量的键。
// @param r 当前请求
// @return 客户端地址;无法解析时退回原始 RemoteAddr
func ClientIP(r *http.Request) string {
	if ip := OriginFrom(r).IP; ip.IsValid() {
		return ip.String()
	}
	return r.RemoteAddr
}

// IsSecure 判定客户端到站点的第一跳是否 HTTPS,决定会话 cookie 是否加 Secure。
// @param r 当前请求
// @return 第一跳为 HTTPS 时为 true
func IsSecure(r *http.Request) bool {
	return OriginFrom(r).Secure
}

// RequestBaseURL 从请求推导对外站点根地址(形如 https://img.example.com,无尾斜杠)。
// @Description: 未配置 base_url 时的兜底:协议取来源判定;经信任代理转发时域名优先取
// X-Forwarded-Host(多级代理取首值),否则取 Host 头。推导结果即客户端本次所见域名;
// 反代/CDN 对外域名与请求头不一致时仍须配置 base_url。硬约束:推导值来自请求,仅可
// 回显给当前请求方(如响应里拼本次上传的 URL);禁止落库、禁止用于拼 CDN purge URL、
// 禁止跨请求复用——这些场景一律以配置的 base_url 为准。
// @param r 当前请求
// @return 站点根地址
func RequestBaseURL(r *http.Request) string {
	o := OriginFrom(r)
	scheme := "http"
	if o.Secure {
		scheme = "https"
	}
	host := r.Host
	if o.Forwarded {
		if v := ForwardedFirst(r.Header.Get("X-Forwarded-Host")); v != "" {
			host = v
		}
	}
	return scheme + "://" + host
}

// ForwardedFirst 取 X-Forwarded-* 头的首个值(多级代理逗号追加,首值为最外层客户端侧)。
// @param v 原始头值
// @return 去空白后的首个值;头缺失时为空串
func ForwardedFirst(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// ImageURL 拼图片的对外绝对地址。
// @Description: 配置了 base_url 一律用配置;未配置则按当前请求推导,保证返回值恒为绝对地址。
// 推导值只回显给当前请求方,见 RequestBaseURL。
// @param r 当前请求
// @param baseURL 运营配置的对外站点地址,可为空
// @param imageID 对外短 ID
// @return 绝对访问地址
func ImageURL(r *http.Request, baseURL, imageID string) string {
	if baseURL == "" {
		baseURL = RequestBaseURL(r)
	}
	return baseURL + "/i/" + imageID
}
