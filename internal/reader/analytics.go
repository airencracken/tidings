// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/airencracken/comfylib/analytics"
	"github.com/airencracken/comfylib/token"
	"time"
)

// Tidings keeps operational health metrics only. It does not record who
// signed in or which stories anyone opened.
func (s *Store) AnalyticsDB() *sql.DB { return s.DB }
func (s *Store) Analytics(ctx context.Context, now time.Time) (analytics.Summary, error) {
	return analytics.Collect(ctx, s.DB, "SELECT id,username FROM users", now)
}

// forgetMemberActivity deletes sign-in and story-open rows that earlier
// releases stored per account. It runs on every open, so a database restored
// from an older backup is cleaned too; nothing writes these rows any more.
func forgetMemberActivity(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "DELETE FROM comfy_analytics_activity")
	return err
}

func (s *Store) LoginSession(ctx context.Context, id int64) (string, error) {
	raw, digest := token.New()
	result, err := s.DB.ExecContext(ctx, "INSERT INTO sessions(token,user_id,expires) SELECT ?,id,? FROM users WHERE id=? AND suspended=0", digest, time.Now().Add(30*24*time.Hour).Unix(), id)
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return "", fmt.Errorf("account unavailable")
	}
	return raw, nil
}
