// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// legacyStore returns a schema-6 database: topics in the category columns,
// tags without positions, as Tidings 0.8.0 and the Skip release stored them.
func legacyStore(t *testing.T, setup string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "reader")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`ALTER TABLE subscription_tags DROP COLUMN position; ALTER TABLE household_tags DROP COLUMN position;
	INSERT INTO users(id,username,password,owner,group_optin) VALUES(1,'host','x',1,1),(2,'reader','x',0,1);` + setup + `; PRAGMA user_version=6`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func feedTags(t *testing.T, s *Store, uid int64, household bool) map[string][]string {
	t.Helper()
	feeds, err := s.Feeds(context.Background(), uid, household)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, f := range feeds {
		out[f.Title] = f.Tags
		if len(f.Tags) > 0 && f.Category != f.Tags[0] {
			t.Errorf("%s: category %q is not the first tag", f.Title, f.Category)
		}
	}
	return out
}

func groupFeeds(t *testing.T, s *Store, u User, group int64) []int64 {
	t.Helper()
	rows, _, err := s.Entries(context.Background(), u.ID, Filter{View: "all", GroupID: group})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, e := range rows {
		if !slices.Contains(ids, e.FeedID) {
			ids = append(ids, e.FeedID)
		}
	}
	slices.Sort(ids)
	return ids
}

const legacySources = `
INSERT INTO feeds(id,url,title) VALUES(1,'https://example.org/a','Research'),(2,'https://example.org/b','Bare'),(3,'https://example.org/c','Duplicate'),
 (4,'https://example.org/d','Blank'),(5,'https://example.org/e','Shared'),(6,'https://example.org/f','Full'),(7,'https://example.org/g','Accents'),(8,'https://example.org/h','Comma');
INSERT INTO subscriptions(user_id,feed_id,category) VALUES(2,1,'Research'),(2,2,'AI'),(2,3,'Tech'),(2,4,'   '),(2,6,'Overflow'),(2,7,'Économie'),(2,8,'Home,  Garden');
INSERT INTO subscription_tags(user_id,feed_id,tag) VALUES(2,1,'zeta'),(2,1,'alpha'),(2,3,'tech'),(2,3,'news'),(2,4,'b'),(2,4,'a'),
 (2,6,'t01'),(2,6,'t02'),(2,6,'t03'),(2,6,'t04'),(2,6,'t05'),(2,6,'t06'),(2,6,'t07'),(2,6,'t08'),(2,6,'t09'),(2,6,'t10'),(2,6,'t11'),(2,6,'t12');
INSERT INTO group_feeds(feed_id,category) VALUES(5,'Infosec');
INSERT INTO household_tags(feed_id,tag) VALUES(5,'security');
INSERT INTO entries(feed_id,key,title,url,body,published) VALUES(1,'a','A','https://example.org/a/1','x',1),(2,'b','B','https://example.org/b/1','x',2),(3,'c','C','https://example.org/c/1','x',3),(5,'e','E','https://example.org/e/1','x',4);
INSERT INTO feed_groups(id,user_id,name,tag) VALUES(1,2,'Alpha','alpha'),(2,2,'News','news'),(3,NULL,'Security','security');
INSERT INTO catalog_categories(name) VALUES('Science'),('AI')`

func TestTopicMigrationMakesTheTopicTheFirstTag(t *testing.T) {
	dir := legacyStore(t, legacySources)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err = s.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 7 {
		t.Fatal(version, err)
	}
	full := []string{"overflow"}
	for i := 1; i <= 12; i++ {
		full = append(full, fmt.Sprintf("t%02d", i))
	}
	want := map[string][]string{
		"Research":  {"research", "alpha", "zeta"}, // topic first, existing order kept
		"Bare":      {"ai"},                        // no tags before
		"Duplicate": {"tech", "news"},              // topic already a tag: moved first, not repeated
		"Blank":     {"a", "b"},                    // blank topic adds nothing
		"Full":      full,                          // a topic never drops a chosen tag
		"Accents":   {"économie"},
		"Comma":     {"home garden"},
	}
	got := feedTags(t, s, 2, false)
	for title, tags := range want {
		if !slices.Equal(got[title], tags) {
			t.Errorf("%s: got %v, want %v", title, got[title], tags)
		}
	}
	if household := feedTags(t, s, 1, true); !slices.Equal(household["Shared"], []string{"infosec", "security"}) {
		t.Errorf("household: %v", household["Shared"])
	}
	// Tag-based groups keep every source they matched before.
	if ids := groupFeeds(t, s, User{ID: 2}, 1); !slices.Equal(ids, []int64{1}) {
		t.Errorf("alpha group: %v", ids)
	}
	if ids := groupFeeds(t, s, User{ID: 2}, 2); !slices.Equal(ids, []int64{3}) {
		t.Errorf("news group: %v", ids)
	}
	if ids := groupFeeds(t, s, User{ID: 2, GroupOptin: true}, 3); !slices.Equal(ids, []int64{5}) {
		t.Errorf("household security group: %v", ids)
	}
	// Suggested categories are an instance setting matched against the
	// catalog, so the same suggestions remain enabled.
	enabled, err := s.CatalogCategories(t.Context())
	if err != nil || len(enabled) != 2 || !enabled["Science"] || !enabled["AI"] {
		t.Fatal(enabled, err)
	}
	// The old topic is now an ordinary tag filter.
	rows, _, err := s.Entries(t.Context(), 2, Filter{View: "all", Tag: "research"})
	if err != nil || len(rows) != 1 || rows[0].FeedID != 1 || rows[0].Category != "research" {
		t.Fatalf("%+v %v", rows, err)
	}
	// Reopening does not migrate twice.
	s.Close()
	again, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if tags := feedTags(t, again, 2, false); !slices.Equal(tags["Research"], want["Research"]) {
		t.Fatal(tags["Research"])
	}
}

func TestTopicMigrationIsAtomic(t *testing.T) {
	// A pre-existing position column makes the migration fail part-way.
	dir := legacyStore(t, legacySources+";ALTER TABLE household_tags ADD COLUMN position TEXT")
	if s, err := OpenStore(dir); err == nil {
		s.Close()
		t.Fatal("conflicting column accepted")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "reader.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, positions, tags int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		t.Fatal("version advanced", version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM pragma_table_info('subscription_tags') WHERE name='position'").Scan(&positions); err != nil || positions != 0 {
		t.Fatal("partial migration published", positions, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM subscription_tags WHERE tag='research'").Scan(&tags); err != nil || tags != 0 {
		t.Fatal("tags changed by a failed migration", tags, err)
	}
}

func TestTagOrderIsKeptAsTyped(t *testing.T) {
	s := testStore(t)
	f := testSource(t, s, 2, "order", false)
	if err := s.SetTags(t.Context(), User{ID: 2}, f.ID, "zebra, apple, mango", false); err != nil {
		t.Fatal(err)
	}
	if tags := feedTags(t, s, 2, false)["order"]; !slices.Equal(tags, []string{"zebra", "apple", "mango"}) {
		t.Fatal(tags)
	}
}

func TestOldExportsAndOPMLImportTopicsAsFirstTags(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	old := `{"version":1,"username":"someone","feeds":[
	 {"id":7,"url":"https://example.org/old","title":"Old","category":"Research","tags":["alpha","research-notes"]},
	 {"id":8,"url":"https://example.org/plain","title":"Plain","category":"","tags":[]},
	 {"id":9,"url":"https://example.org/dup","title":"Dup","category":"Alpha","tags":["beta","alpha"]}]}`
	data, err := DecodeExport([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RestorePersonal(ctx, User{ID: 3}, data); err != nil {
		t.Fatal(err)
	}
	got := feedTags(t, s, 3, false)
	if !slices.Equal(got["Old"], []string{"research", "alpha", "research-notes"}) || len(got["Plain"]) != 0 || !slices.Equal(got["Dup"], []string{"alpha", "beta"}) {
		t.Fatal(got)
	}
	// A new export names the first tag as the category and round-trips.
	exported, err := s.Export(ctx, User{ID: 3})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(exported)
	again, err := DecodeExport(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RestorePersonal(ctx, User{ID: 2}, again); err != nil {
		t.Fatal(err)
	}
	if mine := feedTags(t, s, 2, false); !slices.Equal(mine["Old"], got["Old"]) || !slices.Equal(mine["Dup"], got["Dup"]) {
		t.Fatal(mine)
	}
	// OPML: a folder becomes the first tag; untagged sources stay top level.
	opml := []byte(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Science"><outline text="Sky" xmlUrl="https://example.org/sky"/></outline><outline text="Loose" xmlUrl="https://example.org/loose"/></body></opml>`)
	feeds, err := ParseOPML(opml)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Subscribe(ctx, 1, feeds, false); err != nil {
		t.Fatal(err)
	}
	host := feedTags(t, s, 1, false)
	if !slices.Equal(host["Sky"], []string{"science"}) || len(host["Loose"]) != 0 {
		t.Fatal(host)
	}
	list, _ := s.Feeds(ctx, 1, false)
	out, err := EncodeOPML(list)
	if err != nil || !strings.Contains(string(out), `<outline text="science">`) || strings.Contains(string(out), `<outline text="">`) {
		t.Fatalf("%s %v", out, err)
	}
	round, err := ParseOPML(out)
	if err != nil || len(round) != 2 {
		t.Fatal(round, err)
	}
}

func TestTopicImportRejectsBadValues(t *testing.T) {
	for name, category := range map[string]string{
		"too long": strings.Repeat("x", 41),
		"newline":  "one\ntwo",
		"nul":      "zero\x00",
	} {
		t.Run(name, func(t *testing.T) {
			doc, _ := json.Marshal(map[string]any{"version": 1, "username": "x", "feeds": []map[string]any{{"id": 1, "url": "https://example.org/x", "title": "X", "category": category, "tags": []string{}}}})
			if _, err := DecodeExport(doc); err == nil {
				t.Fatalf("accepted category %q", category)
			}
			if _, err := withTopic(category, nil); err == nil {
				t.Fatalf("withTopic accepted %q", category)
			}
		})
	}
	if _, err := ParseOPML([]byte(`<opml version="2.0"><body><outline text="` + strings.Repeat("y", 41) + `"><outline text="A" xmlUrl="https://example.org/a"/></outline></body></opml>`)); err == nil {
		t.Fatal("long OPML folder accepted")
	}
	// Forty characters is the limit, matching tags.
	if tags, err := withTopic(strings.Repeat("Z", 40), []string{"a"}); err != nil || tags[0] != strings.Repeat("z", 40) {
		t.Fatal(tags, err)
	}
}

func TestTopicBookmarksRedirectToTagFilters(t *testing.T) {
	h := webHarness(t)
	testSource(t, h.store, 2, "bookmarks", false)
	for query, want := range map[string]string{
		"topic=AI":                              "/?tag=ai",
		"view=all&topic=Weekend+Reads":          "/?tag=weekend+reads&view=all",
		"topic=AI&tag=kept":                     "/?tag=kept",
		"topic=":                                "/",
		"topic=" + url.QueryEscape("bad\nline"): "/",
		"topic=" + strings.Repeat("x", 200):     "/",
		"topic=AI&topic=Other&q=" + "%3Cb%3E":   "/?q=%3Cb%3E&tag=ai",
	} {
		w := h.request(2, "GET", "/?"+query, nil)
		if w.Code != 301 || w.Header().Get("Location") != want {
			t.Errorf("%s: %d %q, want %q", query, w.Code, w.Header().Get("Location"), want)
		}
	}
	if w := h.request(2, "GET", "/?tag=tech", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `<option selected>tech</option>`) {
		t.Fatal("tag filter not selected", w.Code)
	}
	for _, injection := range []string{"' OR 1=1 --", "%", "tech' --"} {
		w := h.request(2, "GET", "/?view=all&tag="+url.QueryEscape(injection), nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), `class="story-row`) {
			t.Fatal("tag filter matched an injected value", injection, w.Code)
		}
	}
}

func TestTopicIsGoneFromTheInterface(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "interface", false)
	if w := h.request(2, "POST", fmt.Sprintf("/sources/%d/topic", f.ID), url.Values{"category": {"X"}}); w.Code != 404 && w.Code != 405 {
		t.Fatal("topic route still answers", w.Code)
	}
	for _, path := range []string{"/", "/sources", "/sources?scope=household", "/groups"} {
		page := h.request(1, "GET", path, nil).Body.String()
		for _, gone := range []string{`name="topic"`, `name="category" maxlength`, "Every topic", "Edit tags and topic", "Save topic", `action="/sources/` + fmt.Sprint(f.ID) + `/topic"`} {
			if strings.Contains(page, gone) {
				t.Errorf("%s still shows %q", path, gone)
			}
		}
	}
	home := h.request(2, "GET", "/", nil).Body.String()
	if !strings.Contains(home, `<select id="tag" name="tag">`) || !strings.Contains(home, `class="story-tag" href="/?view=all&amp;tag=tech"`) {
		t.Fatal("tag filter or story tag link missing")
	}
	// Following a suggestion tags the source with its catalog category.
	if w := h.request(2, "POST", "/sources", url.Values{"url": {"https://example.org/suggested"}, "tags": {"Science"}}); w.Code != 303 {
		t.Fatal(w.Code)
	}
	feeds, _ := h.store.Feeds(t.Context(), 2, false)
	for _, feed := range feeds {
		if feed.URL == "https://example.org/suggested" && !slices.Equal(feed.Tags, []string{"science"}) {
			t.Fatal(feed.Tags)
		}
	}
}
