// Package imgrefs 从文本里找出图片引用:HTML 与 Markdown 里的图片,以及站点地图、订阅源里列出的页面与图片。
// 本包只做纯计算,不访问网络;找到的地址是否可抓取、抓回来是不是图片,由调用方判定。
package imgrefs

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// maxUnfetchableRunes 无法抓取的引用原文保留的字符数上限,防止内嵌图片这类超长原文撑大结果。
const maxUnfetchableRunes = 200

// Ref 一处可抓取的图片引用。
type Ref struct {
	Raw string // 引用在文档里的原文,可能是相对路径
	URL string // 解析并规范化后的绝对地址,见 Normalize
}

// Result 一份文档里扫描到的全部引用。
type Result struct {
	Refs        []Ref    // 可抓取的引用,按出现顺序;同一原文只出现一次
	Unfetchable []string // 无法抓取的引用原文,按出现顺序,每条最长 maxUnfetchableRunes 个字符
}

// Normalize 把引用解析成可抓取的绝对地址。
// @Description: 只接受 http 与 https、带域名、不带用户名密码的地址;相对路径与 // 开头的地址按 base 解析,
// base 为 nil 时它们无法解析。结果去掉 # 之后的部分,协议与域名转为小写,所以同一张图的不同写法得到同一个地址。
// @param raw 引用原文
// @param base 解析相对路径的基准地址,可为 nil
// @return 规范化后的绝对地址
// @return 能抓取为 true
func Normalize(raw string, base *url.URL) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", false
	}
	u.Fragment, u.RawFragment = "", ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String(), true
}

// Host 返回规范化地址的域名部分:小写,非默认端口时带端口。
// @param normalized Normalize 的返回值
// @return 域名;地址无法解析时为空串
func Host(normalized string) string {
	u, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	return u.Host
}

// collector 按出现顺序收集原文,同一原文只收一次。
type collector struct {
	raws []string
	seen map[string]struct{}
}

// add 记下一处引用原文。
// @param raw 引用原文,首尾空白已去掉的才会被记下
func (c *collector) add(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if _, dup := c.seen[raw]; dup {
		return
	}
	if c.seen == nil {
		c.seen = map[string]struct{}{}
	}
	c.seen[raw] = struct{}{}
	c.raws = append(c.raws, raw)
}

// resolve 把收集到的原文按基准地址解析成结果。
// @param base 解析相对路径的基准地址,可为 nil
// @return 解析后的结果
func (c *collector) resolve(base *url.URL) Result {
	res := Result{}
	for _, raw := range c.raws {
		if abs, ok := Normalize(raw, base); ok {
			res.Refs = append(res.Refs, Ref{Raw: raw, URL: abs})
			continue
		}
		res.Unfetchable = append(res.Unfetchable, truncate(raw))
	}
	return res
}

// truncate 把原文截到 maxUnfetchableRunes 个字符,超出时末尾加省略号。
// @param s 原文
// @return 截断后的原文
func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxUnfetchableRunes {
		return s
	}
	return string([]rune(s)[:maxUnfetchableRunes]) + "…"
}
