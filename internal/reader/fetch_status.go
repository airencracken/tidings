package reader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type fetchSummary struct {
	Sources, Waiting, Failed int
	Checked                  int64
	NextCheck                int64
}

// Count only feeds this reader can fetch; other people's subscriptions and
// household feeds the reader has not opted into remain private.
func (s *Store) fetchSummary(ctx context.Context, uid int64) (fetchSummary, error) {
	var out fetchSummary
	err := s.DB.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(f.checked=0),0),coalesce(sum(f.error<>''),0),coalesce(max(f.checked),0),coalesce(min(CASE WHEN f.checked=0 THEN 0 ELSE max(f.next_check,f.checked+300) END),0)
	FROM feeds f WHERE EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id AND s.user_id=?)
	OR EXISTS(SELECT 1 FROM group_feeds g JOIN users u ON u.id=? WHERE g.feed_id=f.id AND (u.owner=1 OR u.group_optin=1))`, uid, uid).Scan(&out.Sources, &out.Waiting, &out.Failed, &out.Checked, &out.NextCheck)
	return out, err
}

type fetchOverview struct {
	State     string `json:"state"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	Checked   int64  `json:"checked"`
	NextCheck string `json:"next_check"`
}

func (a *App) fetchOverview(ctx context.Context, uid int64) (fetchOverview, error) {
	summary, err := a.Store.fetchSummary(ctx, uid)
	if err != nil {
		return fetchOverview{}, err
	}
	status := a.RefreshStatus(uid)
	out := fetchOverview{State: "idle", Checked: summary.Checked}
	if summary.NextCheck > 0 {
		out.NextCheck = "Next automatic check at " + time.Unix(summary.NextCheck, 0).UTC().Format("15:04 UTC") + "."
	} else if !status.NextCheck.IsZero() {
		out.NextCheck = "Next automatic check at " + status.NextCheck.UTC().Format("15:04 UTC") + "."
	}
	switch {
	case status.Queued:
		out.State, out.Message = "queued", "Your sources are queued for fetching."
	case status.Running:
		out.State, out.Message = "fetching", "Fetching your sources."
	case summary.Sources == 0:
		out.Message = "Follow a source to begin."
		out.NextCheck = ""
	case summary.Failed > 0:
		out.State, out.Message = "failed", "Some sources could not be fetched."
		out.Detail = fmt.Sprintf("%d of %d sources need another try. See Sources for details.", summary.Failed, summary.Sources)
	case summary.Waiting > 0:
		out.State, out.Message = "waiting", "Waiting for the first fetch."
		out.Detail = fmt.Sprintf("%d sources waiting.", summary.Waiting)
	case summary.Checked > 0:
		out.Message = "Feeds checked at " + time.Unix(summary.Checked, 0).UTC().Format("15:04 UTC") + "."
	}
	return out, nil
}

func (a *App) fetchStatus(w http.ResponseWriter, r *http.Request) {
	status, err := a.fetchOverview(r.Context(), current(r).User.ID)
	if err != nil {
		http.Error(w, "Could not check fetch status", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(status)
}
