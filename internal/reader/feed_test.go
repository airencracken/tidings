package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

var fixtureTime = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

const rssFixture = `<?xml version="1.0"?><rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>A &amp; B</title><item><title>A good story</title><link>https://example.org/story#tracking</link><guid>first</guid><pubDate>Thu, 05 Nov 2026 12:00:00 +0000</pubDate><description><![CDATA[<p>Hello <b>world</b>.</p><script>alert('x')</script><img src="https://tracker.example/pixel">]]></description></item></channel></rss>`

func TestParseFeedFormats(t *testing.T) {
	cases := []struct{ name, xml, title, body, link string }{
		{"rss", rssFixture, "A & B", "Hello world .", "https://example.org/story"},
		{"atom", `<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><entry><id>tag:example,1</id><title>Story</title><link rel="self" href="https://example.org/api"/><link rel="alternate" href="/story"/><published>2026-11-05T12:00:00Z</published><content type="html">&lt;p&gt;Hello atom&lt;/p&gt;</content></entry></feed>`, "Atom", "Hello atom", "https://example.org/story"},
		{"xhtml", `<feed xmlns="http://www.w3.org/2005/Atom"><title>Rich</title><entry><title>Story</title><link href="/story"/><content type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml"><p>Hello rich</p><script>evil()</script></div></content></entry></feed>`, "Rich", "Hello rich", "https://example.org/story"},
		{"rdf", `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/"><channel><title>RDF</title></channel><item><title>Story</title><link>https://example.org/story</link><description>Hello rdf</description></item></rdf:RDF>`, "RDF", "Hello rdf", "https://example.org/story"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := ParseFeed([]byte(tc.xml), "https://example.org/feed", fixtureTime)
			if err != nil {
				t.Fatal(err)
			}
			if f.Title != tc.title || len(f.Entries) != 1 {
				t.Fatalf("parsed %+v", f)
			}
			e := f.Entries[0]
			if e.Body != tc.body || e.URL != tc.link || len(e.Key) != 64 {
				t.Fatalf("entry %+v", e)
			}
			if strings.Contains(e.Body, "evil") || strings.Contains(e.Body, "alert") {
				t.Fatal("active content retained")
			}
		})
	}
}

func TestFeedAdversarial(t *testing.T) {
	for name, data := range map[string]string{"empty": "", "html": "<html><title>Not a feed</title></html>", "broken": "<rss><channel>", "directive": `<!DOCTYPE rss [<!ENTITY x SYSTEM "file:///etc/passwd">]><rss><channel><title>&x;</title></channel></rss>`, "oversized": strings.Repeat("x", MaxFeedBytes+1), "deep": "<rss>" + strings.Repeat("<x>", 40) + strings.Repeat("</x>", 40) + "</rss>"} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFeed([]byte(data), "https://example.org/", fixtureTime); err == nil {
				t.Fatal("accepted invalid feed")
			}
		})
	}
	f, err := ParseFeed([]byte(`<rss><channel><item><title>Bad</title><link>javascript:alert(1)</link></item><item><title>Future</title><link>https://example.org/future</link><pubDate>Tue, 01 Jan 2097 12:00:00 +0000</pubDate></item></channel></rss>`), "https://example.org/", fixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 1 || f.Entries[0].Published != fixtureTime.Unix() {
		t.Fatalf("unsafe or future entry: %+v", f)
	}
}

func TestWebURL(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "ftp://example.org/feed", "https://user:pass@example.org/", "https://example.org:8080/", "//example.org/feed", "https://[broken/", "https://example.org/\nInjected: x"} {
		if _, err := WebURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if u, e := WebURL("https://EXAMPLE.org/feed#track"); e != nil || u != "https://example.org/feed" {
		t.Fatalf("normalization %q %v", u, e)
	}
}

func TestPublicIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "192.0.2.1", "198.18.0.1", "224.0.0.1", "0.0.0.0", "::1", "::ffff:127.0.0.1", "fd00::1", "fe80::1", "2001:db8::1", "2002:7f00:1::", "64:ff9b::7f00:1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("allowed private/special IP %s", ip)
		}
	}
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("blocked public %s", ip)
		}
	}
}

func TestFetcherBlocksPrivateNetwork(t *testing.T) {
	f := NewFetcher()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := f.Client.Get("http://127.0.0.1/"); err == nil || !strings.Contains(err.Error(), "public internet") {
		t.Fatalf("private address was not explicitly rejected: %v", err)
	}
	if _, err := f.Fetch(ctx, "http://127.0.0.1/feed", "", ""); err == nil {
		t.Fatal("loopback feed accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFetcherContract(t *testing.T) {
	for _, status := range []int{200, 304, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("If-None-Match") != "old" || r.Header.Get("If-Modified-Since") != "yesterday" || !strings.Contains(r.UserAgent(), "Tidings") {
					t.Error("missing conditional headers")
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Etag": []string{"new"}}, Body: io.NopCloser(strings.NewReader(rssFixture)), Request: r}, nil
			})}}
			r, err := f.Fetch(context.Background(), "https://example.org/feed", "old", "yesterday")
			if status == 200 {
				if err != nil || r.ETag != "new" || len(r.Feed.Entries) != 1 {
					t.Fatalf("result %+v %v", r, err)
				}
			} else if status == 304 {
				if err != nil || !r.Unchanged {
					t.Fatalf("conditional response %+v %v", r, err)
				}
			} else if err == nil {
				t.Fatal("upstream failure accepted")
			}
		})
	}
	f := NewFetcher()
	req, _ := http.NewRequest("GET", "http://127.0.0.1:8080/", nil)
	if err := f.Client.CheckRedirect(req, nil); err == nil {
		t.Error("unsafe redirect accepted")
	}
	req, _ = http.NewRequest("GET", "https://example.org/", nil)
	if err := f.Client.CheckRedirect(req, make([]*http.Request, 5)); err == nil {
		t.Error("redirect loop accepted")
	}
}

func TestOPML(t *testing.T) {
	want := []Feed{{Title: "A & B", URL: "https://example.org/a?x=1&y=2", Category: "AI"}, {Title: "Security", URL: "https://example.org/b", Category: "Infosec"}}
	data, err := EncodeOPML(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseOPML(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, f := range got {
		if f.Title != want[i].Title || f.URL != want[i].URL || f.Category != want[i].Category {
			t.Errorf("round trip %+v", f)
		}
	}
	for _, bad := range []string{"", `<html/>`, `<!DOCTYPE opml><opml/>`, `<opml><body><outline xmlUrl="file:///etc/passwd"/></body></opml>`, strings.Repeat("x", (1<<20)+1), `<opml><body>` + strings.Repeat(`<outline>`, 20) + strings.Repeat(`</outline>`, 20) + `</body></opml>`} {
		if _, err := ParseOPML([]byte(bad)); err == nil {
			t.Errorf("accepted invalid OPML %.80q", bad)
		}
	}
}

func FuzzParseFeed(f *testing.F) {
	f.Add([]byte(rssFixture))
	f.Add([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"/>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := ParseFeed(data, "https://example.org/feed", fixtureTime)
		if err != nil {
			return
		}
		if len(out.Entries) > MaxEntries {
			t.Fatal("entry limit exceeded")
		}
		for _, e := range out.Entries {
			if _, err = WebURL(e.URL); err != nil {
				t.Fatal(err)
			}
			if e.Published > fixtureTime.Unix() {
				t.Fatal("future date")
			}
		}
	})
}
func FuzzOPML(f *testing.F) {
	f.Add([]byte(`<opml><body><outline text="AI"><outline text="Example" xmlUrl="https://example.org/feed"/></outline></body></opml>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := ParseOPML(data)
		if err != nil {
			return
		}
		if len(out) > 200 {
			t.Fatal("unbounded import")
		}
		b, err := EncodeOPML(out)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseOPML(b)
		if err != nil || len(again) != len(out) {
			t.Fatalf("unstable OPML round trip %v", err)
		}
	})
}

func TestFetchCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewFetcher().Fetch(ctx, "https://example.org/", "", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context ignored %v", err)
	}
}
