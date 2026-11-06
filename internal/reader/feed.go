package reader

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

const MaxFeedBytes = 4 << 20
const MaxEntries = 1000

type Entry struct {
	ID        int64  `json:"id"`
	FeedID    int64  `json:"feed_id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Body      string `json:"body"`
	Published int64  `json:"published"`
	Read      bool   `json:"read"`
	Saved     bool   `json:"saved"`
	Source    string `json:"source"`
	Category  string `json:"category"`
}

type ParsedFeed struct {
	Title   string
	Entries []Entry
}
type xmlText struct {
	Text  string `xml:",chardata"`
	Inner string `xml:",innerxml"`
	Type  string `xml:"type,attr"`
}

func (t xmlText) value() string {
	if strings.EqualFold(t.Type, "xhtml") {
		return plainText(t.Inner, 64000)
	}
	return plainText(t.Text, 64000)
}

func (t xmlText) raw() string {
	if strings.EqualFold(t.Type, "text") {
		return html.EscapeString(t.Text)
	}
	if strings.EqualFold(t.Type, "xhtml") {
		return t.Inner
	}
	return t.Text
}

type xmlLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Text string `xml:",chardata"`
}
type xmlEntry struct {
	Title       xmlText   `xml:"title"`
	Links       []xmlLink `xml:"link"`
	GUID        string    `xml:"guid"`
	ID          string    `xml:"id"`
	Description string    `xml:"description"`
	Summary     xmlText   `xml:"summary"`
	Content     xmlText   `xml:"content"`
	Encoded     string    `xml:"encoded"`
	PubDate     string    `xml:"pubDate"`
	Published   string    `xml:"published"`
	Updated     string    `xml:"updated"`
	Date        string    `xml:"date"`
}

func WebURL(raw string) (string, error) {
	if len(raw) > 2048 {
		return "", errors.New("web address is too long")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Opaque != "" {
		return "", errors.New("use an HTTP or HTTPS address without a username or password")
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return "", errors.New("only web ports 80 and 443 are supported")
	}
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

func plainText(s string, limit int) string { return extractText(s, limit, false) }

func ParseFeed(data []byte, base string, now time.Time) (ParsedFeed, error) {
	var result ParsedFeed
	if len(data) > MaxFeedBytes {
		return result, errors.New("feed exceeds 4 MiB")
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = charset.NewReaderLabel
	root := ""
	depth := 0
	channel := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ParsedFeed{}, fmt.Errorf("invalid feed XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return ParsedFeed{}, errors.New("XML directives are not supported")
		case xml.StartElement:
			depth++
			if depth == 1 && root != "" {
				return ParsedFeed{}, errors.New("feed must contain one XML root")
			}
			if depth > 32 {
				return ParsedFeed{}, errors.New("feed nesting is too deep")
			}
			if root == "" {
				root = t.Name.Local
				if root != "rss" && root != "feed" && root != "RDF" {
					return result, errors.New("this address does not contain an RSS or Atom feed")
				}
				continue
			}
			if t.Name.Local == "channel" {
				channel = true
			}
			if t.Name.Local == "title" && ((root == "feed" && depth == 2) || (channel && depth == 3)) {
				var title xmlText
				if err = d.DecodeElement(&title, &t); err != nil {
					return ParsedFeed{}, err
				}
				result.Title = plainText(title.value(), 200)
				depth--
			}
			if (t.Name.Local == "item" && (root == "rss" || root == "RDF")) || (t.Name.Local == "entry" && root == "feed" && depth == 2) {
				var item xmlEntry
				if err = d.DecodeElement(&item, &t); err != nil {
					return ParsedFeed{}, err
				}
				depth--
				if len(result.Entries) >= MaxEntries {
					continue
				}
				if e, ok := normalizeEntry(item, base, now); ok {
					result.Entries = append(result.Entries, e)
				}
			}
		case xml.EndElement:
			depth--
			if t.Name.Local == "channel" {
				channel = false
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return ParsedFeed{}, errors.New("text outside the feed root")
			}
		}
	}
	if root == "" || depth != 0 {
		return ParsedFeed{}, errors.New("empty or incomplete feed")
	}
	if result.Title == "" {
		u, _ := url.Parse(base)
		if u != nil {
			result.Title = u.Hostname()
		}
	}
	return result, nil
}

func normalizeEntry(x xmlEntry, base string, now time.Time) (Entry, bool) {
	raw := ""
	for _, l := range x.Links {
		if l.Rel == "alternate" || l.Rel == "" {
			raw = l.Href
			if raw == "" {
				raw = strings.TrimSpace(l.Text)
			}
			break
		}
	}
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return Entry{}, false
	}
	b, err := url.Parse(base)
	if err != nil {
		return Entry{}, false
	}
	link, err := WebURL(b.ResolveReference(u).String())
	if err != nil {
		return Entry{}, false
	}
	title := plainText(x.Title.value(), 500)
	if title == "" {
		title = "Untitled story"
	}
	body := x.Encoded
	if body == "" {
		body = x.Content.raw()
	}
	if body == "" {
		body = x.Description
	}
	if body == "" {
		body = x.Summary.raw()
	}
	stamp := now
	for _, rawDate := range []string{x.Published, x.PubDate, x.Date, x.Updated} {
		found := false
		for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, "Mon, 2 Jan 2006 15:04:05 -0700", "2006-01-02"} {
			if t, e := time.Parse(layout, strings.TrimSpace(rawDate)); e == nil {
				stamp = t
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if stamp.After(now) {
		stamp = now
	}
	key := x.GUID
	if key == "" {
		key = x.ID
	}
	if key == "" {
		key = link
	}
	sum := sha256.Sum256([]byte(key))
	return Entry{Key: hex.EncodeToString(sum[:]), Title: title, URL: link, Body: readerText(body, 64000), Published: stamp.Unix()}, true
}
