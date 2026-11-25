package reader

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func recommendationSource(t *testing.T, s *Store, uid int64, name string, household bool) Feed {
	t.Helper()
	f := Feed{URL: "https://example.org/" + name, Title: name}
	if err := s.Subscribe(t.Context(), uid, []Feed{f}, household); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT id FROM feeds WHERE url=?", f.URL).Scan(&f.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func recommendationStory(t *testing.T, s *Store, feedID, owner int64, title string, published time.Time) int64 {
	t.Helper()
	var importOwner any
	if owner != 0 {
		importOwner = owner
	}
	result, err := s.DB.Exec(
		"INSERT INTO entries(feed_id,key,title,url,body,published,import_owner) VALUES(?,?,?,?,?,?,?)",
		feedID, title, title, "https://example.org/story/"+url.PathEscape(title), "A story about a useful idea.", published.Unix(), importOwner,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func recommendationIDs(t *testing.T, s *Store, uid int64, f Filter, now time.Time) []int64 {
	t.Helper()
	f.View = "for-you"
	entries, err := s.recommendations(t.Context(), uid, f, now)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestRecommendationsRespectPrivateFeedbackAndVisibility(t *testing.T) {
	s := testStore(t)
	now := time.Now().Truncate(time.Second)
	liked := recommendationSource(t, s, 2, "saved-source", false)
	related := recommendationSource(t, s, 2, "related-source", false)
	neutral := recommendationSource(t, s, 2, "neutral-source", false)
	skipped := recommendationSource(t, s, 2, "skipped-source", false)
	shared := recommendationSource(t, s, 1, "shared-source", true)
	private := recommendationSource(t, s, 3, "private-source", false)
	for _, f := range []Feed{liked, related, neutral, skipped} {
		f.Tags = []string{"weekend"}
		if f.ID == liked.ID || f.ID == related.ID {
			f.Tags = append(f.Tags, "research")
		} else {
			f.Tags = []string{"general"}
		}
		if err := s.Subscribe(t.Context(), 3, []Feed{f}, false); err != nil {
			t.Fatal(err)
		}
		if err := s.SetTags(t.Context(), User{ID: 2}, f.ID, strings.Join(f.Tags, ", "), false); err != nil {
			t.Fatal(err)
		}
	}
	fresh := now.Add(-time.Hour)
	likedID := recommendationStory(t, s, liked.ID, 0, "Liked pick", fresh)
	relatedID := recommendationStory(t, s, related.ID, 0, "Related pick", fresh)
	neutralID := recommendationStory(t, s, neutral.ID, 0, "Neutral pick", fresh)
	skippedID := recommendationStory(t, s, skipped.ID, 0, "Lower ranked pick", fresh)
	sharedID := recommendationStory(t, s, shared.ID, 0, "Household pick", fresh)
	privateID := recommendationStory(t, s, private.ID, 0, "Other reader's pick", fresh)
	savedFeedback := recommendationStory(t, s, liked.ID, 0, "Saved feedback", fresh)
	skippedFeedback := recommendationStory(t, s, skipped.ID, 0, "Skipped feedback", fresh)
	read := recommendationStory(t, s, neutral.ID, 0, "Read story", fresh)
	old := recommendationStory(t, s, neutral.ID, 0, "Old story", now.AddDate(0, 0, -31))
	future := recommendationStory(t, s, neutral.ID, 0, "Future story", now.Add(time.Hour))
	imported := recommendationStory(t, s, liked.ID, 3, "Private restore", fresh)
	if _, err := s.DB.Exec("INSERT INTO states(user_id,entry_id,saved) VALUES(3,?,1)", imported); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		uid, id int64
		field   string
	}{
		{2, savedFeedback, "saved"},
		{2, skippedFeedback, "skipped"},
		{2, read, "read"},
	} {
		if err := s.SetState(t.Context(), change.uid, change.id, change.field, true); err != nil {
			t.Fatal(err)
		}
	}
	want := []int64{likedID, relatedID, neutralID, skippedID}
	got := recommendationIDs(t, s, 2, Filter{}, now)
	if !slices.Equal(got, want) {
		t.Fatalf("personalized order %v, want %v", got, want)
	}
	if got := recommendationIDs(t, s, 2, Filter{Tag: "research"}, now); !slices.Equal(got, want[:2]) {
		t.Fatalf("tag filter returned %v", got)
	}
	if got := recommendationIDs(t, s, 2, Filter{Query: "Neutral", FeedID: neutral.ID}, now); !slices.Equal(got, []int64{neutralID}) {
		t.Fatalf("search and source filter returned %v", got)
	}
	other := recommendationIDs(t, s, 3, Filter{}, now)
	if !slices.Contains(other, skippedFeedback) || !slices.Contains(other, privateID) || slices.Contains(other, sharedID) || slices.Contains(other, old) || slices.Contains(other, future) {
		t.Fatalf("wrong eligibility for other reader: %v", other)
	}
	if _, err := s.DB.Exec("UPDATE users SET group_optin=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if got := recommendationIDs(t, s, 2, Filter{FeedID: shared.ID}, now); !slices.Equal(got, []int64{sharedID}) {
		t.Fatalf("household opt-in returned %v", got)
	}
	if err := s.SetTags(t.Context(), User{ID: 1, Owner: true}, shared.ID, "household interest", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(t.Context(), 2, []Feed{shared}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE users SET group_optin=0 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.recommendations(t.Context(), 2, Filter{View: "for-you", FeedID: shared.ID}, now)
	if err != nil || len(rows) != 1 || rows[0].Category != "" {
		t.Fatalf("personal subscription exposed a household-only tag: %+v, %v", rows, err)
	}
}

func TestRecommendationsLimitAndSourceVariety(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	byStory := map[int64]int64{}
	newest := map[int64]int64{}
	for n := range recommendationLimit + 3 {
		f := recommendationSource(t, s, 2, fmt.Sprintf("publisher-%d", n), false)
		for m := range 5 {
			id := recommendationStory(t, s, f.ID, 0, fmt.Sprintf("Pick %d-%d", n, m), now.Add(-time.Duration(n*5+m)*time.Minute))
			byStory[id] = f.ID
			if m == 0 {
				newest[f.ID] = id
			}
		}
	}
	ids := recommendationIDs(t, s, 2, Filter{}, now)
	if len(ids) != recommendationLimit || !slices.Equal(ids, recommendationIDs(t, s, 2, Filter{}, now)) {
		t.Fatalf("expected %d stable picks, got %v", recommendationLimit, ids)
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		feed := byStory[id]
		if seen[feed] || id != newest[feed] {
			t.Fatalf("newest stories from different sources should lead the list: %v", ids)
		}
		seen[feed] = true
	}
}

func TestRecommendationsHTTPContract(t *testing.T) {
	h := webHarness(t)
	const shelf = "/?view=for-you"
	f := recommendationSource(t, h.store, 2, "mine", false)
	id := recommendationStory(t, h.store, f.ID, 0, "A fresh recommendation", time.Now().Add(-time.Minute))
	page := h.request(2, "GET", shelf, nil)
	if page.Code != http.StatusOK || page.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(page.Body.String(), "<h1>For You</h1>") ||
		!strings.Contains(page.Body.String(), "A fresh recommendation") ||
		!strings.Contains(page.Body.String(), `href="/?view=for-you" aria-current="page"`) ||
		strings.Contains(page.Body.String(), "Next page") {
		t.Fatalf("For You route returned %d without the expected story shelf", page.Code)
	}
	if guest := h.request(0, "GET", shelf, nil); guest.Code != http.StatusSeeOther {
		t.Fatalf("anonymous shelf status: %d", guest.Code)
	}
	for _, invalid := range []string{"&page=0", "&cursor=123:4", "&direction=older", "&feed=0", "&q=" + strings.Repeat("x", 301)} {
		if w := h.request(2, "GET", shelf+invalid, nil); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid shelf filter %q returned %d", invalid, w.Code)
		}
	}
	values := url.Values{"field": {"skipped"}, "value": {"true"}, "return_to": {shelf}}
	statePath := fmt.Sprintf("/story/%d/state", id)
	if w := h.request(2, "POST", statePath, values); w.Code != http.StatusSeeOther || w.Header().Get("Location") != shelf {
		t.Fatalf("skip returned %d, Location %q", w.Code, w.Header().Get("Location"))
	}
	if w := h.request(2, "GET", shelf, nil); strings.Contains(w.Body.String(), "A fresh recommendation") {
		t.Fatal("skipped story remained in For You")
	}
}

func TestRecommendationsCollapseDuplicateLinks(t *testing.T) {
	s := testStore(t)
	const uid = 2
	favorite := recommendationSource(t, s, uid, "favorite", false)
	related := recommendationSource(t, s, uid, "related", false)
	other := recommendationSource(t, s, uid, "other", false)
	now := time.Now().Truncate(time.Second)
	for _, source := range []Feed{favorite, related} {
		if err := s.SetTags(t.Context(), User{ID: uid}, source.ID, "books", false); err != nil {
			t.Fatal(err)
		}
	}
	saved := recommendationStory(t, s, favorite.ID, 0, "Saved story", now.Add(-time.Hour))
	if err := s.SetState(t.Context(), uid, saved, "saved", true); err != nil {
		t.Fatal(err)
	}
	first := recommendationStory(t, s, favorite.ID, 0, "Duplicate link", now.Add(-time.Minute))
	recommendationStory(t, s, other.ID, 0, "Duplicate link", now)
	second := recommendationStory(t, s, related.ID, 0, "Another story", now)

	got, err := s.recommendations(t.Context(), uid, Filter{View: "for-you"}, now)
	if err != nil || len(got) != 2 || got[0].ID != first || got[1].ID != second ||
		got[0].RecommendationReason != "From a source you save" ||
		got[1].RecommendationReason != "Shares a tag with a source you save" {
		t.Fatalf("expected one copy with its relevance explanation, got %+v: %v", got, err)
	}
}
