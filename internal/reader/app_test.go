package reader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/airencracken/comfylib/token"
)

type harness struct {
	app    *App
	store  *Store
	tokens map[int64]string
}

func webHarness(t *testing.T) *harness {
	t.Helper()
	s := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	a, err := NewApp(ctx, s, Config{BaseURL: "https://tidings.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); a.Wait() })
	h := &harness{app: a, store: s, tokens: map[int64]string{0: strings.Repeat("0", 64)}}
	for _, id := range []int64{1, 2, 3} {
		raw := fmt.Sprintf("%064d", id)
		h.tokens[id] = raw
		if _, err = s.DB.Exec("INSERT INTO sessions(token,user_id,expires) VALUES(?,?,?)", token.Hash(raw), id, time.Now().Add(time.Hour).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	return h
}
func (h *harness) request(uid int64, method, path string, data url.Values) *httptest.ResponseRecorder {
	if data == nil {
		data = url.Values{}
	}
	if method != "GET" && method != "HEAD" {
		if _, exists := data["csrf"]; !exists {
			data.Set("csrf", token.SessionCSRF(h.tokens[uid], "tidings:forms"))
		}
	}
	r := httptest.NewRequest(method, path, strings.NewReader(data.Encode()))
	r.RemoteAddr = "198.51.100.4:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	name := h.app.cookieName()
	if uid == 0 {
		name += "-form"
	}
	r.AddCookie(&http.Cookie{Name: name, Value: h.tokens[uid]})
	w := httptest.NewRecorder()
	h.app.ServeHTTP(w, r)
	return w
}

func TestRoutesAndSecurityHeaders(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "personal", false)
	id := entryID(t, h.store, f)
	g, err := h.store.CreateGroup(context.Background(), User{ID: 2}, "Personal group", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/?view=all", "/?view=saved", "/?view=group", "/sources", "/sources?scope=household", "/account", "/groups", fmt.Sprintf("/groups/%d", g), fmt.Sprintf("/story/%d", id), "/subscriptions.opml", "/api/v1/export"} {
		t.Run(path, func(t *testing.T) {
			w := h.request(2, "GET", path, nil)
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
				t.Fatal("private response lacks security headers")
			}
		})
	}
	for _, path := range []string{"/login", "/join?code=example", "/healthz", "/static/style.css", "/static/theme.js"} {
		w := h.request(0, "GET", path, nil)
		if w.Code != 200 {
			t.Fatalf("public route %s: %d %s", path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/", "/sources", "/groups", "/account", "/api/v1/export"} {
		w := h.request(0, "GET", path, nil)
		if w.Code != 303 || w.Header().Get("Location") != "/login" {
			t.Fatalf("unauthenticated route exposed %s", path)
		}
	}
	if w := h.request(2, "GET", "/not-a-route", nil); w.Code != 404 {
		t.Fatalf("unknown route %d", w.Code)
	}
}

func TestAllMutationRoutesRequireCSRF(t *testing.T) {
	h := webHarness(t)
	for _, path := range []string{"/login", "/join", "/logout", "/refresh", "/read-page", "/story/1/state", "/sources", "/sources/1/remove", "/sources/1/tags", "/sources/import", "/groups", "/groups/1", "/groups/1/delete", "/account/household", "/account/password", "/account/invite"} {
		w := h.request(2, "POST", path, url.Values{"csrf": {"forged"}})
		if w.Code != 403 {
			t.Errorf("%s bypassed CSRF: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/account/household", strings.NewReader(url.Values{"csrf": {token.SessionCSRF(h.tokens[2], "tidings:forms")}, "enabled": {"true"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Origin", "https://attacker.example")
	r.AddCookie(&http.Cookie{Name: "tidings", Value: h.tokens[2]})
	w := httptest.NewRecorder()
	h.app.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin form accepted %d", w.Code)
	}
}

func TestHTTPIsolationAndRoleChecks(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 1, "private-sentinel", false)
	id := entryID(t, h.store, f)
	if w := h.request(2, "GET", fmt.Sprintf("/story/%d", id), nil); w.Code != 404 {
		t.Fatal("private story read through guessed ID")
	}
	if w := h.request(2, "POST", fmt.Sprintf("/story/%d/state", id), url.Values{"field": {"saved"}, "value": {"true"}}); w.Code != 404 {
		t.Fatal("private story state changed through guessed ID")
	}
	for _, path := range []string{"/sources", "/sources/1/remove", "/sources/1/tags", "/sources/import"} {
		if w := h.request(2, "POST", path, url.Values{"scope": {"household"}}); w.Code != 403 {
			t.Errorf("member changed household via %s: %d", path, w.Code)
		}
	}
	if w := h.request(2, "POST", "/account/invite", nil); w.Code != 403 {
		t.Fatal("member issued invitation")
	}
	for _, path := range []string{"/", "/?view=all", "/?q=private", "/sources", "/api/v1/export", "/subscriptions.opml"} {
		w := h.request(2, "GET", path, nil)
		if strings.Contains(w.Body.String(), "private-sentinel") {
			t.Fatalf("private source leaked through %s", path)
		}
	}
}

func TestReadingHTTPFlowAndExportContract(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "personal", false)
	id := entryID(t, h.store, f)
	path := fmt.Sprintf("/story/%d/state", id)
	if w := h.request(2, "POST", path, url.Values{"field": {"saved"}, "value": {"true"}, "return_to": {"//evil.example/"}}); w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal("open redirect or failed save")
	}
	if w := h.request(2, "POST", path, url.Values{"field": {"read"}, "value": {"true"}}); w.Code != 303 {
		t.Fatal(w.Body)
	}
	w := h.request(2, "GET", "/api/v1/export", nil)
	var data Export
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Version != 1 || data.Username != "alice" || data.GroupOptin || len(data.Feeds) != 1 || len(data.Stories) != 1 || !data.Stories[0].Read || !data.Stories[0].Saved {
		t.Fatalf("export contract %+v", data)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "username", "group_optin", "feeds", "stories", "feed_groups"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("export missing %s", key)
		}
	}
	if w.Header().Get("Content-Type") != "application/json" || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("bad export headers")
	}
	for _, query := range []string{"/?page=-1", "/?page=99999999", "/?feed=bad", "/?view=unknown", "/?group=-1", "/?q=" + strings.Repeat("x", 301)} {
		if w := h.request(2, "GET", query, nil); w.Code != 400 {
			t.Errorf("invalid query accepted %s status %d", query, w.Code)
		}
	}
	if w := h.request(2, "GET", path, nil); w.Code != 405 {
		t.Fatalf("state route permits GET %d", w.Code)
	}
	if w := h.request(2, "POST", path, url.Values{"field": {"read"}, "value": {"invalid"}}); w.Code != 400 {
		t.Fatal("invalid state accepted")
	}
}

func TestLoginSignupLogoutFlow(t *testing.T) {
	h := webHarness(t)
	w := h.request(0, "GET", "/login", nil)
	if !strings.Contains(w.Body.String(), "Good tidings.") {
		t.Fatal("login not rendered")
	}
	w = h.request(0, "POST", "/login", url.Values{"username": {"alice"}, "password": {testPassword}})
	if w.Code != 303 {
		t.Fatalf("login failed: %d %s", w.Code, w.Body)
	}
	var raw string
	for _, c := range w.Result().Cookies() {
		if c.Name == "tidings" {
			raw = c.Value
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Fatal("weak session cookie")
			}
		}
	}
	if len(raw) != 64 {
		t.Fatal("no session cookie")
	}
	if w = h.request(0, "POST", "/login", url.Values{"username": {"alice"}, "password": {"wrong"}}); w.Code != 401 {
		t.Fatal("bad password accepted")
	}
	w = h.request(1, "POST", "/account/invite", nil)
	match := regexp.MustCompile(`/join\?code=([a-f0-9]{64})`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatalf("invite not rendered %s", w.Body)
	}
	w = h.request(0, "POST", "/join", url.Values{"code": {match[1]}, "username": {"new-person"}, "password": {testPassword}})
	if w.Code != 303 {
		t.Fatal(w.Body)
	}
	if w = h.request(0, "POST", "/join", url.Values{"code": {match[1]}, "username": {"another"}, "password": {testPassword}}); w.Code != 400 {
		t.Fatal("invitation reusable")
	}
	if w = h.request(2, "POST", "/logout", nil); w.Code != 303 {
		t.Fatal(w.Body)
	}
	if w = h.request(2, "GET", "/", nil); w.Code != 303 {
		t.Fatal("logout left session active")
	}
}

func TestHTTPFeedGroupsAndTags(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "ai", false)
	w := h.request(2, "POST", "/groups", url.Values{"name": {"My AI"}, "kind": {"tag"}, "tag": {"research"}})
	if w.Code != 303 {
		t.Fatal(w.Body)
	}
	loc := w.Header().Get("Location")
	if w = h.request(2, "GET", loc, nil); w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = h.request(2, "POST", fmt.Sprintf("/sources/%d/tags", f.ID), url.Values{"tags": {"AI, research"}})
	if w.Code != 303 {
		t.Fatal(w.Body)
	}
	groupID := strings.TrimPrefix(loc, "/groups/")
	w = h.request(2, "GET", "/?group="+groupID, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "A good story") {
		t.Fatalf("group not readable: %d %s", w.Code, w.Body)
	}
	if w = h.request(3, "GET", loc, nil); w.Code != 404 {
		t.Fatal("another person's group exposed")
	}
	if w = h.request(3, "GET", "/?group="+groupID, nil); w.Code != 404 {
		t.Fatal("another person's collection filter accessible")
	}
}

func TestTemplateEscapesUntrustedText(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "feed", false)
	if _, err := h.store.DB.Exec("UPDATE entries SET title=?,body=? WHERE feed_id=?", `<script>alert('x')</script>`, `<img src=x onerror=alert(1)>`, f.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", fmt.Sprintf("/story/%d", entryID(t, h.store, f))} {
		w := h.request(2, "GET", path, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "<script>alert") || strings.Contains(w.Body.String(), "<img src=x") {
			t.Fatalf("unsafe HTML %s", w.Body)
		}
	}
}
