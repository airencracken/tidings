package reader

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/airencracken/comfylib/autobackup"
	"github.com/airencracken/comfylib/clientip"
	"github.com/airencracken/comfylib/token"
	"github.com/airencracken/comfylib/ui"
)

//go:embed templates/*.html static/* catalog.json
var assets embed.FS

type Config struct {
	TrustedProxies clientip.Resolver
	Backups        *autobackup.Runner
	SecureCookies  bool
	BaseURL        string
	Demo           bool
}
type App struct {
	Store               *Store
	Fetcher             *Fetcher
	images              *imageCache
	articleSlots        chan struct{}
	config              Config
	templates           *template.Template
	mux                 *http.ServeMux
	limit               limiter
	refreshMu           sync.Mutex
	refreshRunning      bool
	refreshPending      map[int64]bool
	refreshAll          bool
	refreshForce        bool
	refreshCurrent      int64
	refreshCurrentAll   bool
	refreshCurrentForce bool
	refreshNextCheck    time.Time
	ctx                 context.Context
	wg                  sync.WaitGroup
	Catalog             []Suggestion
}
type Suggestion struct {
	Title, URL, Category, Description string
	Verified                          string `json:"verified"`
}
type requestContext struct {
	User          User
	Session, CSRF string
}
type contextKey struct{}
type page struct {
	Title, Kind, Error, Notice, CSRF, ReturnTo, Invite string
	User                                               User
	Feeds, GroupFeeds                                  []Feed
	Entries                                            []Entry
	Story                                              Entry
	Filter                                             Filter
	More                                               bool
	Previous, Next                                     string
	Topics                                             []string
	Suggestions                                        []Suggestion
	GroupScope                                         bool
	Date                                               string
	Groups                                             []FeedGroup
	Collection                                         FeedGroup
	Refresh                                            RefreshStatus
	Fetch                                              fetchOverview
	ImagesEnabled                                      bool
	Backup                                             autobackup.Status
	InviteRef                                          string
	Members                                            []User
	Invitations                                        []Invitation
	Discovered                                         []DiscoveredFeed
	CatalogQuery                                       string
	CatalogCategories                                  []CatalogCategory
	SuggestionsEnabled                                 bool
	Demo                                               bool
}

func NewApp(ctx context.Context, s *Store, c Config) (*App, error) {
	a := &App{Store: s, Fetcher: NewFetcher(), images: newImageCache(s.Directory), articleSlots: make(chan struct{}, 2), config: c, ctx: ctx, mux: http.NewServeMux()}
	funcs := template.FuncMap{
		"article":   renderArticle,
		"cleanLink": CleanArticleURL,
		"date":      func(n int64) string { return time.Unix(n, 0).UTC().Format("Jan 2, 2006") },
		"checked": func(n int64) string {
			if n == 0 {
				return "Not fetched yet"
			}
			return time.Unix(n, 0).UTC().Format("Jan 2, 15:04 UTC")
		},
		"excerpt": func(s string) string {
			r := []rune(s)
			if len(r) > 240 {
				return string(r[:240]) + "…"
			}
			return s
		},
		"host": func(s string) string {
			u, _ := url.Parse(s)
			if u == nil {
				return s
			}
			return u.Hostname()
		},

		"joinTags": func(tags []string) string { return strings.Join(tags, ", ") },
		"containsID": func(ids []int64, id int64) bool {
			for _, n := range ids {
				if n == id {
					return true
				}
			}
			return false
		},
	}
	var err error
	a.templates, err = template.New("pages").Funcs(funcs).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	data, err := assets.ReadFile("catalog.json")
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &a.Catalog); err != nil {
		return nil, err
	}
	a.routes()
	return a, nil
}

func current(r *http.Request) requestContext {
	v, _ := r.Context().Value(contextKey{}).(requestContext)
	return v
}
func (a *App) cookieName() string {
	if a.config.SecureCookies {
		return "__Host-tidings"
	}
	return "tidings"
}
func (a *App) cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: a.config.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	if a.config.SecureCookies {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	if r.URL.Path == "/healthz" {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if err := a.Store.DB.PingContext(r.Context()); err != nil {
			http.Error(w, "Unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/favicon.ico" {
		a.mux.ServeHTTP(w, r)
		return
	}
	var rc requestContext
	if c, err := r.Cookie(a.cookieName()); err == nil {
		rc.User, err = a.Store.Session(r.Context(), c.Value)
		if err != nil && err != sql.ErrNoRows {
			a.fail(w, r, err)
			return
		}
		if err == nil {
			rc.Session = c.Value
		}
	}
	public := r.URL.Path == "/login" || r.URL.Path == "/join"
	if rc.User.ID == 0 && !public {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if rc.Session == "" {
		name := a.cookieName() + "-form"
		c, err := r.Cookie(name)
		if err == nil && len(c.Value) == 64 {
			rc.Session = c.Value
		} else {
			rc.Session, _ = token.New()
			a.cookie(w, name, rc.Session, 3600)
		}
	}
	rc.CSRF = token.SessionCSRF(rc.Session, "tidings:forms")
	if r.Method != "GET" && r.Method != "HEAD" {
		formLimit := int64(2 << 20)
		if r.URL.Path == "/account/restore" {
			formLimit = MaxRestoreBytes + (64 << 10)
		}
		r.Body = http.MaxBytesReader(w, r.Body, formLimit)
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(formLimit); err != nil {
				http.Error(w, "Invalid or oversized form", 400)
				return
			}
			if r.MultipartForm != nil {
				defer r.MultipartForm.RemoveAll()
			}
		} else if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid or oversized form", 400)
			return
		}
		if !token.Equal(r.PostFormValue("csrf"), rc.CSRF) {
			http.Error(w, "This form has expired. Reload the page and try again.", 403)
			return
		}
	}
	r = r.WithContext(context.WithValue(r.Context(), contextKey{}, rc))
	http.NewCrossOriginProtection().Handler(a.mux).ServeHTTP(w, r)
}

func (a *App) routes() {
	a.mux.HandleFunc("POST /account/keyboard", a.saveKeyboard)
	a.mux.HandleFunc("POST /story/{id}/fetch", a.fetchArticle)
	a.mux.HandleFunc("GET /story/{id}/image/{key}", a.storyImage)
	a.mux.HandleFunc("GET /api/v1/fetch-status", a.fetchStatus)
	a.mux.HandleFunc("GET /story/{id}/image", a.storyImage)
	a.mux.HandleFunc("POST /account/images", a.saveImages)
	a.mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		http.ServeFileFS(w, r, assets, "static/favicon.ico")
	})
	a.mux.Handle("GET /static/comfy/", http.StripPrefix("/static/comfy/", http.FileServerFS(ui.Assets())))
	a.mux.Handle("GET /static/", http.FileServerFS(assets))
	a.mux.HandleFunc("GET /{$}", a.home)
	a.mux.HandleFunc("GET /story/{id}", a.story)
	a.mux.HandleFunc("POST /story/{id}/state", a.state)
	a.mux.HandleFunc("POST /read-page", a.readPage)
	a.mux.HandleFunc("POST /refresh", a.refresh)
	a.mux.HandleFunc("POST /account/refresh", a.forceRefresh)
	a.mux.HandleFunc("POST /account/suggestions", a.saveSuggestions)
	a.mux.HandleFunc("GET /sources", a.sources)
	a.mux.HandleFunc("POST /sources", a.subscribe)
	a.mux.HandleFunc("POST /sources/discover", a.discover)
	a.mux.HandleFunc("POST /sources/{id}/topic", a.sourceTopic)
	a.mux.HandleFunc("POST /groups/{id}/rename", a.renameGroup)
	a.mux.HandleFunc("POST /account/members/{id}", a.memberAction)
	a.mux.HandleFunc("POST /account/invitations/revoke", a.revokeInvite)
	a.mux.HandleFunc("POST /sources/{id}/remove", a.unsubscribe)
	a.mux.HandleFunc("POST /sources/{id}/tags", a.sourceTags)
	a.mux.HandleFunc("GET /groups", a.groups)
	a.mux.HandleFunc("POST /groups", a.createGroup)
	a.mux.HandleFunc("GET /groups/{id}", a.group)
	a.mux.HandleFunc("POST /groups/{id}", a.saveGroup)
	a.mux.HandleFunc("POST /groups/{id}/delete", a.deleteGroup)
	a.mux.HandleFunc("POST /sources/import", a.importOPML)
	a.mux.HandleFunc("GET /subscriptions.opml", a.exportOPML)
	a.mux.HandleFunc("GET /api/v1/export", a.export)
	a.mux.HandleFunc("GET /account", a.account)
	a.mux.HandleFunc("GET /account/invitations", a.invitations)
	a.mux.HandleFunc("POST /account/restore", a.restorePersonal)
	a.mux.HandleFunc("POST /account/household", a.groupOptin)
	a.mux.HandleFunc("POST /account/password", a.password)
	a.mux.HandleFunc("POST /account/invite", a.invite)
	a.mux.HandleFunc("GET /login", a.login)
	a.mux.HandleFunc("POST /login", a.login)
	a.mux.HandleFunc("GET /join", a.join)
	a.mux.HandleFunc("POST /join", a.join)
	a.mux.HandleFunc("POST /logout", a.logout)
}

func (a *App) render(w http.ResponseWriter, r *http.Request, p page, status int) {
	p.Demo = a.config.Demo
	p.Refresh = a.RefreshStatus(current(r).User.ID)
	if current(r).User.ID > 0 {
		p.ImagesEnabled, _ = a.Store.ImagesEnabled(r.Context())
		var err error
		p.Fetch, err = a.fetchOverview(r.Context(), current(r).User.ID)
		if err != nil {
			p.Fetch = fetchOverview{State: "unavailable", Message: "Fetch status is unavailable."}
		}
	}
	if current(r).User.Owner {
		p.Backup = a.config.Backups.Status()
	}
	rc := current(r)
	p.User = rc.User
	p.CSRF = rc.CSRF
	p.Date = time.Now().Format("Monday, January 2")
	if p.ReturnTo == "" {
		p.ReturnTo = r.URL.RequestURI()
	}
	var b bytes.Buffer
	if err := a.templates.ExecuteTemplate(&b, "layout", p); err != nil {
		slog.Error("render page", "error", err)
		http.Error(w, "Could not display this page", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "path", r.URL.Path, "error", err)
	a.render(w, r, page{Title: "Something went wrong", Kind: "error", Error: "Your change could not be completed. Try again."}, 500)
}
func (a *App) bad(w http.ResponseWriter, r *http.Request, msg string) {
	a.render(w, r, page{Title: "Please check this", Kind: "error", Error: msg}, 400)
}
func back(r *http.Request) string {
	s := r.PostFormValue("return_to")
	u, err := url.Parse(s)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.ContainsAny(s, "\\\r\n") {
		return "/"
	}
	return u.RequestURI()
}
func notice(r *http.Request) string {
	return map[string]string{"keyboard": "Your reading preferences were saved.", "images": "Feed image settings saved.", "suggestions": "Suggested categories saved. Existing subscriptions were kept.", "restored": "Your reading data was merged. Existing choices were kept.", "subscribed": "Source added.", "removed": "Source removed. Your saved stories are kept.", "imported": "Your subscriptions were imported.", "read": "These stories are marked as read.", "joined": "Your account is ready. You can sign in now.", "password": "Password changed. Please sign in again."}[r.URL.Query().Get("notice")]
}
func scope(r *http.Request) bool { return r.FormValue("scope") == "household" }

func parseFilter(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{View: q.Get("view"), Query: strings.TrimSpace(q.Get("q")), Category: q.Get("topic")}
	if f.View == "" {
		f.View = "unread"
	}
	if f.View != "unread" && f.View != "all" && f.View != "saved" && f.View != "group" {
		return f, errors.New("unknown reading shelf")
	}
	if len(f.Query) > 300 || len(f.Category) > 160 {
		return f, errors.New("search or topic is too long")
	}
	if q.Get("page") != "" {
		n, e := strconv.Atoi(q.Get("page"))
		if e != nil || n < 0 || n > 10000 {
			return f, errors.New("invalid page")
		}
		f.Page = n
	}
	if q.Get("feed") != "" {
		n, e := strconv.ParseInt(q.Get("feed"), 10, 64)
		if e != nil || n < 1 {
			return f, errors.New("invalid source")
		}
		f.FeedID = n
	}
	if q.Get("group") != "" {
		n, e := strconv.ParseInt(q.Get("group"), 10, 64)
		if e != nil || n < 1 {
			return f, errors.New("invalid feed group")
		}
		f.GroupID = n
	}
	return f, nil
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	f, err := parseFilter(r)
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	if f.View == "group" && !u.GroupOptin {
		a.render(w, r, page{Title: "The household table", Kind: "group-off"}, 200)
		return
	}
	p := page{Title: "Your reading table", Kind: "home", Filter: f, Notice: notice(r)}
	if f.GroupID > 0 {
		g, e := a.Store.Group(r.Context(), u, f.GroupID)
		if e == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if e != nil {
			a.fail(w, r, e)
			return
		}
		if g.Household && !u.GroupOptin {
			a.render(w, r, page{Title: "The household table", Kind: "group-off"}, 200)
			return
		}
		p.Title = g.Name
		p.Collection = g
	}
	if f.View == "saved" {
		p.Title = "Set aside"
	}
	if f.View == "all" {
		p.Title = "All stories"
	}
	if f.View == "group" {
		p.Title = "The household table"
	}
	p.Groups, err = a.Store.Groups(r.Context(), u)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.Entries, p.More, err = a.Store.Entries(r.Context(), u.ID, f)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.Feeds, err = a.Store.Feeds(r.Context(), u.ID, false)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if u.GroupOptin {
		p.GroupFeeds, err = a.Store.Feeds(r.Context(), u.ID, true)
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	seen := map[string]bool{}
	for _, list := range [][]Feed{p.Feeds, p.GroupFeeds} {
		for _, feed := range list {
			if !seen[feed.Category] {
				p.Topics = append(p.Topics, feed.Category)
				seen[feed.Category] = true
			}
		}
	}
	query := r.URL.Query()
	query.Set("page", strconv.Itoa(f.Page+1))
	p.Next = "/?" + query.Encode()
	query.Set("page", strconv.Itoa(f.Page-1))
	p.Previous = "/?" + query.Encode()
	a.render(w, r, p, 200)
}
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("invalid story or source")
	}
	return id, nil
}
func (a *App) story(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	e, err := a.Store.Entry(r.Context(), current(r).User.ID, id)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, page{Title: e.Title, Kind: "story", Story: e}, 200)
}
func (a *App) state(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	field := r.PostFormValue("field")
	value := r.PostFormValue("value")
	if (field != "read" && field != "saved") || (value != "true" && value != "false") {
		a.bad(w, r, "Invalid reading state")
		return
	}
	err = a.Store.SetState(r.Context(), current(r).User.ID, id, field, value == "true")
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, back(r), 303)
}
func (a *App) readPage(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	for _, s := range r.PostForm["id"] {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id < 1 {
			a.bad(w, r, "Invalid story")
			return
		}
		ids = append(ids, id)
	}
	if err := a.Store.MarkPage(r.Context(), current(r).User.ID, ids); err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
		} else {
			a.fail(w, r, err)
		}
		return
	}
	u, _ := url.Parse(back(r))
	q := u.Query()
	q.Del("page")
	q.Set("notice", "read")
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), 303)
}

func (a *App) sources(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	p := page{Title: "Your sources", Kind: "sources", GroupScope: scope(r), Notice: notice(r)}
	p.CatalogQuery = strings.TrimSpace(r.URL.Query().Get("catalog_q"))
	if len(p.CatalogQuery) > 200 {
		a.bad(w, r, "Search is too long")
		return
	}
	query := strings.ToLower(p.CatalogQuery)
	enabled, err := a.Store.CatalogCategories(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.SuggestionsEnabled = len(enabled) > 0
	for _, item := range a.Catalog {
		if enabled[item.Category] && (query == "" || strings.Contains(strings.ToLower(item.Title+" "+item.Description+" "+item.Category), query)) {
			p.Suggestions = append(p.Suggestions, item)
		}
	}
	if p.GroupScope {
		p.Title = "Household sources"
	}
	p.Feeds, err = a.Store.Feeds(r.Context(), u.ID, p.GroupScope)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p, 200)
}
func (a *App) subscribe(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	group := scope(r)
	if group && !u.Owner {
		http.Error(w, "Only owners can change household sources", 403)
		return
	}
	f := Feed{URL: r.PostFormValue("url"), Category: r.PostFormValue("category")}
	var err error
	f.Tags, err = ParseTags(r.PostFormValue("tags"))
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	if err := a.Store.Subscribe(r.Context(), u.ID, []Feed{f}, group); err != nil {
		a.bad(w, r, err.Error())
		return
	}
	a.StartRefresh(u.ID, false)
	to := "/sources?notice=subscribed"
	if group {
		to += "&scope=household"
	}
	http.Redirect(w, r, to, 303)
}
func (a *App) unsubscribe(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	group := scope(r)
	if group && !u.Owner {
		http.Error(w, "Only owners can change household sources", 403)
		return
	}
	id, err := pathID(r)
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	if err = a.Store.Unsubscribe(r.Context(), u.ID, id, group); err != nil {
		a.fail(w, r, err)
		return
	}
	to := "/sources?notice=removed"
	if group {
		to += "&scope=household"
	}
	http.Redirect(w, r, to, 303)
}
func (a *App) importOPML(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	group := scope(r)
	if group && !u.Owner {
		http.Error(w, "Only owners can change household sources", 403)
		return
	}
	f, _, err := r.FormFile("opml")
	if err != nil {
		a.bad(w, r, "Choose an OPML file to import")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		a.bad(w, r, "Could not read this file")
		return
	}
	feeds, err := ParseOPML(b)
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	if err = a.Store.Subscribe(r.Context(), u.ID, feeds, group); err != nil {
		a.bad(w, r, err.Error())
		return
	}
	a.StartRefresh(u.ID, false)
	to := "/sources?notice=imported"
	if group {
		to += "&scope=household"
	}
	http.Redirect(w, r, to, 303)
}
func (a *App) exportOPML(w http.ResponseWriter, r *http.Request) {
	feeds, err := a.Store.Feeds(r.Context(), current(r).User.ID, false)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	b, err := EncodeOPML(feeds)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tidings.opml"`)
	_, _ = w.Write(b)
}
func (a *App) export(w http.ResponseWriter, r *http.Request) {
	data, err := a.Store.Export(r.Context(), current(r).User)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="tidings.json"`)
	_, _ = w.Write(b)
}

func (a *App) account(w http.ResponseWriter, r *http.Request) {
	p := page{Title: "Your place here", Kind: "account", Notice: notice(r)}
	if current(r).User.Owner {
		var err error
		p.Members, err = a.Store.Members(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
		p.CatalogCategories, err = a.catalogSettings(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.render(w, r, p, 200)
}
func (a *App) groupOptin(w http.ResponseWriter, r *http.Request) {
	value := r.PostFormValue("enabled") == "true"
	_, err := a.Store.DB.ExecContext(r.Context(), "UPDATE users SET group_optin=? WHERE id=?", value, current(r).User.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/account", 303)
}
func (a *App) password(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	var hash []byte
	if err := a.Store.DB.QueryRowContext(r.Context(), "SELECT password FROM users WHERE id=?", u.ID).Scan(&hash); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := checkPassword(hash, r.PostFormValue("current")); err != nil {
		a.bad(w, r, "Current password is incorrect")
		return
	}
	if r.PostFormValue("password") != r.PostFormValue("confirm") {
		a.bad(w, r, "New passwords do not match")
		return
	}
	if err := a.Store.SetPassword(r.Context(), u.Username, r.PostFormValue("password")); err != nil {
		a.bad(w, r, err.Error())
		return
	}
	a.cookie(w, a.cookieName(), "", -1)
	http.Redirect(w, r, "/login?notice=password", 303)
}
func (a *App) invite(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		http.Error(w, "Only owners can invite members", 403)
		return
	}
	base, err := PublicOrigin(a.config.BaseURL)
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	raw, err := a.Store.Invite(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderInvitations(w, r, base+"/join?code="+raw, token.Hash(raw)[:8])
}
func (a *App) clientKey(r *http.Request) string {
	return clientip.NetworkKey(a.config.TrustedProxies.Client(r))
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if current(r).User.ID != 0 {
			http.Redirect(w, r, "/", 303)
			return
		}
		a.render(w, r, page{Title: "Welcome back", Kind: "login", Notice: notice(r)}, 200)
		return
	}
	if !a.limit.allow(a.clientKey(r)) {
		http.Error(w, "Too many attempts. Please wait 15 minutes.", 429)
		return
	}
	raw, err := a.Store.Login(r.Context(), r.PostFormValue("username"), r.PostFormValue("password"))
	if err != nil {
		if !errors.Is(err, ErrCredentials) {
			a.fail(w, r, err)
			return
		}
		a.render(w, r, page{Title: "Welcome back", Kind: "login", Error: err.Error()}, 401)
		return
	}
	a.cookie(w, a.cookieName(), raw, 30*24*3600)
	http.Redirect(w, r, "/", 303)
}
func (a *App) join(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		a.render(w, r, page{Title: "A place at the table", Kind: "join", Invite: r.URL.Query().Get("code")}, 200)
		return
	}
	if !a.limit.allow(a.clientKey(r)) {
		http.Error(w, "Too many attempts. Please wait 15 minutes.", 429)
		return
	}
	code := r.PostFormValue("code")
	err := a.Store.Join(r.Context(), code, r.PostFormValue("username"), r.PostFormValue("password"))
	if err != nil {
		a.render(w, r, page{Title: "A place at the table", Kind: "join", Invite: code, Error: err.Error()}, 400)
		return
	}
	http.Redirect(w, r, "/login?notice=joined", 303)
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	_, err := a.Store.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE token=?", token.Hash(current(r).Session))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.cookie(w, a.cookieName(), "", -1)
	http.Redirect(w, r, "/login", 303)
}

func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	a.StartRefresh(current(r).User.ID, false)
	http.Redirect(w, r, "/?notice=refresh", 303)
}

func (a *App) forceRefresh(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		http.Error(w, "Only owners can fetch all feeds", http.StatusForbidden)
		return
	}
	if !a.startRefresh(0, true, true) {
		http.Error(w, "The server is shutting down. Try again after it restarts.", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "/account?notice=refresh-all", http.StatusSeeOther)
}

type RefreshStatus struct {
	Running   bool
	Queued    bool
	NextCheck time.Time
}

func (a *App) RefreshStatus(uid int64) RefreshStatus {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	return RefreshStatus{Running: a.refreshRunning && (a.refreshCurrent == uid || a.refreshCurrentAll), Queued: a.refreshAll || a.refreshPending[uid], NextCheck: a.refreshNextCheck}
}

// One worker drains a bounded set of account requests, coalescing duplicates.
func (a *App) StartRefresh(uid int64, all bool) bool {
	return a.startRefresh(uid, all, false)
}

func (a *App) startRefresh(uid int64, all, force bool) bool {
	return a.queueRefresh(uid, all, force, false)
}

// recheck schedules a follow-up even if a forced fetch is already running.
// Image metadata backfill needs this after invalidating older validators.
func (a *App) queueRefresh(uid int64, all, force, recheck bool) bool {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.ctx.Err() != nil || (!all && uid <= 0) {
		return false
	}
	if all {
		if force && a.refreshCurrentAll && a.refreshCurrentForce && !recheck {
			return true
		}
		a.refreshAll = true
		a.refreshForce = a.refreshForce || force
		a.refreshPending = map[int64]bool{}
	} else if !a.refreshAll {
		if a.refreshPending == nil {
			a.refreshPending = map[int64]bool{}
		}
		a.refreshPending[uid] = true
	}
	if a.refreshRunning {
		return true
	}
	a.refreshRunning = true
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			a.refreshMu.Lock()
			if a.ctx.Err() != nil {
				a.refreshPending = nil
				a.refreshAll = false
				a.refreshForce = false
				a.refreshRunning = false
				a.refreshCurrent = 0
				a.refreshCurrentAll = false
				a.refreshCurrentForce = false
				a.refreshMu.Unlock()
				return
			}
			all := a.refreshAll
			force := a.refreshForce
			uid := int64(0)
			if all {
				a.refreshAll = false
				a.refreshForce = false
				a.refreshPending = map[int64]bool{}
			} else {
				for id := range a.refreshPending {
					uid = id
					break
				}
				if uid == 0 {
					a.refreshRunning = false
					a.refreshCurrent = 0
					a.refreshCurrentAll = false
					a.refreshCurrentForce = false
					a.refreshMu.Unlock()
					return
				}
				delete(a.refreshPending, uid)
			}
			a.refreshCurrent = uid
			a.refreshCurrentAll = all
			a.refreshCurrentForce = force
			a.refreshMu.Unlock()
			a.refreshFeeds(uid, all, force)
		}
	}()
	return true
}
func (a *App) refreshFeeds(uid int64, all, force bool) {
	feeds, err := a.Store.RefreshFeeds(a.ctx, uid, all)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("list refresh sources", "error", err)
		}
		return
	}
	feeds = fairFeedOrder(feeds)
	gates := map[string]chan struct{}{}
	for _, f := range feeds {
		u, _ := url.Parse(f.URL)
		if gates[u.Hostname()] == nil {
			gates[u.Hostname()] = make(chan struct{}, 1)
		}
	}
	jobs := make(chan Feed)
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for f := range jobs {
				u, _ := url.Parse(f.URL)
				a.refreshSource(f, all, force, gates[u.Hostname()])
			}
		}()
	}
	for _, f := range feeds {
		select {
		case jobs <- f:
		case <-a.ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}
func (a *App) Wait() { a.wg.Wait() }
func (a *App) Poll(ctx context.Context, interval time.Duration) {
	interval = min(interval, time.Minute)
	if interval <= 0 {
		interval = time.Minute
	}
	a.refreshMu.Lock()
	a.refreshNextCheck = time.Now().Add(interval)
	a.refreshMu.Unlock()
	a.StartRefresh(0, true)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.refreshMu.Lock()
			a.refreshNextCheck = now.Add(interval)
			a.refreshMu.Unlock()
			a.StartRefresh(0, true)
		}
	}
}
