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

func (s *Store) AnalyticsDB() *sql.DB { return s.DB }
func (s *Store) Analytics(ctx context.Context, now time.Time) (analytics.Summary, error) {
	return analytics.Collect(ctx, s.DB, "SELECT id,username FROM users", now)
}

func (s *Store) LoginSession(ctx context.Context, id int64) (string, error) {
	raw, digest := token.New()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT INTO sessions(token,user_id,expires) SELECT ?,id,? FROM users WHERE id=? AND suspended=0", digest, time.Now().Add(30*24*time.Hour).Unix(), id)
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return "", fmt.Errorf("account unavailable")
	}
	if err := analytics.RecordLogin(ctx, tx, analytics.MemberID(id), time.Now()); err != nil {
		return "", err
	}
	return raw, tx.Commit()
}
