package reader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCatalogLanguageFollowStatusAndPages(t *testing.T) {
	h := webHarness(t)
	const path = "/sources?catalog_category=East+Asia&catalog_language=Japanese&catalog_unfollowed=1"
	followed := Feed{URL: "https://rocketnews24.com/feed/"}
	if err := h.store.Subscribe(t.Context(), 2, []Feed{followed}, false); err != nil {
		t.Fatal(err)
	}
	got := h.request(2, "GET", path, nil)
	for _, want := range []string{`value="Japanese" selected`, "matching sources", "NHK Japanese News"} {
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), want) {
			t.Errorf("filtered directory missing %q", want)
		}
	}
	for _, excluded := range []string{"RocketNews24 Japan", "SoraNews24 (formerly RocketNews24)"} {
		if strings.Contains(got.Body.String(), excluded) {
			t.Errorf("filtered directory included %q", excluded)
		}
	}

	first := h.request(2, "GET", "/sources?catalog_category=Europe", nil)
	second := h.request(2, "GET", "/sources?catalog_category=Europe&catalog_page=2", nil)
	firstLinks := strings.Count(first.Body.String(), `class="paper catalog-card"`)
	secondLinks := strings.Count(second.Body.String(), `class="paper catalog-card"`)
	if first.Code != http.StatusOK || second.Code != http.StatusOK ||
		firstLinks != catalogPageSize || secondLinks != catalogPageSize ||
		!strings.Contains(first.Body.String(), "Next sources") || !strings.Contains(second.Body.String(), "Previous sources") ||
		!strings.Contains(first.Body.String(), "Euronews") || strings.Contains(second.Body.String(), "Euronews") {
		t.Fatal("category pages did not split European sources correctly", first.Code, second.Code, firstLinks, secondLinks)
	}
	for _, invalid := range []string{"catalog_page=2", "catalog_category=Europe&catalog_page=0", "catalog_category=Europe&catalog_page=101",
		"catalog_unfollowed=anything", "catalog_language=Japanese&catalog_language=English",
	} {
		if w := h.request(2, "GET", "/sources?"+invalid, nil); w.Code != http.StatusBadRequest {
			t.Errorf("invalid directory filter %s returned %d", invalid, w.Code)
		}
	}
}

func TestCatalogPreviewContractAndIsolation(t *testing.T) {
	h := webHarness(t)
	const path = "/sources/preview"
	const address = "https://soranews24.com/feed/"
	now := time.Now().UTC().Truncate(time.Second)
	var payload strings.Builder
	payload.WriteString("<rss version=\"2.0\"><channel><title>Preview source</title>")
	for n := range 7 {
		published := now.Add(-time.Duration(7-n) * time.Hour).Format(time.RFC1123Z)
		fmt.Fprintf(&payload, "<item><title>Preview story %d</title><link>https://example.org/story/%d?utm_source=source</link><pubDate>%s</pubDate></item>", n, n, published)
	}
	payload.WriteString("</channel></rss>")
	count := 0
	upstream := http.StatusOK
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != address {
			t.Fatalf("preview fetched unexpected URL %s", r.URL)
		}
		count++
		return &http.Response{StatusCode: upstream, Request: r, Body: io.NopCloser(strings.NewReader(payload.String()))}, nil
	})}}
	for _, denied := range []struct {
		uid    int64
		method string
		input  url.Values
		code   int
	}{
		{2, "GET", nil, http.StatusMethodNotAllowed},
		{2, "POST", url.Values{"url": {address}, "csrf": {"bad"}}, http.StatusForbidden},
		{2, "POST", url.Values{"url": {address}, "scope": {"household"}}, http.StatusForbidden},
		{2, "POST", url.Values{"url": {address, address}}, http.StatusBadRequest},
		{2, "POST", url.Values{"url": {"http://127.0.0.1/feed"}}, http.StatusNotFound},
		{2, "POST", url.Values{"url": {"https://example.org/unlisted"}}, http.StatusNotFound},
	} {
		if got := h.request(denied.uid, denied.method, path, denied.input); got.Code != denied.code {
			t.Errorf("%s %+v returned %d, want %d", denied.method, denied.input, got.Code, denied.code)
		}
	}
	if count != 0 {
		t.Fatal("a rejected preview reached the publisher")
	}

	values := url.Values{"url": {address}, "catalog_category": {"East Asia"}, "catalog_q": {"sora"}, "catalog_language": {"English"}, "catalog_unfollowed": {"1"}}
	for range cap(h.app.previewSlots) {
		h.app.previewSlots <- struct{}{}
	}
	busy := h.request(2, "POST", path, values)
	for range cap(h.app.previewSlots) {
		<-h.app.previewSlots
	}
	if busy.Code != http.StatusServiceUnavailable || count != 0 {
		t.Fatal("preview bypassed its concurrency limit", busy.Code, count)
	}
	response := h.request(2, "POST", path, values)
	body := response.Body.String()
	if response.Code != http.StatusOK || count != 1 || response.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(body, "SoraNews24 (formerly RocketNews24)") || !strings.Contains(body, "Back to suggested sources") ||
		!strings.Contains(body, `name="catalog_unfollowed" value="1"`) || !strings.Contains(body, "catalog_language=English") ||
		strings.Contains(body, "utm_source=") || strings.Count(body, "Preview story ") != 5 ||
		strings.Index(body, "Preview story 6") > strings.Index(body, "Preview story 2") {
		t.Fatalf("unexpected preview response: %d after %d publisher calls", response.Code, count)
	}
	var subscriptions, entries int
	if err := h.store.DB.QueryRow("SELECT (SELECT count(*) FROM subscriptions),(SELECT count(*) FROM entries)").Scan(&subscriptions, &entries); err != nil ||
		subscriptions != 0 || entries != 0 {
		t.Fatal("preview persisted a follow or an article", subscriptions, entries, err)
	}
	upstream = http.StatusBadGateway
	if got := h.request(2, "POST", path, values); got.Code != http.StatusBadGateway ||
		!strings.Contains(got.Body.String(), "couldn’t load the publisher’s feed") {
		t.Fatal("upstream failure did not leave a usable preview page", got.Code)
	}
	if got := h.request(1, "POST", "/account/suggestions", nil); got.Code != http.StatusSeeOther {
		t.Fatal(got.Code)
	}
	if got := h.request(2, "POST", path, values); got.Code != http.StatusNotFound || count != 2 {
		t.Fatal("disabled source reached publisher", got.Code, count)
	}
}

func TestCatalogPicksUseOnlyReaderFeedback(t *testing.T) {
	h := webHarness(t)
	for _, item := range []struct {
		url, title, category string
	}{
		{"https://example.org/electronics", "Circuit notes", "Electronics"},
		{"https://example.org/photos", "Photo notes", "Photography"},
		{"https://example.org/new-electronics", "More circuits", "Electronics"},
		{"https://example.org/new-photos", "More photos", "Photography"},
		{"https://example.org/disabled", "Hidden source", "Hidden"},
		{"https://example.org/french", "Photo française", "Photography"},
	} {
		language := "English"
		if item.title == "Photo française" {
			language = "French"
		}
		h.app.Catalog = append(h.app.Catalog, Suggestion{Title: item.title, URL: item.url, Category: item.category, Language: language})
	}
	circuit := recommendationSource(t, h.store, 2, "electronics", false)
	photo := recommendationSource(t, h.store, 2, "photos", false)
	if err := h.store.Subscribe(t.Context(), 3, []Feed{circuit, photo}, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Minute)
	for _, input := range []struct {
		uid, id int64
		action  string
	}{
		{2, recommendationStory(t, h.store, circuit.ID, 0, "Photojournalism and circuits", now), "saved"},
		{2, recommendationStory(t, h.store, photo.ID, 0, "Snapshot", now), "skipped"},
		{3, recommendationStory(t, h.store, photo.ID, 0, "Equine photography", now), "saved"},
		{3, recommendationStory(t, h.store, circuit.ID, 0, "Transistor news", now), "skipped"},
	} {
		if err := h.store.SetState(t.Context(), input.uid, input.id, input.action, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, reader := range []struct {
		uid int64
		top string
	}{
		{2, "More circuits"},
		{3, "More photos"},
	} {
		feeds, err := h.store.Feeds(t.Context(), reader.uid, false)
		if err != nil {
			t.Fatal(err)
		}
		feedback, err := h.store.catalogFeedback(t.Context(), reader.uid, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		picks := recommendCatalog(h.app.Catalog[len(h.app.Catalog)-6:], map[string]bool{"Electronics": true, "Photography": true}, feeds, feedback)
		var names []string
		for _, pick := range picks {
			names = append(names, pick.Title)
		}
		if len(names) != 2 || names[0] != reader.top || slices.Contains(names, "Photo française") {
			t.Fatalf("reader %d received %v", reader.uid, names)
		}
	}
	page := h.request(2, "GET", "/sources", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Sources you might like") ||
		!strings.Contains(page.Body.String(), "Because you follow Circuit notes") {
		t.Fatal("the directory omitted its personalized section", page.Code)
	}
}
