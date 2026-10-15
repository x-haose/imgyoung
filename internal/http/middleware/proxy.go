package middleware

import (
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/x-haose/imgyoung/internal/http/httpx"
	"github.com/x-haose/imgyoung/internal/infra/errcode"
)

// ProxyHeaders 按信任的反向代理解析请求来源并放入上下文,须挂在全部中间件之前。
// @Description: 只有连接对端落在 trusted 网段时才采信 X-Forwarded-For / X-Forwarded-Proto:
// 直接暴露在公网的实例对任何人都不信转发头,否则登录限速键与 Secure cookie 判定可被
// 请求方伪造——伪造 X-Forwarded-For 能让限速键归并到同一假地址,把全站登录一起限掉。
// X-Forwarded-For 自右向左跳过信任代理,第一个不在信任网段的地址即客户端(多级代理各追加一跳)。
// @param trusted 信任的反向代理网段;空表示不信任任何转发头
// @return 中间件
func ProxyHeaders(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := httpx.PeerAddr(r)
			origin := httpx.Origin{IP: peer, Secure: r.TLS != nil}
			if peer.IsValid() && inPrefixes(peer, trusted) {
				origin.Forwarded = true
				origin.IP = clientFromForwardedFor(r.Header.Values("X-Forwarded-For"), trusted, peer)
				if proto := httpx.ForwardedFirst(r.Header.Get("X-Forwarded-Proto")); proto != "" {
					origin.Secure = strings.EqualFold(proto, "https")
				}
			}
			next.ServeHTTP(w, r.WithContext(httpx.WithOrigin(r.Context(), origin)))
		})
	}
}

// AllowFrom 来源网段门槛中间件:客户端地址不在 allow 网段内一律 403。
// @Description: 须挂在 ProxyHeaders 之后,按解析后的客户端地址判定;地址无法解析视为不在网段内。
// @param allow 放行的网段
// @param l 结构化日志
// @return 中间件
func AllowFrom(allow []netip.Prefix, l *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := httpx.OriginFrom(r).IP
			if !ip.IsValid() || !inPrefixes(ip, allow) {
				httpx.WriteError(l, w, http.StatusForbidden, errcode.SourceNotAllowed)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientFromForwardedFor 从 X-Forwarded-For 自右向左找出第一个不属于信任代理的地址。
// @Description: 代理把连接对端追加到列表末尾,自右向左跳过信任代理,遇到的第一个外部地址
// 即客户端;无法解析的项跳过;整条链全是信任代理(内网客户端)时取最左侧地址;头缺失或
// 无可用地址时退回连接对端。
// @param values 全部 X-Forwarded-For 头值(每个可含逗号分隔的多跳)
// @param trusted 信任的反向代理网段
// @param peer 连接对端
// @return 客户端地址
func clientFromForwardedFor(values []string, trusted []netip.Prefix, peer netip.Addr) netip.Addr {
	var hops []netip.Addr
	for _, v := range values {
		for item := range strings.SplitSeq(v, ",") {
			if addr, err := netip.ParseAddr(strings.TrimSpace(item)); err == nil {
				hops = append(hops, addr.Unmap())
			}
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		if !inPrefixes(hops[i], trusted) {
			return hops[i]
		}
	}
	if len(hops) > 0 {
		return hops[0]
	}
	return peer
}

// inPrefixes 判定地址是否落在任一网段内。
// @param ip 待判定地址
// @param prefixes 网段列表
// @return 命中为 true
func inPrefixes(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
