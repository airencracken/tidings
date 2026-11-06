// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/airencracken/tidings/internal/reader"
)

const demoPassword = "demo-password"

type sampleFeed struct {
	Slug, Title, Category, Tag string
	Titles                     []string
}

var sampleFeeds = []sampleFeed{
	{"ai", "The AI workbench · sample", "AI", "research", []string{"Sample: a field guide to small language models", "Sample: reading an AI paper with a pencil", "Sample: an afternoon spent testing a local model", "Sample: what makes a useful evaluation?"}},
	{"systems", "The tinkering shed · sample", "Tech", "homelab", []string{"Sample: a small server with a long useful life", "Sample: finding the bug with a simpler tool", "Sample: making a backup you can actually restore", "Sample: a quieter home lab"}},
	{"security", "The watchkeeper · sample", "Infosec", "security", []string{"Sample: a little care with everyday passwords", "Sample: how to read an incident report", "Sample: threat modeling the garden gate", "Sample: a patching checklist for a rainy day"}},
	{"shared", "The village sheet · sample", "Tech", "shared", []string{"Sample: good sources for a shared reading table", "Sample: leaving room for a long read", "Sample: a useful explanation worth passing on", "Sample: the news can wait until after tea"}},
}

func sampleURL(f sampleFeed) string { return "https://example.invalid/tidings/" + f.Slug + ".xml" }
func sampleEntries(f sampleFeed, now time.Time) []reader.Entry {
	var out []reader.Entry
	for i, title := range f.Titles {
		out = append(out, reader.Entry{Key: f.Slug + "-" + fmt.Sprint(i), Title: title, URL: "https://example.invalid/tidings/" + f.Slug + "/" + fmt.Sprint(i), Body: "This is an illustrated example for the disposable Tidings demo, not a report from a real publisher. Read it here, mark it read, or set it aside. Your choices belong to this account alone.\n\nTry following the household table, adding tags, and placing the same source in more than one interest group. Everything in this demo is removed when it stops.", Published: now.Add(-time.Duration(i+1) * time.Hour).Unix()})
	}
	return out
}

func seed(ctx context.Context, s *reader.Store) error {
	if err := s.CreateOwner(ctx, "demo", demoPassword); err != nil {
		return err
	}
	users := map[string]reader.User{}
	for _, name := range []string{"freya", "jules"} {
		code, err := s.Invite(ctx)
		if err != nil {
			return err
		}
		if err = s.Join(ctx, code, name, demoPassword); err != nil {
			return err
		}
	}
	for _, name := range []string{"demo", "freya", "jules"} {
		var u reader.User
		if err := s.DB.QueryRowContext(ctx, "SELECT id,username,owner,group_optin FROM users WHERE username=?", name).Scan(&u.ID, &u.Username, &u.Owner, &u.GroupOptin); err != nil {
			return err
		}
		users[name] = u
	}
	for _, name := range []string{"demo", "freya"} {
		u := users[name]
		if _, err := s.DB.ExecContext(ctx, "UPDATE users SET group_optin=1 WHERE id=?", u.ID); err != nil {
			return err
		}
		u.GroupOptin = true
		users[name] = u
	}
	var ids []int64
	for i, f := range sampleFeeds {
		feed := reader.Feed{URL: sampleURL(f), Title: f.Title, Category: f.Category, Tags: []string{f.Tag, "weekend"}}
		if err := s.Subscribe(ctx, users["demo"].ID, []reader.Feed{feed}, i == 3); err != nil {
			return err
		}
		if i < 3 {
			person := "freya"
			if i == 1 {
				person = "jules"
			}
			if err := s.Subscribe(ctx, users[person].ID, []reader.Feed{feed}, false); err != nil {
				return err
			}
		}
		if err := s.DB.QueryRowContext(ctx, "SELECT id FROM feeds WHERE url=?", feed.URL).Scan(&feed.ID); err != nil {
			return err
		}
		ids = append(ids, feed.ID)
		fixtureClient := &reader.Fetcher{Client: &http.Client{Transport: sampleTransport{}}}
		result, err := fixtureClient.Fetch(ctx, feed.URL, "", "")
		if err != nil {
			return err
		}
		if err = s.ApplyFetch(ctx, feed, result); err != nil {
			return err
		}
	}
	for _, name := range []string{"demo", "freya", "jules"} {
		u := users[name]
		tag := "research"
		if name == "jules" {
			tag = "homelab"
		}
		if _, err := s.CreateGroup(ctx, u, "An interest to follow", tag, false); err != nil {
			return err
		}
		id, err := s.CreateGroup(ctx, u, "Weekend reading", "", false)
		if err != nil {
			return err
		}
		members := []int64{ids[0], ids[2]}
		if name == "demo" {
			members = append(members, ids[1])
		}
		if name == "jules" {
			members = []int64{ids[1]}
		}
		if err = s.SetGroupMembers(ctx, u, id, members); err != nil {
			return err
		}
	}
	if _, err := s.CreateGroup(ctx, users["demo"], "Around the village", "shared", true); err != nil {
		return err
	}
	var storyID int64
	if err := s.DB.QueryRowContext(ctx, "SELECT id FROM entries WHERE feed_id=? ORDER BY id LIMIT 1", ids[0]).Scan(&storyID); err != nil {
		return err
	}
	if err := s.SetState(ctx, users["demo"].ID, storyID, "saved", true); err != nil {
		return err
	}
	if err := s.SetState(ctx, users["demo"].ID, storyID, "read", true); err != nil {
		return err
	}
	return nil
}

type sampleTransport struct{}

// RoundTrip serves only bundled sample feeds; it cannot dial any destination.
func (sampleTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	for _, f := range sampleFeeds {
		if r.Method == "GET" && r.URL.String() == sampleURL(f) {
			type item struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				GUID        string `xml:"guid"`
				Description string `xml:"description"`
				Date        string `xml:"pubDate"`
			}
			var doc struct {
				XMLName xml.Name `xml:"rss"`
				Title   string   `xml:"channel>title"`
				Items   []item   `xml:"channel>item"`
			}
			doc.Title = f.Title
			for _, e := range sampleEntries(f, time.Now().UTC()) {
				doc.Items = append(doc.Items, item{e.Title, e.URL, e.Key, e.Body, time.Unix(e.Published, 0).UTC().Format(time.RFC1123Z)})
			}
			b, err := xml.Marshal(doc)
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/rss+xml"}}, Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
		}
	}
	return nil, errors.New("this disposable demo refreshes only its offline sample feeds; use your own instance for live subscriptions")
}
