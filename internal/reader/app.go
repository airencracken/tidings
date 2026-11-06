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
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/airencracken/comfylib/token"
)

//go:embed templates/*.html static/* catalog.json
var assets embed.FS

type Config struct {
	SecureCookies bool
	BaseURL       string
}
type App struct {
	Store     *Store
	Fetcher   *Fetcher
	config    Config
	templates *template.Template
	mux       *http.ServeMux
	limit     limiter
	refreshMu sync.Mutex
	ctx       context.Context
	wg        sync.WaitGroup
	Catalog   []Suggestion
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
}

func NewApp(ctx context.Context, s *Store, c Config) (*App, error) {
	a := &App{Store: s, Fetcher: NewFetcher(), config: c, ctx: ctx, mux: http.NewServeMux()}
	funcs := template.FuncMap{
		"date": func(n int64) string { return time.Unix(n, 0).UTC().Format("Jan 2, 2006") },
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
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
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
	if strings.HasPrefix(r.URL.Path, "/static/") {
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
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(2 << 20); err != nil {
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
	a.mux.Handle("GET /static/", http.FileServerFS(assets))
	a.mux.HandleFunc("GET /{$}", a.home)
	a.mux.HandleFunc("GET /story/{id}", a.story)
	a.mux.HandleFunc("POST /story/{id}/state", a.state)
	a.mux.HandleFunc("POST /read-page", a.readPage)
	a.mux.HandleFunc("POST /refresh", a.refresh)
	a.mux.HandleFunc("GET /sources", a.sources)
	a.mux.HandleFunc("POST /sources", a.subscribe)
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
	return map[string]string{"subscribed": "Source added. Use Refresh feeds to collect its stories.", "removed": "Source removed. Your saved stories are kept.", "imported": "Your subscriptions were imported.", "refresh": "Refresh requested. You can keep reading; reload when you are ready.", "read": "These stories are marked as read.", "joined": "Your account is ready. You can sign in now.", "password": "Password changed. Please sign in again."}[r.URL.Query().Get("notice")]
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
	p := page{Title: "Your sources", Kind: "sources", GroupScope: scope(r), Notice: notice(r), Suggestions: a.Catalog}
	if p.GroupScope {
		p.Title = "Household sources"
	}
	var err error
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
	a.render(w, r, page{Title: "Your place here", Kind: "account", Notice: notice(r)}, 200)
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
	raw, err := a.Store.Invite(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, page{Title: "An invitation", Kind: "invite", Invite: strings.TrimRight(a.config.BaseURL, "/") + "/join?code=" + raw}, 200)
}
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	ip = ip.Unmap()
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
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
	if !a.limit.allow(clientKey(r)) {
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
	if !a.limit.allow(clientKey(r)) {
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
func (a *App) StartRefresh(uid int64, all bool) bool {
	if !a.refreshMu.TryLock() {
		return false
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.refreshMu.Unlock()
		feeds, err := a.Store.RefreshFeeds(a.ctx, uid, all)
		if err != nil {
			slog.Error("list refresh sources", "error", err)
			return
		}
		jobs := make(chan Feed)
		var workers sync.WaitGroup
		for range 4 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for f := range jobs {
					var checked int64
					if err := a.Store.DB.QueryRowContext(a.ctx, "SELECT checked FROM feeds WHERE id=?", f.ID).Scan(&checked); err != nil {
						continue
					}
					if time.Since(time.Unix(checked, 0)) < 5*time.Minute {
						continue
					}
					r, err := a.Fetcher.Fetch(a.ctx, f.URL, f.ETag, f.Modified)
					if err == nil {
						err = a.Store.ApplyFetch(a.ctx, f, r)
					}
					if err != nil && !errors.Is(err, context.Canceled) {
						_, writeErr := a.Store.DB.ExecContext(a.ctx, "UPDATE feeds SET checked=?,error=? WHERE id=?", time.Now().Unix(), plainText(err.Error(), 300), f.ID)
						if writeErr != nil && !errors.Is(writeErr, context.Canceled) {
							slog.Error("record refresh failure", "error", writeErr)
						}
					}
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
	}()
	return true
}
func (a *App) Wait() { a.wg.Wait() }
func (a *App) Poll(ctx context.Context, interval time.Duration) {
	a.StartRefresh(0, true)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.StartRefresh(0, true)
		}
	}
}
