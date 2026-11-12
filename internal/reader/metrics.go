// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"context"
	"github.com/airencracken/comfylib/admin"
	"time"
)

// Metrics returns aggregate instance totals from one bounded read snapshot.
// Authorization is performed by the owner/admin HTTP handler before this call.
func (s *Store) Metrics(ctx context.Context, now time.Time) ([]admin.Metric, error) {
	return admin.Collect(ctx, s.DB, []admin.Query{
		{Label: "Members", SQL: "SELECT count(*) FROM users WHERE suspended=0", Detail: "", Bytes: false},
		{Label: "Sources", SQL: "SELECT count(*) FROM feeds", Detail: "", Bytes: false},
		{Label: "Subscriptions", SQL: "SELECT count(*) FROM subscriptions", Detail: "", Bytes: false},
		{Label: "Cached stories", SQL: "SELECT count(*) FROM entries", Detail: "", Bytes: false},
		{Label: "Sources with errors", SQL: "SELECT count(*) FROM feeds WHERE error<>''", Detail: "", Bytes: false},
		{Label: "Recent stories", SQL: "SELECT count(*) FROM entries WHERE published>=? AND published<=?", Detail: "Published in the last 7 days; this does not measure reading activity.", Bytes: false, Args: []any{now.Add(-7 * 24 * time.Hour).Unix(), now.Unix()}},
		{Label: "Database size", SQL: "SELECT (SELECT page_count FROM pragma_page_count)*(SELECT page_size FROM pragma_page_size)", Bytes: true, Detail: "Main database allocation; temporary logs, cached media and backups are separate."},
	})
}
