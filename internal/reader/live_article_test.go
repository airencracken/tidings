package reader

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Opt-in integration: feeds and article pages can have different access rules.
// Check one article per catalog source, or set TIDINGS_ARTICLE_URL for a specific
// regression. Ordinary CI stays independent of publisher availability.
func TestLiveArticleFetch(t *testing.T) {
	if os.Getenv("TIDINGS_LIVE_ARTICLE_TEST") != "1" {
		t.Skip("set TIDINGS_LIVE_ARTICLE_TEST=1 to contact article publishers")
	}
	if address := os.Getenv("TIDINGS_ARTICLE_URL"); address != "" {
		f := NewFetcher()
		defer f.Client.CloseIdleConnections()
		body, _, err := f.Article(t.Context(), address)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("extracted %d bytes", len(body))
		return
	}
	data, err := assets.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var sources []struct{ Title, URL string }
	if err = json.Unmarshal(data, &sources); err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		t.Run(source.Title, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			f := NewFetcher()
			defer f.Client.CloseIdleConnections()
			feed, err := f.Fetch(ctx, source.URL, "", "")
			if err != nil {
				t.Fatalf("feed unavailable: %v", err)
				return
			}
			if len(feed.Feed.Entries) == 0 {
				t.Log("feed has no entries")
				return
			}
			entry := feed.Feed.Entries[0]
			body, _, err := f.Article(ctx, entry.URL)
			if err != nil {
				t.Fatalf("ARTICLE FAILED %s: %v", entry.URL, err)
				return
			}
			t.Logf("ARTICLE OK %s: %d bytes", entry.URL, len(body))
		})
	}
}
