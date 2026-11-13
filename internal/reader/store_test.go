package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airencracken/comfylib/token"
	"golang.org/x/crypto/bcrypt"
)

const testPassword = "quiet mornings and books"

func testStore(t testing.TB) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"host", "alice", "bob"} {
		_, err = s.DB.Exec("INSERT INTO users(id,username,password,owner) VALUES(?,?,?,?)", i+1, name, hash, i == 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}
func testSource(t *testing.T, s *Store, uid int64, name string, group bool) Feed {
	t.Helper()
	ctx := context.Background()
	f := Feed{URL: "https://example.org/" + name, Title: name, Category: "Tech"}
	if err := s.Subscribe(ctx, uid, []Feed{f}, group); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT id FROM feeds WHERE url=?", f.URL).Scan(&f.ID); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFeed([]byte(rssFixture), f.URL, fixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Title = name
	if err = s.ApplyFetch(ctx, f, FetchResult{Feed: parsed, ETag: "tag"}); err != nil {
		t.Fatal(err)
	}
	return f
}
func entryID(t *testing.T, s *Store, f Feed) int64 {
	t.Helper()
	var id int64
	if err := s.DB.QueryRow("SELECT id FROM entries WHERE feed_id=?", f.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func entries(t *testing.T, s *Store, uid int64, view string) []Entry {
	t.Helper()
	rows, _, err := s.Entries(context.Background(), uid, Filter{View: view})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestHouseholdOptInAndIsolation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	personal := testSource(t, s, 1, "host-only", false)
	other := testSource(t, s, 2, "alice-only", false)
	group := testSource(t, s, 1, "household", true)
	if got := entries(t, s, 2, "all"); len(got) != 1 || got[0].FeedID != other.ID {
		t.Fatalf("household was followed by default or private sources leaked: %+v", got)
	}
	if err := s.Subscribe(ctx, 2, []Feed{{URL: "https://example.org/forged"}}, true); err == nil {
		t.Fatal("member changed group sources")
	}
	if _, err := s.DB.Exec("UPDATE users SET group_optin=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, s, 2, "all"); len(got) != 2 {
		t.Fatalf("opt-in did not include group: %+v", got)
	}
	for _, id := range []int64{entryID(t, s, personal)} {
		if _, err := s.Entry(ctx, 2, id); err != sql.ErrNoRows {
			t.Fatalf("private article accessible %v", err)
		}
		if err := s.SetState(ctx, 2, id, "saved", true); err != sql.ErrNoRows {
			t.Fatalf("private article state writable %v", err)
		}
	}
	id := entryID(t, s, group)
	if err := s.SetState(ctx, 2, id, "read", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(ctx, 2, id, "saved", true); err != nil {
		t.Fatal(err)
	}
	if len(entries(t, s, 2, "saved")) != 1 || len(entries(t, s, 1, "saved")) != 0 || len(entries(t, s, 3, "saved")) != 0 {
		t.Fatal("reading state crossed accounts")
	}
	if _, err := s.DB.Exec("UPDATE users SET group_optin=0 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if len(entries(t, s, 2, "all")) != 1 || len(entries(t, s, 2, "saved")) != 1 {
		t.Fatal("opt-out lost saved data or retained group visibility")
	}
	if _, err := s.Entry(ctx, 3, id); err != sql.ErrNoRows {
		t.Fatalf("group opt-out allowed detail access %v", err)
	}
	data, err := s.Export(ctx, User{ID: 2, Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(data)
	if strings.Contains(string(b), "host-only") || strings.Contains(string(b), "password") || strings.Contains(string(b), "token") {
		t.Fatalf("export leaked private data: %s", b)
	}
}

func TestStatePreservesOtherField(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "feed", false)
	id := entryID(t, s, f)
	for _, op := range []struct {
		field string
		value bool
	}{{"saved", true}, {"read", true}, {"read", false}} {
		if err := s.SetState(ctx, 2, id, op.field, op.value); err != nil {
			t.Fatal(err)
		}
	}
	e, err := s.Entry(ctx, 2, id)
	if err != nil || e.Read || !e.Saved {
		t.Fatalf("state overwritten %+v %v", e, err)
	}
	if err = s.SetState(ctx, 2, id, "owner", true); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err = s.Unsubscribe(ctx, 2, f.ID, false); err != nil {
		t.Fatal(err)
	}
	if len(entries(t, s, 2, "saved")) != 1 {
		t.Fatal("unsubscribe removed saved story")
	}
	if err = s.MarkPage(ctx, 2, []int64{id}); err != nil {
		t.Fatalf("could not mark own saved story after unfollowing: %v", err)
	}
}

func TestRefreshIdempotentAndDatesStable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 1, "feed", false)
	id := entryID(t, s, f)
	if err := s.SetState(ctx, 1, id, "saved", true); err != nil {
		t.Fatal(err)
	}
	e, err := s.Entry(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	original := e.Published
	e.Published = fixtureTime.Add(time.Hour).Unix()
	e.Title = "An updated title"
	if err = s.ApplyFetch(ctx, f, FetchResult{Feed: ParsedFeed{Title: "Updated feed", Entries: []Entry{e}}, ETag: "new"}); err != nil {
		t.Fatal(err)
	}
	got := entries(t, s, 1, "all")
	if len(got) != 1 || !got[0].Saved || got[0].Published != original || got[0].Title != e.Title {
		t.Fatalf("refresh lost state or duplicated %+v", got)
	}
	if err = s.ApplyFetch(ctx, f, FetchResult{Unchanged: true}); err != nil {
		t.Fatal(err)
	}
	var tag, title string
	if err = s.DB.QueryRow("SELECT etag,title FROM feeds WHERE id=?", f.ID).Scan(&tag, &title); err != nil || tag != "new" || title != "Updated feed" {
		t.Fatalf("304 erased metadata %q %q %v", tag, title, err)
	}
}

func TestSubscriptionAtomicity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, err := s.DB.Exec(`CREATE TRIGGER fail_subscription BEFORE INSERT ON subscriptions WHEN NEW.feed_id=(SELECT id FROM feeds WHERE url='https://example.org/b') BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Subscribe(ctx, 2, []Feed{{URL: "https://example.org/a"}, {URL: "https://example.org/b"}}, false)
	if err == nil {
		t.Fatal("injected failure was ignored")
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM feeds").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial import persisted: %d %v", count, err)
	}
	if err = s.Subscribe(ctx, 2, []Feed{{URL: "https://example.org/valid"}, {URL: "file:///etc/passwd"}}, false); err == nil {
		t.Fatal("invalid import accepted")
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM feeds").Scan(&count); err != nil || count != 0 {
		t.Fatal("validation wrote partial import")
	}
}

func TestRefreshAtomicity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 1, "feed", false)
	_, err := s.DB.Exec(`CREATE TRIGGER fail_entry BEFORE INSERT ON entries WHEN NEW.key='bad' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err != nil {
		t.Fatal(err)
	}
	r := FetchResult{Feed: ParsedFeed{Title: "Corrupted", Entries: []Entry{{Key: "good", Title: "Good", URL: "https://example.org/g", Published: 1}, {Key: "bad", Title: "Bad", URL: "https://example.org/b", Published: 1}}}, ETag: "wrong"}
	if err = s.ApplyFetch(ctx, f, r); err == nil {
		t.Fatal("failure ignored")
	}
	var count int
	var title, tag string
	if err = s.DB.QueryRow("SELECT count(*) FROM entries").Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial refresh: %d %v", count, err)
	}
	if err = s.DB.QueryRow("SELECT title,etag FROM feeds WHERE id=?", f.ID).Scan(&title, &tag); err != nil || title != "feed" || tag != "tag" {
		t.Fatal("partial metadata committed")
	}
}

func TestMarkPageAtomicity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "feed", false)
	other := testSource(t, s, 1, "private", false)
	id := entryID(t, s, f)
	err := s.MarkPage(ctx, 2, []int64{id, entryID(t, s, other)})
	if err != sql.ErrNoRows {
		t.Fatalf("unauthorized batch accepted %v", err)
	}
	e, err := s.Entry(ctx, 2, id)
	if err != nil || e.Read {
		t.Fatalf("partial batch persisted %+v %v", e, err)
	}
	if err = s.MarkPage(ctx, 2, make([]int64, 31)); err == nil {
		t.Fatal("unbounded batch accepted")
	}
}

func TestSchemaValidationAndReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`INSERT INTO users(username,password,owner) VALUES('a','x',1)`, `INSERT INTO users(username,password,group_optin) VALUES('test','x',2)`, `INSERT INTO subscriptions(user_id,feed_id) VALUES(99,99)`, `INSERT INTO states(user_id,entry_id,read) VALUES(99,99,3)`} {
		if _, err = s.DB.Exec(query); err == nil {
			t.Errorf("schema accepted %s", query)
		}
	}
	if err = s.CreateOwner(context.Background(), "host", testPassword); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM users").Scan(&n); err != nil || n != 1 {
		t.Fatal("data did not survive reopen")
	}
	if _, err = s.DB.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if next, e := OpenStore(dir); e == nil {
		next.Close()
		t.Fatal("future schema accepted")
	}
	info, err := os.Stat(filepath.Join(dir, "reader.db"))
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("database permissions %v %v", info, err)
	}
}

func TestSearchPaginationAndTopics(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "feed", false)
	var es []Entry
	for i := range 65 {
		es = append(es, Entry{Key: fmt.Sprint(i), Title: fmt.Sprintf("Story %03d", i), URL: "https://example.org/" + fmt.Sprint(i), Body: "100% useful_knowledge", Published: int64(i + 1)})
	}
	if err := s.ApplyFetch(ctx, f, FetchResult{Feed: ParsedFeed{Title: "feed", Entries: es}}); err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for p := range 3 {
		got, more, err := s.Entries(ctx, 2, Filter{View: "all", Page: p})
		if err != nil {
			t.Fatal(err)
		}
		if (p < 2) != more {
			t.Fatalf("bad pagination page %d more %v", p, more)
		}
		for _, e := range got {
			if seen[e.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[e.ID] = true
		}
	}
	if len(seen) != 66 {
		t.Fatalf("lost rows %d", len(seen))
	}
	for query, want := range map[string]int{"100%": 30, "_knowledge": 30, "' OR 1=1 --": 0, "absent": 0} {
		got, _, err := s.Entries(ctx, 2, Filter{View: "all", Query: query})
		if err != nil || len(got) != want {
			t.Errorf("search %q got %d want %d err %v", query, len(got), want, err)
		}
	}
	if got, _, err := s.Entries(ctx, 2, Filter{View: "all", Tag: "ai"}); err != nil || len(got) != 0 {
		t.Fatal("tag filter ignored")
	}
}

func TestStateProperties(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "shared", false)
	if err := s.Subscribe(ctx, 3, []Feed{{URL: f.URL}}, false); err != nil {
		t.Fatal(err)
	}
	id := entryID(t, s, f)
	type state struct{ read, saved bool }
	model := map[int64]state{}
	rng := rand.New(rand.NewPCG(14, 62))
	for range 200 {
		uid := int64(2 + rng.IntN(2))
		field := "read"
		if rng.IntN(2) == 1 {
			field = "saved"
		}
		value := rng.IntN(2) == 1
		if err := s.SetState(ctx, uid, id, field, value); err != nil {
			t.Fatal(err)
		}
		want := model[uid]
		if field == "read" {
			want.read = value
		} else {
			want.saved = value
		}
		model[uid] = want
		for _, user := range []int64{2, 3} {
			got, err := s.Entry(ctx, user, id)
			expected := model[user]
			if err != nil || got.Read != expected.read || got.Saved != expected.saved {
				t.Fatalf("state property failed: user %d %+v want %+v err %v", user, got, expected, err)
			}
		}
	}
}

func TestAuthAndSessionExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	raw, err := s.Login(ctx, "alice", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Session(ctx, raw)
	if err != nil || u.ID != 2 || u.GroupOptin {
		t.Fatalf("session %+v %v", u, err)
	}
	if _, err = s.Login(ctx, "alice", "wrong"); err != ErrCredentials {
		t.Fatal("bad password accepted")
	}
	if _, err = s.Login(ctx, "nobody", testPassword); err != ErrCredentials {
		t.Fatal("unknown account accepted")
	}
	var stored string
	if err = s.DB.QueryRow("SELECT token FROM sessions WHERE user_id=2").Scan(&stored); err != nil || stored == raw || stored != token.Hash(raw) {
		t.Fatal("raw session token stored")
	}
	if _, err = s.DB.Exec("UPDATE sessions SET expires=?", time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, raw); err != sql.ErrNoRows {
		t.Fatal("expired session accepted")
	}
	raw, err = s.Login(ctx, "alice", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetPassword(ctx, "alice", "a different quiet morning"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, raw); err != sql.ErrNoRows {
		t.Fatal("password change left session valid")
	}
	for _, bad := range []struct{ u, p string }{{"a", testPassword}, {"has space", testPassword}, {"normal", "short"}, {"normal", strings.Repeat("x", 73)}} {
		if ValidateCredentials(bad.u, bad.p) == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
}

func TestInvitationAtomicityAndRace(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	raw, err := s.Invite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Join(ctx, raw, "alice", testPassword); err == nil {
		t.Fatal("duplicate username accepted")
	}
	var used bool
	if err = s.DB.QueryRow("SELECT used FROM invites WHERE token=?", token.Hash(raw)).Scan(&used); err != nil || used {
		t.Fatal("failed registration consumed invitation")
	}
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Join(ctx, raw, fmt.Sprintf("person%d", i), testPassword) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("one invitation admitted %d people", success)
	}
	var opted int
	if err = s.DB.QueryRow("SELECT count(*) FROM users WHERE group_optin=1").Scan(&opted); err != nil || opted != 0 {
		t.Fatal("new users automatically followed group")
	}
	expired, err := s.Invite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE invites SET expires=0 WHERE token=?", token.Hash(expired)); err != nil {
		t.Fatal(err)
	}
	if err = s.Join(ctx, expired, "latecomer", testPassword); err == nil {
		t.Fatal("expired invite accepted")
	}
}
