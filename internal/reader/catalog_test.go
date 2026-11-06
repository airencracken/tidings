package reader

import (
	"context"
	"encoding/json"
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
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	categories := map[string]int{}
	if len(rows) < 30 {
		t.Fatalf("expected a broad catalog, got %d", len(rows))
	}
	for _, r := range rows {
		if r.Title == "" || r.Description == "" {
			t.Fatal("catalog has empty metadata")
		}
		if _, err = time.Parse("2006-01-02", r.Verified); err != nil {
			t.Errorf("%s has no verification date", r.Title)
		}
		if _, err = WebURL(r.URL); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(r.URL, "https://") {
			t.Fatal("catalog uses insecure URL")
		}
		if seen[r.URL] {
			t.Fatalf("duplicate %s", r.URL)
		}
		seen[r.URL] = true
		categories[r.Category]++
	}
	for _, cat := range []string{"AI", "Tech", "Infosec"} {
		if categories[cat] < 8 {
			t.Errorf("catalog category %s has only %d sources", cat, categories[cat])
		}
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
	for file, cat := range map[string]string{"all": "", "ai": "AI", "tech": "Tech", "infosec": "Infosec", "news": "News", "science": "Science", "culture": "Culture", "food": "Food", "sports": "Sports", "nature": "Nature"} {
		b, err = os.ReadFile(filepath.Join("..", "..", "feeds", file+".opml"))
		if err != nil {
			t.Fatal(err)
		}
		feeds, err := ParseOPML(b)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]bool{}
		for _, r := range rows {
			if cat == "" || cat == r.Category {
				want[r.URL] = true
			}
		}
		if len(want) != len(feeds) {
			t.Fatalf("%s bundle has %d feeds, want %d", file, len(feeds), len(want))
		}
		for _, f := range feeds {
			if !want[f.URL] {
				t.Fatalf("unexpected bundled feed %s", f.URL)
			}
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
