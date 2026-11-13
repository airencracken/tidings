package reader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxRestoreBytes = 16 << 20

func DecodeExport(b []byte) (Export, error) {
	var data Export
	if len(b) > MaxRestoreBytes {
		return data, errors.New("reading-data file must be 16 MB or smaller")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&data); err != nil {
		return data, errors.New("invalid reading-data JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return data, errors.New("unexpected data after JSON")
	}
	return data, validateExport(data)
}
func validateExport(data Export) error {
	if data.Version != 1 {
		return errors.New("unsupported reading-data version")
	}
	if len(data.Feeds)+len(data.GroupFeeds) > 4000 || len(data.StorySources) > 2000 || len(data.Stories) > 10000 || len(data.Groups) > 100 {
		return errors.New("reading-data file has too many sources, stories, or groups")
	}
	sources := map[int64]string{}
	urls := map[string]int64{}
	for _, list := range [][]Feed{data.Feeds, data.GroupFeeds, data.StorySources} {
		for _, f := range list {
			address, err := WebURL(f.URL)
			if err != nil {
				return err
			}
			if f.ID < 1 {
				return errors.New("invalid source ID")
			}
			if old, ok := sources[f.ID]; ok && old != address {
				return errors.New("conflicting source IDs")
			}
			if id, ok := urls[address]; ok && id != f.ID {
				return errors.New("duplicate source address with different IDs")
			}
			sources[f.ID] = address
			urls[address] = f.ID
			if _, err = category(f.Category); err != nil {
				return err
			}
			if _, err = ParseTags(strings.Join(f.Tags, ",")); err != nil {
				return err
			}
			if len(f.Title) > 1000 {
				return errors.New("source title is too long")
			}
		}
	}
	seen := map[string]bool{}
	for _, e := range data.Stories {
		if sources[e.FeedID] == "" {
			return errors.New("a story has no source; export it again with a current Tidings version")
		}
		if _, err := WebURL(e.URL); err != nil {
			return err
		}
		if e.Key == "" || len(e.Key) > 4096 || len(e.Title) > 2000 || len([]rune(e.Body)) > 64000 || !utf8.ValidString(e.Body) || len(e.BodyHTML) > maxArticleHTML || !utf8.ValidString(e.BodyHTML) || (e.Published < -62135596800 || e.Published > 253402300799) {
			return errors.New("invalid or oversized story")
		}
		key := fmt.Sprintf("%d:%s", e.FeedID, e.Key)
		if seen[key] {
			return errors.New("duplicate story key")
		}
		seen[key] = true
	}
	names := map[string]bool{}
	for _, g := range data.Groups {
		if len(g.FeedIDs) > 2000 {
			return errors.New("too many feeds in a group")
		}
		if g.Household {
			continue
		}
		if g.Name == "" || strings.TrimSpace(g.Name) != g.Name || len([]rune(g.Name)) > 60 || strings.ContainsAny(g.Name, "\r\n\x00") || names[g.Name] {
			return errors.New("invalid or duplicate group name")
		}
		names[g.Name] = true
		tags, err := ParseTags(g.Tag)
		if err != nil || len(tags) > 1 {
			return errors.New("invalid group tag")
		}
		for _, id := range g.FeedIDs {
			if sources[id] == "" {
				return errors.New("a group references an unknown source")
			}
		}
	}
	return nil
}

// RestorePersonal merges a complete validated document in one transaction. Existing
// subscriptions, group definitions and marked states win over imported values.
// Household choices and shared content are never changed by a personal import.
func (s *Store) RestorePersonal(ctx context.Context, u User, data Export) error {
	if err := validateExport(data); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := map[int64]int64{}
	subscribed := map[int64]bool{}
	for _, list := range [][]Feed{data.Feeds, data.GroupFeeds, data.StorySources} {
		for _, f := range list {
			if ids[f.ID] != 0 {
				continue
			}
			address, _ := WebURL(f.URL)
			if _, err = tx.ExecContext(ctx, "INSERT INTO feeds(url,title) VALUES(?,?) ON CONFLICT(url) DO NOTHING", address, plainText(f.Title, 200)); err != nil {
				return err
			}
			var id int64
			if err = tx.QueryRowContext(ctx, "SELECT id FROM feeds WHERE url=?", address).Scan(&id); err != nil {
				return err
			}
			ids[f.ID] = id
		}
	}
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM feeds").Scan(&n); err != nil {
		return err
	}
	if n > 2000 {
		return errors.New("instance feed limit reached (2000)")
	}
	for _, f := range data.Feeds {
		id := ids[f.ID]
		topic, _ := category(f.Category)
		res, e := tx.ExecContext(ctx, "INSERT INTO subscriptions(user_id,feed_id,category) VALUES(?,?,?) ON CONFLICT(user_id,feed_id) DO NOTHING", u.ID, id, topic)
		if e != nil {
			return e
		}
		count, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if count > 0 {
			tags, _ := ParseTags(strings.Join(f.Tags, ","))
			if err = writeTags(ctx, tx, u.ID, id, tags, false); err != nil {
				return err
			}
		}
		subscribed[id] = true
	}
	for _, e := range data.Stories {
		fid := ids[e.FeedID]
		var id int64
		err = tx.QueryRowContext(ctx, "SELECT id FROM entries WHERE feed_id=? AND key=? AND url=? AND import_owner IS NULL", fid, e.Key, e.URL).Scan(&id)
		if err == sql.ErrNoRows {
			key := fmt.Sprintf("restore:%d:%x", u.ID, sha256.Sum256([]byte(e.Key)))
			if _, err = tx.ExecContext(ctx, `INSERT INTO entries(feed_id,key,original_key,import_owner,title,url,body,body_html,published) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(feed_id,key) DO NOTHING`, fid, key, e.Key, u.ID, plainText(e.Title, 500), e.URL, e.Body, articleHTML(e.BodyHTML, e.URL, func(address string) string { return address }), e.Published); err != nil {
				return err
			}
			if err = tx.QueryRowContext(ctx, "SELECT id FROM entries WHERE feed_id=? AND key=? AND import_owner=?", fid, key, u.ID).Scan(&id); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		state, err := tx.ExecContext(ctx, "INSERT INTO states(user_id,entry_id,read,saved,skipped) VALUES(?,?,?,?,?) ON CONFLICT(user_id,entry_id) DO NOTHING", u.ID, id, e.Read, e.Saved, e.Skipped && !e.Saved)
		if err != nil {
			return err
		}
		inserted, err := state.RowsAffected()
		if err != nil {
			return err
		}
		if inserted > 0 && e.FullStory {
			_, err = tx.ExecContext(ctx, "INSERT INTO article_copies(user_id,entry_id,body,body_html,fetched) VALUES(?,?,?,?,?) ON CONFLICT(user_id,entry_id) DO NOTHING", u.ID, id, e.Body, articleHTML(e.BodyHTML, e.URL, func(address string) string { return address }), time.Now().Unix())
			if err != nil {
				return err
			}
		}
	}
	for _, g := range data.Groups {
		if g.Household {
			continue
		}
		tag := ""
		tags, _ := ParseTags(g.Tag)
		if len(tags) > 0 {
			tag = tags[0]
		}
		res, e := tx.ExecContext(ctx, "INSERT INTO feed_groups(user_id,name,tag) VALUES(?,?,?) ON CONFLICT(user_id,name) DO NOTHING", u.ID, g.Name, tag)
		if e != nil {
			return e
		}
		count, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if count == 0 {
			continue
		}
		id, e := res.LastInsertId()
		if e != nil {
			return e
		}
		for _, oldID := range g.FeedIDs {
			fid := ids[oldID]
			if !subscribed[fid] {
				continue
			}
			if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO feed_group_members(group_id,feed_id) VALUES(?,?)", id, fid); err != nil {
				return err
			}
		}
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM feed_groups WHERE user_id=?", u.ID).Scan(&n); err != nil {
		return err
	}
	if n > 100 {
		return errors.New("use at most 100 feed groups per reading table")
	}
	return tx.Commit()
}
func (a *App) restorePersonal(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("confirm") != "merge" {
		a.bad(w, r, "Confirm that you want to merge this file into your reading data")
		return
	}
	file, _, err := r.FormFile("reading_data")
	if err != nil {
		a.bad(w, r, "Choose a Tidings JSON export")
		return
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, MaxRestoreBytes+1))
	if err != nil {
		a.bad(w, r, "Could not read this file")
		return
	}
	data, err := DecodeExport(b)
	if err == nil {
		err = a.Store.RestorePersonal(r.Context(), current(r).User, data)
	}
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/account?notice=restored", 303)
}
