// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func hasEntry(list []Entry, id int64) bool {
	for _, e := range list {
		if e.ID == id {
			return true
		}
	}
	return false
}

func TestSkipLeavesShelvesWithoutMarkingReadOrSaved(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "skipping", false)
	id := entryID(t, s, f)
	g, err := s.CreateGroup(ctx, User{ID: 2}, "Interest", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetGroupMembers(ctx, User{ID: 2}, g, []int64{f.ID}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetState(ctx, 2, id, "skipped", true); err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{"unread", "", "all"} {
		if hasEntry(entries(t, s, 2, view), id) {
			t.Fatalf("skipped story still on %q", view)
		}
	}
	grouped, _, err := s.Entries(ctx, 2, Filter{View: "unread", GroupID: g})
	if err != nil || hasEntry(grouped, id) {
		t.Fatal("skipped story still on its group shelf", err)
	}
	skipped := entries(t, s, 2, "skipped")
	if !hasEntry(skipped, id) {
		t.Fatal("skipped view does not list the story")
	}
	e, err := s.Entry(ctx, 2, id)
	if err != nil || !e.Skipped || e.Read || e.Saved {
		t.Fatalf("skip changed other markers: %+v %v", e, err)
	}
	// Another member's shelves are unaffected.
	testSource(t, s, 3, "skipping", false)
	if !hasEntry(entries(t, s, 3, "unread"), id) || hasEntry(entries(t, s, 3, "skipped"), id) {
		t.Fatal("skip leaked to another member")
	}
	// Undo brings it back to Unread.
	if err = s.SetState(ctx, 2, id, "skipped", false); err != nil {
		t.Fatal(err)
	}
	if !hasEntry(entries(t, s, 2, "unread"), id) || hasEntry(entries(t, s, 2, "skipped"), id) {
		t.Fatal("undo skip did not restore the story")
	}
}

func TestSavingASkippedStoryUnskipsIt(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id := entryID(t, s, testSource(t, s, 2, "unskip", false))
	if err := s.SetState(ctx, 2, id, "skipped", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(ctx, 2, id, "saved", true); err != nil {
		t.Fatal(err)
	}
	e, err := s.Entry(ctx, 2, id)
	if err != nil || !e.Saved || e.Skipped {
		t.Fatalf("saving did not unskip: %+v %v", e, err)
	}
	// Saving a story with no earlier marker also works.
	other := testSource(t, s, 2, "fresh-save", false)
	fresh := entryID(t, s, other)
	if err = s.SetState(ctx, 2, fresh, "saved", true); err != nil {
		t.Fatal(err)
	}
	if e, err = s.Entry(ctx, 2, fresh); err != nil || !e.Saved || e.Skipped {
		t.Fatalf("%+v %v", e, err)
	}
	// Removing from saved leaves the skip marker alone.
	if err = s.SetState(ctx, 2, id, "saved", false); err != nil {
		t.Fatal(err)
	}
	if e, err = s.Entry(ctx, 2, id); err != nil || e.Saved || e.Skipped {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestSkipStateRejectsBadFieldsValuesAndStrangers(t *testing.T) {
	h := webHarness(t)
	id := entryID(t, h.store, testSource(t, h.store, 2, "adversarial", false))
	path := fmt.Sprintf("/story/%d/state", id)
	for _, values := range []url.Values{
		{"field": {"skip"}, "value": {"true"}},
		{"field": {"Skipped"}, "value": {"true"}},
		{"field": {"skipped "}, "value": {"true"}},
		{"field": {"skipped"}, "value": {"1"}},
		{"field": {"skipped"}, "value": {"TRUE"}},
		{"field": {"skipped"}, "value": {""}},
		{"field": {"skipped"}},
		{"value": {"true"}},
		{"field": {"skipped", "read"}, "value": {"true"}},
		{"field": {"skipped"}, "value": {"true", "false"}},
		{"field": {"skipped=1,read"}, "value": {"true"}},
		{"field": {"read) VALUES(1,1,1);--"}, "value": {"true"}},
		{"field": {"user_id"}, "value": {"true"}},
	} {
		if w := h.request(2, "POST", path, values); w.Code != 400 {
			t.Errorf("%v accepted: %d", values, w.Code)
		}
	}
	if err := h.store.SetState(t.Context(), 2, id, "skipped,read", true); err == nil {
		t.Fatal("store accepted a compound field")
	}
	var skipped int
	if err := h.store.DB.QueryRow("SELECT coalesce(sum(skipped),0) FROM states").Scan(&skipped); err != nil || skipped != 0 {
		t.Fatal("rejected request changed state", skipped, err)
	}
	// Another member cannot skip a story they cannot see; unknown IDs are 404.
	if w := h.request(3, "POST", path, url.Values{"field": {"skipped"}, "value": {"true"}}); w.Code != 404 {
		t.Fatal("stranger skipped a story", w.Code)
	}
	if w := h.request(2, "POST", "/story/99999999/state", url.Values{"field": {"skipped"}, "value": {"true"}}); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := h.request(2, "POST", path, url.Values{"field": {"skipped"}, "value": {"true"}, "return_to": {"//evil.example/"}}); w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal("skip redirect", w.Code, w.Header().Get("Location"))
	}
}

func TestSkippedViewRouteAndLabels(t *testing.T) {
	h := webHarness(t)
	id := entryID(t, h.store, testSource(t, h.store, 2, "routes", false))
	empty := h.request(2, "GET", "/?view=skipped", nil)
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), "Nothing skipped.") || !strings.Contains(empty.Body.String(), "<title>Skipped · Tidings</title>") {
		t.Fatal(empty.Code, empty.Body.String())
	}
	home := h.request(2, "GET", "/", nil).Body.String()
	for _, want := range []string{`href="/?view=skipped"`, ">Skipped stories</a>", ">Saved for later</a>", ">Save for later</button>", ">Skip</button>"} {
		if !strings.Contains(home, want) {
			t.Errorf("reading table is missing %q", want)
		}
	}
	if w := h.request(2, "POST", fmt.Sprintf("/story/%d/state", id), url.Values{"field": {"skipped"}, "value": {"true"}}); w.Code != 303 {
		t.Fatal(w.Code)
	}
	page := h.request(2, "GET", "/?view=skipped", nil).Body.String()
	if !strings.Contains(page, ">Undo skip</button>") || !strings.Contains(page, fmt.Sprintf(`href="/story/%d"`, id)) {
		t.Fatal("skipped view cannot undo")
	}
	article := h.request(2, "GET", fmt.Sprintf("/story/%d", id), nil).Body.String()
	for _, want := range []string{">Save for later</button>", ">Undo skip</button>", "<button>Mark read and return</button>"} {
		if !strings.Contains(article, want) {
			t.Errorf("article is missing %q", want)
		}
	}
	if strings.Contains(article, ">Mark read</button>") || strings.Contains(article, ">Mark unread</button>") {
		t.Error("article header still carries a separate read toggle")
	}
	if saved := h.request(2, "GET", "/?view=saved", nil).Body.String(); !strings.Contains(saved, "<title>Saved for later · Tidings</title>") {
		t.Fatal("saved shelf title")
	}
	if w := h.request(2, "GET", "/?view=Skipped", nil); w.Code != 400 {
		t.Fatal("unknown view accepted", w.Code)
	}
}

func TestSkipMigrationAddsColumnAndKeepsState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reader")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO users(id,username,password) VALUES(1,'reader','x');
	INSERT INTO feeds(id,url,title) VALUES(1,'https://example.org/f','F');
	INSERT INTO entries(id,feed_id,key,title,url,body,published) VALUES(1,1,'k','T','https://example.org/s','B',1);
	INSERT INTO states(user_id,entry_id,read,saved) VALUES(1,1,1,1);
	ALTER TABLE states DROP COLUMN skipped;ALTER TABLE subscription_tags DROP COLUMN position;ALTER TABLE household_tags DROP COLUMN position; PRAGMA user_version=5`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var version, read, saved, skipped int
	if err = upgraded.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 7 {
		t.Fatal(version, err)
	}
	if err = upgraded.DB.QueryRow("SELECT read,saved,skipped FROM states WHERE user_id=1 AND entry_id=1").Scan(&read, &saved, &skipped); err != nil || read != 1 || saved != 1 || skipped != 0 {
		t.Fatal(read, saved, skipped, err)
	}
	if _, err = upgraded.DB.Exec("UPDATE states SET skipped=2"); err == nil {
		t.Fatal("skipped accepts a non-boolean value")
	}
	// A newer database refuses to open in this version.
	if _, err = upgraded.DB.Exec("PRAGMA user_version=8"); err != nil {
		t.Fatal(err)
	}
	upgraded.Close()
	if newer, err := OpenStore(dir); err == nil {
		newer.Close()
		t.Fatal("opened a newer schema")
	}
}

func TestSkipMigrationIsAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reader")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A conflicting column makes the migration fail and roll back.
	if _, err = s.DB.Exec("ALTER TABLE states DROP COLUMN skipped;ALTER TABLE subscription_tags DROP COLUMN position;ALTER TABLE household_tags DROP COLUMN position; ALTER TABLE states ADD COLUMN skipped TEXT; PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if broken, err := OpenStore(dir); err == nil {
		broken.Close()
		t.Fatal("conflicting column accepted")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "reader.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("partial migration published", version, err)
	}
}

func TestSkipSurvivesExportAndRestore(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "roundtrip", false)
	id := entryID(t, s, f)
	if err := s.SetState(ctx, 2, id, "skipped", true); err != nil {
		t.Fatal(err)
	}
	data, err := s.Export(ctx, User{ID: 2})
	if err != nil || len(data.Stories) != 1 || !data.Stories[0].Skipped {
		t.Fatalf("export lost skip: %+v %v", data.Stories, err)
	}
	encoded, err := json.Marshal(data)
	if err != nil || !strings.Contains(string(encoded), `"skipped":true`) {
		t.Fatal("skip missing from JSON", err)
	}
	decoded, err := DecodeExport(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RestorePersonal(ctx, User{ID: 3}, decoded); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Export(ctx, User{ID: 3})
	if err != nil || len(restored.Stories) != 1 || !restored.Stories[0].Skipped || restored.Stories[0].Saved || restored.Stories[0].Read {
		t.Fatalf("restore lost skip: %+v %v", restored.Stories, err)
	}
	// Exports without skips omit the field, so earlier versions can import them,
	// and exports from earlier versions (no field) import as not skipped.
	if err = s.SetState(ctx, 2, id, "skipped", false); err != nil {
		t.Fatal(err)
	}
	if err = s.SetState(ctx, 2, id, "read", true); err != nil {
		t.Fatal(err)
	}
	plain, _ := s.Export(ctx, User{ID: 2})
	encoded, _ = json.Marshal(plain)
	if strings.Contains(string(encoded), "skipped") {
		t.Fatal("unskipped export mentions skipped")
	}
	old, err := DecodeExport(encoded)
	if err != nil || old.Stories[0].Skipped {
		t.Fatal(err)
	}
	// A forged export marking a story both saved and skipped restores as saved.
	both := decoded
	both.Stories = append([]Entry(nil), decoded.Stories...)
	both.Stories[0].Saved, both.Stories[0].Skipped = true, true
	if err = s.RestorePersonal(ctx, User{ID: 1}, both); err != nil {
		t.Fatal(err)
	}
	host, _ := s.Export(ctx, User{ID: 1})
	if len(host.Stories) != 1 || !host.Stories[0].Saved || host.Stories[0].Skipped {
		t.Fatalf("%+v", host.Stories)
	}
	// Strict decoding still rejects a mistyped field.
	bad := strings.Replace(string(encoded), `"read":true`, `"read":true,"skiped":true`, 1)
	if _, err = DecodeExport([]byte(bad)); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err = DecodeExport([]byte(strings.Replace(string(encoded), `"read":true`, `"read":true,"skipped":"yes"`, 1))); err == nil {
		t.Fatal("non-boolean skipped accepted")
	}
}

func TestSkipFollowsTheStoryLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	f := testSource(t, s, 2, "lifecycle", false)
	id := entryID(t, s, f)
	if err := s.SetState(ctx, 2, id, "skipped", true); err != nil {
		t.Fatal(err)
	}
	// Like read markers, skips are removed with their story; there is no
	// separate retention for skipped stories.
	if _, err := s.DB.Exec("DELETE FROM entries WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB.QueryRow("SELECT count(*) FROM states WHERE entry_id=?", id).Scan(&n); err != nil || n != 0 {
		t.Fatal("skip outlived its story", n, err)
	}
	// Marking a page read does not clear skips elsewhere.
	g := testSource(t, s, 2, "lifecycle-two", false)
	other := entryID(t, s, g)
	if err := s.SetState(ctx, 2, other, "skipped", true); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPage(ctx, 2, []int64{other}); err != nil {
		t.Fatal(err)
	}
	e, err := s.Entry(ctx, 2, other)
	if err != nil || !e.Read || !e.Skipped {
		t.Fatalf("%+v %v", e, err)
	}
}
