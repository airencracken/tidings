package reader

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkEntriesScale(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s := testStore(b)
			ctx := context.Background()
			_, err := s.DB.Exec(`INSERT INTO feeds(id,url,title) VALUES(1,'https://example.org/feed','Benchmark');INSERT INTO subscriptions(user_id,feed_id,category) VALUES(1,1,'Tech')`)
			if err != nil {
				b.Fatal(err)
			}
			_, err = s.DB.Exec(`WITH RECURSIVE seq(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x<?) INSERT INTO entries(feed_id,key,title,url,body,published) SELECT 1,printf('%d',x),'A quiet morning','https://example.org/story','An ordinary day worth remembering',x FROM seq`, n)
			if err != nil {
				b.Fatal(err)
			}
			for _, page := range []int{0, n/30 - 2} {
				b.Run(fmt.Sprint(page), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						rows, _, err := s.Entries(ctx, 1, Filter{View: "all", Page: page})
						if err != nil || len(rows) != 30 {
							b.Fatal(len(rows), err)
						}
					}
				})
			}
		})
	}
}
func BenchmarkEntriesCursorScale(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s := testStore(b)
			_, err := s.DB.Exec(`INSERT INTO feeds(id,url,title) VALUES(1,'https://example.org/feed','Benchmark');INSERT INTO subscriptions(user_id,feed_id) VALUES(1,1);WITH RECURSIVE seq(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x<?) INSERT INTO entries(feed_id,key,title,url,body,published) SELECT 1,printf('%d',x),'A story','https://example.org','Body',x FROM seq`, n)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				rows, _, err := s.Entries(b.Context(), 1, Filter{View: "all", Page: n/30 - 2, CursorID: 60, CursorPublished: 60})
				if err != nil || len(rows) != 30 {
					b.Fatal(err, len(rows))
				}
			}
		})
	}
}
