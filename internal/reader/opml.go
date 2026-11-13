package reader

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	URL      string    `xml:"xmlUrl,attr,omitempty"`
	Children []outline `xml:"outline"`
}
type opmlDoc struct {
	XMLName  xml.Name  `xml:"opml"`
	Version  string    `xml:"version,attr"`
	Title    string    `xml:"head>title"`
	Outlines []outline `xml:"body>outline"`
}

func ParseOPML(data []byte) ([]Feed, error) {
	if len(data) > 1<<20 {
		return nil, errors.New("OPML file must be 1 MB or smaller")
	}
	// Check depth before recursive decoding, including unrecognized elements.
	d := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	roots := 0
	for {
		t, err := d.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		switch t.(type) {
		case xml.Directive:
			return nil, errors.New("XML directives are not supported")
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return nil, errors.New("OPML must contain one XML root")
				}
			}
			depth++
			if depth > 16 {
				return nil, errors.New("OPML nesting is too deep")
			}
		case xml.EndElement:
			depth--
		}
	}
	var doc opmlDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var out []Feed
	var walk func([]outline, string) error
	walk = func(items []outline, cat string) error {
		for _, o := range items {
			label := o.Title
			if label == "" {
				label = o.Text
			}
			if o.URL != "" {
				u, err := WebURL(o.URL)
				if err != nil {
					return err
				}
				c := strings.TrimSpace(cat)
				if _, err := topicTag(c); err != nil {
					return err
				}
				out = append(out, Feed{URL: u, Title: label, Category: c})
				if len(out) > 200 {
					return errors.New("import at most 200 feeds at a time")
				}
			} else {
				if err := walk(o.Children, label); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(doc.Outlines, ""); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("no feed addresses found in this OPML file")
	}
	return out, nil
}

func EncodeOPML(feeds []Feed) ([]byte, error) {
	d := opmlDoc{Version: "2.0", Title: "Tidings subscriptions"}
	indexes := map[string]int{}
	for _, f := range feeds {
		// A source's first tag names its folder; untagged sources stay at
		// the top level, so importing them adds no tag.
		if f.Category == "" {
			d.Outlines = append(d.Outlines, outline{Text: f.Title, Title: f.Title, Type: "rss", URL: f.URL})
			continue
		}
		i, ok := indexes[f.Category]
		if !ok {
			i = len(d.Outlines)
			indexes[f.Category] = i
			d.Outlines = append(d.Outlines, outline{Text: f.Category})
		}
		d.Outlines[i].Children = append(d.Outlines[i].Children, outline{Text: f.Title, Title: f.Title, Type: "rss", URL: f.URL})
	}
	b, err := xml.MarshalIndent(d, "", "  ")
	return append([]byte(xml.Header), b...), err
}
