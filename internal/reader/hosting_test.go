package reader

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airencracken/comfylib/token"
)

func TestOwnerForceFetchContract(t *testing.T) {
	h := webHarness(t)
	mine := testSource(t, h.store, 1, "owner", false)
	other := testSource(t, h.store, 2, "member", false)
	shared := testSource(t, h.store, 1, "household", true)
	if _, err := h.store.DB.Exec("INSERT INTO states(user_id,entry_id,read,saved) VALUES(2,?,1,1)", entryID(t, h.store, other)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		seen[r.URL.String()]++
		mu.Unlock()
		if r.Header.Get("If-None-Match") != "tag" {
			t.Error("conditional request validator lost")
		}
		return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	for _, tc := range []struct {
		uid          int64
		method, csrf string
		status       int
	}{
		{0, "POST", "", 303}, {2, "POST", "", 403},
		{1, "GET", "", 405}, {1, "POST", "invalid", 403},
	} {
		data := url.Values{}
		if tc.csrf != "" {
			data.Set("csrf", tc.csrf)
		}
		if w := h.request(tc.uid, tc.method, "/account/refresh", data); w.Code != tc.status {
			t.Fatalf("authorization: %+v: %d", tc, w.Code)
		}
	}
	if len(seen) != 0 {
		t.Fatal("unauthorized fetch occurred")
	}
	if w := h.request(1, "POST", "/refresh", nil); w.Code != 303 {
		t.Fatal(w.Code)
	}
	h.app.Wait()
	if len(seen) != 0 {
		t.Fatal("ordinary refresh bypassed cooldown")
	}
	if w := h.request(1, "POST", "/account/refresh", nil); w.Code != 303 || w.Header().Get("Location") != "/account?notice=refresh-all" {
		t.Fatal(w.Code, w.Body)
	}
	h.app.Wait()
	for _, f := range []Feed{mine, other, shared} {
		if seen[f.URL] != 1 {
			t.Fatal("force did not fetch each active feed exactly once", seen)
		}
	}
	var read, saved, subscriptions int
	if err := h.store.DB.QueryRow("SELECT read,saved FROM states WHERE user_id=2").Scan(&read, &saved); err != nil || read != 1 || saved != 1 {
		t.Fatal("fetch changed member state", read, saved, err)
	}
	if err := h.store.DB.QueryRow("SELECT count(*) FROM subscriptions").Scan(&subscriptions); err != nil || subscriptions != 2 {
		t.Fatal(subscriptions, err)
	}
	if w := h.request(1, "GET", "/account", nil); !strings.Contains(w.Body.String(), "Fetch all feeds now") {
		t.Fatal("owner action missing")
	}
	if w := h.request(2, "GET", "/account", nil); strings.Contains(w.Body.String(), "Fetch all feeds now") {
		t.Fatal("owner action visible to member")
	}
}

func TestForcedFetchQueuedAndCoalesced(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "queued", false)
	if _, err := h.store.DB.Exec("UPDATE feeds SET checked=0"); err != nil {
		t.Fatal(err)
	}
	started, release, forced, finish := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(finish)
	defer close(release)
	var mu sync.Mutex
	calls := 0
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if r.URL.String() != f.URL {
			t.Error("unexpected source")
		}
		if n == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		} else if n == 2 {
			close(forced)
			select {
			case <-finish:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	h.app.StartRefresh(2, false)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch never started")
	}
	for range 3 {
		if !h.app.startRefresh(0, true, true) {
			t.Fatal("force rejected")
		}
	}
	// Unblock without closing, so deferred cleanup also works on assertion failure.
	release <- struct{}{}
	select {
	case <-forced:
	case <-time.After(5 * time.Second):
		t.Fatal("queued force lost or suppressed by cooldown")
	}
	for range 3 {
		h.app.startRefresh(0, true, true)
	}
	finish <- struct{}{}
	h.app.Wait()
	if calls != 2 {
		t.Fatal("duplicate forced requests were not coalesced", calls)
	}
}

func TestSuggestionCategoriesDefaultAuthorizationAndAtomicity(t *testing.T) {
	h := webHarness(t)
	ctx := context.Background()
	available, err := h.app.catalogSettings(ctx)
	if err != nil || len(available) < 3 {
		t.Fatal(available, err)
	}
	if enabled, err := h.store.CatalogCategories(ctx); err != nil || len(enabled) != 0 {
		t.Fatal("suggestions must start disabled", enabled, err)
	}
	if w := h.request(2, "GET", "/sources?catalog_q=AI", nil); strings.Contains(w.Body.String(), "catalog-card") {
		t.Fatal("disabled suggestion disclosed")
	}
	if w := h.request(2, "POST", "/account/suggestions", url.Values{"category": {"AI"}}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := h.request(1, "POST", "/account/suggestions", url.Values{"csrf": {"bad"}, "category": {"AI"}}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := h.request(1, "GET", "/account/suggestions", nil); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := h.request(1, "POST", "/account/suggestions", url.Values{"category": {"AI"}}); w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	w := h.request(2, "GET", "/sources", nil)
	if !strings.Contains(w.Body.String(), "Simon Willison") || strings.Contains(w.Body.String(), "Krebs on Security") {
		t.Fatal("enabled categories not enforced")
	}
	for _, bad := range [][]string{{"unknown"}, {"AI", "AI"}, {"Tech", "<script>"}, {"AI\nTech"}} {
		if err := h.store.SetCatalogCategories(ctx, 1, bad, available); err == nil {
			t.Fatal("bad category accepted", bad)
		}
		enabled, err := h.store.CatalogCategories(ctx)
		if err != nil || !reflect.DeepEqual(enabled, map[string]bool{"AI": true}) {
			t.Fatal("invalid update partially applied", enabled, err)
		}
	}
	if _, err := h.store.DB.Exec("CREATE TRIGGER fail_catalog BEFORE INSERT ON catalog_categories WHEN NEW.name='Tech' BEGIN SELECT RAISE(ABORT,'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetCatalogCategories(ctx, 1, []string{"Infosec", "Tech"}, available); err == nil {
		t.Fatal("injected transaction failure ignored")
	}
	enabled, err := h.store.CatalogCategories(ctx)
	if err != nil || !reflect.DeepEqual(enabled, map[string]bool{"AI": true}) {
		t.Fatal("failed transaction changed settings", enabled, err)
	}
	if _, err := h.store.DB.Exec("DROP TRIGGER fail_catalog"); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{0, 2, 999} {
		if err := h.store.SetCatalogCategories(ctx, uid, nil, available); err == nil {
			t.Fatal("non-owner changed settings", uid)
		}
	}
	if _, err := h.store.DB.Exec("UPDATE users SET suspended=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetCatalogCategories(ctx, 1, nil, available); err == nil {
		t.Fatal("suspended owner changed settings")
	}
}

func TestSuggestionCategorySubsetsAndPreservedSubscriptions(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "keep", false)
	ctx := context.Background()
	available, err := h.app.catalogSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for mask := 0; mask < 1<<3; mask++ {
		var selected []string
		want := map[string]bool{}
		for i, category := range available[:3] {
			if mask&(1<<i) != 0 {
				selected = append(selected, category.Name)
				want[category.Name] = true
			}
		}
		if err := h.store.SetCatalogCategories(ctx, 1, selected, available); err != nil {
			t.Fatal(err)
		}
		got, err := h.store.CatalogCategories(ctx)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(mask, got, want, err)
		}
		feeds, err := h.store.Feeds(ctx, 2, false)
		if err != nil || len(feeds) != 1 || feeds[0].ID != f.ID {
			t.Fatal("settings changed subscriptions", feeds, err)
		}
	}
}

func TestV2MigrationPreservesDataAndStartsSuggestionsDisabled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reader")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("DROP TABLE catalog_categories; PRAGMA user_version=2; INSERT INTO users(username,password,owner) VALUES('existing','hash',1)"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version, users int
	if err := s.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatal(version, err)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM users WHERE username='existing'").Scan(&users); err != nil || users != 1 {
		t.Fatal(users, err)
	}
	if enabled, err := s.CatalogCategories(context.Background()); err != nil || len(enabled) != 0 {
		t.Fatal(enabled, err)
	}
	if err := verifyDatabase(context.Background(), filepath.Join(dir, "reader.db")); err != nil {
		t.Fatal("new schema backup rejected", err)
	}
}

func TestInvitationPageAndConfiguredOriginContract(t *testing.T) {
	h := webHarness(t)
	if w := h.request(2, "GET", "/account/invitations", nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := h.request(1, "GET", "/account/invitations", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "invitation-layout") {
		t.Fatal(w.Code, w.Body)
	}
	w := h.request(1, "POST", "/account/invite", nil)
	match := regexp.MustCompile(`value="(https://tidings.example.test/join\?code=([a-f0-9]{64}))"`).FindStringSubmatch(w.Body.String())
	if w.Code != 200 || len(match) != 3 {
		t.Fatal(w.Code, w.Body)
	}
	for _, class := range []string{"invitation-create", "invitation-list", "invitation-result", "invitation-status"} {
		if !strings.Contains(w.Body.String(), class) {
			t.Fatal("shared layout missing", class)
		}
	}
	if strings.Contains(w.Body.String(), "add your Tidings site's address") {
		t.Fatal("incomplete link instructions retained")
	}
	w = h.request(1, "GET", "/account/invitations", nil)
	if strings.Contains(w.Body.String(), match[2]) {
		t.Fatal("raw invitation disclosed after creation")
	}
	w = h.request(1, "POST", "/account/invitations/revoke", url.Values{"invitation": {token.Hash(match[2])}})
	if w.Code != 303 || w.Header().Get("Location") != "/account/invitations" {
		t.Fatal(w.Code)
	}
	if err := h.store.Join(context.Background(), match[2], "new-member", testPassword); err == nil {
		t.Fatal("revoked invitation accepted")
	}
	h.app.config.BaseURL = ""
	var before, after int
	h.store.DB.QueryRow("SELECT count(*) FROM invites").Scan(&before)
	if w := h.request(1, "POST", "/account/invite", nil); w.Code != 400 {
		t.Fatal("unconfigured origin minted a broken link", w.Code)
	}
	h.store.DB.QueryRow("SELECT count(*) FROM invites").Scan(&after)
	if after != before {
		t.Fatal("failed invitation creation mutated state")
	}
}

func TestPublicOriginAdversarialInputs(t *testing.T) {
	for _, raw := range []string{"", "//evil.example", "http://example.org", "http://8.8.8.8", "https://user@example.org", "https://example.org/path", "https://example.org?", "https://example.org?q=x", "https://example.org#fragment", "https://example.org\n"} {
		if _, err := PublicOrigin(raw); err == nil {
			t.Fatal("bad origin accepted", raw)
		}
	}
	for _, raw := range []string{"https://example.org", "https://example.org/", "http://127.0.0.1:8085", "http://[::1]:8085"} {
		if got, err := PublicOrigin(raw); err != nil || got != strings.TrimRight(raw, "/") {
			t.Fatal(got, err)
		}
	}
}
