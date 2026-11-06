package reader

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type FeedGroup struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Tag       string  `json:"tag,omitempty"`
	Household bool    `json:"household"`
	FeedIDs   []int64 `json:"feed_ids"`
}

func ParseTags(raw string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	if len(raw) > 1000 {
		return nil, errors.New("tags are too long")
	}
	for _, part := range strings.Split(raw, ",") {
		s := strings.ToLower(strings.TrimSpace(part))
		if s == "" {
			continue
		}
		if len([]rune(s)) > 40 || strings.ContainsAny(s, "\r\n\x00") {
			return nil, errors.New("each tag must be 1–40 characters on one line")
		}
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
		if len(out) > 12 {
			return nil, errors.New("use at most 12 tags per source")
		}
	}
	return out, nil
}

func writeTags(ctx context.Context, tx *sql.Tx, uid, fid int64, tags []string, household bool) error {
	q := "DELETE FROM subscription_tags WHERE user_id=? AND feed_id=?"
	args := []any{uid, fid}
	if household {
		q = "DELETE FROM household_tags WHERE feed_id=?"
		args = []any{fid}
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return err
	}
	for _, tag := range tags {
		q = "INSERT INTO subscription_tags(user_id,feed_id,tag) VALUES(?,?,?)"
		args = []any{uid, fid, tag}
		if household {
			q = "INSERT INTO household_tags(feed_id,tag) VALUES(?,?)"
			args = []any{fid, tag}
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetTags(ctx context.Context, u User, fid int64, raw string, household bool) error {
	tags, err := ParseTags(raw)
	if err != nil {
		return err
	}
	if household && !u.Owner {
		return sql.ErrNoRows
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := "SELECT count(*) FROM subscriptions WHERE user_id=? AND feed_id=?"
	args := []any{u.ID, fid}
	if household {
		q = "SELECT count(*) FROM group_feeds WHERE feed_id=? AND EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1)"
		args = []any{fid, u.ID}
	}
	var n int
	if err = tx.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if err = writeTags(ctx, tx, u.ID, fid, tags, household); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Groups(ctx context.Context, u User) ([]FeedGroup, error) {
	return groupList(ctx, s.DB, u)
}

func groupList(ctx context.Context, db queryer, u User) ([]FeedGroup, error) {
	rows, err := db.QueryContext(ctx, "SELECT id,name,tag,user_id IS NULL FROM feed_groups WHERE user_id=? OR (user_id IS NULL AND (? OR ?)) ORDER BY user_id IS NULL,name", u.ID, u.Owner, u.GroupOptin)
	if err != nil {
		return nil, err
	}
	out := []FeedGroup{}
	for rows.Next() {
		var g FeedGroup
		if err = rows.Scan(&g.ID, &g.Name, &g.Tag, &g.Household); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].FeedIDs = []int64{}
		rows, err = db.QueryContext(ctx, "SELECT feed_id FROM feed_group_members WHERE group_id=? ORDER BY feed_id", out[i].ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out[i].FeedIDs = append(out[i].FeedIDs, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) Group(ctx context.Context, u User, id int64) (FeedGroup, error) {
	groups, err := s.Groups(ctx, u)
	if err != nil {
		return FeedGroup{}, err
	}
	for _, g := range groups {
		if g.ID == id {
			return g, nil
		}
	}
	return FeedGroup{}, sql.ErrNoRows
}

func (s *Store) CreateGroup(ctx context.Context, u User, name, tag string, household bool) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 || strings.ContainsAny(name, "\r\n\x00") {
		return 0, errors.New("group name must be 1–60 characters on one line")
	}
	tags, err := ParseTags(tag)
	if err != nil {
		return 0, err
	}
	if len(tags) > 1 {
		return 0, errors.New("a tag-based group matches one tag")
	}
	tag = ""
	if len(tags) == 1 {
		tag = tags[0]
	}
	if household && !u.Owner {
		return 0, sql.ErrNoRows
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var owner any = u.ID
	if household {
		owner = nil
		var host bool
		if err = tx.QueryRowContext(ctx, "SELECT owner FROM users WHERE id=?", u.ID).Scan(&host); err != nil {
			return 0, err
		}
		if !host {
			return 0, sql.ErrNoRows
		}
	}
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM feed_groups WHERE user_id IS ?", owner).Scan(&n); err != nil {
		return 0, err
	}
	if n >= 100 {
		return 0, errors.New("use at most 100 feed groups per reading table")
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO feed_groups(user_id,name,tag) VALUES(?,?,?)", owner, name, tag)
	if err != nil {
		return 0, errors.New("a group with this name already exists")
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Store) SetGroupMembers(ctx context.Context, u User, id int64, ids []int64) error {
	if len(ids) > 2000 {
		return errors.New("too many feeds in this group")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var household bool
	var tag string
	err = tx.QueryRowContext(ctx, "SELECT user_id IS NULL,tag FROM feed_groups WHERE id=? AND (user_id=? OR (user_id IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1)))", id, u.ID, u.ID).Scan(&household, &tag)
	if err != nil {
		return err
	}
	if tag != "" {
		return errors.New("tag-based groups update automatically; change the source tags instead")
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM feed_group_members WHERE group_id=?", id); err != nil {
		return err
	}
	for _, fid := range ids {
		q := `INSERT OR IGNORE INTO feed_group_members(group_id,feed_id) SELECT ?,f.id FROM feeds f WHERE f.id=? AND (EXISTS(SELECT 1 FROM subscriptions sub WHERE sub.user_id=? AND sub.feed_id=f.id) OR EXISTS(SELECT 1 FROM group_feeds gf JOIN users u ON u.id=? WHERE u.group_optin=1 AND gf.feed_id=f.id))`
		args := []any{id, fid, u.ID, u.ID}
		if household {
			q = "INSERT OR IGNORE INTO feed_group_members(group_id,feed_id) SELECT ?,feed_id FROM group_feeds WHERE feed_id=?"
			args = []any{id, fid}
		}
		res, e := tx.ExecContext(ctx, q, args...)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			var duplicate int
			if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM feed_group_members WHERE group_id=? AND feed_id=?", id, fid).Scan(&duplicate); e != nil {
				return e
			}
			if duplicate == 0 {
				return sql.ErrNoRows
			}
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteGroup(ctx context.Context, u User, id int64) error {
	res, err := s.DB.ExecContext(ctx, "DELETE FROM feed_groups WHERE id=? AND (user_id=? OR (user_id IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1)))", id, u.ID, u.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

const groupMatch = `EXISTS(SELECT 1 FROM feed_groups fg WHERE fg.id=? AND (fg.user_id=? OR (fg.user_id IS NULL AND EXISTS(SELECT 1 FROM users gu WHERE gu.id=? AND gu.group_optin=1))) AND ((fg.tag='' AND EXISTS(SELECT 1 FROM feed_group_members fm WHERE fm.group_id=fg.id AND fm.feed_id=e.feed_id)) OR (fg.tag<>'' AND ((fg.user_id IS NOT NULL AND EXISTS(SELECT 1 FROM subscription_tags t WHERE t.user_id=fg.user_id AND t.feed_id=e.feed_id AND t.tag=fg.tag)) OR (fg.user_id IS NULL AND EXISTS(SELECT 1 FROM household_tags t WHERE t.feed_id=e.feed_id AND t.tag=fg.tag))))))`
