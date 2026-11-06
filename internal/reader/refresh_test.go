package reader

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestRefreshScopeAndConditionalPersistence(t *testing.T) {
	h := webHarness(t)
	mine := testSource(t, h.store, 2, "mine", false)
	testSource(t, h.store, 3, "not-mine", false)
	testSource(t, h.store, 1, "shared", true)
	if _, err := h.store.DB.Exec("UPDATE feeds SET checked=0"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var urls []string
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		urls = append(urls, r.URL.String())
		mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{"Etag": []string{"fresh"}}, Body: io.NopCloser(strings.NewReader(rssFixture)), Request: r}, nil
	})}}
	if !h.app.StartRefresh(2, false) {
		t.Fatal("first refresh rejected")
	}
	h.app.Wait()
	mu.Lock()
	if len(urls) != 1 || urls[0] != mine.URL {
		t.Fatalf("refresh crossed subscription boundaries: %v", urls)
	}
	mu.Unlock()
	var tag string
	if err := h.store.DB.QueryRow("SELECT etag FROM feeds WHERE id=?", mine.ID).Scan(&tag); err != nil || tag != "fresh" {
		t.Fatalf("validator not persisted: %q %v", tag, err)
	}
	h.app.StartRefresh(2, false)
	h.app.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(urls) != 1 {
		t.Fatal("refresh cooldown was ignored")
	}
}

func TestOnlyOneRefreshRuns(t *testing.T) {
	h := webHarness(t)
	testSource(t, h.store, 2, "mine", false)
	if _, err := h.store.DB.Exec("UPDATE feeds SET checked=0"); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, context.Canceled
		}
		return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	if !h.app.StartRefresh(2, false) {
		t.Fatal("refresh not started")
	}
	<-started
	if h.app.StartRefresh(2, false) {
		close(release)
		t.Fatal("duplicate refresh was launched")
	}
	close(release)
	h.app.Wait()
}
