package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type DiscoveredFeed struct{ URL, Title string }

// Discover uses the fetcher's public-address-only transport, including redirects.
// It never follows discovered links automatically.
func (f *Fetcher) Discover(ctx context.Context, raw string) ([]DiscoveredFeed, error) {
	address, err := WebURL(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Tidings/0.1 (+https://github.com/airencracken/tidings)")
	req.Header.Set("Accept", "text/html, application/rss+xml, application/atom+xml")
	res, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("website did not return a successful response")
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFeedBytes {
		return nil, errors.New("website is too large")
	}
	base := res.Request.URL
	if parsed, err := ParseFeed(b, base.String(), time.Now()); err == nil {
		return []DiscoveredFeed{{URL: base.String(), Title: parsed.Title}}, nil
	}
	return discoverLinks(string(b), base)
}
func discoverLinks(body string, base *url.URL) ([]DiscoveredFeed, error) {
	z := html.NewTokenizer(strings.NewReader(body))
	out := []DiscoveredFeed{}
	seen := map[string]bool{}
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, z.Err()
			}
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		if t.Data != "link" {
			continue
		}
		attrs := map[string]string{}
		for _, a := range t.Attr {
			attrs[a.Key] = a.Val
		}
		alternate := false
		for _, rel := range strings.Fields(strings.ToLower(attrs["rel"])) {
			if rel == "alternate" {
				alternate = true
			}
		}
		mime := strings.ToLower(strings.TrimSpace(strings.Split(attrs["type"], ";")[0]))
		if !alternate || (mime != "application/rss+xml" && mime != "application/atom+xml") {
			continue
		}
		u, err := url.Parse(attrs["href"])
		if err != nil || attrs["href"] == "" {
			continue
		}
		address, err := WebURL(base.ResolveReference(u).String())
		if err != nil || seen[address] {
			continue
		}
		seen[address] = true
		title := plainText(attrs["title"], 200)
		if title == "" {
			title = address
		}
		out = append(out, DiscoveredFeed{URL: address, Title: title})
		if len(out) == 20 {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no RSS or Atom links found; try the site's direct feed address")
	}
	return out, nil
}
func (a *App) discover(w http.ResponseWriter, r *http.Request) {
	if scope(r) && !current(r).User.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	feeds, err := a.Fetcher.Discover(r.Context(), r.PostFormValue("website"))
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	a.render(w, r, page{Title: "Choose a feed", Kind: "discovery", Discovered: feeds, GroupScope: scope(r)}, 200)
}
