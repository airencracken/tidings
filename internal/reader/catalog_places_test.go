package reader

import (
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLocalNewsRequiresPlace(t *testing.T) {
	h := webHarness(t)
	const local = "/sources?catalog_category=Local+News"
	landing := h.request(2, "GET", local, nil)
	for _, want := range []string{`<option value="">Choose a place</option>`, `value="Canada"`,
		`value="France"`, `value="Philippines"`, `value="United States"`, `href="#add-source"`, `id="add-source"`} {
		if !strings.Contains(landing.Body.String(), want) {
			t.Errorf("Local News landing page missing %q", want)
		}
	}
	if landing.Code != http.StatusOK || strings.Contains(landing.Body.String(), " selected>Canada") ||
		strings.Contains(landing.Body.String(), " selected>United States") || strings.Contains(landing.Body.String(), "catalog-card") {
		t.Fatal("Local News preselected a place or displayed news before choosing", landing.Code)
	}

	canada := h.request(2, "GET", local+"&catalog_country=Canada&catalog_q=local+British+Columbia", nil)
	if canada.Code != http.StatusOK || !strings.Contains(canada.Body.String(), `value="Canada" selected`) ||
		!strings.Contains(canada.Body.String(), "The Tyee") || !strings.Contains(canada.Body.String(), "British Columbia · Canada") ||
		strings.Contains(canada.Body.String(), "CalMatters") || strings.Contains(canada.Body.String(), "Toronto · Canada") {
		t.Fatal("choosing a place did not scope city and region search", canada.Code)
	}
	france := h.request(2, "GET", local+"&catalog_country=France&catalog_language=French", nil)
	if france.Code != http.StatusOK || !strings.Contains(france.Body.String(), "Marsactu") ||
		strings.Contains(france.Body.String(), "The Tyee") {
		t.Fatal("changing country displayed the wrong sources", france.Code)
	}
	search := h.request(2, "GET", "/sources?catalog_q=Marsactu", nil)
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), "Marseille · France") {
		t.Fatal("publisher search did not find a local outlet", search.Code)
	}
	unknown := h.request(2, "GET", local+"&catalog_country=%3Cscript%3E", nil)
	if unknown.Code != http.StatusOK || !strings.Contains(unknown.Body.String(), "No suggested sources match your filters.") ||
		strings.Contains(unknown.Body.String(), "<script>") {
		t.Fatal("unknown place did not show a safe empty result", unknown.Code)
	}
	switched := h.request(2, "GET", "/sources?catalog_category=Electronics&catalog_country=France", nil)
	if switched.Code != http.StatusOK || strings.Contains(switched.Body.String(), `id="catalog-country"`) ||
		!strings.Contains(switched.Body.String(), "catalog-card") || strings.Contains(switched.Body.String(), `name="catalog_country"`) {
		t.Fatal("a previous place constrained another catalog category", switched.Code)
	}
	if response := h.request(1, "POST", "/account/suggestions", url.Values{"category": {"Electronics"}}); response.Code != http.StatusSeeOther {
		t.Fatal("could not save the operator's category choice", response.Code)
	}
	if disabled := h.request(2, "GET", local+"&catalog_country=France", nil); disabled.Code != http.StatusOK ||
		strings.Contains(disabled.Body.String(), "Marsactu") || strings.Contains(disabled.Body.String(), `id="catalog-country"`) {
		t.Fatal("disabled Local News still exposed its place directory", disabled.Code)
	}
}

func TestLocalNewsPreviewAndFollowKeepPlace(t *testing.T) {
	h := webHarness(t)
	const address = "https://thetyee.ca/rss2.xml"
	const rss = `<rss version="2.0"><channel><title>The Tyee</title><item><title>Local story</title><link>https://thetyee.ca/story</link></item></channel></rss>`
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(rss)), Request: r}, nil
	})}}
	values := url.Values{"catalog_country": {"Canada"}, "catalog_category": {localNewsCategory},
		"catalog_q": {"British Columbia"}, "url": {address}, "tags": {localNewsCategory}}
	for _, invalid := range [][]string{{"Canada", "France"}, {strings.Repeat("x", 101)}, {"Ca\x00nada"}, {"\xff"}} {
		bad := maps.Clone(values)
		bad["catalog_country"] = invalid
		for _, endpoint := range []string{"/sources", "/sources/preview"} {
			if got := h.request(2, "POST", endpoint, bad); got.Code != http.StatusBadRequest {
				t.Errorf("POST %s accepted invalid places %q with %d", endpoint, invalid, got.Code)
			}
		}
		if got := h.request(2, "GET", "/sources?"+bad.Encode(), nil); got.Code != http.StatusBadRequest {
			t.Errorf("GET /sources accepted invalid places %q with %d", invalid, got.Code)
		}
	}
	preview := h.request(2, "POST", "/sources/preview", values)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "Local story") ||
		!strings.Contains(preview.Body.String(), `name="catalog_country" value="Canada"`) ||
		!strings.Contains(preview.Body.String(), "catalog_country=Canada") {
		t.Fatal("preview did not preserve the location in the follow form and back link", preview.Code)
	}
	if feeds, err := h.store.Feeds(t.Context(), 2, false); err != nil || len(feeds) != 0 {
		t.Fatal("rejected request or preview left a subscription", feeds, err)
	}
	if other := h.request(3, "GET", "/sources?catalog_category=Local+News", nil); other.Code != http.StatusOK ||
		strings.Contains(other.Body.String(), "The Tyee") || strings.Contains(other.Body.String(), `value="Canada" selected`) {
		t.Fatal("another reader inherited the place", other.Code)
	}
	follow := h.request(2, "POST", "/sources", values)
	target, err := url.Parse(follow.Header().Get("Location"))
	if follow.Code != http.StatusSeeOther || err != nil || target.Path != "/sources" || target.Fragment != "suggested-sources" ||
		target.Query().Get("catalog_country") != "Canada" || target.Query().Get("catalog_q") != "British Columbia" {
		t.Fatal("follow did not return to the same place", follow.Code, target, err)
	}
	if after := h.request(2, "GET", target.RequestURI(), nil); after.Code != http.StatusOK ||
		!strings.Contains(after.Body.String(), "Following <span") {
		t.Fatal("filtered view did not show the new follow", after.Code)
	}
}

func TestLocalNewsStaysOutOfGeneralRecommendations(t *testing.T) {
	feeds := []Suggestion{
		{Title: "California feed", URL: "https://example.org/california", Language: "English", Category: localNewsCategory, Country: "United States"},
		{Title: "Chicago feed", URL: "https://example.org/chicago", Language: "English", Category: localNewsCategory, Country: "United States"},
		{Title: "Toronto feed", URL: "https://example.org/toronto", Language: "English", Category: localNewsCategory, Country: "Canada"},
		{Title: "Home lab notes", URL: "https://example.org/home-lab", Language: "English", Category: "Making"},
	}
	follows := []Feed{{URL: feeds[0].URL}, {Tags: []string{"Making"}}}
	enabled := map[string]bool{localNewsCategory: true, "Making": true}
	picks := recommendCatalog(feeds, enabled, follows, nil)
	if len(picks) != 1 || picks[0].URL != feeds[3].URL {
		t.Fatalf("recommendations included news from an unchosen place: %+v", picks)
	}
}

func FuzzCatalogPlaceRoundTrip(f *testing.F) {
	for _, country := range []string{"", "Canada", "  United Kingdom  ", "Côte d’Ivoire", "Trinidad & Tobago", "a\x00b"} {
		f.Add(country)
	}
	f.Fuzz(func(t *testing.T, country string) {
		filter, err := readCatalogFilter(url.Values{"catalog_category": {localNewsCategory}, "catalog_country": {country}})
		if err != nil {
			return
		}
		location, err := url.Parse(catalogLocation(filter, true, "subscribed"))
		if err != nil || location.Path != "/sources" || location.Fragment != "suggested-sources" ||
			location.Query().Get("scope") != "household" {
			t.Fatalf("unsafe redirect: %v %v", location, err)
		}
		restored, err := readCatalogFilter(location.Query())
		if err != nil || restored != filter {
			t.Fatalf("place %q lost after redirect: %+v %v", country, restored, err)
		}
	})
}
