// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"context"
	"fmt"
	"github.com/airencracken/comfylib/analytics"
	"github.com/airencracken/comfylib/token"
	"time"
)

// Analytics returns site health for the owner dashboard: requests, errors and
// response times per route, and security counters. Nothing names an account
// or a story.
func (s *Store) Analytics(ctx context.Context, now time.Time) (analytics.Summary, error) {
	return analytics.Collect(ctx, s.DB, "", now)
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
