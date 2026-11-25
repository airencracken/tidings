package reader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCatalogSchema(t *testing.T) {
	b, err := assets.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []Suggestion
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&rows); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	categories := map[string]int{}
	languages := map[string]int{}
	if len(rows) < 900 {
		t.Fatalf("expected a broad catalog, got %d", len(rows))
	}
	for _, r := range rows {
		if strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.Description) == "" || strings.TrimSpace(r.Category) != r.Category || r.Category == "" ||
			strings.TrimSpace(r.Language) != r.Language || r.Language == "" || len(r.Language) > 40 {
			t.Fatal("catalog has empty metadata")
		}
		if _, err = time.Parse("2006-01-02", r.Verified); err != nil {
			t.Errorf("%s has no verification date", r.Title)
		}
		if normalized, err := WebURL(r.URL); err != nil || normalized != r.URL {
			t.Fatalf("invalid or noncanonical feed URL %q: %v", r.URL, err)
		}
		if !strings.HasPrefix(r.URL, "https://") {
			t.Fatal("catalog uses insecure URL")
		}
		if seen[r.URL] {
			t.Fatalf("duplicate %s", r.URL)
		}
		seen[r.URL] = true
		categories[r.Category]++
		languages[r.Language]++
	}
	if len(languages) < 20 {
		t.Fatalf("expected at least 20 languages, got %v", languages)
	}
	minimums := map[string]int{
		"AI": 8, "Africa & Middle East": 5, "Amateur Radio": 8, "Books": 5, "Business": 5, "Climate": 10,
		"Comics & Anime": 7, "Culture": 12, "Design": 5, "East Asia": 40, "Economics": 10, "Education": 5, "Electronics": 10,
		"Europe": 60, "Film & TV": 10, "Food": 10, "Games": 20, "Gardening": 5, "Health": 4, "History": 5,
		"Infosec": 20, "Latin America": 5, "Local News": 10, "Making": 10, "Music": 24, "Nature": 8,
		"News": 12, "Newsletters": 40, "Oceania": 10, "Outdoors": 7, "Personal Computing": 10, "Photography": 24, "Politics & Policy": 25, "Science": 15,
		"South Asia": 20, "Southeast Asia": 30, "Space": 5, "Sports": 40, "Style": 8, "Tech": 8, "Transportation": 14, "Travel": 8,
	}
	for cat, count := range minimums {
		if categories[cat] < count {
			t.Errorf("catalog category %s has only %d sources", cat, categories[cat])
		}
	}
}

func TestCatalogBrowseFollowAndScope(t *testing.T) {
	h := webHarness(t)
	page := h.request(2, "GET", "/sources", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Source categories") ||
		!strings.Contains(page.Body.String(), "Amateur Radio") || strings.Contains(page.Body.String(), "catalog-card") {
		t.Fatal("unfiltered catalog should show the topic directory", page.Code)
	}
	for _, item := range []string{"Vietnam Weekly", "Risky Bulletin", "Basketball Poetry", "Python on Microcontrollers Newsletter"} {
		path := "/sources?catalog_category=Newsletters&catalog_q=" + url.QueryEscape(item)
		newsletters := h.request(2, "GET", path, nil)
		if newsletters.Code != http.StatusOK || !strings.Contains(newsletters.Body.String(), item) {
			t.Fatalf("newsletter category is missing %q", item)
		}
	}
	filters := url.Values{"catalog_q": {" jAPANESE    RocketNews24 "}, "catalog_category": {"East Asia"}}
	page = h.request(2, "GET", "/sources?"+filters.Encode(), nil)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "SoraNews24 (formerly RocketNews24)") ||
		!strings.Contains(body, "RocketNews24 Japan") || strings.Contains(body, "Tokyo Cheapo") {
		t.Fatal("category and multiword search returned the wrong sources", page.Code)
	}
	const feed = "https://soranews24.com/feed/"
	filters.Set("url", feed)
	filters.Set("tags", "East Asia")
	follow := h.request(2, "POST", "/sources", filters)
	target, err := url.Parse(follow.Header().Get("Location"))
	if follow.Code != http.StatusSeeOther || err != nil || target.Path != "/sources" ||
		target.Fragment != "suggested-sources" || target.Query().Get("catalog_category") != "East Asia" ||
		target.Query().Get("catalog_q") != "jAPANESE    RocketNews24" {
		t.Fatal("catalog follow lost its filters", follow.Code, target, err)
	}
	page = h.request(2, "GET", target.RequestURI(), nil)
	if !strings.Contains(page.Body.String(), "Following <span") ||
		strings.Contains(page.Body.String(), `aria-label="Follow SoraNews24 (formerly RocketNews24)"`) {
		t.Fatal("already-followed source still offered a Follow button")
	}
	feeds, err := h.store.Feeds(t.Context(), 2, false)
	if err != nil || len(feeds) != 1 || feeds[0].URL != feed || feeds[0].Category != "east asia" {
		t.Fatal("following did not save the category as a tag", feeds, err)
	}
	for _, path := range []string{"/sources?catalog_category=East+Asia", "/sources?catalog_category=East+Asia&scope=household"} {
		if owner := h.request(1, "GET", path, nil); !strings.Contains(owner.Body.String(), `aria-label="Follow SoraNews24 (formerly RocketNews24)"`) {
			t.Fatal("personal follow leaked to another reader or scope", path)
		}
	}
	if member := h.request(2, "GET", "/sources?scope=household&catalog_category=East+Asia", nil); strings.Contains(member.Body.String(), "catalog-card") {
		t.Fatal("member can browse the household follow directory")
	}
	for _, bad := range []string{"catalog_q=a&catalog_q=b", "catalog_category=AI&catalog_category=Sports", "catalog_q=%ff%xx", "catalog_category=" + strings.Repeat("x", 161)} {
		if response := h.request(2, "GET", "/sources?"+bad, nil); response.Code != http.StatusBadRequest {
			t.Errorf("malformed filters %q returned %d", bad, response.Code)
		}
	}
	invalid := h.request(2, "GET", "/sources?catalog_q=%3Cscript%3E", nil)
	if invalid.Code != http.StatusOK || strings.Contains(invalid.Body.String(), `value="<script>"`) ||
		!strings.Contains(invalid.Body.String(), "No suggested sources match your filters.") {
		t.Fatal("catalog did not safely handle a search containing markup")
	}
}

func TestCatalogEnableAllAndKeepSavedChoices(t *testing.T) {
	h := webHarness(t)
	if w := h.request(1, "POST", "/account/suggestions", nil); w.Code != http.StatusSeeOther {
		t.Fatal(w.Code)
	}
	reopened, err := OpenStore(h.store.Directory)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.CatalogCategories(t.Context())
	if closeErr := reopened.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || len(after) != 0 {
		t.Fatal("reopening a store replaced its saved choice", after, err)
	}
	for _, body := range []url.Values{
		{"enable_all": {"true", "true"}},
		{"enable_all": {"false"}},
		{"enable_all": {"true"}, "category": {"Sports"}},
	} {
		if w := h.request(1, "POST", "/account/suggestions", body); w.Code != http.StatusBadRequest {
			t.Fatal("invalid settings form accepted", w.Code)
		}
	}
	if w := h.request(2, "POST", "/account/suggestions", url.Values{"enable_all": {"true"}}); w.Code != http.StatusForbidden {
		t.Fatal("non-owner could enable the directory", w.Code)
	}
	if _, err := h.store.DB.Exec("CREATE TRIGGER fail_all BEFORE INSERT ON catalog_categories WHEN NEW.name='Sports' BEGIN SELECT RAISE(ABORT,'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	if w := h.request(1, "POST", "/account/suggestions", url.Values{"enable_all": {"true"}}); w.Code == http.StatusSeeOther {
		t.Fatal("failed write reported success")
	}
	if got, err := h.store.CatalogCategories(t.Context()); err != nil || len(got) != 0 {
		t.Fatal("failure left categories partially enabled", got, err)
	}
	if _, err := h.store.DB.Exec("DROP TRIGGER fail_all"); err != nil {
		t.Fatal(err)
	}
	response := h.request(1, "POST", "/account/suggestions", url.Values{"enable_all": {"true"}, "return_to": {"/admin"}})
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin?notice=suggestions#suggestions" {
		t.Fatal("enable all did not return to Admin", response.Code, response.Header().Get("Location"))
	}
	enabled, err := h.store.CatalogCategories(t.Context())
	if err != nil || len(enabled) != len(catalogCategories(h.app.Catalog, nil)) {
		t.Fatal("enable all did not enable the whole catalog", enabled, err)
	}
}

func TestCatalogBundleImportAndLimit(t *testing.T) {
	bundle, err := os.ReadFile(filepath.Join("..", "..", "feeds", "all.opml"))
	if err != nil {
		t.Fatal(err)
	}
	feeds, err := ParseOPML(bundle)
	if err != nil {
		t.Fatal(err)
	}
	s := testStore(t)
	if err := s.Subscribe(t.Context(), 2, feeds, false); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := s.Feeds(t.Context(), 2, false)
	if err != nil || len(subscriptions) != len(feeds) {
		t.Fatalf("import stored %d of %d sources: %v", len(subscriptions), len(feeds), err)
	}
	var opml strings.Builder
	opml.WriteString("<opml><body>")
	for n := range maxOPMLFeeds + 1 {
		fmt.Fprintf(&opml, `<outline text="Test" xmlUrl="https://example.org/%d"/>`, n)
	}
	opml.WriteString("</body></opml>")
	if _, err := ParseOPML([]byte(opml.String())); err == nil {
		t.Fatal("oversized OPML import accepted")
	}
	if err := s.Subscribe(t.Context(), 2, make([]Feed, maxOPMLFeeds+1), false); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatal("oversized direct import was not rejected before validating URLs", err)
	}
}

func TestOPMLBundlesMatchCatalog(t *testing.T) {
	b, err := assets.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []Suggestion
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	bundles := map[string]string{"all": ""}
	for _, r := range rows {
		slug := strings.NewReplacer(" & ", "-", " ", "-").Replace(strings.ToLower(r.Category))
		bundles[slug] = r.Category
	}
	for file, cat := range bundles {
		b, err = os.ReadFile(filepath.Join("..", "..", "feeds", file+".opml"))
		if err != nil {
			t.Fatal(err)
		}
		feeds, err := ParseOPML(b)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]Suggestion{}
		for _, r := range rows {
			if cat == "" || cat == r.Category {
				want[r.URL] = r
			}
		}
		if len(want) != len(feeds) {
			t.Fatalf("%s bundle has %d feeds, want %d", file, len(feeds), len(want))
		}
		for _, f := range feeds {
			item, ok := want[f.URL]
			if !ok || item.Category != f.Category || item.Title != f.Title {
				t.Fatalf("unexpected bundled feed %+v in %s", f, file)
			}
			delete(want, f.URL)
		}
	}
}

// Opt-in live integration check uses exactly the parser and bounded public-only
// HTTP client used by the application. It never subscribes anyone.
func TestLiveCatalog(t *testing.T) {
	if os.Getenv("TIDINGS_LIVE_FEEDS") != "1" {
		t.Skip("set TIDINGS_LIVE_FEEDS=1 for live publisher checks")
	}
	b, err := assets.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []Suggestion
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	type result struct {
		Suggestion
		Count int    `json:"entries"`
		Error string `json:"error,omitempty"`
	}
	reports := make([]result, len(rows))
	jobs := make(chan int)
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := NewFetcher()
			for i := range jobs {
				r := result{Suggestion: rows[i]}
				out, err := f.Fetch(ctx, r.URL, "", "")
				if err != nil {
					r.Error = err.Error()
				} else if len(out.Feed.Entries) == 0 {
					r.Error = "feed contains no usable entries"
				} else {
					r.Count = len(out.Feed.Entries)
					r.Verified = time.Now().UTC().Format("2006-01-02")
				}
				reports[i] = r
			}
		}()
	}
	for i := range rows {
		if categories := os.Getenv("TIDINGS_LIVE_CATEGORIES"); categories != "" && !strings.Contains(","+categories+",", ","+rows[i].Category+",") {
			continue
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, r := range reports {
		if r.URL == "" {
			continue
		}
		if r.Error != "" {
			t.Errorf("%s: %s", r.Title, r.Error)
		} else {
			t.Logf("%s: %d entries", r.Title, r.Count)
		}
	}
	if path := os.Getenv("TIDINGS_FEED_REPORT"); path != "" {
		b, err = json.MarshalIndent(reports, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
