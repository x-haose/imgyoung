// Package urlfetch 按 URL 抓取远端内容,面向"用户给一个地址、服务端替他去下载"的场景。
// 这类入口等于把外发请求的控制权交给了请求方,故本包的存在意义就是那组 SSRF 约束:
// 协议白名单、连接前逐次校验目标 IP(覆盖 DNS rebinding 与每一跳重定向)、跳数上限、
// 响应大小与总时长上限。抓取结果只是字节,后续净化与落库由上传管道负责。
package urlfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// 抓取失败的原因哨兵,由 API 层映射为对用户可读的拒绝理由。
var (
	// ErrScheme URL 协议不在白名单内(只允许 http/https)。
	ErrScheme = errors.New("urlfetch: 仅支持 http/https 地址")
	// ErrBlockedAddress 目标解析到非公网地址(内网/环回/link-local/云元数据等)。
	ErrBlockedAddress = errors.New("urlfetch: 目标地址不可达(仅允许公网地址)")
	// ErrTooManyRedirects 重定向跳数超出上限。
	ErrTooManyRedirects = errors.New("urlfetch: 重定向次数过多")
	// ErrTooLarge 响应体超出大小上限。
	ErrTooLarge = errors.New("urlfetch: 远端内容超出大小上限")
	// ErrStatus 远端返回非 2xx。
	ErrStatus = errors.New("urlfetch: 远端返回错误状态")
)

// maxRedirects 允许的重定向跳数上限;每一跳都会重新过一遍地址校验。
const maxRedirects = 5

// dialTimeout 单次 TCP 建连超时;总时长另有上限,此值只防单跳久挂。
const dialTimeout = 10 * time.Second

// Fetcher 带 SSRF 约束的抓取器,无可变状态、并发安全。
type Fetcher struct {
	client    *http.Client          // 复用连接池;其 Transport 的 Dial 链路带地址校验
	maxBytes  func() int64          // 响应大小上限读取口,逐次取值(与单图上限同源,热生效)
	timeout   time.Duration         // 单次抓取的总时长上限
	allowAddr func(netip.Addr) bool // 目标地址准入判定;恒为 isPublic,仅同包测试可换成放行环回的桩
}

// New 创建抓取器。
// @Description: 地址校验挂在 Dialer.Control 上——它拿到的是即将连接的真实地址,
// 因而 DNS 解析结果、重定向后的新目标、rebinding 时的第二次解析都逃不过同一道校验,
// 无需在解析与连接之间自行搬运 IP。
// @param maxBytes 响应大小上限读取口(字节);与单图上限同源,故取快照而非定值
// @param timeout 单次抓取的总时长上限
// @return 就绪的抓取器
func New(maxBytes func() int64, timeout time.Duration) *Fetcher {
	f := &Fetcher{maxBytes: maxBytes, timeout: timeout, allowAddr: isPublic}
	dialer := &net.Dialer{
		Timeout: dialTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			return f.checkDialAddress(address)
		},
	}
	f.client = &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   dialTimeout,
			ResponseHeaderTimeout: dialTimeout,
			DisableKeepAlives:     true, // 每次抓取重新建连,避免复用到校验时刻之外建立的连接
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// via 含最初那次请求:跟随第 n 跳时 len(via) == n。
			if len(via) > maxRedirects {
				return ErrTooManyRedirects
			}
			// net/http 跟随重定向时把上一跳的完整地址写进 Referer,用户提交的地址不外泄给重定向目标。
			req.Header.Del("Referer")
			return checkScheme(req.URL)
		},
	}
	return f
}

// Fetch 抓取指定地址的内容。
// @Description: 返回的字节数受大小上限约束——超限即报错而非截断,截断的图片会在后续
// 解码阶段变成一个含义模糊的"图片损坏"。
// @param ctx 控制取消;总时长另受配置上限约束
// @param rawURL 待抓取地址
// @return 响应体字节
// @return 远端返回的 Content-Type(仅作日志与文件名推断参考,格式判定仍以 magic bytes 为准)
// @return ErrScheme / ErrBlockedAddress / ErrTooManyRedirects / ErrTooLarge / ErrStatus 或网络错误
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("%w: 地址无法解析", ErrScheme)
	}
	if err := checkScheme(target); err != nil {
		return nil, "", err
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("构造抓取请求: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", unwrapFetchError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("%w: HTTP %d", ErrStatus, resp.StatusCode)
	}
	limit := f.maxBytes()
	if resp.ContentLength > limit {
		return nil, "", ErrTooLarge
	}
	// 多读一个字节:读满上限说明远端还有内容(或谎报了 Content-Length),按超限拒绝。
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取远端内容: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, "", ErrTooLarge
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// checkScheme 校验协议白名单。
// @param u 目标地址
// @return 非 http/https 时返回 ErrScheme
func checkScheme(u *url.URL) error {
	switch u.Scheme {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrScheme, u.Scheme)
	}
}

// checkDialAddress 校验即将建连的地址。
// @Description: 由 Dialer.Control 在每次 connect 前调用,拿到的是解析后的真实 IP,
// 故 DNS rebinding(校验与连接之间换解析结果)在此天然失效。
// @param address host:port 形式的目标地址
// @return 非公网地址返回 ErrBlockedAddress
func (f *Fetcher) checkDialAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: 地址格式异常", ErrBlockedAddress)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: 目标非 IP 地址", ErrBlockedAddress)
	}
	if !f.allowAddr(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
	}
	return nil
}

// blockedPrefixes 一律拒绝抓取的网段。
// @Description: 除常规内网段外,重点是几类"把内网地址裹进 IPv6 外壳"的写法——
// 它们的 IsPrivate/IsLoopback 都为假,只按通用判定会整片放行。
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // 本网络,含 0.1.2.3 这类可路由到本机的写法
	netip.MustParsePrefix("10.0.0.0/8"),     // 私有段
	netip.MustParsePrefix("100.64.0.0/10"),  // 运营商级 NAT
	netip.MustParsePrefix("127.0.0.0/8"),    // 环回
	netip.MustParsePrefix("169.254.0.0/16"), // link-local,含 169.254.169.254 云元数据
	netip.MustParsePrefix("172.16.0.0/12"),  // 私有段
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF 协议分配
	netip.MustParsePrefix("192.168.0.0/16"), // 私有段
	netip.MustParsePrefix("198.18.0.0/15"),  // 基准测试网段
	netip.MustParsePrefix("224.0.0.0/4"),    // 多播
	netip.MustParsePrefix("240.0.0.0/4"),    // 保留段,含 255.255.255.255 广播
	netip.MustParsePrefix("::/128"),         // 未指定
	netip.MustParsePrefix("::1/128"),        // 环回
	netip.MustParsePrefix("::/96"),          // IPv4-compatible 外壳(::127.0.0.1)
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64 外壳,可编码任意 IPv4
	netip.MustParsePrefix("64:ff9b:1::/48"), // 本地 NAT64
	netip.MustParsePrefix("2002::/16"),      // 6to4 外壳,可编码任意 IPv4
	netip.MustParsePrefix("fc00::/7"),       // 唯一本地地址
	netip.MustParsePrefix("fe80::/10"),      // link-local
	netip.MustParsePrefix("ff00::/8"),       // 多播
}

// isPublic 判定 IP 是否属于可对外抓取的公网地址。
// @Description: 逐条比对拒绝网段;IPv4-mapped IPv6(::ffff:127.0.0.1)先解回 IPv4 本体,
// 否则会绕过全部 IPv4 段判定。NAT64 / 6to4 / IPv4-compatible 这三类外壳同样能编码内网地址,
// 故整段拒绝而不逐个还原。
// @param ip 目标 IP
// @return 可抓取为 true
func isPublic(ip netip.Addr) bool {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	if !ip.IsValid() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// unwrapFetchError 把 client.Do 包装过的错误还原成本包哨兵,供上层精确映射。
// @param err client.Do 返回的错误
// @return 还原后的错误
func unwrapFetchError(err error) error {
	for _, sentinel := range []error{ErrBlockedAddress, ErrTooManyRedirects, ErrScheme} {
		if errors.Is(err, sentinel) {
			return sentinel
		}
	}
	return fmt.Errorf("抓取远端内容: %w", err)
}
