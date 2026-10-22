package imgrefs

import (
	"bytes"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// imageExtensions 常见图片扩展名,小写、不带点;data-* 属性与 <source src> 只有路径以它们结尾才算图片引用。
var imageExtensions = map[string]struct{}{
	".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {}, ".avif": {}, ".svg": {}, ".bmp": {}, ".ico": {},
}

// metaImageKeys 声明页面代表图的 <meta> 的 property 或 name 取值。
var metaImageKeys = map[string]struct{}{
	"og:image": {}, "og:image:url": {}, "og:image:secure_url": {}, "twitter:image": {}, "twitter:image:src": {},
}

// cssURL 匹配 CSS 里的 url(...),三种写法:双引号、单引号、无引号。
var cssURL = regexp.MustCompile(`url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s]*))\s*\)`)

// FromHTML 找出一份 HTML 里的图片引用。
// @Description: 扫描 <img> 的 src、srcset 与路径带图片扩展名的 data-* 属性,<source> 的 srcset 与带图片扩展名的 src,
// 声明代表图的 <meta> 与 <link rel="image_src">,以及任意元素 style 属性里的 url(...)。不扫描 <style> 块与脚本。
// 文档里的 <base href> 优先于传入的 base 作为相对路径的基准。
// @param src HTML 文本
// @param base 文档自身的地址或调用方给定的基准地址,可为 nil
// @return 扫描到的引用
func FromHTML(src []byte, base *url.URL) Result {
	var c collector
	baseHref := scanHTML(src, &c)
	if baseHref != "" {
		if u, err := url.Parse(strings.TrimSpace(baseHref)); err == nil {
			if base != nil {
				u = base.ResolveReference(u)
			}
			base = u
		}
	}
	return c.resolve(base)
}

// scanHTML 逐个标签收集引用原文。
// @param src HTML 文本
// @param c 收集器
// @return 文档里第一个 <base> 的 href;没有为空串
func scanHTML(src []byte, c *collector) string {
	z := html.NewTokenizer(bytes.NewReader(src))
	baseHref := ""
	for {
		switch z.Next() {
		case html.ErrorToken:
			return baseHref
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			if tok.Data == "base" && baseHref == "" {
				baseHref = attrValue(tok.Attr, "href")
			}
			collectTag(tok.Data, tok.Attr, c)
		default:
			// 文本、注释、结束标签没有属性,跳过。
		}
	}
}

// attrValue 取属性值;属性不存在为空串。
// @param attrs 标签的属性,键已是小写
// @param key 小写属性名
// @return 已还原实体的属性值
func attrValue(attrs []html.Attribute, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// collectTag 收集一个标签上的图片引用原文,按属性出现顺序。
// @param tag 小写标签名
// @param attrs 标签的属性
// @param c 收集器
func collectTag(tag string, attrs []html.Attribute, c *collector) {
	switch tag {
	case "img":
		collectImg(attrs, c)
	case "source":
		addSrcset(attrValue(attrs, "srcset"), c)
		if src := attrValue(attrs, "src"); hasImageExtension(src) {
			c.add(src)
		}
	case "meta":
		collectMeta(attrs, c)
	case "link":
		if slicesContainsFold(strings.Fields(attrValue(attrs, "rel")), "image_src") {
			c.add(attrValue(attrs, "href"))
		}
	default:
		// 其他标签只可能在 style 属性里带图片。
	}
	for _, m := range cssURL.FindAllStringSubmatch(attrValue(attrs, "style"), -1) {
		c.add(m[1] + m[2] + m[3])
	}
}

// collectImg 收集 <img> 的 src、srcset 与路径带图片扩展名的 data-* 属性。
// @param attrs 标签的属性
// @param c 收集器
func collectImg(attrs []html.Attribute, c *collector) {
	c.add(attrValue(attrs, "src"))
	addSrcset(attrValue(attrs, "srcset"), c)
	for _, a := range attrs {
		if strings.HasPrefix(a.Key, "data-") && hasImageExtension(a.Val) {
			c.add(a.Val)
		}
	}
}

// collectMeta 收集声明页面代表图的 <meta>。
// @param attrs 标签的属性
// @param c 收集器
func collectMeta(attrs []html.Attribute, c *collector) {
	key := strings.ToLower(attrValue(attrs, "property"))
	if key == "" {
		key = strings.ToLower(attrValue(attrs, "name"))
	}
	if _, ok := metaImageKeys[key]; ok {
		c.add(attrValue(attrs, "content"))
	}
}

// slicesContainsFold 判断切片里是否有与 want 忽略大小写相等的元素。
// @param items 待查元素
// @param want 目标值
// @return 有为 true
func slicesContainsFold(items []string, want string) bool {
	for _, it := range items {
		if strings.EqualFold(it, want) {
			return true
		}
	}
	return false
}

// hasImageExtension 判断地址的路径是否以常见图片扩展名结尾,忽略查询串与 # 之后的部分。
// @param raw 引用原文
// @return 是图片扩展名为 true
func hasImageExtension(raw string) bool {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	_, ok := imageExtensions[strings.ToLower(path.Ext(raw))]
	return ok
}

// addSrcset 把 srcset 属性里的每个候选地址收进收集器。
// @param srcset srcset 属性值:逗号分隔的候选,每个候选是地址加可选的描述符
// @param c 收集器
func addSrcset(srcset string, c *collector) {
	for _, candidate := range parseSrcset(srcset) {
		c.add(candidate)
	}
}

// parseSrcset 按 HTML 规范的算法取出 srcset 里的地址:候选以逗号分隔,地址本身可以含逗号(只要不在末尾),
// 描述符里括号内的逗号不算分隔。
// @param s srcset 属性值
// @return 候选地址,按出现顺序
func parseSrcset(s string) []string {
	var out []string
	i := 0
	for {
		i = skipWhile(s, i, func(b byte) bool { return isSpace(b) || b == ',' })
		start := i
		i = skipWhile(s, i, func(b byte) bool { return !isSpace(b) })
		if start == i {
			return out
		}
		token := s[start:i]
		if strings.HasSuffix(token, ",") {
			out = append(out, strings.TrimRight(token, ","))
			continue
		}
		out = append(out, token)
		i = skipDescriptors(s, i)
	}
}

// skipWhile 从 i 起跳过满足条件的字节。
// @param s 文本
// @param i 起点
// @param skip 判定要跳过的字节
// @return 第一个不满足条件的位置
func skipWhile(s string, i int, skip func(byte) bool) int {
	for i < len(s) && skip(s[i]) {
		i++
	}
	return i
}

// skipDescriptors 跳过候选的描述符,停在分隔候选的逗号上;括号内的逗号不算。
// @param s srcset 属性值
// @param i 描述符起点
// @return 分隔逗号的位置,或结尾
func skipDescriptors(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch {
		case s[i] == '(':
			depth++
		case s[i] == ')' && depth > 0:
			depth--
		case s[i] == ',' && depth == 0:
			return i
		default:
			// 其他字符都属于描述符。
		}
	}
	return i
}

// isSpace 判断是否为 HTML 的空白字符。
// @param b 字节
// @return 是空白为 true
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}
