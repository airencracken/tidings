package reader

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func entryImage(entry xmlEntry, body, base string) string {
	candidates := []string{}
	for _, image := range append(entry.Thumbnails, entry.MediaGroup.Thumbnails...) {
		candidates = append(candidates, image.URL)
	}
	for _, image := range append(append(entry.Enclosures, entry.Contents...), entry.MediaGroup.Contents...) {
		if strings.HasPrefix(image.Type, "image/") || image.Medium == "image" {
			candidates = append(candidates, image.URL)
		}
	}
	for _, link := range entry.Links {
		if link.Rel == "enclosure" && strings.HasPrefix(link.Type, "image/") {
			candidates = append(candidates, link.Href)
		}
	}
	z := html.NewTokenizer(strings.NewReader(body))
	for z.Next() != html.ErrorToken {
		t := z.Token()
		if t.Type != html.StartTagToken && t.Type != html.SelfClosingTagToken || t.Data != "img" {
			continue
		}
		src, tiny := "", false
		for _, attr := range t.Attr {
			if attr.Key == "src" {
				src = attr.Val
			}
			if (attr.Key == "width" || attr.Key == "height") && (attr.Val == "0" || attr.Val == "1") {
				tiny = true
			}
		}
		if !tiny {
			candidates = append(candidates, src)
		}
		if len(candidates) > 32 {
			break
		}
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if address, err := WebURL(b.ResolveReference(u).String()); err == nil {
			return address
		}
	}
	return ""
}
