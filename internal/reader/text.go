package reader

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

func readerText(s string, limit int) string { return extractText(s, limit, true) }
func extractText(s string, limit int, blocks bool) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	suppressed := 0
	pre := 0
	block := func(tag string) bool {
		return strings.Contains("|p|div|section|article|blockquote|h1|h2|h3|h4|ul|ol|pre|table|tr|", "|"+tag+"|")
	}
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		t := z.Token()
		switch kind {
		case html.StartTagToken, html.SelfClosingTagToken:
			if t.Data == "script" || t.Data == "style" || t.Data == "iframe" {
				if kind != html.SelfClosingTagToken {
					suppressed++
				}
				continue
			}
			if suppressed > 0 {
				continue
			}
			if blocks {
				if block(t.Data) {
					b.WriteString("\n\n")
				}
				if t.Data == "br" {
					b.WriteByte('\n')
				}
				if t.Data == "li" {
					if !strings.HasSuffix(b.String(), "\n") {
						b.WriteByte('\n')
					}
					b.WriteString("- ")
				}
				if t.Data == "pre" {
					pre++
				}
			}
		case html.EndTagToken:
			if (t.Data == "script" || t.Data == "style" || t.Data == "iframe") && suppressed > 0 {
				suppressed--
				continue
			}
			if suppressed > 0 {
				continue
			}
			if blocks {
				if t.Data == "pre" && pre > 0 {
					pre--
				}
				if block(t.Data) {
					b.WriteString("\n\n")
				}
				if t.Data == "li" {
					b.WriteByte('\n')
				}
			}
		case html.TextToken:
			if suppressed > 0 {
				continue
			}
			if blocks && pre > 0 {
				b.WriteString(t.Data)
			} else {
				raw := t.Data
				text := strings.Join(strings.Fields(raw), " ")
				first, _ := utf8.DecodeRuneInString(raw)
				last, _ := utf8.DecodeLastRuneInString(raw)
				if len(raw) > 0 && unicode.IsSpace(first) {
					b.WriteByte(' ')
				}
				b.WriteString(text)
				if len(raw) > 0 && unicode.IsSpace(last) {
					b.WriteByte(' ')
				}
			}
		}
		if b.Len() > limit*6 {
			break
		}
	}
	out := b.String()
	if blocks {
		lines := strings.Split(out, "\n")
		clean := []string{}
		blank := false
		for _, line := range lines {
			line = strings.TrimRight(line, " \t\r")
			if strings.TrimSpace(line) == "" {
				if blank {
					continue
				}
				blank = true
				line = ""
			} else {
				blank = false
			}
			clean = append(clean, line)
		}
		out = strings.TrimSpace(strings.Join(clean, "\n"))
	} else {
		out = strings.Join(strings.Fields(out), " ")
	}
	runes := []rune(out)
	if len(runes) > limit {
		out = string(runes[:limit])
	}
	return out
}
