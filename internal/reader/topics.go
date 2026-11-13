// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Topics were a second, single-valued way to organise a source. Since schema
// 7 a source's tags are its only organisation: a topic, whether stored by an
// earlier version or supplied by an old export or an OPML folder, becomes the
// source's first tag. The category columns remain only so older databases and
// backups open; nothing reads them.

// topicTag normalises a legacy topic the way ParseTags normalises one tag.
// Commas would split the tag when edited, so they become spaces.
func topicTag(topic string) (string, error) {
	tag := strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(topic, ",", " ")), " "))
	if strings.ContainsAny(topic, "\r\n\x00") {
		return "", errors.New("topic must be at most 40 characters on one line")
	}
	if len([]rune(tag)) > 40 || strings.ContainsRune(tag, 0) {
		return "", errors.New("topic must be at most 40 characters on one line")
	}
	return tag, nil
}

// withTopic puts the topic first and keeps the other tags in order. A topic
// may become a thirteenth tag rather than dropping one the reader chose.
func withTopic(topic string, tags []string) ([]string, error) {
	tag, err := topicTag(topic)
	if err != nil || tag == "" {
		return tags, err
	}
	out := []string{tag}
	for _, t := range tags {
		if t != tag {
			out = append(out, t)
		}
	}
	if len(out) > maxSourceTags {
		return nil, errors.New("use at most 13 tags per source, including the topic")
	}
	return out, nil
}

// migrateTopicsToTags runs inside the schema transaction. Existing tags keep
// their order (alphabetical, as earlier versions listed them) after the topic.
func migrateTopicsToTags(ctx context.Context, tx *sql.Tx) error {
	for _, step := range []string{
		"ALTER TABLE subscription_tags ADD COLUMN position INTEGER NOT NULL DEFAULT 0 CHECK(position>=0)",
		"ALTER TABLE household_tags ADD COLUMN position INTEGER NOT NULL DEFAULT 0 CHECK(position>=0)",
	} {
		if _, err := tx.ExecContext(ctx, step); err != nil {
			return err
		}
	}
	type source struct {
		user  sql.NullInt64
		feed  int64
		topic string
	}
	var sources []source
	rows, err := tx.QueryContext(ctx, "SELECT user_id,feed_id,category FROM subscriptions UNION ALL SELECT NULL,feed_id,category FROM group_feeds")
	if err != nil {
		return err
	}
	for rows.Next() {
		var s source
		if err = rows.Scan(&s.user, &s.feed, &s.topic); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, s)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, s := range sources {
		household := !s.user.Valid
		q := "SELECT tag FROM subscription_tags WHERE user_id=? AND feed_id=? ORDER BY tag"
		args := []any{s.user.Int64, s.feed}
		if household {
			q = "SELECT tag FROM household_tags WHERE feed_id=? ORDER BY tag"
			args = []any{s.feed}
		}
		tags, err := scanTags(ctx, tx, q, args...)
		if err != nil {
			return err
		}
		tag, err := topicTag(s.topic)
		if err != nil {
			// Stored topics passed the same length and line checks, so this
			// cannot happen; keep the tags rather than fail the upgrade.
			tag = ""
		}
		ordered, _ := withTopic(tag, tags)
		if err = writeTags(ctx, tx, s.user.Int64, s.feed, ordered, household); err != nil {
			return err
		}
	}
	return nil
}

func scanTags(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err = rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}
