package reader

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

//go:embed migration_v2.sql
var migrationV2 string

//go:embed migration_v3.sql
var migrationV3 string

//go:embed migration_v4.sql
var migrationV4 string

//go:embed migration_v5.sql
var migrationV5 string

type Store struct {
	DB        *sql.DB
	lock      *flock.Flock
	Directory string
}

func (s *Store) Close() error { return errors.Join(s.DB.Close(), s.lock.Close()) }

type User struct {
	ID                int64
	Username          string
	Owner, GroupOptin bool
	Suspended         bool
	KeyboardShortcuts bool
}
type Feed struct {
	ID        int64    `json:"id"`
	URL       string   `json:"url"`
	Title     string   `json:"title"`
	Category  string   `json:"category"`
	ETag      string   `json:"-"`
	Modified  string   `json:"-"`
	Checked   int64    `json:"checked"`
	NextCheck int64    `json:"next_check,omitempty"`
	Error     string   `json:"error,omitempty"`
	Group     bool     `json:"group"`
	Tags      []string `json:"tags"`
}
type Filter struct {
	View, Query, Category string
	FeedID                int64
	Page                  int
	Before                int64
	GroupID               int64
}

func OpenStore(dir string) (store *Store, failure error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("data directory must be accessible only by its owner (mode 0700)")
	}
	lock := flock.New(filepath.Join(dir, ".instance.lock"), flock.SetPermissions(0600))
	locked, err := lock.TryRLock()
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("instance is locked for restore")
	}
	defer func() {
		if failure != nil {
			lock.Close()
		}
	}()
	file := filepath.Join(dir, "reader.db")
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(file, 0600); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: file}).String() + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version > 5 {
		db.Close()
		return nil, errors.New("database schema is newer than this application")
	}
	if _, err = tx.Exec(schema); err == nil && version < 2 {
		_, err = tx.Exec(migrationV2)
	}
	if err == nil && version < 3 {
		_, err = tx.Exec(migrationV3)
	}
	if err == nil && version < 4 {
		_, err = tx.Exec(migrationV4)
	}
	if err == nil {
		if version < 5 {
			_, err = tx.Exec(migrationV5)
		}
	}
	if err == nil {
		_, err = tx.Exec("PRAGMA user_version=5")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, lock: lock, Directory: dir}, nil
}

func category(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "General"
	}
	if len([]rune(s)) > 40 || strings.ContainsAny(s, "\r\n\x00") {
		return "", errors.New("topic must be 1–40 characters on one line")
	}
	return s, nil
}

// Subscriptions, including an entire OPML import, are committed together.
func (s *Store) Subscribe(ctx context.Context, uid int64, feeds []Feed, group bool) error {
	if len(feeds) > 200 {
		return errors.New("import at most 200 feeds at a time")
	}
	for i := range feeds {
		var err error
		feeds[i].URL, err = WebURL(feeds[i].URL)
		if err != nil {
			return err
		}
		feeds[i].Category, err = category(feeds[i].Category)
		if err != nil {
			return err
		}
		if len(feeds[i].Tags) == 0 {
			feeds[i].Tags = []string{feeds[i].Category}
		}
		feeds[i].Tags, err = ParseTags(strings.Join(feeds[i].Tags, ","))
		if err != nil {
			return err
		}
		feeds[i].Title = plainText(feeds[i].Title, 200)
		if feeds[i].Title == "" {
			u, _ := url.Parse(feeds[i].URL)
			feeds[i].Title = u.Hostname()
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if group {
		var owner bool
		if err = tx.QueryRowContext(ctx, "SELECT owner FROM users WHERE id=?", uid).Scan(&owner); err != nil {
			return err
		}
		if !owner {
			return errors.New("only an owner can change household sources")
		}
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM feeds").Scan(&count); err != nil {
		return err
	}
	newURLs := map[string]bool{}
	for _, f := range feeds {
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM feeds WHERE url=?", f.URL).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			newURLs[f.URL] = true
		}
	}
	if count+len(newURLs) > 2000 {
		return errors.New("instance feed limit reached (2000)")
	}
	for _, f := range feeds {
		if _, err = tx.ExecContext(ctx, "INSERT INTO feeds(url,title) VALUES(?,?) ON CONFLICT(url) DO NOTHING", f.URL, f.Title); err != nil {
			return err
		}
		var existed int
		table := "SELECT count(*) FROM subscriptions s JOIN feeds f ON f.id=s.feed_id WHERE s.user_id=? AND f.url=?"
		params := []any{uid, f.URL}
		if group {
			table = "SELECT count(*) FROM group_feeds s JOIN feeds f ON f.id=s.feed_id WHERE f.url=?"
			params = []any{f.URL}
		}
		if err = tx.QueryRowContext(ctx, table, params...).Scan(&existed); err != nil {
			return err
		}
		if group {
			_, err = tx.ExecContext(ctx, "INSERT INTO group_feeds(feed_id,category) SELECT id,? FROM feeds WHERE url=? ON CONFLICT(feed_id) DO NOTHING", f.Category, f.URL)
		} else {
			_, err = tx.ExecContext(ctx, "INSERT INTO subscriptions(user_id,feed_id,category) SELECT ?,id,? FROM feeds WHERE url=? ON CONFLICT(user_id,feed_id) DO NOTHING", uid, f.Category, f.URL)
		}
		if err != nil {
			return err
		}
		var fid int64
		if err = tx.QueryRowContext(ctx, "SELECT id FROM feeds WHERE url=?", f.URL).Scan(&fid); err != nil {
			return err
		}
		if existed == 0 {
			if err = writeTags(ctx, tx, uid, fid, f.Tags, group); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) Feeds(ctx context.Context, uid int64, group bool) ([]Feed, error) {
	return feedList(ctx, s.DB, uid, group)
}

func feedList(ctx context.Context, db queryer, uid int64, group bool) ([]Feed, error) {
	q := "SELECT f.id,f.url,f.title,s.category,f.etag,f.modified,f.checked,f.next_check,f.error,(SELECT json_group_array(tag) FROM subscription_tags t WHERE t.user_id=s.user_id AND t.feed_id=f.id) FROM feeds f JOIN subscriptions s ON s.feed_id=f.id WHERE s.user_id=? ORDER BY s.category,f.title"
	args := []any{uid}
	if group {
		q = "SELECT f.id,f.url,f.title,g.category,f.etag,f.modified,f.checked,f.next_check,f.error,(SELECT json_group_array(tag) FROM household_tags t WHERE t.feed_id=f.id) FROM feeds f JOIN group_feeds g ON g.feed_id=f.id ORDER BY g.category,f.title"
		args = nil
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Feed{}
	for rows.Next() {
		var f Feed
		f.Group = group
		var tags string
		if err = rows.Scan(&f.ID, &f.URL, &f.Title, &f.Category, &f.ETag, &f.Modified, &f.Checked, &f.NextCheck, &f.Error, &tags); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(tags), &f.Tags); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

const visible = `((e.import_owner IS NULL OR e.import_owner=st.user_id) AND (EXISTS(SELECT 1 FROM subscriptions sub WHERE sub.user_id=? AND sub.feed_id=e.feed_id) OR EXISTS(SELECT 1 FROM group_feeds g JOIN users u ON u.id=? WHERE u.group_optin=1 AND g.feed_id=e.feed_id)))`

func entryWhere(uid int64, f Filter) (string, []any) {
	where := visible
	args := []any{uid, uid}
	if f.View == "saved" {
		where = "(coalesce(st.saved,0)=1)"
		args = nil
	} else if f.View == "unread" || f.View == "" {
		where += " AND coalesce(st.read,0)=0"
	}
	if f.View == "group" {
		where += " AND EXISTS(SELECT 1 FROM group_feeds g WHERE g.feed_id=e.feed_id)"
	}
	if f.Query != "" {
		where += " AND instr(lower(e.title || ' ' || e.body),lower(?))>0"
		args = append(args, f.Query)
	}
	if f.FeedID > 0 {
		where += " AND e.feed_id=?"
		args = append(args, f.FeedID)
	}
	if f.Category != "" {
		where += ` AND (EXISTS(SELECT 1 FROM subscriptions sub WHERE sub.user_id=? AND sub.feed_id=e.feed_id AND sub.category=?) OR EXISTS(SELECT 1 FROM group_feeds g JOIN users u ON u.id=? WHERE u.group_optin=1 AND g.feed_id=e.feed_id AND g.category=?))`
		args = append(args, uid, f.Category, uid, f.Category)
	}
	if f.GroupID > 0 {
		where += " AND " + groupMatch
		args = append(args, f.GroupID, uid, uid)
	}
	if f.Before > 0 {
		where += " AND e.id<=?"
		args = append(args, f.Before)
	}
	return where, args
}

func (s *Store) Entries(ctx context.Context, uid int64, f Filter) ([]Entry, bool, error) {
	where, args := entryWhere(uid, f)
	q := `SELECT e.id,e.feed_id,coalesce(nullif(e.original_key,''),e.key),e.title,e.url,e.body,e.body_html,e.image_url,e.published,coalesce(st.read,0),coalesce(st.saved,0),f.title,coalesce((SELECT sub.category FROM subscriptions sub WHERE sub.user_id=? AND sub.feed_id=e.feed_id),(SELECT g.category FROM group_feeds g WHERE g.feed_id=e.feed_id),'General') FROM entries e JOIN feeds f ON f.id=e.feed_id LEFT JOIN states st ON st.entry_id=e.id AND st.user_id=? WHERE ` + where + ` ORDER BY e.published DESC,e.id DESC LIMIT 31 OFFSET ?`
	params := []any{uid, uid}
	params = append(params, args...)
	params = append(params, f.Page*30)
	rows, err := s.DB.QueryContext(ctx, q, params...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.FeedID, &e.Key, &e.Title, &e.URL, &e.Body, &e.BodyHTML, &e.ImageURL, &e.Published, &e.Read, &e.Saved, &e.Source, &e.Category); err != nil {
			return nil, false, err
		}
		out = append(out, e)
	}
	more := len(out) > 30
	if more {
		out = out[:30]
	}
	return out, more, rows.Err()
}

func (s *Store) Entry(ctx context.Context, uid, id int64) (Entry, error) {
	var e Entry
	q := `SELECT e.id,e.feed_id,coalesce(nullif(e.original_key,''),e.key),e.title,e.url,e.body,e.body_html,e.image_url,e.published,coalesce(st.read,0),coalesce(st.saved,0),f.title FROM entries e JOIN feeds f ON f.id=e.feed_id LEFT JOIN states st ON st.entry_id=e.id AND st.user_id=? WHERE e.id=? AND (` + visible + ` OR coalesce(st.saved,0)=1)`
	err := s.DB.QueryRowContext(ctx, q, uid, id, uid, uid).Scan(&e.ID, &e.FeedID, &e.Key, &e.Title, &e.URL, &e.Body, &e.BodyHTML, &e.ImageURL, &e.Published, &e.Read, &e.Saved, &e.Source)
	if err == nil {
		var body, rich string
		copyErr := s.DB.QueryRowContext(ctx, "SELECT body,body_html FROM article_copies WHERE user_id=? AND entry_id=?", uid, id).Scan(&body, &rich)
		if copyErr == nil {
			e.Body, e.BodyHTML, e.FullStory = body, rich, true
		} else if copyErr != sql.ErrNoRows {
			return Entry{}, copyErr
		}
		e.URL = CleanArticleURL(e.URL)
	}
	return e, err
}

func (s *Store) SetState(ctx context.Context, uid, id int64, field string, value bool) error {
	if field != "read" && field != "saved" {
		return errors.New("unknown reading state")
	}
	// The authorization and state write use one SQLite statement.
	q := `INSERT INTO states(user_id,entry_id,` + field + `) SELECT ?,e.id,? FROM entries e LEFT JOIN states st ON st.entry_id=e.id AND st.user_id=? WHERE e.id=? AND (` + visible + ` OR coalesce(st.saved,0)=1) ON CONFLICT(user_id,entry_id) DO UPDATE SET ` + field + `=excluded.` + field
	res, err := s.DB.ExecContext(ctx, q, uid, value, uid, id, uid, uid)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) MarkPage(ctx context.Context, uid int64, ids []int64) error {
	if len(ids) > 30 {
		return errors.New("mark at most one page at a time")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		res, e := tx.ExecContext(ctx, `INSERT INTO states(user_id,entry_id,read) SELECT ?,e.id,1 FROM entries e LEFT JOIN states st ON st.entry_id=e.id AND st.user_id=? WHERE e.id=? AND (`+visible+` OR coalesce(st.saved,0)=1) ON CONFLICT(user_id,entry_id) DO UPDATE SET read=1`, uid, uid, id, uid, uid)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return sql.ErrNoRows
		}
	}
	return tx.Commit()
}

func (s *Store) ApplyFetch(ctx context.Context, f Feed, r FetchResult) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	added := 0
	if !r.Unchanged {
		for _, e := range r.Feed.Entries {
			var exists int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM entries WHERE feed_id=? AND key=?", f.ID, e.Key).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				added++
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO entries(feed_id,key,title,url,body,body_html,image_url,published) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(feed_id,key) DO UPDATE SET title=excluded.title,url=excluded.url,body=excluded.body,body_html=excluded.body_html,image_url=excluded.image_url WHERE entries.import_owner IS NULL`, f.ID, e.Key, e.Title, e.URL, e.Body, e.BodyHTML, e.ImageURL, e.Published)
			if err != nil {
				return err
			}

			// When the publisher later supplies a restored story, move the
			// reader's markers onto that canonical entry, without publishing
			// any uploaded text. Existing canonical markers take precedence.
			if _, err = tx.ExecContext(ctx, `INSERT INTO states(user_id,entry_id,read,saved)
              SELECT st.user_id,real.id,st.read,st.saved FROM entries own
              JOIN states st ON st.entry_id=own.id AND st.user_id=own.import_owner
              JOIN entries real ON real.feed_id=own.feed_id AND real.key=own.original_key AND real.url=own.url AND real.import_owner IS NULL
              WHERE own.feed_id=? AND own.original_key=? AND own.url=?
              ON CONFLICT(user_id,entry_id) DO NOTHING`, f.ID, e.Key, e.URL); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM entries WHERE import_owner IS NOT NULL AND feed_id=? AND original_key=? AND url=? AND EXISTS(SELECT 1 FROM entries real WHERE real.feed_id=entries.feed_id AND real.key=entries.original_key AND real.url=entries.url AND real.import_owner IS NULL)`, f.ID, e.Key, e.URL); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE feeds SET title=?,etag=?,modified=? WHERE id=?`, r.Feed.Title, r.ETag, r.Modified, f.ID)
		if err != nil {
			return err
		}
	}
	err = saveSchedule(ctx, tx, f.ID, added, r.NotBefore)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RefreshFeeds(ctx context.Context, uid int64, all bool) ([]Feed, error) {
	q := `SELECT f.id,f.url,f.title,f.etag,f.modified FROM feeds f WHERE EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id AND s.user_id=?) OR EXISTS(SELECT 1 FROM group_feeds g JOIN users u ON u.id=? WHERE g.feed_id=f.id AND (u.owner=1 OR u.group_optin=1))`
	args := []any{uid, uid}
	if all {
		q = `SELECT f.id,f.url,f.title,f.etag,f.modified FROM feeds f WHERE EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id) OR EXISTS(SELECT 1 FROM group_feeds g WHERE g.feed_id=f.id)`
		args = nil
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feed
	for rows.Next() {
		var f Feed
		if err = rows.Scan(&f.ID, &f.URL, &f.Title, &f.ETag, &f.Modified); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) Unsubscribe(ctx context.Context, uid, id int64, group bool) error {
	q := "DELETE FROM subscriptions WHERE user_id=? AND feed_id=?"
	args := []any{uid, id}
	if group {
		q = "DELETE FROM group_feeds WHERE feed_id=? AND EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1)"
		args = []any{id, uid}
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Export struct {
	StorySources []Feed      `json:"story_sources,omitempty"`
	Version      int         `json:"version"`
	Username     string      `json:"username"`
	GroupOptin   bool        `json:"group_optin"`
	Feeds        []Feed      `json:"feeds"`
	GroupFeeds   []Feed      `json:"household_feeds,omitempty"`
	Stories      []Entry     `json:"stories"`
	Groups       []FeedGroup `json:"feed_groups"`
}

func (s *Store) Export(ctx context.Context, u User) (Export, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Export{}, err
	}
	defer tx.Rollback()
	out := Export{Version: 1, Username: u.Username, GroupOptin: u.GroupOptin, Stories: []Entry{}}
	out.Feeds, err = feedList(ctx, tx, u.ID, false)
	if err != nil {
		return out, err
	}
	if u.GroupOptin {
		out.GroupFeeds, err = feedList(ctx, tx, u.ID, true)
		if err != nil {
			return out, err
		}
	}
	out.Groups, err = groupList(ctx, tx, u)
	if err != nil {
		return out, err
	}
	// The export includes only personally marked stories and subscribed sources.
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.feed_id,coalesce(nullif(e.original_key,''),e.key),e.title,e.url,coalesce(c.body,e.body),coalesce(c.body_html,e.body_html),e.image_url,e.published,st.read,st.saved,f.title,c.entry_id IS NOT NULL FROM states st JOIN entries e ON e.id=st.entry_id JOIN feeds f ON f.id=e.feed_id LEFT JOIN article_copies c ON c.entry_id=e.id AND c.user_id=st.user_id WHERE st.user_id=? ORDER BY e.id`, u.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.FeedID, &e.Key, &e.Title, &e.URL, &e.Body, &e.BodyHTML, &e.ImageURL, &e.Published, &e.Read, &e.Saved, &e.Source, &e.FullStory); err != nil {
			return out, err
		}
		out.Stories = append(out.Stories, e)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	sourceRows, err := tx.QueryContext(ctx, `SELECT DISTINCT f.id,f.url,f.title FROM feeds f WHERE EXISTS(SELECT 1 FROM entries e JOIN states st ON st.entry_id=e.id WHERE e.feed_id=f.id AND st.user_id=?) OR EXISTS(SELECT 1 FROM feed_group_members gm JOIN feed_groups g ON g.id=gm.group_id WHERE gm.feed_id=f.id AND g.user_id=?) ORDER BY f.id`, u.ID, u.ID)
	if err != nil {
		return out, err
	}
	defer sourceRows.Close()
	for sourceRows.Next() {
		var f Feed
		if err = sourceRows.Scan(&f.ID, &f.URL, &f.Title); err != nil {
			return out, err
		}
		out.StorySources = append(out.StorySources, f)
	}
	if err = sourceRows.Err(); err != nil {
		return out, err
	}
	sourceRows.Close()
	return out, tx.Commit()
}

func (s *Store) String() string { return fmt.Sprintf("reader store %p", s.DB) }
