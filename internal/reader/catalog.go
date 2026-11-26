package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Suggestion struct {
	Title, URL, Category, Description string
	Language                          string
	Country                           string `json:",omitempty"`
	Area                              string `json:",omitempty"`
	Verified                          string `json:"verified"`
}

type CatalogCategory struct {
	Name    string
	Count   int
	Enabled bool
}

type catalogFilter struct {
	Query, Category, Language, Country string
	Unfollowed                         bool
	Page                               int
}

type catalogSource struct {
	Suggestion
	FollowedID int64
	Reason     string
}

type catalogSection struct {
	Name    string
	Count   int
	Sources []catalogSource
	HasMore bool
}

const (
	catalogPageSize   = 24
	localNewsCategory = "Local News"
)

func loadCatalog() ([]Suggestion, error) {
	data, err := assets.ReadFile("catalog.json")
	if err != nil {
		return nil, err
	}
	var catalog []Suggestion
	err = json.Unmarshal(data, &catalog)
	return catalog, err
}

func readCatalogFilter(values url.Values) (catalogFilter, error) {
	f := catalogFilter{
		Query:      strings.TrimSpace(values.Get("catalog_q")),
		Category:   strings.TrimSpace(values.Get("catalog_category")),
		Language:   strings.TrimSpace(values.Get("catalog_language")),
		Country:    strings.TrimSpace(values.Get("catalog_country")),
		Unfollowed: values.Get("catalog_unfollowed") == "1",
	}
	if len(f.Query) > 200 || len(f.Category) > 160 || len(f.Language) > 40 || len(f.Country) > 100 ||
		!utf8.ValidString(f.Country) || strings.ContainsFunc(f.Country, unicode.IsControl) {
		return catalogFilter{}, errors.New("invalid catalog filters")
	}
	for _, key := range []string{"catalog_q", "catalog_category", "catalog_language", "catalog_country", "catalog_unfollowed", "catalog_page"} {
		if len(values[key]) > 1 {
			return catalogFilter{}, errors.New("invalid catalog filters")
		}
	}
	if v := values.Get("catalog_unfollowed"); v != "" && v != "1" {
		return catalogFilter{}, errors.New("invalid catalog filters")
	}
	if !f.LocalNews() {
		f.Country = ""
	}
	if raw := values.Get("catalog_page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 || f.Category == "" {
			return catalogFilter{}, errors.New("invalid catalog page")
		}
		f.Page = n - 1
	}
	return f, nil
}

func (f catalogFilter) addTo(values url.Values) {
	if f.Query != "" {
		values.Set("catalog_q", f.Query)
	}
	if f.Category != "" {
		values.Set("catalog_category", f.Category)
	}
	if f.Language != "" {
		values.Set("catalog_language", f.Language)
	}
	if f.Country != "" {
		values.Set("catalog_country", f.Country)
	}
	if f.Unfollowed {
		values.Set("catalog_unfollowed", "1")
	}
	if f.Page > 0 {
		values.Set("catalog_page", strconv.Itoa(f.Page+1))
	}
}

func (f catalogFilter) Active() bool { return f != (catalogFilter{}) }

func (f catalogFilter) LocalNews() bool { return f.Category == localNewsCategory }

type catalogCardView struct {
	catalogSource
	CSRF       string
	GroupScope bool
	Filter     catalogFilter
}

func catalogLocation(f catalogFilter, group bool, notice string) string {
	values := url.Values{}
	f.addTo(values)
	if group {
		values.Set("scope", "household")
	}
	target := url.URL{Path: "/sources", RawQuery: values.Encode(), Fragment: "suggested-sources"}
	if notice != "" {
		values.Set("notice", notice)
		target.RawQuery = values.Encode()
	}
	return target.String()
}

func catalogLanguages(catalog []Suggestion, enabled map[string]bool) []string {
	languages := map[string]bool{}
	for _, item := range catalog {
		if enabled[item.Category] {
			languages[item.Language] = true
		}
	}
	result := make([]string, 0, len(languages))
	for language := range languages {
		result = append(result, language)
	}
	sort.Strings(result)
	return result
}

func localNewsCountries(catalog []Suggestion) []string {
	countries := map[string]bool{}
	for _, item := range catalog {
		if item.Category == localNewsCategory && item.Country != "" {
			countries[item.Country] = true
		}
	}
	result := make([]string, 0, len(countries))
	for country := range countries {
		result = append(result, country)
	}
	sort.Strings(result)
	return result
}

func catalogCategories(catalog []Suggestion, enabled map[string]bool) []CatalogCategory {
	counts := map[string]int{}
	for _, item := range catalog {
		counts[item.Category]++
	}
	out := make([]CatalogCategory, 0, len(counts))
	for name, count := range counts {
		out = append(out, CatalogCategory{Name: name, Count: count, Enabled: enabled[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func browseCatalog(catalog []Suggestion, enabled map[string]bool, followed []Feed, f catalogFilter) []catalogSection {
	followedIDs := map[string]int64{}
	for _, feed := range followed {
		followedIDs[feed.URL] = feed.ID
	}
	terms := strings.Fields(strings.ToLower(f.Query))
	requireCountry := f.Active() && (f.LocalNews() || f.Query == "")
	groups := map[string][]catalogSource{}
	for _, item := range catalog {
		if !enabled[item.Category] || (f.Category != "" && f.Category != item.Category) ||
			(f.Language != "" && f.Language != item.Language) || (f.Unfollowed && followedIDs[item.URL] != 0) {
			continue
		}
		if item.Category == localNewsCategory && requireCountry && (f.Country == "" || item.Country != f.Country) {
			continue
		}
		haystack := strings.ToLower(item.Title + " " + item.Category + " " + item.Description + " " + item.URL +
			" " + item.Area + " " + item.Country)
		if !matchesCatalogTerms(haystack, terms) {
			continue
		}
		groups[item.Category] = append(groups[item.Category], catalogSource{Suggestion: item, FollowedID: followedIDs[item.URL]})
	}
	out := make([]catalogSection, 0, len(groups))
	const previewSize = 4
	for name, sources := range groups {
		if len(terms) > 0 {
			sort.SliceStable(sources, func(i, j int) bool {
				return catalogTitleMatches(sources[i].Title, terms) > catalogTitleMatches(sources[j].Title, terms)
			})
		}
		count := len(sources)
		more := count > previewSize
		if f.Category != "" {
			offset := min(f.Page*catalogPageSize, count)
			end := min(offset+catalogPageSize, count)
			sources, more = sources[offset:end], count > end
		} else if more {
			sources = sources[:previewSize]
		}
		out = append(out, catalogSection{Name: name, Count: count, Sources: sources, HasMore: more})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func catalogTitleMatches(title string, terms []string) int {
	title = strings.ToLower(title)
	score := 0
	for _, term := range terms {
		if strings.Contains(title, term) {
			score++
		}
	}
	return score
}

func matchesCatalogTerms(text string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

func insertCatalogCategories(ctx context.Context, tx *sql.Tx, names []string) error {
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, "INSERT INTO catalog_categories(name) VALUES(?)", name); err != nil {
			return err
		}
	}
	return nil
}

func categoryNames(categories []CatalogCategory) []string {
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, category.Name)
	}
	return names
}

func seedCatalogCategories(ctx context.Context, tx *sql.Tx) error {
	catalog, err := loadCatalog()
	if err != nil {
		return err
	}
	return insertCatalogCategories(ctx, tx, categoryNames(catalogCategories(catalog, nil)))
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
	if err := insertCatalogCategories(ctx, tx, selected); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) catalogSettings(ctx context.Context) ([]CatalogCategory, error) {
	enabled, err := a.Store.CatalogCategories(ctx)
	if err != nil {
		return nil, err
	}
	return catalogCategories(a.Catalog, enabled), nil
}

func (a *App) saveSuggestions(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	available, err := a.catalogSettings(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	selected := r.PostForm["category"]
	if all := r.PostForm["enable_all"]; len(all) > 0 {
		if len(all) != 1 || all[0] != "true" || len(selected) != 0 {
			a.bad(w, r, "Choose categories or enable all")
			return
		}
		selected = categoryNames(available)
	}
	if err = a.Store.SetCatalogCategories(r.Context(), current(r).User.ID, selected, available); err != nil {
		a.bad(w, r, err.Error())
		return
	}
	http.Redirect(w, r, adminReturn(r, "?notice=suggestions#suggestions"), http.StatusSeeOther)
}
