package reader

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInterestProviderContract(t *testing.T) {
	for _, bad := range []string{"https://", "file:///search", "https://user:secret@example.org/search",
		"https://example.org/search?key=value", "http://example.org/search#token"} {
		if _, err := newInterestSearch(bad); err == nil {
			t.Errorf("accepted invalid search endpoint %q", bad)
		}
	}
	disabled, err := newInterestSearch("")
	if disabled != nil || err != nil {
		t.Fatal("empty setting did not disable search")
	}
	const query = "radio & fotografia em São Paulo"
	var redirects, requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "" {
			t.Error("provider received an unexpected request method, URL, or identifying header")
		}
		if err := r.ParseForm(); err != nil || len(r.PostForm) != 3 || r.PostForm.Get("q") != query+" RSS" ||
			r.PostForm.Get("format") != "json" || r.PostForm.Get("language") != "all" {
			t.Errorf("provider received incorrect search parameters: %v", r.PostForm)
		}
		switch r.URL.Path {
		case "/search":
			fmt.Fprint(w, `{"results":[{"url":"https://news.example.org/story"},{"url":"https://blog.example.org/feed"},
				{"url":"file:///bad"},{"url":"https://journal.example.net/news#headline"}]}`)
		case "/redirect":
			http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
		case "/other":
			redirects.Add(1)
		case "/oversized":
			fmt.Fprint(w, `{"results":[]}`+strings.Repeat(" ", maxSearchResponse))
		case "/invalid":
			fmt.Fprint(w, `{"unexpected":[]}`)
		case "/broad":
			rows := make([]string, maxInterestSites+5)
			for i := range rows {
				rows[i] = fmt.Sprintf(`{"url":"https://example%d.org/feed"}`, i)
			}
			fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(rows, ","))
		}
	}))
	t.Cleanup(server.Close)
	search, err := newInterestSearch(server.URL + "/search")
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := search.candidates(t.Context(), query)
	if err != nil || !slices.Equal(candidates, []string{"https://news.example.org/story", "https://journal.example.net/news"}) {
		t.Fatalf("search candidates were malformed or repeated a publisher: %v %v", candidates, err)
	}
	for _, path := range []string{"/redirect", "/oversized", "/invalid"} {
		search.endpoint = server.URL + path
		if _, err := search.candidates(t.Context(), query); err == nil {
			t.Errorf("accepted provider failure at %s", path)
		}
	}
	search.endpoint = server.URL + "/broad"
	if got, err := search.candidates(t.Context(), query); err != nil || len(got) != maxInterestSites {
		t.Errorf("expected a bounded number of candidate sites, got %d %v", len(got), err)
	}
	if redirects.Load() != 0 || requests.Load() != 5 {
		t.Fatal("search request followed a provider redirect")
	}
}

const interestTestSearch = "/sources/search"
const interestTestFollow = "/sources/search/follow"

func TestInterestDiscoveryFlow(t *testing.T) {
	h := webHarness(t)
	seed := Feed{URL: "https://example.com/subscribed", Tags: []string{"education", "community reporting"}}
	other := Feed{URL: "https://example.com/private", Tags: []string{"private outsider tag"}}
	for i, feed := range []Feed{seed, other} {
		if err := h.store.Subscribe(t.Context(), int64(i+2), []Feed{feed}, false); err != nil {
			t.Fatal(err)
		}
	}
	const query = "community reporting in Cebu"
	const newsURL = "https://news.example.org/feed/"
	const journalURL = "https://journal.example.net/recent"
	var requests atomic.Int32
	search, err := newInterestSearch("https://search.example.com/search")
	if err != nil {
		t.Fatal(err)
	}
	search.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("q") != query+" RSS" {
			t.Errorf("search used unexpected terms: %v", r.PostForm)
		}
		return interestResponse(r, http.StatusOK, `{"results":[{"url":"https://news.example.org/cebu"},
			{"url":"https://blog.example.org/duplicate"},{"url":"https://journal.example.net/recent"},{"url":"https://broken.example.com/feed"}]}`), nil
	})
	h.app.interestSearch = search
	h.app.Fetcher.Client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		address := r.URL.String()
		switch address {
		case "https://news.example.org/cebu":
			return interestResponse(r, http.StatusOK, `<html><head><title>Community reporting</title></head></html>`), nil
		case "https://news.example.org/":
			return interestResponse(r, http.StatusOK, `<link rel="alternate" type="application/rss+xml" href="/comments/feed/">`), nil
		case newsURL:
			var feed strings.Builder
			feed.WriteString(`<rss version="2.0"><channel><title>Cebu Reporter</title>`)
			for _, day := range []int{1, 2, 4, 3} {
				fmt.Fprintf(&feed, `<item><title>Headline %d</title><link>https://example.com/story-%d?utm_source=example</link><pubDate>2020-11-%02dT12:00:00Z</pubDate></item>`, day, day, day)
			}
			feed.WriteString("</channel></rss>")
			return interestResponse(r, http.StatusOK, feed.String()), nil
		case journalURL:
			return interestResponse(r, http.StatusOK, `<feed xmlns="http://www.w3.org/2005/Atom"><title>Neighborhood Journal</title>
				<entry><title>Journal headline</title><id>journal-one</id><link href="https://journal.example.net/story"/><updated>2020-11-04T12:00:00Z</updated></entry></feed>`), nil
		case "https://broken.example.com/feed", "https://broken.example.com/":
			return interestResponse(r, http.StatusOK, `<link rel="alternate" type="application/rss+xml" href="/empty.xml">`), nil
		case "https://broken.example.com/empty.xml":
			return interestResponse(r, http.StatusOK, `<rss><channel><title>Empty feed</title></channel></rss>`), nil
		case "https://broken.example.com/feed/":
			return interestResponse(r, http.StatusOK, `<rss><channel><title>Comments for a blog</title>
				<item><title>Off-topic comment</title><link>https://broken.example.com/comment</link></item></channel></rss>`), nil
		case seed.URL:
			return interestResponse(r, http.StatusNotModified, ""), nil
		default:
			t.Errorf("unexpected publisher request to %s", address)
			return interestResponse(r, http.StatusNotFound, ""), nil
		}
	})
	landing := h.request(2, "GET", "/sources", nil)
	if landing.Code != http.StatusOK || !strings.Contains(landing.Body.String(), "Find feeds by interest") ||
		!strings.Contains(landing.Body.String(), "<option>community reporting</option>") ||
		strings.Contains(landing.Body.String(), "private outsider tag") || strings.Contains(landing.Body.String(), "Source categories") {
		t.Fatal("dynamic Sources landing did not use the reader's own tags", landing.Code)
	}
	if catalog := h.request(2, "GET", "/sources?catalog=1", nil); catalog.Code != http.StatusOK ||
		!strings.Contains(catalog.Body.String(), "Source categories") || requests.Load() != 0 {
		t.Fatal("loading Sources or browsing the bundled catalog sent a search", catalog.Code)
	}
	got := h.request(2, "POST", interestTestSearch, url.Values{"interest": {query}, "private_tag": {"do not send"}})
	body := got.Body.String()
	if got.Code != http.StatusOK || requests.Load() != 1 || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("search did not complete as a single private request", got.Code, requests.Load())
	}
	for _, want := range []string{"Cebu Reporter", "Neighborhood Journal", "Journal headline", "Follow selected feeds", "Headline 4", "Headline 3",
		`value="community reporting in Cebu"`, `value="https://news.example.org/feed/"`} {
		if !strings.Contains(body, want) {
			t.Errorf("search results missing %q", want)
		}
	}
	if strings.Contains(body, "Empty feed") || strings.Contains(body, "Off-topic comment") ||
		strings.Contains(body, "Headline 1") || strings.Contains(body, "utm_source") ||
		strings.Index(body, "Headline 4") > strings.Index(body, "Headline 3") {
		t.Fatal("search results included unusable feeds or incorrect previews")
	}
	if feeds, err := h.store.Feeds(t.Context(), 2, false); err != nil || len(feeds) != 1 {
		t.Fatal("preview changed the reader's subscriptions", feeds, err)
	}
	follow := h.request(2, "POST", interestTestFollow, url.Values{"url": {newsURL, journalURL}})
	if follow.Code != http.StatusSeeOther || follow.Header().Get("Location") != "/sources?notice=feeds-followed" || requests.Load() != 1 {
		t.Fatal("following the selection did not succeed without rerunning the search", follow.Code, requests.Load())
	}
	if feeds, err := h.store.Feeds(t.Context(), 2, false); err != nil || len(feeds) != 3 {
		t.Fatal("selected feeds were not added", feeds, err)
	}
	again := h.request(2, "POST", interestTestSearch, url.Values{"interest": {query}})
	if again.Code != http.StatusOK || strings.Contains(again.Body.String(), "Follow selected feeds") ||
		strings.Count(again.Body.String(), "Following <span") != 2 {
		t.Fatal("the search did not recognise feeds already followed", again.Code)
	}
}

func TestInterestSearchAuthorizationAndFailures(t *testing.T) {
	h := webHarness(t)
	for _, path := range []string{interestTestSearch, interestTestFollow} {
		if got := h.request(2, "GET", path, nil); got.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s returned %d", path, got.Code)
		}
		if got := h.request(2, "POST", path, url.Values{"interest": {"radio"}}); got.Code != http.StatusNotFound {
			t.Errorf("unconfigured %s returned %d", path, got.Code)
		}
	}
	search, err := newInterestSearch("https://search.example.com/search")
	if err != nil {
		t.Fatal(err)
	}
	h.app.interestSearch = search
	var fail atomic.Bool
	var requests atomic.Int32
	fail.Store(true)
	search.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if fail.Load() {
			return interestResponse(r, http.StatusServiceUnavailable, "internal-provider-message"), nil
		}
		return interestResponse(r, http.StatusOK, `{"results":[]}`), nil
	})
	for _, path := range []string{interestTestSearch, interestTestFollow} {
		for _, tc := range []struct {
			uid  int64
			form url.Values
			code int
		}{
			{0, nil, http.StatusSeeOther},
			{2, url.Values{"scope": {"household"}}, http.StatusForbidden},
			{2, url.Values{"csrf": {"invalid"}}, http.StatusForbidden},
		} {
			if got := h.request(tc.uid, "POST", path, tc.form); got.Code != tc.code {
				t.Errorf("%s rejected reader %d with %d, want %d", path, tc.uid, got.Code, tc.code)
			}
		}
	}
	for _, bad := range [][]string{nil, {" "}, {"one", "two"}, {"a\nb"}, {"\xff"}, {strings.Repeat("攝", maxInterestLength+1)}} {
		if got := h.request(2, "POST", interestTestSearch, url.Values{"interest": bad}); got.Code != http.StatusBadRequest {
			t.Errorf("invalid interest %q returned %d", bad, got.Code)
		}
	}
	values := url.Values{"interest": {"field recording"}}
	for range cap(search.slots) {
		search.slots <- struct{}{}
	}
	if got := h.request(2, "POST", interestTestSearch, values); got.Code != http.StatusServiceUnavailable || requests.Load() != 0 {
		t.Fatal("search reached provider after it should have been rejected", got.Code, requests.Load())
	}
	for range cap(search.slots) {
		<-search.slots
	}
	if got := h.request(2, "POST", interestTestSearch, values); got.Code != http.StatusBadGateway ||
		!strings.Contains(got.Body.String(), "search service is unavailable") || strings.Contains(got.Body.String(), "internal-provider-message") {
		t.Fatal("provider failure did not render a usable error page", got.Code)
	}
	fail.Store(false)
	if got := h.request(2, "POST", interestTestSearch, values); got.Code != http.StatusOK ||
		!strings.Contains(got.Body.String(), "No working feeds found") || !strings.Contains(got.Body.String(), "#add-source") {
		t.Fatal("empty search results did not give the reader another way to add feeds", got.Code)
	}
}

func TestInterestFollowIsAtomic(t *testing.T) {
	h := webHarness(t)
	search, err := newInterestSearch("http://127.0.0.1:8080/search")
	if err != nil {
		t.Fatal(err)
	}
	h.app.interestSearch = search
	const first = "https://one.example.org/feed"
	const second = "https://two.example.org/feed"
	for _, addresses := range [][]string{nil, {first, "file:///bad"}, make([]string, maxInterestSites+1)} {
		if got := h.request(2, "POST", interestTestFollow, url.Values{"url": addresses}); got.Code != http.StatusBadRequest {
			t.Errorf("accepted invalid feed selection with %d", got.Code)
		}
	}
	if _, err := h.store.DB.Exec(`CREATE TRIGGER fail_interest BEFORE INSERT ON feeds WHEN NEW.url='https://two.example.org/feed'
		BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if got := h.request(2, "POST", interestTestFollow, url.Values{"url": {first, second}}); got.Code != http.StatusBadRequest {
		t.Fatal("failed write returned success", got.Code)
	}
	var feeds int
	if err := h.store.DB.QueryRow("SELECT count(*) FROM feeds").Scan(&feeds); err != nil || feeds != 0 {
		t.Fatal("failed write left a partial follow", feeds, err)
	}
}

func FuzzInterestRoundTrip(f *testing.F) {
	for _, sample := range []string{" community radio ", "高雄 攝影", "", "two\nlines", "news & culture", strings.Repeat("攝", maxInterestLength)} {
		f.Add(sample)
	}
	f.Fuzz(func(t *testing.T, sample string) {
		query, err := readInterest([]string{sample})
		if err != nil {
			return
		}
		form, err := url.ParseQuery(url.Values{"interest": {query}}.Encode())
		if err != nil {
			t.Fatal(err)
		}
		if restored, err := readInterest(form["interest"]); err != nil || restored != query || query != strings.TrimSpace(sample) {
			t.Fatal("interest changed during form round trip")
		}
	})
}

func interestResponse(r *http.Request, status int, payload string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(payload)), Request: r}
}
