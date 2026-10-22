package imgrefs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrMalformed 站点地图或订阅源的内容无法解析。
var ErrMalformed = errors.New("imgrefs: 内容无法解析为站点地图或订阅源")

// Kind 一份抓回的内容属于哪一类。
type Kind int

// 内容的类别,由 Sniff 判定。
const (
	KindHTML         Kind = iota // 网页,以及其他无法识别的内容
	KindSitemap                  // 站点地图:根元素 urlset,列出页面,可附带图片
	KindSitemapIndex             // 站点地图索引:根元素 sitemapindex,列出子站点地图
	KindFeed                     // 订阅源:RSS、RSS 1.0 或 Atom,列出文章页面
	KindTextList                 // 文本站点地图:每个非空行是一个 http 或 https 绝对地址
)

// ListedImage 站点地图里随页面列出的图片。
type ListedImage struct {
	Page  string // 图片所在的页面地址;页面缺失时为空串
	Image string // 图片地址原文
}

// Listing 站点地图、站点地图索引、订阅源或文本站点地图的内容。地址都是原文,调用方负责规范化。
type Listing struct {
	Pages    []string      // 文章页面地址
	Children []string      // 子站点地图地址,仅站点地图索引有
	Images   []ListedImage // 站点地图随页面列出的图片,仅站点地图有
}

// Sniff 按内容判定类别。
// @Description: 以 XML 根元素区分:urlset 为站点地图,sitemapindex 为索引,rss、feed、RDF 为订阅源;
// 不是 XML 时,每个非空行都是 http 或 https 绝对地址的按文本站点地图处理;其余都当作网页。
// 只读到根元素为止,不要求整份内容是合法 XML。
// @param data 抓回的内容,已解压
// @return 类别
func Sniff(data []byte) Kind {
	trimmed := bytes.TrimLeft(data, "\xef\xbb\xbf \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("<")) {
		return sniffXML(trimmed)
	}
	if isTextList(trimmed) {
		return KindTextList
	}
	return KindHTML
}

// sniffXML 读到第一个元素,按它的名字判定类别。
// @param data 以 < 开头的内容
// @return 类别;读不到元素时按网页处理
func sniffXML(data []byte) Kind {
	d := newDecoder(data)
	for {
		tok, err := d.Token()
		if err != nil {
			return KindHTML
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "urlset":
			return KindSitemap
		case "sitemapindex":
			return KindSitemapIndex
		case "rss", "feed", "RDF":
			return KindFeed
		default:
			return KindHTML
		}
	}
}

// isTextList 判断每个非空行是否都是 http 或 https 绝对地址,且至少有一行。
// @param data 已去掉开头空白的内容
// @return 是文本站点地图为 true
func isTextList(data []byte) bool {
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := Normalize(line, nil); !ok {
			return false
		}
		found = true
	}
	return found
}

// newDecoder 构造容错的 XML 解码器:不严格校验(真实站点地图里常有没转义的 &),声明的字符集一律按字节透传
// (地址是 ASCII 或 UTF-8)。
// @param data XML 内容
// @return 解码器
func newDecoder(data []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	return d
}

// ParseListing 解析站点地图、站点地图索引、订阅源或文本站点地图。
// @Description: 站点地图取 <url><loc> 为页面,<image:image><image:loc> 为随该页面列出的图片;索引取 <sitemap><loc>
// 为子站点地图;订阅源取 RSS 与 RSS 1.0 的 <item><link> 文本、Atom 的 <entry><link> 里 rel 缺省或为 alternate 的 href。
// @param data 抓回的内容,已解压
// @param kind Sniff 判定的类别;KindHTML 没有列表可解析,返回空结果
// @return 列出的地址
// @return 内容不是合法的 XML 时包裹 ErrMalformed
func ParseListing(data []byte, kind Kind) (Listing, error) {
	switch kind {
	case KindTextList:
		return parseTextList(data), nil
	case KindSitemap, KindSitemapIndex, KindFeed:
		return parseXMLListing(data, kind)
	default:
		return Listing{}, nil
	}
}

// parseTextList 取文本站点地图里的每个非空行。
// @param data 文本内容
// @return 页面地址
func parseTextList(data []byte) Listing {
	var l Listing
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			l.Pages = append(l.Pages, line)
		}
	}
	return l
}

// listingParser 解析 XML 列表时的状态。
type listingParser struct {
	kind    Kind
	out     Listing
	path    []string        // 当前元素路径,本地名
	text    strings.Builder // 当前正在收集文本的元素内容
	page    string          // 站点地图里当前 <url> 的 <loc>
	images  []string        // 站点地图里当前 <url> 下列出的图片
	capture bool            // 当前元素的文本是否要收集
}

// parseXMLListing 流式解析 XML 列表。
// @param data XML 内容
// @param kind 类别,决定取哪些元素
// @return 列出的地址
// @return 解析失败时包裹 ErrMalformed
func parseXMLListing(data []byte, kind Kind) (Listing, error) {
	p := &listingParser{kind: kind}
	d := newDecoder(data)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return p.out, nil
		}
		if err != nil {
			return Listing{}, fmt.Errorf("%w: %w", ErrMalformed, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			p.start(t)
		case xml.CharData:
			if p.capture {
				p.text.Write(t)
			}
		case xml.EndElement:
			p.end()
		default:
			// 声明、注释、处理指令不含地址。
		}
	}
}

// parent 返回当前元素的父元素本地名;没有父元素为空串。
// @return 父元素本地名
func (p *listingParser) parent() string {
	if len(p.path) < 2 {
		return ""
	}
	return p.path[len(p.path)-2]
}

// start 处理一个开始标签。
// @param t 开始标签
func (p *listingParser) start(t xml.StartElement) {
	p.path = append(p.path, t.Name.Local)
	p.text.Reset()
	p.capture = false
	parent := p.parent()
	switch p.kind {
	case KindSitemap:
		if t.Name.Local == "url" {
			p.page, p.images = "", nil
		}
		p.capture = t.Name.Local == "loc" && (parent == "url" || parent == "image")
	case KindSitemapIndex:
		p.capture = t.Name.Local == "loc" && parent == "sitemap"
	case KindFeed:
		switch {
		case t.Name.Local == "link" && parent == "item":
			p.capture = true
		case t.Name.Local == "link" && parent == "entry":
			p.atomLink(t)
		default:
			// 其他元素不含文章链接。
		}
	default:
		// 文本站点地图与网页不走 XML 解析。
	}
}

// atomLink 取 Atom 条目里指向文章页面的 <link href>。
// @param t <link> 开始标签
func (p *listingParser) atomLink(t xml.StartElement) {
	rel, href := "", ""
	for _, a := range t.Attr {
		switch a.Name.Local {
		case "rel":
			rel = a.Value
		case "href":
			href = a.Value
		default:
			// 其他属性与文章链接无关。
		}
	}
	if (rel == "" || rel == "alternate") && strings.TrimSpace(href) != "" {
		p.out.Pages = append(p.out.Pages, strings.TrimSpace(href))
	}
}

// end 处理一个结束标签:把收集到的文本归到对应的列表里。
func (p *listingParser) end() {
	name := p.path[len(p.path)-1]
	parent := p.parent()
	if p.capture {
		value := strings.TrimSpace(p.text.String())
		p.capture = false
		p.text.Reset()
		if value != "" {
			p.collect(name, parent, value)
		}
	}
	if p.kind == KindSitemap && name == "url" {
		p.flushURL()
	}
	p.path = p.path[:len(p.path)-1]
}

// collect 把一个 <loc> 或 <link> 的文本放进列表。
// @param name 元素本地名
// @param parent 父元素本地名
// @param value 元素文本
func (p *listingParser) collect(name, parent, value string) {
	switch {
	case p.kind == KindSitemap && name == "loc" && parent == "url":
		p.page = value
	case p.kind == KindSitemap && name == "loc" && parent == "image":
		p.images = append(p.images, value)
	case p.kind == KindSitemapIndex:
		p.out.Children = append(p.out.Children, value)
	case p.kind == KindFeed:
		p.out.Pages = append(p.out.Pages, value)
	default:
		// 其他元素的文本不是地址。
	}
}

// flushURL 站点地图的一个 <url> 结束:页面与随它列出的图片入列表。
func (p *listingParser) flushURL() {
	if p.page != "" {
		p.out.Pages = append(p.out.Pages, p.page)
	}
	for _, img := range p.images {
		p.out.Images = append(p.out.Images, ListedImage{Page: p.page, Image: img})
	}
	p.page, p.images = "", nil
}
