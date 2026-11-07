package reader

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Store) ImagesEnabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.DB.QueryRowContext(ctx, "SELECT images_enabled FROM instance_options WHERE id=1").Scan(&enabled)
	return enabled, err
}

func (s *Store) SetImagesEnabled(ctx context.Context, actor int64, enabled bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before bool
	if err = tx.QueryRowContext(ctx, "SELECT images_enabled FROM instance_options WHERE id=1").Scan(&before); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE instance_options SET images_enabled=? WHERE id=1 AND EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1 AND suspended=0)`, enabled, actor)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	// Older parser versions did not store image addresses. A conditional 304
	// cannot fill those fields; request full feed bodies when images turn on.
	if enabled && !before {
		if _, err = tx.ExecContext(ctx, "UPDATE feeds SET etag='',modified='',checked=0"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *App) saveImages(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		http.Error(w, "Only owners can change feed images", http.StatusForbidden)
		return
	}
	values := r.PostForm["enabled"]
	if len(values) > 1 || len(values) == 1 && values[0] != "true" {
		a.bad(w, r, "Choose whether to show feed images")
		return
	}
	if err := a.Store.SetImagesEnabled(r.Context(), current(r).User.ID, len(values) == 1); err != nil {
		a.fail(w, r, err)
		return
	}
	if len(values) == 1 {
		a.queueRefresh(0, true, true, true)
	}
	http.Redirect(w, r, "/account?notice=images#feed-images", http.StatusSeeOther)
}

func (a *App) storyImage(w http.ResponseWriter, r *http.Request) {
	enabled, err := a.Store.ImagesEnabled(r.Context())
	if err != nil {
		http.Error(w, "Images are unavailable", http.StatusServiceUnavailable)
		return
	}
	if !enabled {
		http.NotFound(w, r)
		return
	}
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	entry, err := a.Store.Entry(r.Context(), current(r).User.ID, id)
	if errors.Is(err, sql.ErrNoRows) || err == nil && entry.ImageURL == "" {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Image unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	data, err := a.images.get(ctx, entry.ImageURL, a.Fetcher.Client)
	if err != nil {
		http.Error(w, "Image unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "preview.jpg", time.Time{}, bytes.NewReader(data))
}
