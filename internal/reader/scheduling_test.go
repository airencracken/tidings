package reader

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdaptivePollBounds(t *testing.T) {
	now := int64(1800000000)
	for _, tc := range []struct {
		name     string
		previous time.Duration
		checked  int64
		added    int
		want     time.Duration
	}{
		{"first check", initialPoll, 0, 1000, initialPoll},
		{"quiet", initialPoll, now - 1800, 0, time.Hour},
		{"busy", initialPoll, now - 1800, 6, 17*time.Minute + 30*time.Second},
		{"fast lower bound", minPoll, now - 300, 1000, minPoll},
		{"quiet upper bound", maxPoll, now - 86400, 0, maxPoll},
		{"invalid previous", 0, 0, 0, initialPoll},
		{"future date", minPoll, now + 900, 2, minPoll},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pollDelay(tc.previous, tc.checked, now, tc.added); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
}

func TestPublisherScheduleHints(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"120", 2 * time.Minute}, {"999999999999", maxPoll}, {"-1", 0}, {"nonsense", 0}, {now.Add(3 * time.Hour).Format(http.TimeFormat), 3 * time.Hour}, {now.Add(-time.Hour).Format(http.TimeFormat), 0},
	} {
		got := retryAfter(tc.raw, now)
		if tc.want == 0 {
			if !got.IsZero() {
				t.Fatal(tc, got)
			}
		} else if got.Sub(now) != tc.want {
			t.Fatal(tc, got)
		}
	}
	if got := publisherDelay(http.Header{"Cache-Control": {"public, max-age=7200"}}, now); got.Sub(now) != 2*time.Hour {
		t.Fatal(got)
	}
	if got := publisherDelay(http.Header{"Expires": {now.Add(48 * time.Hour).Format(http.TimeFormat)}}, now); got.Sub(now) != maxPoll {
		t.Fatal(got)
	}
	f := &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"7200"}}, Body: io.NopCloser(strings.NewReader("slow down")), Request: r}, nil
	})}}
	_, err := f.Fetch(t.Context(), "https://example.org/feed", "", "")
	var upstream *publisherError
	if !errors.As(err, &upstream) || time.Until(upstream.Retry) < 119*time.Minute {
		t.Fatal("retry hint was lost", err)
	}
}

func TestSchedulePersistenceAndAtomicity(t *testing.T) {
	s := testStore(t)
	f := testSource(t, s, 2, "schedule", false)
	now := time.Now()
	if _, err := s.DB.Exec("UPDATE feeds SET checked=?,poll_interval=1800,failures=3 WHERE id=?", now.Add(-30*time.Minute).Unix(), f.ID); err != nil {
		t.Fatal(err)
	}
	failure := &publisherError{Status: 503, Retry: now.Add(3 * time.Hour)}
	if err := s.recordFetchFailure(t.Context(), f.ID, failure, now); err != nil {
		t.Fatal(err)
	}
	var failures int
	var next, interval int64
	if err := s.DB.QueryRow("SELECT failures,next_check FROM feeds WHERE id=?", f.ID).Scan(&failures, &next); err != nil || failures != 4 || next != failure.Retry.Unix() {
		t.Fatal(failures, next, err)
	}
	for range 25 {
		if err := s.recordFetchFailure(t.Context(), f.ID, errors.New("down"), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.QueryRow("SELECT failures,next_check FROM feeds WHERE id=?", f.ID).Scan(&failures, &next); err != nil || failures != 20 || next != now.Add(6*time.Hour).Unix() {
		t.Fatal("unbounded failure backoff", failures, next, err)
	}
	if err := s.ApplyFetch(t.Context(), f, FetchResult{Unchanged: true, NotBefore: now.Add(4 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT failures,next_check,poll_interval FROM feeds WHERE id=?", f.ID).Scan(&failures, &next, &interval); err != nil || failures != 0 || interval != 3600 || next < now.Add(4*time.Hour).Unix() {
		t.Fatal("success did not reset failures or honor cache", failures, next, interval, err)
	}
	var checked int64
	if err := s.DB.QueryRow("SELECT checked FROM feeds WHERE id=?", f.ID).Scan(&checked); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("CREATE TRIGGER fail_article BEFORE INSERT ON entries BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	err := s.ApplyFetch(t.Context(), f, FetchResult{Feed: ParsedFeed{Title: "Changed", Entries: []Entry{{Key: "new", Title: "New", URL: "https://example.org/new"}}}})
	if err == nil {
		t.Fatal("failed entry write accepted")
	}
	var after int64
	if err = s.DB.QueryRow("SELECT next_check FROM feeds WHERE id=?", f.ID).Scan(&after); err != nil || after != next {
		t.Fatal("failed fetch advanced schedule", after, next, err)
	}
	if err = s.DB.QueryRow("SELECT checked FROM feeds WHERE id=?", f.ID).Scan(&after); err != nil || after != checked {
		t.Fatal("failed fetch advanced checked time", after, checked, err)
	}
	for _, query := range []string{"UPDATE feeds SET poll_interval=1", "UPDATE feeds SET next_check=-1", "UPDATE feeds SET failures=21"} {
		if _, err = s.DB.Exec(query); err == nil {
			t.Fatal("invalid schema state accepted", query)
		}
	}
}

func TestScheduledDueAndManualRefresh(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "future", false)
	if _, err := h.store.DB.Exec("UPDATE feeds SET checked=?,next_check=? WHERE id=?", time.Now().Add(-10*time.Minute).Unix(), time.Now().Add(4*time.Hour).Unix(), f.ID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	h.app.StartRefresh(0, true)
	h.app.Wait()
	if calls != 0 {
		t.Fatal("scheduled fetch ignored future due time", calls)
	}
	h.app.StartRefresh(2, false)
	h.app.Wait()
	if calls != 1 {
		t.Fatal("explicit refresh could not check source", calls)
	}
	h.app.StartRefresh(2, false)
	h.app.Wait()
	if calls != 1 {
		t.Fatal("manual cooldown ignored", calls)
	}
	h.app.startRefresh(0, true, true)
	h.app.Wait()
	if calls != 2 {
		t.Fatal("owner force no longer bypasses cooldown", calls)
	}
}

func TestRefreshHostConcurrency(t *testing.T) {
	h := webHarness(t)
	for i := range 8 {
		address := fmt.Sprintf("https://publisher%d.example.org/feed/%d", i%2, i)
		if err := h.store.Subscribe(t.Context(), 2, []Feed{{URL: address}}, false); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	active := map[string]int{}
	maxHost, total, maxTotal, calls := 0, 0, 0, 0
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		active[r.URL.Hostname()]++
		total++
		calls++
		maxHost = max(maxHost, active[r.URL.Hostname()])
		maxTotal = max(maxTotal, total)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active[r.URL.Hostname()]--
		total--
		mu.Unlock()
		return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	h.app.StartRefresh(0, true)
	h.app.Wait()
	if calls != 8 || maxHost != 1 || maxTotal > 4 || maxTotal < 2 {
		t.Fatal("host or worker bounds violated", calls, maxHost, maxTotal)
	}
}

func TestNextCheckKeepsReaderScope(t *testing.T) {
	h := webHarness(t)
	mine := testSource(t, h.store, 2, "mine", false)
	other := testSource(t, h.store, 3, "other", false)
	now := time.Now()
	if _, err := h.store.DB.Exec("UPDATE feeds SET checked=?,next_check=CASE id WHEN ? THEN ? WHEN ? THEN ? END", now.Add(-time.Hour).Unix(), mine.ID, now.Add(3*time.Hour).Unix(), other.ID, now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	summary, err := h.store.fetchSummary(t.Context(), 2)
	if err != nil || summary.NextCheck != now.Add(3*time.Hour).Unix() {
		t.Fatal("next check leaks other readers' schedules", summary, err)
	}
}

func FuzzAdaptiveScheduleBounds(f *testing.F) {
	f.Add(int64(1800), int64(1800000000), uint16(10))
	f.Add(int64(86400), int64(0), uint16(0))
	f.Fuzz(func(t *testing.T, seconds, checked int64, added uint16) {
		got := pollDelay(time.Duration(seconds)*time.Second, checked, 1800000000, int(added))
		if got < 5*time.Minute || got > 24*time.Hour {
			t.Fatal("schedule outside documented bounds", got)
		}
	})
}
