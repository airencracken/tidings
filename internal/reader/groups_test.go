package reader

import (
	"context"
	"database/sql"
	"testing"
)

func TestExplicitAndTagBasedGroups(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := User{ID: 2, Username: "alice"}
	f := testSource(t, s, 2, "ai", false)
	other := testSource(t, s, 2, "security", false)
	private := testSource(t, s, 3, "bob-only", false)
	id, err := s.CreateGroup(ctx, u, "Weekend reading", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetGroupMembers(ctx, u, id, []int64{f.ID, other.ID}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Entries(ctx, u.ID, Filter{View: "all", GroupID: id})
	if err != nil || len(got) != 2 {
		t.Fatalf("explicit group %+v %v", got, err)
	}
	if err = s.SetGroupMembers(ctx, u, id, []int64{private.ID}); err != sql.ErrNoRows {
		t.Fatalf("private source added to collection: %v", err)
	}
	g, err := s.Group(ctx, u, id)
	if err != nil || len(g.FeedIDs) != 2 {
		t.Fatal("failed update lost old collection membership")
	}
	if _, err = s.Group(ctx, User{ID: 3}, id); err != sql.ErrNoRows {
		t.Fatal("private group exposed to another member")
	}
	if err = s.SetGroupMembers(ctx, User{ID: 3}, id, []int64{private.ID}); err != sql.ErrNoRows {
		t.Fatal("another member changed a private group")
	}
	if err = s.SetTags(ctx, u, f.ID, "AI, Research, ai", false); err != nil {
		t.Fatal(err)
	}
	tagID, err := s.CreateGroup(ctx, u, "Research", "RESEARCH", false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err = s.Entries(ctx, u.ID, Filter{View: "all", GroupID: tagID})
	if err != nil || len(got) != 1 || got[0].FeedID != f.ID {
		t.Fatalf("tag group %+v %v", got, err)
	}
	if err = s.SetTags(ctx, u, other.ID, "research", false); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.Entries(ctx, u.ID, Filter{View: "all", GroupID: tagID})
	if err != nil || len(got) != 2 {
		t.Fatal("newly tagged source did not join group")
	}
	if err = s.SetTags(ctx, u, f.ID, "ai", false); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.Entries(ctx, u.ID, Filter{View: "all", GroupID: tagID})
	if err != nil || len(got) != 1 || got[0].FeedID != other.ID {
		t.Fatal("untagged source stayed in group")
	}
	if err = s.SetGroupMembers(ctx, u, tagID, []int64{f.ID}); err == nil {
		t.Fatal("explicit writes to automatic group accepted")
	}
	if err = s.SetTags(ctx, u, private.ID, "research", false); err != sql.ErrNoRows {
		t.Fatal("another person's tags writable")
	}
	if err = s.DeleteGroup(ctx, User{ID: 3}, id); err != sql.ErrNoRows {
		t.Fatal("another person's group removable")
	}
	if err = s.DeleteGroup(ctx, u, id); err != nil {
		t.Fatal(err)
	}
	if len(entries(t, s, 2, "all")) != 2 {
		t.Fatal("deleting group removed subscriptions")
	}
}

func TestHouseholdGroupsRespectOptInAndTags(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	host := User{ID: 1, Owner: true}
	member := User{ID: 2}
	f := testSource(t, s, 1, "shared", true)
	id, err := s.CreateGroup(ctx, host, "Shared research", "research", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetTags(ctx, host, f.ID, "research", true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateGroup(ctx, member, "Unauthorized", "", true); err != sql.ErrNoRows {
		t.Fatal("member created household group")
	}
	if err = s.SetTags(ctx, member, f.ID, "anything", true); err != sql.ErrNoRows {
		t.Fatal("member changed household tags")
	}
	got, _, err := s.Entries(ctx, member.ID, Filter{View: "all", GroupID: id})
	if err != nil || len(got) != 0 {
		t.Fatal("collection bypassed household opt-in")
	}
	if _, err = s.DB.Exec("UPDATE users SET group_optin=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	member.GroupOptin = true
	got, _, err = s.Entries(ctx, member.ID, Filter{View: "all", GroupID: id})
	if err != nil || len(got) != 1 {
		t.Fatal("household tag group unavailable after opting in")
	}
	personal, err := s.CreateGroup(ctx, member, "My shared reading", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetGroupMembers(ctx, member, personal, []int64{f.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE users SET group_optin=0 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.Entries(ctx, member.ID, Filter{View: "all", GroupID: personal})
	if err != nil || len(got) != 0 {
		t.Fatal("personal collection retained opted-out household visibility")
	}
}

func TestTagsAreScopedAndLiteral(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := User{ID: 2}
	v := User{ID: 3}
	f := testSource(t, s, 2, "same-source", false)
	if err := s.Subscribe(ctx, v.ID, []Feed{{URL: f.URL, Tags: []string{"different"}}}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTags(ctx, u, f.ID, "100% useful, a_b", false); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateGroup(ctx, v, "Not mine", "100% useful", false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Entries(ctx, v.ID, Filter{View: "all", GroupID: id})
	if err != nil || len(got) != 0 {
		t.Fatal("tag ownership or literal matching failed")
	}
	if err = s.SetTags(ctx, v, f.ID, "bob-only-tag", false); err != nil {
		t.Fatal(err)
	}
	privateTagID, err := s.CreateGroup(ctx, u, "Also not mine", "bob-only-tag", false)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err = s.Entries(ctx, u.ID, Filter{View: "all", GroupID: privateTagID})
	if err != nil || len(got) != 0 {
		t.Fatal("another member's tags leaked into personal grouping")
	}
	feeds, err := s.Feeds(ctx, u.ID, false)
	if err != nil || len(feeds) != 1 || len(feeds[0].Tags) != 2 {
		t.Fatalf("tags not exported with sources: %+v %v", feeds, err)
	}
	for _, raw := range []string{"bad\nvalue", string(make([]byte, 41))} {
		if _, err := ParseTags(raw); err == nil {
			t.Fatal("invalid tag accepted")
		}
	}
}
