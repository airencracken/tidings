package reader

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchStatusScopeAndHTTPContract(t *testing.T) {
	h := webHarness(t)
	testSource(t, h.store, 2, "mine", false)
	testSource(t, h.store, 3, "someone-elses", false)
	testSource(t, h.store, 1, "household", true)
	if _, err := h.store.DB.Exec("UPDATE feeds SET checked=0,error=''"); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{2, 3} {
		summary, err := h.store.fetchSummary(t.Context(), uid)
		if err != nil || summary.Sources != 1 || summary.Waiting != 1 {
			t.Fatal("private scope lost", uid, summary, err)
		}
	}
	if _, err := h.store.DB.Exec("UPDATE users SET group_optin=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	summary, err := h.store.fetchSummary(t.Context(), 2)
	if err != nil || summary.Sources != 2 {
		t.Fatal("opted-in household missing", summary, err)
	}
	w := h.request(2, "GET", "/api/v1/fetch-status?user=3&all=true", nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal(w.Code, w.Header(), w.Body)
	}
	var status fetchOverview
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || status.State != "waiting" || !strings.Contains(status.Detail, "2 sources") {
		t.Fatal(status, err)
	}
	if strings.Contains(w.Body.String(), "someone-elses") || strings.Contains(w.Body.String(), "https://") {
		t.Fatal("publisher addresses leaked in status")
	}
	if w := h.request(0, "GET", "/api/v1/fetch-status", nil); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal("status is public", w.Code)
	}
	if w := h.request(2, "POST", "/api/v1/fetch-status", nil); w.Code != 405 {
		t.Fatal("status route accepts writes", w.Code)
	}
}

func TestFetchStatusTransitionsAndImmediateFollow(t *testing.T) {
	h := webHarness(t)
	started, release := make(chan struct{}), make(chan struct{})
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, context.Canceled
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(rssFixture)), Request: r}, nil
	})}}
	w := h.request(2, "POST", "/sources", url.Values{"url": {"https://example.org/new-feed"}})
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("following a source did not start fetching")
	}
	h.app.refreshMu.Lock()
	h.app.refreshNextCheck = time.Now().Add(30 * time.Minute)
	h.app.refreshMu.Unlock()
	status, err := h.app.fetchOverview(t.Context(), 2)
	if err != nil || status.State != "fetching" || status.NextCheck == "" {
		t.Fatal("running status missing", status, err)
	}
	page := h.request(2, "GET", "/sources", nil).Body.String()
	if !strings.Contains(page, "data-fetch-status") || !strings.Contains(page, "Fetching your sources.") || !strings.Contains(page, "/static/fetch-status.js") {
		t.Fatal("first fetch has no visible status")
	}
	close(release)
	h.app.Wait()
	status, err = h.app.fetchOverview(t.Context(), 2)
	if err != nil || status.State != "idle" || status.Checked == 0 || !strings.Contains(status.Message, "Feeds checked at") {
		t.Fatal("completion status missing", status, err)
	}
	feeds, err := h.store.Feeds(t.Context(), 2, false)
	if err != nil || len(feeds) != 1 || feeds[0].Checked == 0 {
		t.Fatal("first fetch did not persist", feeds, err)
	}
}

func TestFetchStatusFailuresAndCopy(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "mine", false)
	if _, err := h.store.DB.Exec("UPDATE feeds SET error='publisher returned HTTP 503',checked=? WHERE id=?", time.Now().Unix(), f.ID); err != nil {
		t.Fatal(err)
	}
	status, err := h.app.fetchOverview(t.Context(), 2)
	if err != nil || status.State != "failed" || !strings.Contains(status.Detail, "1 of 1") {
		t.Fatal(status, err)
	}
	for _, path := range []string{"/", "/sources", "/account", "/story/1"} {
		page := h.request(2, "GET", path, nil).Body.String()
		for _, removed := range []string{"without scripts or remote images", "Feed text, without remote images", "Dates use UTC", "Some publishers share only an excerpt"} {
			if strings.Contains(page, removed) {
				t.Fatal("reading disclaimer returned", path, removed)
			}
		}
	}
}

func TestAutomaticFetchScheduleIsVisible(t *testing.T) {
	h := webHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); h.app.Poll(ctx, time.Hour) }()
	defer func() { cancel(); <-done }()
	deadline := time.After(time.Second)
	for h.app.RefreshStatus(2).NextCheck.IsZero() {
		select {
		case <-deadline:
			t.Fatal("automatic schedule never became visible")
		case <-time.After(time.Millisecond):
		}
	}
	next := h.app.RefreshStatus(2).NextCheck
	if time.Until(next) < 59*time.Second || time.Until(next) > time.Minute {
		t.Fatal("incorrect next check", next)
	}
}
