package imgrefs

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// mustParse 解析测试用的基准地址。
func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析基准地址 %q: %v", raw, err)
	}
	return u
}

// urls 取出结果里的规范化地址。
func urls(res Result) []string {
	var out []string
	for _, r := range res.Refs {
		out = append(out, r.URL)
	}
	return out
}

// TestNormalize 规范化规则:只收 http 与 https 的绝对地址,相对路径按基准解析,去掉 # 之后的部分,协议与域名转小写。
func TestNormalize(t *testing.T) {
	base := mustParse(t, "https://blog.example.com/posts/1/index.html")
	cases := []struct {
		name string
		raw  string
		base *url.URL
		want string
		ok   bool
	}{
		{"绝对地址", "https://img.example.com/a.png", nil, "https://img.example.com/a.png", true},
		{"协议与域名转小写", "HTTPS://IMG.Example.COM/A.png", nil, "https://img.example.com/A.png", true},
		{"去掉 https 默认端口", "https://img.example.com:443/a.png", nil, "https://img.example.com/a.png", true},
		{"去掉 http 默认端口", "http://img.example.com:80/a.png", nil, "http://img.example.com/a.png", true},
		{"保留非默认端口", "https://img.example.com:8443/a.png", nil, "https://img.example.com:8443/a.png", true},
		{"http 上的 443 不是默认端口", "http://img.example.com:443/a.png", nil, "http://img.example.com:443/a.png", true},
		{"IPv6 地址去掉默认端口", "https://[2001:db8::1]:443/a.png", nil, "https://[2001:db8::1]/a.png", true},
		{"去掉片段", "https://img.example.com/a.png#top", nil, "https://img.example.com/a.png", true},
		{"保留查询串", "https://img.example.com/a.png?v=1&w=2", nil, "https://img.example.com/a.png?v=1&w=2", true},
		{"首尾空白", "  https://img.example.com/a.png \n", nil, "https://img.example.com/a.png", true},
		{"相对路径按页面解析", "../img/a.png", base, "https://blog.example.com/posts/img/a.png", true},
		{"根相对路径", "/img/a.png", base, "https://blog.example.com/img/a.png", true},
		{"协议相对地址", "//cdn.example.com/a.png", base, "https://cdn.example.com/a.png", true},
		{"没有基准的相对路径", "img/a.png", nil, "", false},
		{"没有基准的协议相对地址", "//cdn.example.com/a.png", nil, "", false},
		{"data 地址", "data:image/png;base64,AAAA", base, "", false},
		{"file 协议", "file:///etc/passwd", base, "", false},
		{"带用户名密码", "https://user:pw@img.example.com/a.png", nil, "", false},
		{"空串", "   ", base, "", false},
		{"无法解析", "http://exa mple.com/a.png", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Normalize(tc.raw, tc.base)
			if got != tc.want || ok != tc.ok {
				t.Errorf("Normalize(%q) = (%q, %t), 期望 (%q, %t)", tc.raw, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestHost 域名取规范化地址的主机部分,非默认端口保留。
func TestHost(t *testing.T) {
	if got := Host("https://img.example.com:8443/a.png"); got != "img.example.com:8443" {
		t.Errorf("Host = %q", got)
	}
	if got := Host("https://img.example.com/a.png"); got != "img.example.com" {
		t.Errorf("Host = %q", got)
	}
}

// TestFromHTML 覆盖 HTML 里各处图片引用:img 的 src、srcset 与 data-* 属性、source、meta、link、行内样式,
// 以及 <base href> 对相对路径的影响。
func TestFromHTML(t *testing.T) {
	page := mustParse(t, "https://blog.example.com/posts/1/")
	cases := []struct {
		name string
		html string
		base *url.URL
		want []string
	}{
		{"img src", `<img src="/a.png">`, page, []string{"https://blog.example.com/a.png"}},
		{"实体还原", `<img src="https://i.example.com/a.png?x=1&amp;y=2">`, page,
			[]string{"https://i.example.com/a.png?x=1&y=2"}},
		{"srcset 每个候选", `<img src="a.png" srcset="a-2x.png 2x, a-3x.png 3x">`, page,
			[]string{"https://blog.example.com/posts/1/a.png", "https://blog.example.com/posts/1/a-2x.png",
				"https://blog.example.com/posts/1/a-3x.png"}},
		{"懒加载 data 属性", `<img src="placeholder.gif" data-src="real.jpg" data-id="42" data-original="o.webp?v=1">`, page,
			[]string{"https://blog.example.com/posts/1/placeholder.gif", "https://blog.example.com/posts/1/real.jpg",
				"https://blog.example.com/posts/1/o.webp?v=1"}},
		{"picture source", `<picture><source srcset="/a.webp"><source src="/b.png"><img src="/c.jpg"></picture>`, page,
			[]string{
				"https://blog.example.com/a.webp", "https://blog.example.com/b.png",
				"https://blog.example.com/c.jpg",
			}},
		{"video 的 source src 不是图片", `<video><source src="/clip.mp4"></video>`, page, nil},
		{"og:image", `<meta property="og:image" content="/cover.png"><meta name="twitter:image" content="/t.png">`,
			page, []string{"https://blog.example.com/cover.png", "https://blog.example.com/t.png"}},
		{"无关的 meta", `<meta name="description" content="/not-an-image.png">`, page, nil},
		{"link image_src", `<link rel="image_src" href="/l.png"><link rel="stylesheet" href="/s.css">`, page,
			[]string{"https://blog.example.com/l.png"}},
		{"行内样式", `<div style="background: url('/bg.jpg') no-repeat, url(/b2.png)"></div>`, page,
			[]string{"https://blog.example.com/bg.jpg", "https://blog.example.com/b2.png"}},
		{"style 块不扫描", `<style>.a{background:url(/x.png)}</style><script>var u="/y.png"</script>`, page, nil},
		{"base href 优先", `<base href="https://cdn.example.com/assets/"><img src="a.png">`, page,
			[]string{"https://cdn.example.com/assets/a.png"}},
		{"同一原文只记一次", `<img src="/a.png"><img src="/a.png">`, page, []string{"https://blog.example.com/a.png"}},
		{"不同写法同一地址都保留原文", `<img src="/a.png"><img src="https://blog.example.com/a.png">`, page,
			[]string{"https://blog.example.com/a.png", "https://blog.example.com/a.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := urls(FromHTML([]byte(tc.html), tc.base))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("得 %v, 期望 %v", got, tc.want)
			}
		})
	}
}

// TestFromHTMLUnfetchable 无法抓取的引用单独列出:没有基准的相对路径、data 地址;超长原文被截断。
func TestFromHTMLUnfetchable(t *testing.T) {
	long := "data:image/png;base64," + strings.Repeat("A", 500)
	res := FromHTML([]byte(`<img src="rel/a.png"><img src="`+long+`"><img src="https://i.example.com/ok.png">`), nil)
	if !reflect.DeepEqual(urls(res), []string{"https://i.example.com/ok.png"}) {
		t.Errorf("可抓取的引用 = %v", urls(res))
	}
	if len(res.Unfetchable) != 2 || res.Unfetchable[0] != "rel/a.png" {
		t.Fatalf("无法抓取的引用 = %v", res.Unfetchable)
	}
	if got := []rune(res.Unfetchable[1]); len(got) != maxUnfetchableRunes+1 || got[len(got)-1] != '…' {
		t.Errorf("超长原文应截到 %d 个字符加省略号,得 %d 个字符", maxUnfetchableRunes, len(got))
	}
}

// TestFromMarkdown 覆盖 Markdown 里的图片写法,代码里的不算,相对路径按给定基准解析。
func TestFromMarkdown(t *testing.T) {
	base := mustParse(t, "https://blog.example.com/")
	src := "# 标题\n\n" +
		"![行内](https://i.example.com/a.png \"标题\")\n\n" +
		"![相对](./img/b.png)\n\n" +
		"![引用式][pic]\n\n" +
		"[pic]: https://i.example.com/c.png\n\n" +
		"行内 HTML <img src=\"/d.png\"> 与链接 [不是图](https://i.example.com/link.png)\n\n" +
		"<div><img src=\"/e.png\"></div>\n\n" +
		"`![代码](https://i.example.com/code.png)`\n\n" +
		"```\n![围栏](https://i.example.com/fence.png)\n<img src=\"/fence2.png\">\n```\n\n" +
		"![[obsidian.png]]\n"
	got := urls(FromMarkdown([]byte(src), base))
	want := []string{
		"https://i.example.com/a.png",
		"https://blog.example.com/img/b.png",
		"https://i.example.com/c.png",
		"https://blog.example.com/d.png",
		"https://blog.example.com/e.png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("得 %v, 期望 %v", got, want)
	}
}

// TestFromMarkdownWithoutBase 没有基准地址时,相对路径记入无法抓取的引用,绝对地址照常。
func TestFromMarkdownWithoutBase(t *testing.T) {
	res := FromMarkdown([]byte("![a](img/a.png)\n\n![b](https://i.example.com/b.png)\n"), nil)
	if !reflect.DeepEqual(urls(res), []string{"https://i.example.com/b.png"}) {
		t.Errorf("可抓取的引用 = %v", urls(res))
	}
	if !reflect.DeepEqual(res.Unfetchable, []string{"img/a.png"}) {
		t.Errorf("无法抓取的引用 = %v", res.Unfetchable)
	}
}

// TestParseSrcset 按 HTML 规范拆候选:地址里的逗号保留,描述符里括号内的逗号不分隔,末尾逗号不进地址。
func TestParseSrcset(t *testing.T) {
	cases := map[string][]string{
		"a.png":                              {"a.png"},
		"a.png 1x, b.png 2x":                 {"a.png", "b.png"},
		"a.png 1x,b.png 2x":                  {"a.png", "b.png"},
		"a.png,b.png":                        {"a.png,b.png"},
		"a.png, b.png":                       {"a.png", "b.png"},
		"  a.png   100w ,  b.png  200w ":     {"a.png", "b.png"},
		"a.png calc(1,2) , b.png":            {"a.png", "b.png"},
		"":                                   nil,
		"https://i.example.com/a,b.jpg 640w": {"https://i.example.com/a,b.jpg"},
	}
	for in, want := range cases {
		if got := parseSrcset(in); !reflect.DeepEqual(got, want) {
			t.Errorf("parseSrcset(%q) = %v, 期望 %v", in, got, want)
		}
	}
}
