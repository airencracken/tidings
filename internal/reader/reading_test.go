package reader

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestTrackingCleanup(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://example.org/a?utm_source=mail&id=7&fbclid=abc#section", "https://example.org/a?id=7#section"},
		{"https://example.org/?q=a%20b&tag=one&tag=two&UTM_Medium=web&empty=", "https://example.org/?q=a%20b&tag=one&tag=two&empty="},
		{"https://example.org/?%75tm_campaign=test&lang=en", "https://example.org/?lang=en"},
		{"https://example.org/?utm_source=only", "https://example.org/"},
		{"https://example.org/?ref=docs&source=api&token=secret", "https://example.org/?ref=docs&source=api&token=secret"},
		{"https://example.org/?utm_source=x&X-Amz-Signature=abc", "https://example.org/?utm_source=x&X-Amz-Signature=abc"},
		{"https://example.org/?sig=abc&utm_source=x", "https://example.org/?sig=abc&utm_source=x"},
		{"https://example.org/?bad%zz=x&utm_source=a", "https://example.org/?bad%zz=x&utm_source=a"},
	} {
		if got := CleanArticleURL(tc.in); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func FuzzCleanArticleURL(f *testing.F) {
	for _, seed := range []string{"https://example.org/?utm_source=x&id=3#part", "https://example.org/?tag=a&tag=b", "https://example.org/?sig=abc&utm_source=x", "broken%zz"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 10000 {
			return
		}
		clean := CleanArticleURL(raw)
		if CleanArticleURL(clean) != clean {
			t.Fatal("cleanup is not idempotent")
		}
		before, e1 := url.Parse(raw)
		after, e2 := url.Parse(clean)
		if e1 == nil && e2 == nil {
			if before.Scheme != after.Scheme || before.Host != after.Host || before.EscapedPath() != after.EscapedPath() || before.Fragment != after.Fragment {
				t.Fatal("changed URL outside query")
			}
		}
	})
}

func TestArticleMarkup(t *testing.T) {
	raw := `<h2 onclick="bad()">A heading</h2><p>Text <a href="../other?utm_source=x&amp;id=9#part" ping="https://evil.test/" style="x">link</a></p><ul><li>First</li></ul><blockquote>Quote</blockquote><pre><code>  indented &lt;code&gt;\nnext</code></pre><img src="/photo.jpg" alt="A photo" onerror="bad()"><img width="1" src="/pixel"><script>bad()</script><style>body{}</style><iframe src="https://evil.test/"></iframe><svg><a href="javascript:bad()">svg</a></svg><a href="javascript:bad()">unsafe</a><input autofocus><form>secret form</form>`
	rich := articleHTML(raw, "https://example.org/posts/story", nil)
	for _, want := range []string{"<h2>A heading</h2>", "<ul><li>First</li></ul>", "<blockquote>Quote</blockquote>", "<pre><code>  indented", "https://example.org/other?id=9#part"} {
		if !strings.Contains(rich, want) {
			t.Errorf("missing %s in %s", want, rich)
		}
	}
	for _, bad := range []string{"onclick", "onerror", "<script", "<style", "<iframe", "<svg", "javascript:", "ping=", "<img", "<input", "secret form"} {
		if strings.Contains(rich, bad) {
			t.Errorf("unsafe markup %q: %s", bad, rich)
		}
	}
	e := Entry{ID: 42, URL: "https://example.org/posts/story", BodyHTML: raw}
	if got := string(renderArticle(e, false)); strings.Contains(got, "<img") {
		t.Fatal("inline images enabled without opt-in")
	}
	shown := string(renderArticle(e, true))
	if !strings.Contains(shown, "/story/42/image/"+imageKey("https://example.org/photo.jpg")) {
		t.Fatal("inline image not local", shown)
	}
	if strings.Contains(shown, `src="https:`) || strings.Contains(shown, "/pixel") {
		t.Fatal("browser has a remote image", shown)
	}
	if got := string(renderArticle(Entry{Body: "<script>alert(1)</script>"}, false)); strings.Contains(got, "<script>") {
		t.Fatal("legacy body trusted")
	}
	if got := articleHTML(strings.Repeat("x", maxArticleHTML+1), e.URL, nil); got != "" {
		t.Fatal("oversized markup accepted")
	}
}

func FuzzArticleMarkup(f *testing.F) {
	for _, seed := range []string{`<p>Hello <b>world</b></p>`, `<svg><script>bad()</script></svg>`, `<img src="https://evil.test/a" onerror="bad()">`, `<a href="java&#x73;cript:bad()">bad</a>`, `<math><mtext><table><mglyph><style><!--</style><img src=x onerror=bad()>`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 16000 {
			return
		}
		rich := articleHTML(raw, "https://example.org/a", nil)
		doc, err := html.Parse(strings.NewReader(rich))
		if err != nil {
			t.Fatal(err)
		}
		var check func(*html.Node)
		check = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch n.Data {
				case "script", "style", "iframe", "img", "svg", "math", "form", "input", "object", "embed":
					t.Fatal("active element", n.Data)
				}
				for _, a := range n.Attr {
					if a.Key != "href" && a.Key != "rel" && a.Key != "referrerpolicy" {
						t.Fatal("unexpected attribute", a.Key)
					}
					if a.Key == "href" {
						u, err := url.Parse(a.Val)
						if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
							t.Fatal("unsafe link", a.Val)
						}
					}
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				check(child)
			}
		}
		check(doc)
	})
}

func fullArticleFixture() string {
	return `<html><head><title>A fuller story</title></head><body><nav>Skip this menu</nav><article><h1>A fuller story</h1><h2>How it works</h2><p>` + strings.Repeat("This is the original article, with useful details about the tools and ideas behind it. ", 20) + `</p><p><a href="/reference?utm_source=story&amp;id=4">Reference</a></p><pre><code>  useful code</code></pre><img src="/full.jpg"></article><footer>Ignore the footer</footer></body></html>`
}

func TestFullArticleIsolationAndRoutes(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "article", false)
	id := entryID(t, h.store, f)
	if err := h.store.Subscribe(t.Context(), 3, []Feed{{URL: f.URL}}, false); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(fullArticleFixture())), Request: r}, nil
	})}}
	path := fmt.Sprintf("/story/%d/fetch", id)
	if w := h.request(0, "POST", path, nil); w.Code != 303 {
		t.Fatal("anonymous fetch", w.Code)
	}
	if w := h.request(1, "POST", path, nil); w.Code != 404 || calls != 0 {
		t.Fatal("private article leaked", w.Code, calls)
	}
	if w := h.request(2, "GET", path, nil); w.Code != 405 {
		t.Fatal("GET performed fetch", w.Code)
	}
	if w := h.request(2, "POST", path, url.Values{"csrf": {"wrong"}}); w.Code != 403 || calls != 0 {
		t.Fatal("missing CSRF", w.Code)
	}
	if w := h.request(2, "POST", path, nil); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	mine, err := h.store.Entry(t.Context(), 2, id)
	if err != nil || !mine.FullStory || !strings.Contains(mine.Body, "original article") {
		t.Fatal(mine, err)
	}
	theirs, err := h.store.Entry(t.Context(), 3, id)
	if err != nil || theirs.FullStory || strings.Contains(theirs.Body, "original article") {
		t.Fatal("another reader's excerpt changed", theirs, err)
	}
	if err = h.store.SetState(t.Context(), 2, id, "saved", true); err != nil {
		t.Fatal(err)
	}
	exported, err := h.store.Export(t.Context(), User{ID: 2})
	if err != nil || len(exported.Stories) != 1 || exported.Stories[0].Body != mine.Body {
		t.Fatal("downloaded story missing from export", exported, err)
	}
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("offline") })}}
	if w := h.request(2, "POST", path, nil); w.Code != 502 || !strings.Contains(w.Body.String(), "original article") {
		t.Fatal("failed download discarded readable copy", w.Code, w.Body.String())
	}
	if err = h.store.Unsubscribe(t.Context(), 3, f.ID, false); err != nil {
		t.Fatal(err)
	}
	if err = h.store.saveArticle(t.Context(), 3, id, "should not save", "<p>no</p>"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("write bypasses current subscription ACL", err)
	}
	if w := h.request(2, "GET", fmt.Sprintf("/story/%d/image/%s", id, imageKey("https://example.org/not-in-story.png")), nil); w.Code != 404 {
		t.Fatal("arbitrary media key accepted", w.Code)
	}
}

func TestArticleDownloadLimits(t *testing.T) {
	for _, tc := range []struct {
		name, kind, body string
		status           int
	}{
		{"html", "text/html", fullArticleFixture(), 200},
		{"non html", "image/svg+xml", "<svg/>", 200},
		{"too large", "text/html", fullArticleFixture() + "<!--" + strings.Repeat("x", maxArticleBytes) + "-->", 200},
		{"empty", "text/html", "<html><body></body></html>", 200},
		{"blocked", "text/html", "Forbidden", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.RawQuery, "utm_") {
					t.Error("tracking sent upstream")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.kind}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}}
			body, rich, err := f.Article(t.Context(), "https://example.org/story?utm_source=email&id=5")
			if tc.name == "html" {
				if err != nil || !strings.Contains(body, "original article") || !strings.Contains(rich, "<h2>") {
					t.Fatal(body, rich, err)
				}
			} else if err == nil {
				t.Fatal("invalid article accepted")
			}
		})
	}
	for _, address := range []string{"http://127.0.0.1/", "http://169.254.169.254/"} {
		if _, _, err := NewFetcher().Article(t.Context(), address); err == nil {
			t.Fatal("private article host accepted")
		}
	}
}

func TestKeyboardPreference(t *testing.T) {
	h := webHarness(t)
	if w := h.request(2, "GET", "/", nil); strings.Contains(w.Body.String(), `src="/static/keyboard.js"`) {
		t.Fatal("shortcuts default on")
	}
	if w := h.request(2, "POST", "/account/keyboard", url.Values{"enabled": {"true"}}); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if w := h.request(2, "GET", "/", nil); !strings.Contains(w.Body.String(), `src="/static/keyboard.js"`) {
		t.Fatal("opt-in did not enable shortcuts")
	}
	if w := h.request(3, "GET", "/", nil); strings.Contains(w.Body.String(), `src="/static/keyboard.js"`) {
		t.Fatal("preference shared with another reader")
	}
	if w := h.request(2, "POST", "/account/keyboard", url.Values{"enabled": {"false"}}); w.Code != 400 {
		t.Fatal("invalid boolean", w.Code)
	}
	if w := h.request(2, "POST", "/account/keyboard", url.Values{"enabled": {"true", "true"}}); w.Code != 400 {
		t.Fatal("duplicate field", w.Code)
	}
	if w := h.request(2, "POST", "/account/keyboard", url.Values{"csrf": {"bad"}}); w.Code != 403 {
		t.Fatal("missing CSRF", w.Code)
	}
	if w := h.request(2, "POST", "/account/keyboard", nil); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if w := h.request(2, "GET", "/", nil); strings.Contains(w.Body.String(), `src="/static/keyboard.js"`) {
		t.Fatal("disabled shortcuts still loaded")
	}
	if _, err := h.store.DB.Exec("UPDATE users SET keyboard_shortcuts=2 WHERE id=2"); err == nil {
		t.Fatal("schema accepted nonboolean preference")
	}
}

const dropV5 = `ALTER TABLE states DROP COLUMN skipped;DROP TABLE article_copies;ALTER TABLE users DROP COLUMN keyboard_shortcuts;ALTER TABLE entries DROP COLUMN body_html;ALTER TABLE feeds DROP COLUMN next_check;ALTER TABLE feeds DROP COLUMN poll_interval;ALTER TABLE feeds DROP COLUMN failures;`

func TestReadingMigrationAtomicity(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "reader")
			s, err := OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.DB.Exec(dropV5 + "INSERT INTO users(username,password) VALUES('before','unchanged');PRAGMA user_version=4"); err != nil {
				t.Fatal(err)
			}
			if conflict {
				if _, err = s.DB.Exec("CREATE TABLE article_copies(unrelated TEXT)"); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			upgraded, err := OpenStore(dir)
			if !conflict {
				if err != nil {
					t.Fatal(err)
				}
				defer upgraded.Close()
				var enabled bool
				var version int
				if err = upgraded.DB.QueryRow("SELECT keyboard_shortcuts FROM users WHERE username='before'").Scan(&enabled); err != nil || enabled {
					t.Fatal(enabled, err)
				}
				if err = upgraded.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
					t.Fatal(version, err)
				}
				return
			}
			if err == nil {
				upgraded.Close()
				t.Fatal("conflict accepted")
			}
			db, err := sql.Open("sqlite", filepath.Join(dir, "reader.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var version, columns int
			if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
				t.Fatal(version, err)
			}
			if err = db.QueryRow("SELECT count(*) FROM pragma_table_info('users') WHERE name='keyboard_shortcuts'").Scan(&columns); err != nil || columns != 0 {
				t.Fatal("partial migration published", columns, err)
			}
		})
	}
}

func TestArticleExportRestoreAndAtomicity(t *testing.T) {
	s := testStore(t)
	f := testSource(t, s, 2, "article-restore", false)
	id := entryID(t, s, f)
	body := "My private downloaded copy"
	rich := `<h2>A full story</h2><p>` + body + `</p><script>bad()</script>`
	if err := s.saveArticle(t.Context(), 2, id, body, rich); err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(t.Context(), 2, id, "saved", true); err != nil {
		t.Fatal(err)
	}
	data, err := s.Export(t.Context(), User{ID: 2})
	if err != nil || len(data.Stories) != 1 || !data.Stories[0].FullStory {
		t.Fatal(data, err)
	}
	if err = s.RestorePersonal(t.Context(), User{ID: 3}, data); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Entry(t.Context(), 3, id)
	if err != nil || restored.Body != body || !restored.FullStory || strings.Contains(restored.BodyHTML, "<script") {
		t.Fatal(restored, err)
	}
	// An existing reader's own copy and markers win over an imported file.
	data.Stories[0].Body = "Changed imported copy"
	if err = s.RestorePersonal(t.Context(), User{ID: 3}, data); err != nil {
		t.Fatal(err)
	}
	restored, err = s.Entry(t.Context(), 3, id)
	if err != nil || restored.Body != body {
		t.Fatal("import overwrote an existing choice", restored, err)
	}
	if _, err = s.DB.Exec("DELETE FROM article_copies WHERE user_id=3;DELETE FROM states WHERE user_id=3;CREATE TRIGGER fail_copy BEFORE INSERT ON article_copies WHEN NEW.user_id=3 BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.RestorePersonal(t.Context(), User{ID: 3}, data); err == nil {
		t.Fatal("failed private copy write accepted")
	}
	var markers int
	if err = s.DB.QueryRow("SELECT count(*) FROM states WHERE user_id=3").Scan(&markers); err != nil || markers != 0 {
		t.Fatal("failed copy import left markers", markers, err)
	}
}

func TestThemeBootstrapPrecedesPagePaint(t *testing.T) {
	h := webHarness(t)
	for _, path := range []string{"/login", "/join", "/", "/account", "/sources"} {
		uid := int64(2)
		if path == "/login" || path == "/join" {
			uid = 0
		}
		body := h.request(uid, "GET", path, nil).Body.String()
		bootstrap := strings.Index(body, `<script src="/static/theme.js"></script>`)
		styles := strings.Index(body, `<link rel="stylesheet"`)
		if bootstrap < 0 || styles < bootstrap || strings.Contains(body, `src="/static/theme.js" defer`) {
			t.Fatal("theme can be applied after paint", path)
		}
	}
}

func TestInlineImageAuthorization(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "inline", false)
	id := entryID(t, h.store, f)
	if _, err := h.store.DB.Exec("UPDATE entries SET body_html=? WHERE id=?", `<p>Article</p><img src="https://example.org/inline.jpg">`, id); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetImagesEnabled(t.Context(), 1, true); err != nil {
		t.Fatal(err)
	}
	data := imageFixture(t, 20, 10)
	calls := 0
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://example.org/inline.jpg" {
			t.Error("arbitrary address fetched", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
	})}}
	path := fmt.Sprintf("/story/%d/image/%s", id, imageKey("https://example.org/inline.jpg"))
	if w := h.request(3, "GET", path, nil); w.Code != 404 || calls != 0 {
		t.Fatal("another reader's image exposed", w.Code, calls)
	}
	if w := h.request(0, "GET", path, nil); w.Code != 303 || calls != 0 {
		t.Fatal("anonymous image exposed", w.Code, calls)
	}
	if w := h.request(2, "GET", path, nil); w.Code != 200 || calls != 1 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal("valid inline image unavailable", w.Code, calls, w.Body.String())
	}
	if w := h.request(2, "GET", fmt.Sprintf("/story/%d/image/%s", id, imageKey("https://example.org/arbitrary.jpg")), nil); w.Code != 404 || calls != 1 {
		t.Fatal("arbitrary image fetched", w.Code, calls)
	}
	if err := h.store.SetImagesEnabled(t.Context(), 1, false); err != nil {
		t.Fatal(err)
	}
	if w := h.request(2, "GET", path, nil); w.Code != 404 || calls != 1 {
		t.Fatal("disabled inline images remain available", w.Code, calls)
	}
}
