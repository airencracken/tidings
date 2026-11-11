package reader

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
	"testing/quick"
)

func TestCursorStableTiesReverseAndPrivacy(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	_, e := s.DB.Exec(`INSERT INTO feeds(id,url,title) VALUES(1,'https://example.org/a','A'),(2,'https://example.org/b','B');INSERT INTO subscriptions(user_id,feed_id) VALUES(1,1),(2,2);WITH RECURSIVE seq(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x<95) INSERT INTO entries(feed_id,key,title,url,body,published) SELECT CASE WHEN x%4=0 THEN 2 ELSE 1 END,printf('%d',x),'Story','https://example.org','Body',x/3 FROM seq`)
	if e != nil {
		t.Fatal(e)
	}
	ids := func(rows []Entry) []int64 {
		out := []int64{}
		for _, r := range rows {
			if r.FeedID != 1 {
				t.Fatal("private source leaked")
			}
			out = append(out, r.ID)
		}
		return out
	}
	f := Filter{View: "all"}
	first, more, e := s.Entries(ctx, 1, f)
	if e != nil || !more {
		t.Fatal(e, more)
	}
	last := first[29]
	f.CursorID = last.ID
	f.CursorPublished = last.Published
	f.Page = 1
	second, more, e := s.Entries(ctx, 1, f)
	if e != nil || !more {
		t.Fatal(e, more)
	}
	offset, _, e := s.Entries(ctx, 1, Filter{View: "all", Page: 1})
	if e != nil || !reflect.DeepEqual(ids(second), ids(offset)) {
		t.Fatal("cursor differs from legacy ordering", e)
	}
	if _, e = s.DB.Exec(`INSERT INTO entries(feed_id,key,title,url,body,published) VALUES(1,'new','New','https://example.org','New',9999)`); e != nil {
		t.Fatal(e)
	}
	again, _, e := s.Entries(ctx, 1, f)
	if e != nil || !reflect.DeepEqual(ids(second), ids(again)) {
		t.Fatal("concurrent insert shifted older page", e)
	}
	f.CursorID = second[0].ID
	f.CursorPublished = second[0].Published
	f.Reverse = true
	previous, _, e := s.Entries(ctx, 1, f)
	if e != nil || !reflect.DeepEqual(ids(previous), ids(first)) {
		t.Fatal("reverse lost the adjacent page", e, ids(previous), ids(first))
	}
	f.CursorID = second[29].ID
	f.CursorPublished = second[29].Published
	f.Reverse = false
	third, more, e := s.Entries(ctx, 1, f)
	if e != nil || more || len(third) == 0 {
		t.Fatal("last page", len(third), more, e)
	}
	if _, e = s.DB.Exec("DELETE FROM entries"); e != nil {
		t.Fatal(e)
	}
	f.Reverse = true
	empty, more, e := s.Entries(ctx, 1, f)
	if e != nil || more || len(empty) != 0 {
		t.Fatal("empty reverse page offers a phantom next page", more, e)
	}
}
func TestCursorInputValidation(t *testing.T) {
	for _, q := range []string{"cursor=1:0&direction=older", "cursor=1:2&direction=sideways", "cursor=x:1&direction=older", "cursor=1:2:3&direction=older", "cursor=1:2&cursor=2:3&direction=older", "direction=newer", "cursor=1:2&direction=older&direction=newer"} {
		if _, e := parseFilter(httptest.NewRequest("GET", "/?"+q, nil)); e == nil {
			t.Fatal("accepted", q)
		}
	}
	if e := quick.Check(func(p int64, id uint32) bool {
		raw := fmt.Sprintf("/?cursor=%d:%d&direction=older", p, uint64(id)+1)
		f, e := parseFilter(httptest.NewRequest("GET", raw, nil))
		return e == nil && f.CursorID == int64(id)+1 && f.CursorPublished == p
	}, nil); e != nil {
		t.Fatal(e)
	}
}
