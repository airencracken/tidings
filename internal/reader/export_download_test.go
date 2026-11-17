package reader

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLargeReadingDownloadCanBeImported(t *testing.T) {
	h := webHarness(t)
	feed := testSource(t, h.store, 2, "large-library", false)
	rich := "<p>" + strings.Repeat("a", 450<<10) + "</p>"
	entries := make([]Entry, 40)
	for i := range entries {
		entries[i] = Entry{
			Key: fmt.Sprint(i), Title: fmt.Sprintf("Story %d", i),
			URL: fmt.Sprintf("https://example.org/story/%d", i),
		}
	}
	if err := h.store.ApplyFetch(t.Context(), feed, FetchResult{Feed: ParsedFeed{Title: feed.Title, Entries: entries}}); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		var id int64
		if err := h.store.DB.QueryRow("SELECT id FROM entries WHERE feed_id=? AND key=?", feed.ID, entry.Key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if err := h.store.saveArticle(t.Context(), 2, id, "Saved article text", rich); err != nil {
			t.Fatal(err)
		}
		if err := h.store.SetState(t.Context(), 2, id, "saved", true); err != nil {
			t.Fatal(err)
		}
	}
	response := h.request(2, "GET", "/account/export", nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("large export returned %d %s", response.Code, response.Header().Get("Content-Type"))
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) < 2 {
		t.Fatalf("expected multiple files, got %d", len(archive.File))
	}
	destination := testStore(t)
	for i, part := range archive.File {
		if part.Name != fmt.Sprintf("tidings-%06d.json", i+1) {
			t.Fatalf("unexpected archive entry %q", part.Name)
		}
		content, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(content)
		content.Close()
		if err != nil {
			t.Fatal(err)
		}
		data, err := DecodeExport(b)
		if err != nil {
			t.Fatalf("part %d is not importable: %v", i+1, err)
		}
		if err := destination.RestorePersonal(t.Context(), User{ID: 3}, data); err != nil {
			t.Fatal(err)
		}
	}
	exported, err := destination.Export(t.Context(), User{ID: 3})
	if err != nil || len(exported.Stories) != len(entries) {
		t.Fatalf("after importing all parts got %d stories, want %d: %v", len(exported.Stories), len(entries), err)
	}
	for _, story := range exported.Stories {
		if !story.Saved || !story.FullStory || story.Body != "Saved article text" {
			t.Fatalf("saved full-text copy missing from %q", story.Title)
		}
	}
}

func TestRestoredPrivateArticleSurvivesFeedRefresh(t *testing.T) {
	source := testStore(t)
	feed := testSource(t, source, 2, "portable-copy", false)
	storyID := entryID(t, source, feed)
	const copyText = "The reader's saved full-text copy."
	if err := source.saveArticle(t.Context(), 2, storyID, copyText, "<p>"+copyText+"</p>"); err != nil {
		t.Fatal(err)
	}
	if err := source.SetState(t.Context(), 2, storyID, "saved", true); err != nil {
		t.Fatal(err)
	}
	data, err := source.Export(t.Context(), User{ID: 2})
	if err != nil {
		t.Fatal(err)
	}

	destination := testStore(t)
	if err := destination.RestorePersonal(t.Context(), User{ID: 3}, data); err != nil {
		t.Fatal(err)
	}
	checkSavedCopy := func(when string) {
		t.Helper()
		saved := entries(t, destination, 3, "saved")
		if len(saved) != 1 || !saved[0].Saved {
			t.Fatalf("%s: expected one saved story, got %+v", when, saved)
		}
		entry, err := destination.Entry(t.Context(), 3, saved[0].ID)
		if err != nil || !entry.FullStory || entry.Body != copyText {
			t.Fatalf("%s: full-text copy missing (body %q, full story %t): %v", when, entry.Body, entry.FullStory, err)
		}
	}
	checkSavedCopy("before feed refresh")
	story := data.Stories[0]
	story.Body = "Short publisher excerpt."
	story.BodyHTML = ""
	target := Feed{URL: feed.URL}
	if err := destination.DB.QueryRow("SELECT id FROM feeds WHERE url=?", feed.URL).Scan(&target.ID); err != nil {
		t.Fatal(err)
	}
	fetch := FetchResult{Feed: ParsedFeed{Title: feed.Title, Entries: []Entry{story}}}
	if _, err := destination.DB.Exec(`CREATE TRIGGER block_copy_move BEFORE INSERT ON article_copies
		BEGIN SELECT RAISE(ABORT, 'fixture blocked copy move'); END`); err != nil {
		t.Fatal(err)
	}
	if err := destination.ApplyFetch(t.Context(), target, fetch); err == nil {
		t.Fatal("expected the copy move to fail")
	}
	checkSavedCopy("after failed refresh")
	if _, err := destination.DB.Exec("DROP TRIGGER block_copy_move"); err != nil {
		t.Fatal(err)
	}
	if err := destination.ApplyFetch(t.Context(), target, fetch); err != nil {
		t.Fatal(err)
	}
	checkSavedCopy("after feed refresh")
}

func TestSmallReadingDownloadKeepsJSONContract(t *testing.T) {
	h := webHarness(t)
	feed := testSource(t, h.store, 2, "small-library", false)
	if err := h.store.SetState(t.Context(), 2, entryID(t, h.store, feed), "saved", true); err != nil {
		t.Fatal(err)
	}
	if got := h.request(0, "GET", "/account/export", nil); got.Code != http.StatusSeeOther {
		t.Fatalf("anonymous export returned %d", got.Code)
	}
	response := h.request(2, "GET", "/account/export", nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("small export returned %d %s", response.Code, response.Header().Get("Content-Type"))
	}
	data, err := DecodeExport(response.Body.Bytes())
	if err != nil || len(data.Stories) != 1 || !data.Stories[0].Saved {
		t.Fatalf("small export failed to round-trip: %+v, %v", data, err)
	}
}
