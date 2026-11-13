// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"errors"

	"fmt"
	"github.com/airencracken/comfylib/password"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchStatusShowsOnSourcesAndOnlyBrieflyOnHome(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "mine", false)
	id := entryID(t, h.store, f)
	if _, err := h.store.DB.Exec("UPDATE feeds SET checked=? WHERE id=?", time.Now().Unix(), f.ID); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"/sources": true, "/sources?scope=household": true, "/?notice=refresh": true,
		"/": false, "/?view=all": false, fmt.Sprintf("/story/%d", id): false, "/account": false, "/groups": false,
	} {
		page := h.request(2, "GET", path, nil).Body.String()
		if got := strings.Contains(page, "data-fetch-status"); got != want {
			t.Errorf("%s: fetch status shown=%v, want %v", path, got, want)
		}
		if got := strings.Contains(page, "/static/fetch-status.js"); got != want {
			t.Errorf("%s: fetch status script loaded=%v, want %v", path, got, want)
		}
	}
	// The reading table gets the brief form: a message and an update link,
	// never failure details or the schedule.
	home := h.request(2, "GET", "/?notice=refresh", nil).Body.String()
	if !strings.Contains(home, "fetch-status-brief") || strings.Contains(home, "data-fetch-detail") || strings.Contains(home, "data-fetch-next") {
		t.Fatal("home banner is not brief", home)
	}
	if page := h.request(2, "GET", "/sources", nil).Body.String(); !strings.Contains(page, "data-fetch-detail") || strings.Contains(page, "fetch-status-brief") {
		t.Fatal("sources banner lost its detail")
	}
}

func TestFetchFailuresAppearOnSourceRowsNotHome(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "mine", false)
	if _, err := h.store.DB.Exec("UPDATE feeds SET error='publisher returned HTTP 503',checked=? WHERE id=?", time.Now().Unix(), f.ID); err != nil {
		t.Fatal(err)
	}
	status, err := h.app.fetchOverview(t.Context(), 2)
	if err != nil || status.State != "failed" || !strings.HasPrefix(status.Message, "Feeds checked at") || strings.Contains(status.Message, "could not") {
		t.Fatal("failure leaked into the shared status message", status, err)
	}
	sources := h.request(2, "GET", "/sources", nil).Body.String()
	if !strings.Contains(sources, `<p class="feed-error">Last check failed: publisher returned HTTP 503</p>`) || !strings.Contains(sources, "1 of 1 sources need another try") {
		t.Fatal("source row does not show its failure")
	}
	home := h.request(2, "GET", "/?notice=refresh", nil).Body.String()
	if strings.Contains(home, "HTTP 503") || strings.Contains(home, "need another try") {
		t.Fatal("failure details shown on the reading table")
	}
}

func TestHomeShowsFetchWhileReadersOwnRefreshIsPending(t *testing.T) {
	h := webHarness(t)
	h.app.refreshMu.Lock()
	h.app.refreshPending = map[int64]bool{2: true}
	h.app.refreshMu.Unlock()
	if !h.app.RefreshStatus(2).Requested || h.app.RefreshStatus(3).Requested {
		t.Fatal("requested status is not per reader")
	}
	// A scheduled check of every source is not this reader's request.
	h.app.refreshMu.Lock()
	h.app.refreshPending = map[int64]bool{}
	h.app.refreshRunning, h.app.refreshCurrentAll = true, true
	h.app.refreshMu.Unlock()
	if h.app.RefreshStatus(2).Requested || showFetch(httptest.NewRequest("GET", "/", nil), "home", fetchOverview{State: "fetching"}, h.app.RefreshStatus(2)) {
		t.Fatal("background fetch interrupts the reading table")
	}
	h.app.refreshMu.Lock()
	h.app.refreshRunning, h.app.refreshCurrentAll = false, false
	h.app.refreshMu.Unlock()
	for _, tc := range []struct {
		kind, target string
		fetch        fetchOverview
		status       RefreshStatus
		want         bool
	}{
		{"home", "/", fetchOverview{State: "idle"}, RefreshStatus{Requested: true}, true},
		{"home", "/", fetchOverview{State: "waiting"}, RefreshStatus{}, true},
		{"home", "/?notice=refresh", fetchOverview{State: "idle"}, RefreshStatus{}, true},
		{"home", "/?notice=refreshed", fetchOverview{State: "failed"}, RefreshStatus{}, false},
		{"story", "/story/1?notice=refresh", fetchOverview{State: "fetching"}, RefreshStatus{Requested: true}, false},
		{"account", "/account", fetchOverview{State: "queued"}, RefreshStatus{Requested: true}, false},
		{"sources", "/sources", fetchOverview{State: "idle"}, RefreshStatus{}, true},
	} {
		if got := showFetch(httptest.NewRequest("GET", tc.target, nil), tc.kind, tc.fetch, tc.status); got != tc.want {
			t.Errorf("%s %s: got %v", tc.kind, tc.target, got)
		}
	}
}

func TestHomeFiltersSitInADisclosureThatOpensWhenActive(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "mine", false)
	closed := h.request(2, "GET", "/", nil).Body.String()
	if !strings.Contains(closed, `<details class="filter-disclosure"><summary>Filter</summary>`) {
		t.Fatal("filters are not behind a closed disclosure")
	}
	if strings.Index(closed, "filter-disclosure") > strings.Index(closed, "story-list") {
		t.Fatal("stories should lead the reading column")
	}
	for _, query := range []string{"q=tools", "tag=tech", fmt.Sprintf("feed=%d", f.ID), "view=all&q=%3Cscript%3E"} {
		page := h.request(2, "GET", "/?"+query, nil)
		if page.Code != 200 || !strings.Contains(page.Body.String(), `<details class="filter-disclosure" open><summary>Filter</summary>`) {
			t.Fatal("active filter left the disclosure closed", query, page.Code)
		}
		if strings.Contains(page.Body.String(), "<script>") {
			t.Fatal("search text was not escaped")
		}
	}
	for _, bad := range []string{"feed=abc", "feed=-1", "tag=" + strings.Repeat("x", 161), "q=" + strings.Repeat("x", 301)} {
		if w := h.request(2, "GET", "/?"+bad, nil); w.Code != 400 {
			t.Fatal("bad filter accepted", bad, w.Code)
		}
	}
	// One refresh control, styled as secondary text, in the list heading.
	if n := strings.Count(closed, `action="/refresh"`); n != 1 || !strings.Contains(closed, `<button class="text-button">Refresh feeds</button>`) {
		t.Fatal("refresh control is missing or prominent", n)
	}
}

func TestEachActionHasOneHome(t *testing.T) {
	h := webHarness(t)
	ownerAccount := h.request(1, "GET", "/account", nil).Body.String()
	for _, household := range []string{`action="/account/invite"`, "Household members", "Suggested sources", "Instance backups", "Fetch all feeds now", "Feed images", `href="/account/invitations"`} {
		if strings.Contains(ownerAccount, household) {
			t.Errorf("personal account page carries owner action %q", household)
		}
	}
	admin := h.request(1, "GET", "/admin", nil).Body.String()
	for _, want := range []string{`href="/account/invitations"`, "Household members", "Suggested sources", "Instance backups", "Fetch all feeds now", "Feed images", `href="/admin/metrics"`, "tidings set-password"} {
		if !strings.Contains(admin, want) {
			t.Errorf("admin page is missing %q", want)
		}
	}
	if strings.Contains(admin, `action="/account/invite"`) || strings.Contains(admin, "Your personal account settings") {
		t.Error("admin duplicates invitation creation or personal links")
	}
	invitations := h.request(1, "GET", "/account/invitations", nil).Body.String()
	if strings.Count(invitations, `action="/account/invite"`) != 1 || !strings.Contains(invitations, `href="/admin"`) {
		t.Fatal("invitations page should be the single place to create invitations")
	}
	if w := h.request(2, "GET", "/account/invitations", nil); w.Code == 200 && strings.Contains(w.Body.String(), `action="/account/invite"`) {
		t.Fatal("member can reach invitation creation")
	}
	if w := h.request(2, "POST", "/account/invite", nil); w.Code != 403 {
		t.Fatal("member created an invitation", w.Code)
	}
	// Owner admin settings still return to Admin after saving.
	if w := h.request(1, "POST", "/account/images", url.Values{"enabled": {"true"}, "return_to": {"/admin"}}); w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/admin") {
		t.Fatal(w.Code, w.Header().Get("Location"))
	}
}

func TestKeyboardShortcutGuideHasOneSource(t *testing.T) {
	h := webHarness(t)
	if _, err := h.store.DB.Exec("UPDATE users SET keyboard_shortcuts=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	page := h.request(2, "GET", "/account", nil).Body.String()
	guide := "j / k: next / previous story<br>Enter: read selected story"
	if strings.Count(page, guide) != 2 {
		t.Fatal("dialog and account guide should both render the shared list")
	}
	data, err := assets.ReadFile("templates/pages.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), guide) != 1 || !strings.Contains(string(data), `{{define "shortcut-list"}}`) {
		t.Fatal("shortcut guide copied instead of shared")
	}
}

func TestArticleHasOnePrimaryAction(t *testing.T) {
	h := webHarness(t)
	id := entryID(t, h.store, testSource(t, h.store, 2, "mine", false))
	page := h.request(2, "GET", fmt.Sprintf("/story/%d", id), nil).Body.String()
	article := page[strings.Index(page, `<article class="paper reading-paper"`):]
	header := article[strings.Index(article, `<div class="story-actions">`):strings.Index(article, "</header>")]
	if strings.Contains(header, `class="button"`) || strings.Contains(header, "<button>") {
		t.Fatal("article header has a primary action", header)
	}
	if !strings.Contains(header, `rel="noopener noreferrer">Read on `) {
		t.Fatal("read-on-host link missing")
	}
	footer := article[strings.Index(article, "<footer><form"):]
	if !strings.Contains(footer, "<button>Mark read and return</button>") {
		t.Fatal("primary action missing")
	}
}

func TestTemplateVoiceUsesPlainHouseholdWords(t *testing.T) {
	var all strings.Builder
	for _, name := range []string{"templates/pages.html", "templates/groups.html", "templates/metrics.html"} {
		data, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	source := all.String()
	for _, retired := range []string{"gathering", "Your place here", "'s place", "usual seat", "open seat", "A place at the table", "Tridactyl", "MiB", "12–72", "bytes", "Set aside", "set aside"} {
		if strings.Contains(source, retired) {
			t.Errorf("templates still say %q", retired)
		}
	}
	for _, kept := range []string{"reading table", "Save for later", "Saved for later", "Skip", "12 or more characters", `up to {{uploadLimit "opml"}}`, `up to {{uploadLimit "restore"}}`} {
		if !strings.Contains(source, kept) {
			t.Errorf("templates lost %q", kept)
		}
	}
}

func TestRejectedFormsUseTheStyledLayout(t *testing.T) {
	h := webHarness(t)
	for _, tc := range []struct {
		uid    int64
		path   string
		values url.Values
		status int
		text   string
	}{
		{2, "/story/1/state", url.Values{"csrf": {"stale"}}, 403, "Reload it and try again."},
		{0, "/login", url.Values{"csrf": {""}, "username": {"alice"}}, 403, "Reload it and try again."},
		{2, "/account/keyboard", url.Values{"csrf": {"x"}, "bad": {"%zz"}}, 403, "Reload it and try again."},
	} {
		w := h.request(tc.uid, "POST", tc.path, tc.values)
		body := w.Body.String()
		if w.Code != tc.status || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, `class="site-header"`) || !strings.Contains(body, tc.text) {
			t.Fatal(tc.path, w.Code, w.Header().Get("Content-Type"), body)
		}
	}
	// Signed-in members keep their navigation on the error page.
	if body := h.request(2, "POST", "/refresh", url.Values{"csrf": {"stale"}}).Body.String(); !strings.Contains(body, "Signed in as alice") {
		t.Fatal("error page lost the signed-in header")
	}
	r := httptest.NewRequest("POST", "/sources", strings.NewReader("url=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookieFor(h, 2))
	w := httptest.NewRecorder()
	h.app.ServeHTTP(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "That didn&#39;t work") || !strings.Contains(w.Body.String(), `class="site-header"`) {
		t.Fatal("malformed form not styled", w.Code, w.Body.String())
	}
}

func cookieFor(h *harness, uid int64) *http.Cookie {
	return &http.Cookie{Name: h.app.cookieName(), Value: h.tokens[uid]}
}

func TestRateLimitedSignInAndJoinStayOnTheirForms(t *testing.T) {
	h := webHarness(t)
	var w *httptest.ResponseRecorder
	for range 25 {
		w = h.request(0, "POST", "/login", url.Values{"username": {"alice"}, "password": {"wrong password here"}})
	}
	if w.Code != 429 || !strings.Contains(w.Body.String(), "Too many attempts. Wait 15 minutes, then try again.") || !strings.Contains(w.Body.String(), `action="/login"`) {
		t.Fatal("rate-limited sign-in is not a styled form", w.Code, w.Body.String())
	}
	w = h.request(0, "POST", "/join", url.Values{"code": {"abc<def"}, "username": {"newcomer"}, "password": {testPassword}})
	if w.Code != 429 || !strings.Contains(w.Body.String(), `action="/join"`) || !strings.Contains(w.Body.String(), `value="abc&lt;def"`) {
		t.Fatal("rate-limited join is not a styled form", w.Code, w.Body.String())
	}
}

// Comfylib owns the password rule; Tidings applies it everywhere an account
// chooses a password and keeps its own username rule.
func TestAccountsUseTheSharedPasswordRule(t *testing.T) {
	if err := ValidateCredentials("reader", "short"); !errors.Is(err, password.ErrTooShort) {
		t.Fatal(err)
	}
	if err := ValidateCredentials("reader", strings.Repeat("a", password.MaxBytes+1)); !errors.Is(err, password.ErrTooLong) {
		t.Fatal(err)
	}
	if err := ValidateCredentials("no spaces allowed", testPassword); err == nil || errors.Is(err, password.ErrTooShort) {
		t.Fatal("username rule lost", err)
	}
	h := webHarness(t)
	raw, err := h.store.Invite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	w := h.request(0, "POST", "/join", url.Values{"code": {raw}, "username": {"newcomer"}, "password": {"short"}})
	if w.Code != 400 || !strings.Contains(w.Body.String(), password.ErrTooShort.Error()) {
		t.Fatal("join does not show the shared message", w.Code)
	}
	// A password over bcrypt's limit is refused at sign-in without hashing.
	if _, err = h.store.Login(t.Context(), "alice", strings.Repeat("x", password.MaxBytes+1)); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
}
