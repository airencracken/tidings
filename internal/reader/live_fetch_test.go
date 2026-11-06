package reader

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// This opt-in check uses the production fetcher and parser against publishers.
// Ordinary CI stays deterministic; run it before curating or releasing feeds:
// TIDINGS_LIVE_FETCH_TEST=1 go test ./internal/reader -run TestLiveCatalogFetch -v -parallel 4
func TestLiveCatalogFetch(t *testing.T) {
	if os.Getenv("TIDINGS_LIVE_FETCH_TEST") != "1" {
		t.Skip("set TIDINGS_LIVE_FETCH_TEST=1 to contact publishers")
	}
	data, err := assets.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog []struct{ Title, URL string }
	if err = json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, source := range catalog {
		t.Run(source.Title, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			fetcher := NewFetcher()
			defer fetcher.Client.CloseIdleConnections()
			result, err := fetcher.Fetch(ctx, source.URL, "", "")
			if err != nil {
				t.Fatalf("%s: %v", source.URL, err)
			}
			t.Logf("%s: parsed %d stories", source.URL, len(result.Feed.Entries))
		})
	}
}
