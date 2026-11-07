package reader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const minPoll = 5 * time.Minute
const maxPoll = 24 * time.Hour
const initialPoll = 30 * time.Minute

func pollDelay(previous time.Duration, checked, now int64, added int) time.Duration {
	if previous < minPoll || previous > maxPoll {
		previous = initialPoll
	}
	if checked == 0 {
		return previous
	}
	interval := previous * 2
	if added > 0 {
		elapsed := time.Duration(max(int64(0), min(int64(maxPoll/time.Second), now-checked))) * time.Second
		target := max(minPoll, min(maxPoll, elapsed/time.Duration(added)))
		interval = (previous + target) / 2
	}
	return max(minPoll, min(maxPoll, interval))
}

func publisherDelay(header http.Header, now time.Time) time.Time {
	var until time.Time
	for _, part := range strings.Split(header.Get("Cache-Control"), ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(name, "max-age") {
			if seconds, err := strconv.ParseInt(strings.Trim(value, "\""), 10, 64); err == nil && seconds > 0 {
				until = now.Add(time.Duration(min(seconds, int64(maxPoll/time.Second))) * time.Second)
			}
		}
	}
	if expires, err := http.ParseTime(header.Get("Expires")); err == nil && expires.After(until) {
		until = expires
	}
	if until.After(now.Add(maxPoll)) {
		until = now.Add(maxPoll)
	}
	return until
}

func retryAfter(raw string, now time.Time) time.Time {
	var until time.Time
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
		until = now.Add(time.Duration(min(seconds, int64(maxPoll/time.Second))) * time.Second)
	} else if date, err := http.ParseTime(raw); err == nil {
		until = date
	}
	if until.Before(now) {
		return time.Time{}
	}
	if until.After(now.Add(maxPoll)) {
		return now.Add(maxPoll)
	}
	return until
}

type publisherError struct {
	Status int
	Retry  time.Time
}

func (e *publisherError) Error() string { return fmt.Sprintf("publisher returned HTTP %d", e.Status) }

func (s *Store) recordFetchFailure(ctx context.Context, id int64, failure error, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var failures int
	if err = tx.QueryRowContext(ctx, "SELECT failures FROM feeds WHERE id=?", id).Scan(&failures); err != nil {
		return err
	}
	failures = min(failures+1, 20)
	delay := min(6*time.Hour, minPoll*time.Duration(1<<min(failures-1, 7)))
	next := now.Add(delay)
	var upstream *publisherError
	if errors.As(failure, &upstream) && upstream.Retry.After(next) {
		next = upstream.Retry
	}
	_, err = tx.ExecContext(ctx, "UPDATE feeds SET checked=?,error=?,failures=?,next_check=? WHERE id=?", now.Unix(), plainText(failure.Error(), 300), failures, next.Unix(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func fairFeedOrder(feeds []Feed) []Feed {
	byHost := map[string][]Feed{}
	hosts := []string{}
	for _, f := range feeds {
		u, _ := url.Parse(f.URL)
		host := u.Hostname()
		if _, ok := byHost[host]; !ok {
			hosts = append(hosts, host)
		}
		byHost[host] = append(byHost[host], f)
	}
	out := make([]Feed, 0, len(feeds))
	for len(out) < len(feeds) {
		for _, host := range hosts {
			if len(byHost[host]) > 0 {
				out = append(out, byHost[host][0])
				byHost[host] = byHost[host][1:]
			}
		}
	}
	return out
}

func (a *App) refreshSource(f Feed, all, force bool, gate chan struct{}) {
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-a.ctx.Done():
		return
	}
	var checked, next int64
	if err := a.Store.DB.QueryRowContext(a.ctx, "SELECT checked,next_check FROM feeds WHERE id=?", f.ID).Scan(&checked, &next); err != nil {
		return
	}
	if !force && time.Since(time.Unix(checked, 0)) < 5*time.Minute {
		return
	}
	if all && !force && next > time.Now().Unix() {
		return
	}
	result, err := a.Fetcher.Fetch(a.ctx, f.URL, f.ETag, f.Modified)
	if err == nil {
		err = a.Store.ApplyFetch(a.ctx, f, result)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		if failure := a.Store.recordFetchFailure(a.ctx, f.ID, err, time.Now()); failure != nil && !errors.Is(failure, context.Canceled) {
			slog.Error("record refresh failure", "error", failure)
		}
	}
}

// Kept small enough for a transaction to use the same schedule state as its
// article upserts. A failed upsert never advances the next check.
func saveSchedule(ctx context.Context, tx *sql.Tx, id int64, added int, notBefore time.Time) error {
	var checked, interval int64
	if err := tx.QueryRowContext(ctx, "SELECT checked,poll_interval FROM feeds WHERE id=?", id).Scan(&checked, &interval); err != nil {
		return err
	}
	now := time.Now()
	delay := pollDelay(time.Duration(interval)*time.Second, checked, now.Unix(), added)
	next := now.Add(delay)
	if notBefore.After(next) {
		next = notBefore
	}
	if next.After(now.Add(maxPoll)) {
		next = now.Add(maxPoll)
	}
	_, err := tx.ExecContext(ctx, "UPDATE feeds SET checked=?,error='',failures=0,poll_interval=?,next_check=? WHERE id=?", now.Unix(), int64(delay/time.Second), next.Unix(), id)
	return err
}
