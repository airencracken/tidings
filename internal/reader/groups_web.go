package reader

import (
	"database/sql"
	"net/http"
	"strconv"
)

func (a *App) groups(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	gs, err := a.Store.Groups(r.Context(), u)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, page{Title: "Feed groups", Kind: "groups", Groups: gs}, 200)
}
func (a *App) createGroup(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	household := scope(r)
	if household && !u.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	if r.PostFormValue("kind") != "explicit" && r.PostFormValue("kind") != "tag" {
		a.bad(w, r, "Choose an explicit or tag-based group")
		return
	}
	tag := ""
	if r.PostFormValue("kind") == "tag" {
		tag = r.PostFormValue("tag")
		tags, err := ParseTags(tag)
		if err != nil || len(tags) != 1 {
			a.bad(w, r, "Choose one tag for this group")
			return
		}
	}
	id, err := a.Store.CreateGroup(r.Context(), u, r.PostFormValue("name"), tag, household)
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(id, 10), 303)
}
func (a *App) group(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	id, err := pathID(r)
	if err != nil {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	g, err := a.Store.Group(r.Context(), u, id)
	if err == sql.ErrNoRows {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := page{Title: g.Name, Kind: "collection", Collection: g, GroupScope: g.Household}
	p.Feeds, err = a.Store.Feeds(r.Context(), u.ID, g.Household)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !g.Household && u.GroupOptin {
		shared, err := a.Store.Feeds(r.Context(), u.ID, true)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		seen := map[int64]bool{}
		for _, f := range p.Feeds {
			seen[f.ID] = true
		}
		for _, f := range shared {
			if !seen[f.ID] {
				p.Feeds = append(p.Feeds, f)
			}
		}
	}
	a.render(w, r, p, 200)
}
func (a *App) saveGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	var ids []int64
	for _, raw := range r.PostForm["feed"] {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 1 {
			a.bad(w, r, "Invalid feed choice")
			return
		}
		ids = append(ids, n)
	}
	err = a.Store.SetGroupMembers(r.Context(), current(r).User, id, ids)
	if err == sql.ErrNoRows {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(id, 10), 303)
}
func (a *App) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	err = a.Store.DeleteGroup(r.Context(), current(r).User, id)
	if err == sql.ErrNoRows {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/groups", 303)
}
func (a *App) sourceTags(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	household := scope(r)
	if household && !u.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	id, err := pathID(r)
	if err != nil {
		a.problem(w, r, http.StatusNotFound, "", nil)
		return
	}
	if err = a.Store.SetTags(r.Context(), u, id, r.PostFormValue("tags"), household); err != nil {
		if err == sql.ErrNoRows {
			a.problem(w, r, http.StatusNotFound, "", nil)
		} else {
			a.bad(w, r, err.Error())
		}
		return
	}
	to := "/sources"
	if household {
		to += "?scope=household"
	}
	http.Redirect(w, r, to, 303)
}
