package reader

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
)

type CatalogCategory struct {
	Name    string
	Count   int
	Enabled bool
}

func (s *Store) CatalogCategories(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT name FROM catalog_categories")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	enabled := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		enabled[name] = true
	}
	return enabled, rows.Err()
}

// Changes to instance suggestions are atomic and authorized in the transaction.
func (s *Store) SetCatalogCategories(ctx context.Context, actor int64, selected []string, available []CatalogCategory) error {
	allowed := map[string]bool{}
	for _, category := range available {
		allowed[category.Name] = true
	}
	if len(selected) > len(allowed) {
		return errors.New("choose each suggestion category at most once")
	}
	seen := map[string]bool{}
	for _, name := range selected {
		if !allowed[name] || seen[name] {
			return errors.New("choose only the listed suggestion categories, once each")
		}
		seen[name] = true
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner bool
	if err = tx.QueryRowContext(ctx, "SELECT owner FROM users WHERE id=? AND suspended=0", actor).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM catalog_categories"); err != nil {
		return err
	}
	for _, name := range selected {
		if _, err = tx.ExecContext(ctx, "INSERT INTO catalog_categories(name) VALUES(?)", name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *App) catalogSettings(ctx context.Context) ([]CatalogCategory, error) {
	enabled, err := a.Store.CatalogCategories(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, item := range a.Catalog {
		counts[item.Category]++
	}
	out := make([]CatalogCategory, 0, len(counts))
	for name, count := range counts {
		out = append(out, CatalogCategory{Name: name, Count: count, Enabled: enabled[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (a *App) saveSuggestions(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		http.Error(w, "Only owners can change suggested categories", http.StatusForbidden)
		return
	}
	available, err := a.catalogSettings(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err = a.Store.SetCatalogCategories(r.Context(), current(r).User.ID, r.PostForm["category"], available); err != nil {
		a.bad(w, r, err.Error())
		return
	}
	http.Redirect(w, r, adminReturn(r, "?notice=suggestions#suggestions"), http.StatusSeeOther)
}
