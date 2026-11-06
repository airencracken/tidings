package reader

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/airencracken/comfylib/clientip"
	"github.com/airencracken/comfylib/token"
)

func TestReaderParagraphsAndPunctuation(t *testing.T) {
	input := `<p>Hello <b>world</b>. A <a href="x">link</a>!</p><p>Next<br>line.</p><ul><li>One</li><li>Two</li></ul><pre>if x &lt; 3:\n  print(x)</pre><script>bad()</script><style>hidden</style>`
	got := readerText(input, 64000)
	for _, want := range []string{"Hello world. A link!", "\n\nNext\nline.", "- One\n- Two", "if x < 3:\\n  print(x)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "bad()") || strings.Contains(got, "hidden") {
		t.Fatal("active content included")
	}
	for n := 0; n < 100; n++ {
		got := readerText(strings.Repeat("<p>é日</p>", 100), n)
		if !utf8.ValidString(got) || len([]rune(got)) > n {
			t.Fatal("unicode cap")
		}
	}
}
func TestSubscribePreservesMetadataAndExplicitEdits(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "metadata", false)
	u := User{ID: 2}
	if err := s.SetTags(ctx, u, f.ID, "weekend, custom", false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTopic(ctx, u, f.ID, "My topic", false); err != nil {
		t.Fatal(err)
	}
	f.Category = "Changed"
	f.Tags = []string{"replacement"}
	if err := s.Subscribe(ctx, 2, []Feed{f}, false); err != nil {
		t.Fatal(err)
	}
	feeds, err := s.Feeds(ctx, 2, false)
	if err != nil || feeds[0].Category != "My topic" || strings.Join(feeds[0].Tags, ",") != "custom,weekend" {
		t.Fatalf("duplicate reset metadata: %+v %v", feeds, err)
	}
	if err = s.SetTopic(ctx, User{ID: 3}, f.ID, "Other", false); err != sql.ErrNoRows {
		t.Fatal("cross account topic", err)
	}
	g, err := s.CreateGroup(ctx, u, "Before", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RenameGroup(ctx, User{ID: 3}, g, "Wrong"); err != sql.ErrNoRows {
		t.Fatal("cross-account rename", err)
	}
	if err = s.RenameGroup(ctx, u, g, "After"); err != nil {
		t.Fatal(err)
	}
	group, _ := s.Group(ctx, u, g)
	if group.Name != "After" {
		t.Fatal(group)
	}
	if err = s.RenameGroup(ctx, u, g, "bad\nname"); err == nil {
		t.Fatal("bad rename accepted")
	}
}
func TestRefreshQueueKeepsOtherAccounts(t *testing.T) {
	h := webHarness(t)
	first := testSource(t, h.store, 2, "first", false)
	second := testSource(t, h.store, 3, "second", false)
	h.store.DB.Exec("UPDATE feeds SET checked=0")
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	seen := map[string]int{}
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		seen[r.URL.String()]++
		mu.Unlock()
		if r.URL.String() == first.URL {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	h.app.StartRefresh(2, false)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	h.app.StartRefresh(3, false)
	h.app.StartRefresh(3, false)
	if !h.app.RefreshStatus(3).Queued || h.app.RefreshStatus(3).Running || !h.app.RefreshStatus(2).Running {
		t.Fatal("incorrect account queue status")
	}
	close(release)
	h.app.Wait()
	if seen[first.URL] != 1 || seen[second.URL] != 1 {
		t.Fatal("lost or duplicate account refresh", seen)
	}
	if h.app.RefreshStatus(3).Queued || h.app.RefreshStatus(2).Running {
		t.Fatal("queue did not drain")
	}
}
func TestMemberActionsAndRevokedInvitations(t *testing.T) {
	h := webHarness(t)
	ctx := context.Background()
	raw, err := h.store.Invite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if w := h.request(2, "POST", "/account/invitations/revoke", url.Values{"invitation": {token.Hash(raw)}}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := h.request(1, "POST", "/account/invitations/revoke", url.Values{"invitation": {token.Hash(raw)}}); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if err = h.store.Join(ctx, raw, "new-person", testPassword); err == nil {
		t.Fatal("revoked invitation used")
	}
	for _, tc := range []struct {
		actor, target             int64
		action, confirm, password string
		ok                        bool
	}{{2, 3, "suspend", "", testPassword, false}, {1, 1, "suspend", "", testPassword, false}, {1, 1, "remove", "host", testPassword, false}, {1, 2, "suspend", "", "wrong", false}, {1, 2, "suspend", "", testPassword, true}} {
		err = h.store.ManageMember(ctx, tc.actor, tc.target, tc.action, tc.confirm, tc.password)
		if (err == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if _, err = h.store.Session(ctx, h.tokens[2]); err != sql.ErrNoRows {
		t.Fatal("suspended session accepted", err)
	}
	if _, err = h.store.Login(ctx, "alice", testPassword); err == nil {
		t.Fatal("suspended login accepted")
	}
	if err = h.store.ManageMember(ctx, 1, 2, "resume", "", testPassword); err != nil {
		t.Fatal(err)
	}
	if _, err = h.store.Login(ctx, "alice", testPassword); err != nil {
		t.Fatal(err)
	}
	if err = h.store.ManageMember(ctx, 1, 2, "remove", "wrong", testPassword); err == nil {
		t.Fatal("removal confirmation ignored")
	}
	testSource(t, h.store, 2, "private", false)
	if err = h.store.ManageMember(ctx, 1, 2, "remove", "alice", testPassword); err != nil {
		t.Fatal(err)
	}
	var count int
	h.store.DB.QueryRow("SELECT count(*) FROM subscriptions WHERE user_id=2").Scan(&count)
	if count != 0 {
		t.Fatal("member data retained")
	}
	if w := h.request(3, "GET", "/account", nil); strings.Contains(w.Body.String(), "Instance backups") || strings.Contains(w.Body.String(), "Household members") {
		t.Fatal("owner controls visible to member")
	}
}
func TestTrustedProxyRateLimitKey(t *testing.T) {
	h := webHarness(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:2000"
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	if got := h.app.clientKey(r); got != "127.0.0.1" {
		t.Fatal("untrusted header believed", got)
	}
	trusted, err := clientip.ParseTrusted("127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	h.app.config.TrustedProxies = clientip.Resolver{Trusted: trusted}
	if got := h.app.clientKey(r); got != "8.8.8.8" {
		t.Fatal(got)
	}
}
func TestDiscoveryAndCatalogContracts(t *testing.T) {
	h := webHarness(t)
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(`<link rel="alternate" type="application/rss+xml" href="/feed" title="One"><link rel="ALTERNATE stylesheet" type="application/atom+xml" href="two"><link rel="alternate" type="application/rss+xml" href="/feed"><link rel="alternate" type="application/rss+xml" href="javascript:alert(1)">`))}, nil
	})}}
	found, err := h.app.Fetcher.Discover(context.Background(), "https://example.org/blog/")
	if err != nil || len(found) != 2 || found[0].URL != "https://example.org/feed" || found[1].URL != "https://example.org/blog/two" {
		t.Fatalf("%+v %v", found, err)
	}
	w := h.request(2, "POST", "/sources/discover", url.Values{"website": {"https://example.org/"}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Follow this feed") {
		t.Fatal(w.Code, w.Body)
	}
	w = h.request(2, "POST", "/sources/discover", url.Values{"website": {"https://example.org/"}, "scope": {"household"}})
	if w.Code != 403 {
		t.Fatal("household discovery allowed", w.Code)
	}
	w = h.request(2, "GET", "/sources?catalog_q=zzzzabsent", nil)
	if strings.Contains(w.Body.String(), "catalog-card") {
		t.Fatal("catalog search ignored")
	}
	for _, raw := range []string{"file:///etc/passwd", "https://user:pass@example.org/", "http://example.org:8000/"} {
		if _, err = h.app.Fetcher.Discover(context.Background(), raw); err == nil {
			t.Fatal("unsafe discovery URL", raw)
		}
	}
}
func TestSchemaV1Upgrade(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := sql.Open("sqlite", filepath.Join(dir, "reader.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP INDEX entries_import_owner;ALTER TABLE entries DROP COLUMN import_owner;ALTER TABLE entries DROP COLUMN original_key;ALTER TABLE users DROP COLUMN suspended;ALTER TABLE invites DROP COLUMN revoked;PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err = s.DB.QueryRow("PRAGMA user_version").Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
func multipartRestore(t *testing.T, h *harness, uid int64, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	m.WriteField("csrf", token.SessionCSRF(h.tokens[uid], "tidings:forms"))
	m.WriteField("confirm", "merge")
	f, err := m.CreateFormFile("reading_data", "tidings.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(data)
	m.Close()
	r := httptest.NewRequest("POST", "/account/restore", &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.AddCookie(&http.Cookie{Name: "tidings", Value: h.tokens[uid]})
	w := httptest.NewRecorder()
	h.app.ServeHTTP(w, r)
	return w
}
func TestPersonalRestoreRoundTripIsolationAndAtomicity(t *testing.T) {
	h := webHarness(t)
	ctx := context.Background()
	f := testSource(t, h.store, 2, "roundtrip", false)
	id := entryID(t, h.store, f)
	h.store.SetState(ctx, 2, id, "saved", true)
	g, err := h.store.CreateGroup(ctx, User{ID: 2}, "Research", "", false)
	if err != nil {
		t.Fatal(err)
	}
	h.store.SetGroupMembers(ctx, User{ID: 2}, g, []int64{f.ID})
	data, err := h.store.Export(ctx, User{ID: 2, Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	data.Stories[0].Key = "private-import"
	data.Stories[0].Title = "Only in my copy"
	data.Stories[0].Body = "<script>This remains escaped text</script>"
	b, _ := json.Marshal(data)
	w := multipartRestore(t, h, 3, b)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	got, err := h.store.Export(ctx, User{ID: 3})
	if err != nil || len(got.Stories) != 1 || got.Stories[0].Key != "private-import" || !got.Stories[0].Saved || len(got.Groups) != 1 {
		t.Fatalf("restore: %+v %v", got, err)
	}
	privateID := got.Stories[0].ID
	if _, err = h.store.Entry(ctx, 2, privateID); err != sql.ErrNoRows {
		t.Fatal("private import leaked", err)
	}
	if err = h.store.SetState(ctx, 2, privateID, "saved", true); err != sql.ErrNoRows {
		t.Fatal("private import state accessible", err)
	}
	if err = h.store.RestorePersonal(ctx, User{ID: 3}, data); err != nil {
		t.Fatal("idempotent import", err)
	}
	again, _ := h.store.Export(ctx, User{ID: 3})
	if len(again.Stories) != 1 || len(again.Groups) != 1 {
		t.Fatal("restore not idempotent")
	}
	// Existing publisher text wins even when an upload forges that story's title.
	data.Stories[0].Key = "one"
	var key string
	h.store.DB.QueryRow("SELECT key FROM entries WHERE id=?", id).Scan(&key)
	data.Stories[0].Key = key
	data.Stories[0].Title = "Forged publisher content"
	if err = h.store.RestorePersonal(ctx, User{ID: 3}, data); err != nil {
		t.Fatal(err)
	}
	original, err := h.store.Entry(ctx, 2, id)
	if err != nil || original.Title == "Forged publisher content" {
		t.Fatal("publisher text overwritten", err)
	}
	before, _ := h.store.Export(ctx, User{ID: 3})
	data.Feeds = append(data.Feeds, Feed{ID: 1000, URL: "https://example.net/new", Title: "New"})
	h.store.DB.Exec(`CREATE TRIGGER restore_fail BEFORE INSERT ON states WHEN NEW.user_id=3 BEGIN SELECT RAISE(ABORT,'injected restore failure'); END`)
	err = h.store.RestorePersonal(ctx, User{ID: 3}, data)
	if err == nil {
		t.Fatal("injected failure absent")
	}
	after, _ := h.store.Export(ctx, User{ID: 3})
	x, _ := json.Marshal(before)
	y, _ := json.Marshal(after)
	if string(x) != string(y) {
		t.Fatal("partial restore committed")
	}
	for _, bad := range []string{`{"version":99}`, `{"version":1,"unknown":true}`, `{"version":1} {}`, strings.Repeat("x", MaxRestoreBytes+1)} {
		if _, err = DecodeExport([]byte(bad)); err == nil {
			t.Fatal("invalid export accepted")
		}
	}
}
func TestNewRoutesAndCSRF(t *testing.T) {
	h := webHarness(t)
	for _, path := range []string{"/sources/discover", "/sources/1/topic", "/groups/1/rename", "/account/restore", "/account/invitations/revoke", "/account/members/2"} {
		w := h.request(2, "POST", path, url.Values{"csrf": {"invalid"}})
		if w.Code != 403 {
			t.Fatal(path, w.Code)
		}
		w = h.request(2, "POST", path, nil)
		if w.Code == 404 || w.Code == 405 {
			t.Fatal("missing route", path, w.Code)
		}
	}
	for _, path := range []string{"/account", "/sources?catalog_q=security"} {
		if w := h.request(1, "GET", path, nil); w.Code != 200 {
			t.Fatal(fmt.Sprint(w.Code), w.Body)
		}
	}
}
func FuzzDecodeExport(f *testing.F) {
	f.Add([]byte(`{"version":1,"feeds":[],"stories":[],"feed_groups":[]}`))
	f.Add([]byte(`{"version":1,"stories":[{"feed_id":1}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		data, err := DecodeExport(b)
		if err == nil && data.Version != 1 {
			t.Fatal("unsupported version")
		}
	})
}

func TestRestoredStateFollowsLaterPublisherFetch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "later", false)
	existing := entryID(t, s, f)
	s.SetState(ctx, 2, existing, "saved", true)
	data, err := s.Export(ctx, User{ID: 2})
	if err != nil {
		t.Fatal(err)
	}
	data.Stories[0].Key = "later-story"
	data.Stories[0].URL = "https://example.org/later-story"
	data.Stories[0].Body = "Private imported text"
	if err = s.RestorePersonal(ctx, User{ID: 3}, data); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.Export(ctx, User{ID: 3})
	privateID := restored.Stories[0].ID
	canonical := data.Stories[0]
	canonical.Body = "Publisher text"
	if err = s.ApplyFetch(ctx, f, FetchResult{Feed: ParsedFeed{Title: f.Title, Entries: []Entry{canonical}}}); err != nil {
		t.Fatal(err)
	}
	out, err := s.Export(ctx, User{ID: 3})
	if err != nil || len(out.Stories) != 1 || out.Stories[0].Body != "Publisher text" || !out.Stories[0].Saved || out.Stories[0].ID == privateID {
		t.Fatalf("restored markers not reconciled: %+v %v", out, err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM entries WHERE import_owner IS NOT NULL").Scan(&n)
	if n != 0 {
		t.Fatal("private duplicate retained")
	}
}
func TestAtomExplicitPlainTextPreservesAngleBrackets(t *testing.T) {
	body := `<feed xmlns="http://www.w3.org/2005/Atom"><title>Test</title><entry><title>Code</title><link href="https://example.org/story"/><content type="text">Use &lt;typename&gt; in code.</content></entry></feed>`
	parsed, err := ParseFeed([]byte(body), "https://example.org/feed", fixtureTime)
	if err != nil || len(parsed.Entries) != 1 || parsed.Entries[0].Body != "Use <typename> in code." {
		t.Fatal(parsed, err)
	}
}
func TestSchemaV2Constraints(t *testing.T) {
	s := testStore(t)
	for _, query := range []string{"UPDATE users SET suspended=2 WHERE id=2", "INSERT INTO invites(token,expires,revoked) VALUES('bad',1,2)", "INSERT INTO entries(feed_id,key,title,url,body,published,import_owner) VALUES(999,'k','t','https://example.org/','',1,999)"} {
		if _, err := s.DB.Exec(query); err == nil {
			t.Fatal("schema accepted invalid value", query)
		}
	}
}
