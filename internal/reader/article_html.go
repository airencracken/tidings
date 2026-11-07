package reader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxArticleHTML = 512 << 10

// Render only this allowlist, rebuilding every attribute. The same boundary is
// used for publisher HTML, downloaded articles, and stored/restored content.
func articleHTML(raw, base string, imageURL func(string) string) string {
	if len(raw) > maxArticleHTML {
		return ""
	}
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(raw), context)
	if err != nil {
		return ""
	}
	root := &html.Node{Type: html.ElementNode, Data: "div"}
	count := 0
	var walk func(*html.Node, *html.Node, int)
	walk = func(n, parent *html.Node, depth int) {
		count++
		if count > 12000 || depth > 64 {
			return
		}
		if n.Type == html.TextNode {
			parent.AppendChild(&html.Node{Type: html.TextNode, Data: n.Data})
			return
		}
		if n.Type != html.ElementNode {
			return
		}
		tag := n.Data
		if strings.Contains("|script|style|iframe|object|embed|svg|math|form|input|button|textarea|select|template|noscript|head|nav|footer|", "|"+tag+"|") {
			return
		}
		out := parent
		allowed := strings.Contains("|p|br|hr|h1|h2|h3|h4|h5|h6|ul|ol|li|blockquote|pre|code|strong|b|em|i|s|del|sub|sup|a|figure|figcaption|table|thead|tbody|tfoot|tr|th|td|dl|dt|dd|div|section|article|", "|"+tag+"|")
		attrs := map[string]string{}
		for _, a := range n.Attr {
			if a.Namespace == "" {
				attrs[a.Key] = a.Val
			}
		}
		if tag == "img" {
			if imageURL == nil || attrs["width"] == "0" || attrs["width"] == "1" || attrs["height"] == "0" || attrs["height"] == "1" {
				return
			}
			address := mediaAddress(attrs["src"], base)
			if address == "" {
				return
			}
			local := imageURL(address)
			if local == "" {
				return
			}
			out = &html.Node{Type: html.ElementNode, Data: "img", Attr: []html.Attribute{{Key: "src", Val: local}, {Key: "alt", Val: plainText(attrs["alt"], 300)}, {Key: "loading", Val: "lazy"}, {Key: "decoding", Val: "async"}, {Key: "data-feed-image", Val: ""}}}
			parent.AppendChild(out)
			return
		}
		if allowed {
			out = &html.Node{Type: html.ElementNode, Data: tag}
			if tag == "a" {
				if link := articleLink(attrs["href"], base); link != "" {
					out.Attr = []html.Attribute{{Key: "href", Val: link}, {Key: "rel", Val: "noopener noreferrer"}, {Key: "referrerpolicy", Val: "no-referrer"}}
				}
			}
			parent.AppendChild(out)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, out, depth+1)
		}
	}
	for _, n := range nodes {
		walk(n, root, 0)
	}
	var b bytes.Buffer
	for n := root.FirstChild; n != nil; n = n.NextSibling {
		if html.Render(&b, n) != nil {
			return ""
		}
	}
	if b.Len() > maxArticleHTML {
		return ""
	}
	return b.String()
}

func mediaAddress(raw, base string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u, err := b.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return ""
	}
	address, err := WebURL(u.String())
	if err != nil {
		return ""
	}
	return address
}
func imageKey(address string) string {
	sum := sha256.Sum256([]byte(address))
	return hex.EncodeToString(sum[:])
}
func inlineImages(raw, base string) map[string]string {
	out := map[string]string{}
	articleHTML(raw, base, func(address string) string {
		key := imageKey(address)
		if len(out) < 64 {
			out[key] = address
		}
		return "/image/" + key
	})
	return out
}
func renderArticle(e Entry, images bool) template.HTML {
	if e.BodyHTML == "" {
		return template.HTML("<p class=\"plain-article\">" + template.HTMLEscapeString(e.Body) + "</p>")
	}
	var local func(string) string
	if images {
		allowed := inlineImages(e.BodyHTML, e.URL)
		local = func(address string) string {
			key := imageKey(address)
			if allowed[key] == "" {
				return ""
			}
			return fmt.Sprintf("/story/%d/image/%s", e.ID, key)
		}
	}
	return template.HTML(articleHTML(e.BodyHTML, e.URL, local))
}
