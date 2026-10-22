package imgrefs

import (
	"errors"
	"reflect"
	"testing"
)

// TestSniff 按内容判定类别:XML 看根元素,纯文本看是否每行都是地址,其余当网页。
func TestSniff(t *testing.T) {
	const sitemapNS = "http://www.sitemaps.org/schemas/sitemap/0.9"
	cases := []struct {
		name string
		data string
		want Kind
	}{
		{"站点地图", `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="` + sitemapNS + `"></urlset>`, KindSitemap},
		{"站点地图索引", `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"/>`, KindSitemapIndex},
		{"RSS", `<?xml version="1.0"?><rss version="2.0"><channel/></rss>`, KindFeed},
		{"Atom", `<feed xmlns="http://www.w3.org/2005/Atom"/>`, KindFeed},
		{"RSS 1.0", `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/>`, KindFeed},
		{"带字节序标记与空白", "\xef\xbb\xbf \n<urlset/>", KindSitemap},
		{"声明了非 UTF-8 字符集", `<?xml version="1.0" encoding="ISO-8859-1"?><urlset/>`, KindSitemap},
		{"HTML 文档", `<!DOCTYPE html><html><body></body></html>`, KindHTML},
		{"XHTML", `<?xml version="1.0"?><!DOCTYPE html><html xmlns="http://www.w3.org/1999/xhtml"></html>`, KindHTML},
		{"文本站点地图", "https://a.example.com/1\n\nhttps://a.example.com/2\n", KindTextList},
		{"含非地址行的文本", "https://a.example.com/1\nhello\n", KindHTML},
		{"空内容", "", KindHTML},
		{"二进制", "\x00\x01\x02", KindHTML},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sniff([]byte(tc.data)); got != tc.want {
				t.Errorf("Sniff = %v, 期望 %v", got, tc.want)
			}
		})
	}
}

// TestParseSitemap 站点地图取页面与随页面列出的图片;图片扩展的元素顺序不影响与页面的配对。
func TestParseSitemap(t *testing.T) {
	data := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"
        xmlns:image="http://www.google.com/schemas/sitemap-image/1.1">
  <url>
    <loc>https://blog.example.com/a</loc>
    <lastmod>2026-01-01</lastmod>
    <image:image><image:loc>https://img.example.com/a1.png</image:loc></image:image>
    <image:image><image:loc>https://img.example.com/a2.png</image:loc></image:image>
  </url>
  <url>
    <image:image><image:loc>https://img.example.com/b1.png</image:loc></image:image>
    <loc>https://blog.example.com/b?x=1&y=2</loc>
  </url>
  <url><loc> https://blog.example.com/c </loc></url>
</urlset>`
	got, err := ParseListing([]byte(data), KindSitemap)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	wantPages := []string{
		"https://blog.example.com/a", "https://blog.example.com/b?x=1&y=2", "https://blog.example.com/c",
	}
	if !reflect.DeepEqual(got.Pages, wantPages) {
		t.Errorf("页面 = %v, 期望 %v", got.Pages, wantPages)
	}
	wantImages := []ListedImage{
		{Page: "https://blog.example.com/a", Image: "https://img.example.com/a1.png"},
		{Page: "https://blog.example.com/a", Image: "https://img.example.com/a2.png"},
		{Page: "https://blog.example.com/b?x=1&y=2", Image: "https://img.example.com/b1.png"},
	}
	if !reflect.DeepEqual(got.Images, wantImages) {
		t.Errorf("图片 = %v, 期望 %v", got.Images, wantImages)
	}
}

// TestParseSitemapIndex 索引取子站点地图地址。
func TestParseSitemapIndex(t *testing.T) {
	data := `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://blog.example.com/sitemap-1.xml</loc><lastmod>2026-01-01</lastmod></sitemap>
  <sitemap><loc>https://blog.example.com/sitemap-2.xml.gz</loc></sitemap>
</sitemapindex>`
	got, err := ParseListing([]byte(data), KindSitemapIndex)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := []string{"https://blog.example.com/sitemap-1.xml", "https://blog.example.com/sitemap-2.xml.gz"}
	if !reflect.DeepEqual(got.Children, want) || len(got.Pages) != 0 {
		t.Errorf("子站点地图 = %v 页面 = %v", got.Children, got.Pages)
	}
}

// TestParseFeed 订阅源只取文章链接:RSS 的 item/link,Atom entry 里 rel 缺省或为 alternate 的 link;频道自己的链接不取。
func TestParseFeed(t *testing.T) {
	rss := `<rss version="2.0"><channel><link>https://blog.example.com/</link>
<item><title>t</title><link>https://blog.example.com/p1</link></item>
<item><link><![CDATA[https://blog.example.com/p2]]></link></item></channel></rss>`
	got, err := ParseListing([]byte(rss), KindFeed)
	if err != nil {
		t.Fatalf("解析 RSS 失败: %v", err)
	}
	want := []string{"https://blog.example.com/p1", "https://blog.example.com/p2"}
	if !reflect.DeepEqual(got.Pages, want) {
		t.Errorf("RSS 页面 = %v, 期望 %v", got.Pages, want)
	}

	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><link rel="self" href="https://blog.example.com/feed"/>
<entry><link href="https://blog.example.com/p1"/></entry>
<entry><link rel="alternate" href="https://blog.example.com/p2"/>
<link rel="enclosure" href="https://blog.example.com/p2.mp3"/></entry></feed>`
	got, err = ParseListing([]byte(atom), KindFeed)
	if err != nil {
		t.Fatalf("解析 Atom 失败: %v", err)
	}
	want = []string{"https://blog.example.com/p1", "https://blog.example.com/p2"}
	if !reflect.DeepEqual(got.Pages, want) {
		t.Errorf("Atom 页面 = %v, 期望 %v", got.Pages, want)
	}
}

// TestParseTextList 文本站点地图每个非空行一个页面。
func TestParseTextList(t *testing.T) {
	got, err := ParseListing([]byte("https://a.example.com/1\r\n\r\n https://a.example.com/2 \n"), KindTextList)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if want := []string{"https://a.example.com/1", "https://a.example.com/2"}; !reflect.DeepEqual(got.Pages, want) {
		t.Errorf("页面 = %v, 期望 %v", got.Pages, want)
	}
}

// TestParseListingMalformed 内容在中途被截断时报错,不返回只解析了一半的列表。
func TestParseListingMalformed(t *testing.T) {
	data := `<urlset><url><loc>https://blog.example.com/a</loc></url><url><loc>https://blog.exa`
	if _, err := ParseListing([]byte(data), KindSitemap); !errors.Is(err, ErrMalformed) {
		t.Fatalf("期望 ErrMalformed,得 %v", err)
	}
}

// TestParseListingTolerance 没转义的 & 与声明的非 UTF-8 字符集不影响解析。
func TestParseListingTolerance(t *testing.T) {
	data := `<?xml version="1.0" encoding="GB2312"?>
<urlset><url><loc>https://blog.example.com/a?x=1&y=2</loc></url></urlset>`
	got, err := ParseListing([]byte(data), KindSitemap)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if want := []string{"https://blog.example.com/a?x=1&y=2"}; !reflect.DeepEqual(got.Pages, want) {
		t.Errorf("页面 = %v, 期望 %v", got.Pages, want)
	}
}
