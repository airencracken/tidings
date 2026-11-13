// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func routeHits(t *testing.T, s *Store, route string) int64 {
	t.Helper()
	var n int64
	if err := s.DB.QueryRow("SELECT coalesce(sum(requests),0) FROM comfy_analytics_routes WHERE route=?", route).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Tidings measures site health per route. Nothing it stores names an account
// or a story, whichever way a member signs in or reads.
func TestSiteHealthIsCountedPerRouteWithoutNamingAnyone(t *testing.T) {
	h := webHarness(t)
	feed := testSource(t, h.store, 2, "personal", false)
	id := entryID(t, h.store, feed)
	if _, err := h.store.Login(t.Context(), "alice", testPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.sharedSignIn(httptest.NewRecorder(), httptest.NewRequest("GET", "http://example.test/auth/shared/callback", nil), 2); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fmt.Sprintf("/story/%d", id), fmt.Sprintf("/story/%04d", id), "/", "/?view=all"} {
		if w := h.request(2, "GET", path, nil); w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
	h.request(2, "GET", "/story/99999999", nil)
	if err := h.app.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := routeHits(t, h.store, "GET /story/{id}"); n != 3 {
		t.Fatalf("story route counted %d times", n)
	}
	if n := routeHits(t, h.store, "GET /{$}"); n != 2 {
		t.Fatalf("home route counted %d times", n)
	}
	// Routes are labelled by pattern: no story ID or query string is stored.
	rows, err := h.store.DB.Query("SELECT route FROM comfy_analytics_routes")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var route string
		if err = rows.Scan(&route); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(route, fmt.Sprint(id)) || strings.Contains(route, "?") || strings.Contains(route, "alice") {
			t.Fatalf("route label names something: %q", route)
		}
	}
	summary, err := h.store.Analytics(t.Context(), time.Now())
	if err != nil || summary.Requests < 5 || summary.Logins != 0 || summary.Views != 0 || summary.ActiveMembers != 0 || len(summary.LoginsByAccount) != 0 || len(summary.Popular) != 0 {
		t.Fatalf("%+v %v", summary, err)
	}
	var detail int
	if err = h.store.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='comfy_analytics_activity'").Scan(&detail); err != nil || detail != 0 {
		t.Fatal("per-account activity table exists", detail, err)
	}
	for _, uid := range []int64{0, 2, 3} {
		if w := h.request(uid, "GET", "/admin/metrics?owner=true", nil); w.Code == 200 || strings.Contains(w.Body.String(), "Instance overview") {
			t.Fatal("metrics exposed", uid, w.Code)
		}
	}
	page := h.request(1, "GET", "/admin/metrics", nil).Body.String()
	for _, want := range []string{"Sources with errors", "Database size", "Site health"} {
		if !strings.Contains(page, want) {
			t.Errorf("metrics page is missing %q", want)
		}
	}
	for _, gone := range []string{"Recent sign-ins by account", "Most viewed content", "alice"} {
		if strings.Contains(page, gone) {
			t.Errorf("metrics page shows %q", gone)
		}
	}
}

func TestSecurityEventsAreCounted(t *testing.T) {
	h := webHarness(t)
	security := func() (failed, limited, rejected int64) {
		t.Helper()
		summary, err := h.store.Analytics(t.Context(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return summary.FailedSignIns, summary.RateLimited, summary.RejectedTokens
	}
	h.request(0, "POST", "/login", url.Values{"username": {"alice"}, "password": {"wrong password here"}})
	if f, l, r := security(); f != 1 || l != 0 || r != 0 {
		t.Fatal("failed sign-in", f, l, r)
	}
	// A correct sign-in is not counted at all.
	h.request(0, "POST", "/login", url.Values{"username": {"alice"}, "password": {testPassword}})
	if f, _, _ := security(); f != 1 {
		t.Fatal("successful sign-in counted as failed", f)
	}
	h.request(0, "POST", "/join", url.Values{"code": {"not-a-real-code"}, "username": {"newcomer"}, "password": {testPassword}})
	if _, _, r := security(); r != 1 {
		t.Fatal("rejected invitation not counted", r)
	}
	// A valid invitation with a taken username is not a rejected token.
	raw, err := h.store.Invite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	h.request(0, "POST", "/join", url.Values{"code": {raw}, "username": {"alice"}, "password": {testPassword}})
	if _, _, r := security(); r != 1 {
		t.Fatal("taken username counted as a rejected token", r)
	}
	for range 25 {
		h.request(0, "POST", "/login", url.Values{"username": {"alice"}, "password": {"wrong password here"}})
	}
	if _, l, _ := security(); l == 0 {
		t.Fatal("rate limit not counted")
	}
}

func TestSharedSignInStillRejectsSuspendedAccounts(t *testing.T) {
	h := webHarness(t)
	request := httptest.NewRequest("GET", "http://example.test/auth/shared/callback", nil)
	var before int
	if err := h.store.DB.QueryRow("SELECT count(*) FROM sessions WHERE user_id=2").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB.Exec("UPDATE users SET suspended=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if _, err := h.app.sharedSignIn(response, request, 2); err == nil {
		t.Fatal("suspended account signed in")
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("suspended account received a session")
	}
	var sessions int
	if err := h.store.DB.QueryRow("SELECT count(*) FROM sessions WHERE user_id=2").Scan(&sessions); err != nil || sessions != before {
		t.Fatal(sessions, err)
	}
	if _, err := h.app.sharedSignIn(response, request, 99999); err == nil {
		t.Fatal("unknown account signed in")
	}
}

// Rows stored by 0.7.0 and 0.8.0, including in restored backups, go away the
// next time the database opens. Other data is untouched.
func TestOpeningDatabaseRemovesStoredMemberActivity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reader")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO users(username,password) VALUES('kept','hash');
	CREATE TABLE comfy_analytics_activity(day INTEGER, kind TEXT, member TEXT, item TEXT, count INTEGER, last_at INTEGER);
	INSERT INTO comfy_analytics_activity VALUES(1,'login','1','',3,1),(1,'story','1','4812',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var tables, users int
	if err = reopened.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='comfy_analytics_activity'").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("stored activity survived: %d %v", tables, err)
	}
	if err = reopened.DB.QueryRow("SELECT count(*) FROM users WHERE username='kept'").Scan(&users); err != nil || users != 1 {
		t.Fatal("unrelated data changed", users, err)
	}
}
