package reader

import (
	"context"
	"net/http"
	"sort"
	"time"
)

func (a *App) previewSource(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	group := scope(r)
	if group && !u.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	filter, err := readCatalogFilter(r.PostForm)
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	addresses := r.PostForm["url"]
	if len(addresses) != 1 || len(addresses[0]) > 2048 {
		a.bad(w, r, "Choose one suggested source")
		return
	}
	enabled, err := a.Store.CatalogCategories(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var source catalogSource
	for _, item := range a.Catalog {
		if item.URL == addresses[0] && enabled[item.Category] {
			source.Suggestion = item
			break
		}
	}
	if source.URL == "" {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	feeds, err := a.Store.Feeds(r.Context(), u.ID, group)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for _, feed := range feeds {
		if feed.URL == source.URL {
			source.FollowedID = feed.ID
			break
		}
	}
	p := page{
		Title: source.Title, Kind: "catalog-preview", GroupScope: group,
		CatalogPreview: source, CatalogFilter: filter, CatalogBack: catalogLocation(filter, group, ""),
	}
	select {
	case a.previewSlots <- struct{}{}:
		defer func() { <-a.previewSlots }()
	default:
		p.Error = "Feed previews are busy. Try again in a moment."
		a.render(w, r, p, http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	fetched, err := a.Fetcher.Fetch(ctx, source.URL, "", "")
	if err != nil {
		p.Error = "Tidings couldn’t load the publisher’s feed. You can still follow the source."
		a.render(w, r, p, http.StatusBadGateway)
		return
	}
	p.Entries = recentHeadlines(fetched.Feed.Entries, 5)
	a.render(w, r, p, http.StatusOK)
}

func recentHeadlines(entries []Entry, size int) []Entry {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Published > entries[j].Published })
	return entries[:min(len(entries), size)]
}
