package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airencracken/tidings/internal/reader"
)

func seededStore(t *testing.T) *reader.Store {
	t.Helper()
	s, err := reader.OpenStore(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	if err = seed(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	return s
}
func demoUser(t *testing.T, s *reader.Store, name string) reader.User {
	t.Helper()
	raw, err := s.Login(context.Background(), name, demoPassword)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Session(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSeedAccountsSourcesGroupsAndIndependentState(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name                     string
		owner, optin             bool
		personal, visible, saved int
	}{{"demo", true, true, 3, 16, 1}, {"freya", false, true, 2, 12, 0}, {"jules", false, false, 1, 4, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			u := demoUser(t, s, tc.name)
			if u.Owner != tc.owner || u.GroupOptin != tc.optin {
				t.Fatalf("account policy %+v", u)
			}
			feeds, err := s.Feeds(ctx, u.ID, false)
			if err != nil || len(feeds) != tc.personal {
				t.Fatalf("personal sources %+v %v", feeds, err)
			}
			stories, _, err := s.Entries(ctx, u.ID, reader.Filter{View: "all"})
			if err != nil || len(stories) != tc.visible {
				t.Fatalf("visible stories %d %v", len(stories), err)
			}
			saved, _, err := s.Entries(ctx, u.ID, reader.Filter{View: "saved"})
			if err != nil || len(saved) != tc.saved {
				t.Fatalf("saved state %d %v", len(saved), err)
			}
			groups, err := s.Groups(ctx, u)
			if err != nil {
				t.Fatal(err)
			}
			var explicit, tag bool
			for _, g := range groups {
				if g.Household {
					continue
				}
				if g.Tag == "" {
					explicit = true
				} else {
					tag = true
				}
				out, _, err := s.Entries(ctx, u.ID, reader.Filter{View: "all", GroupID: g.ID})
				if err != nil || len(out) == 0 {
					t.Fatalf("empty sample interest group %+v %v", g, err)
				}
			}
			if !explicit || !tag {
				t.Fatal("both kinds of interest groups must be demonstrated")
			}
			data, err := s.Export(ctx, u)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), demoPassword) || strings.Contains(string(b), `"password"`) {
				t.Fatal("export leaks demo credentials")
			}
		})
	}
}

func TestSampleTransportContractAndAdversarialURLs(t *testing.T) {
	transport := sampleTransport{}
	for _, f := range sampleFeeds {
		req, err := http.NewRequest("GET", sampleURL(f), nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := reader.ParseFeed(data, sampleURL(f), time.Now())
		if err != nil || res.StatusCode != 200 || len(parsed.Entries) != len(f.Titles) {
			t.Fatalf("offline feed contract %+v %v", parsed, err)
		}
		for _, e := range parsed.Entries {
			if !strings.HasPrefix(e.Title, "Sample:") {
				t.Fatal("sample could be mistaken for publisher reporting")
			}
		}
	}
	for _, raw := range []string{"https://example.org/", "http://127.0.0.1/", "http://169.254.169.254/", "https://example.invalid.evil.test/tidings/ai.xml", "https://example.invalid/tidings/ai.xml?next=https://example.org/"} {
		req, err := http.NewRequest("GET", raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = transport.RoundTrip(req); err == nil {
			t.Fatalf("demo allowed nonfixture request %s", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", sampleURL(sampleFeeds[0]), nil)
	if _, err := transport.RoundTrip(req); err != context.Canceled {
		t.Fatal("sample refresh ignored cancellation")
	}
}

func TestDemoSchemaAndFailedSeedDoNotServePartialData(t *testing.T) {
	s := seededStore(t)
	var broken int
	if err := s.DB.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&broken); err != nil || broken != 0 {
		t.Fatalf("invalid demo schema %d %v", broken, err)
	}
	var openInvites int
	if err := s.DB.QueryRow("SELECT count(*) FROM invites WHERE used=0").Scan(&openInvites); err != nil || openInvites != 0 {
		t.Fatal("demo left reusable invitations")
	}
	if err := seed(context.Background(), s); err == nil {
		t.Fatal("seeding an existing store should fail, not overwrite it")
	}
	var accounts int
	if err := s.DB.QueryRow("SELECT count(*) FROM users").Scan(&accounts); err != nil || accounts != 3 {
		t.Fatal("failed reseed changed accounts")
	}
}

func TestDemoStateProperties(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	host := demoUser(t, s, "demo")
	member := demoUser(t, s, "freya")
	var id int64
	if err := s.DB.QueryRow("SELECT e.id FROM entries e JOIN group_feeds g ON g.feed_id=e.feed_id ORDER BY e.id LIMIT 1").Scan(&id); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(19, 27))
	model := map[int64]bool{}
	for range 60 {
		u := host
		if rng.IntN(2) == 0 {
			u = member
		}
		value := rng.IntN(2) == 0
		if err := s.SetState(ctx, u.ID, id, "read", value); err != nil {
			t.Fatal(err)
		}
		model[u.ID] = value
		for _, who := range []reader.User{host, member} {
			e, err := s.Entry(ctx, who.ID, id)
			if err != nil || e.Read != model[who.ID] {
				t.Fatalf("demo shares reading state: %s %+v %v", who.Username, e, err)
			}
		}
	}
	jules := demoUser(t, s, "jules")
	if _, err := s.Entry(ctx, jules.ID, id); err != sql.ErrNoRows {
		t.Fatal("demo household opt-out bypassed")
	}
}

func TestSampleRefreshIsIdempotent(t *testing.T) {
	s := seededStore(t)
	ctx := context.Background()
	f := sampleFeeds[0]
	var fid int64
	if err := s.DB.QueryRow("SELECT id FROM feeds WHERE url=?", sampleURL(f)).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	client := &reader.Fetcher{Client: &http.Client{Transport: sampleTransport{}}}
	out, err := client.Fetch(ctx, sampleURL(f), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApplyFetch(ctx, reader.Feed{ID: fid}, out); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB.QueryRow("SELECT count(*) FROM entries WHERE feed_id=?", fid).Scan(&n); err != nil || n != len(f.Titles) {
		t.Fatalf("sample refresh duplicates stories: %d %v", n, err)
	}
	u := demoUser(t, s, "demo")
	stories, _, err := s.Entries(ctx, u.ID, reader.Filter{View: "saved"})
	if err != nil || len(stories) != 1 || !stories[0].Read {
		t.Fatal(fmt.Sprintf("sample refresh lost saved state: %+v %v", stories, err))
	}
}
