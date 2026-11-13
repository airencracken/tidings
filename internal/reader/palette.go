// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/airencracken/comfylib/ui"
)

// defaultPalette is Tidings' own look: paper, ink and green.
const defaultPalette = ui.Sage

// SitePalette returns the owner's site palette, or Tidings' default when none
// is saved or the saved name is no longer a known palette.
func (s *Store) SitePalette(ctx context.Context) (ui.Palette, error) {
	var name string
	err := s.DB.QueryRowContext(ctx, "SELECT palette FROM site_palette WHERE id=1").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return ui.SiteDefault(name, defaultPalette), err
}

// SetSitePalette saves a palette chosen by an active owner.
func (s *Store) SetSitePalette(ctx context.Context, actor int64, name string) error {
	palette, err := ui.ParsePalette(name)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO site_palette(id,palette) SELECT 1,? WHERE EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1 AND suspended=0)
	 ON CONFLICT(id) DO UPDATE SET palette=excluded.palette`, string(palette), actor)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.Join(err, sql.ErrNoRows)
	}
	return nil
}

func (a *App) savePalette(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	if len(r.PostForm["palette"]) != 1 {
		a.bad(w, r, "Choose one colour palette")
		return
	}
	if err := a.Store.SetSitePalette(r.Context(), current(r).User.ID, r.PostFormValue("palette")); err != nil {
		if errors.Is(err, ui.ErrUnknownPalette) {
			a.bad(w, r, "Choose one of the listed colour palettes")
			return
		}
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin?notice=palette#site-palette", http.StatusSeeOther)
}
