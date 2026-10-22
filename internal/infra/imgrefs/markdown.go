package imgrefs

import (
	"net/url"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// markdown CommonMark 解析器,无状态、并发安全。
var markdown = goldmark.New()

// FromMarkdown 找出一份 Markdown 里的图片引用。
// @Description: 按 CommonMark 解析:行内图片 ![](地址)、引用式图片 ![][ref](按引用定义解析),以及行内与块级的 HTML
// (按 HTML 的规则扫描)。代码块与行内代码里的内容不是图片,不会被扫描。
// @param src Markdown 文本
// @param base 调用方给定的基准地址,可为 nil
// @return 扫描到的引用
func FromMarkdown(src []byte, base *url.URL) Result {
	var c collector
	doc := markdown.Parser().Parse(text.NewReader(src))
	// Walk 只在回调返回错误时才返回错误,回调不产生错误。
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Image:
			c.add(string(node.Destination))
		case *ast.RawHTML:
			scanHTML(node.Segments.Value(src), &c)
		case *ast.HTMLBlock:
			scanHTML(node.Lines().Value(src), &c)
		default:
			// 其他节点不含图片引用。
		}
		return ast.WalkContinue, nil
	})
	return c.resolve(base)
}
