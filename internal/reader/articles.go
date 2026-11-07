package reader

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html/charset"
)

const maxArticleBytes = 2 << 20

func (f *Fetcher) Article(ctx context.Context, raw string) (string, string, error) {
	address, err := WebURL(CleanArticleURL(raw))
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "Tidings (+https://github.com/airencracken/tidings)")
	req.Header.Set("Accept", "text/html, application/xhtml+xml")
	res, err := f.Client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("publisher returned HTTP %d", res.StatusCode)
	}
	kind, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if kind != "text/html" && kind != "application/xhtml+xml" {
		return "", "", errors.New("publisher did not return an article page")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxArticleBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(data) > maxArticleBytes {
		return "", "", errors.New("article page exceeds 2 MiB")
	}
	reader, err := charset.NewReader(bytes.NewReader(data), res.Header.Get("Content-Type"))
	if err != nil {
		return "", "", err
	}
	parser := readability.NewParser()
	parser.MaxElemsToParse = 12000
	article, err := parser.Parse(reader, res.Request.URL)
	if err != nil {
		return "", "", err
	}
	var rich bytes.Buffer
	if err = article.RenderHTML(&rich); err != nil {
		return "", "", err
	}
	if rich.Len() > maxArticleHTML {
		return "", "", errors.New("article is too long")
	}
	markup := articleHTML(rich.String(), res.Request.URL.String(), func(address string) string { return address })
	text := readerText(markup, 64000)
	if strings.TrimSpace(text) == "" {
		return "", "", errors.New("no readable story found")
	}
	return text, markup, nil
}

func (s *Store) saveArticle(ctx context.Context, uid, id int64, body, rich string) error {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO article_copies(user_id,entry_id,body,body_html,fetched)
 SELECT ?,e.id,?,?,? FROM entries e LEFT JOIN states st ON st.entry_id=e.id AND st.user_id=? WHERE e.id=? AND (`+visible+` OR coalesce(st.saved,0)=1)
 ON CONFLICT(user_id,entry_id) DO UPDATE SET body=excluded.body,body_html=excluded.body_html,fetched=excluded.fetched`, uid, body, rich, time.Now().Unix(), uid, id, uid, uid)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return sql.ErrNoRows
	}
	return err
}

func (a *App) fetchArticle(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	entry, err := a.Store.Entry(r.Context(), current(r).User.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if a.config.Demo {
		a.render(w, r, page{Title: entry.Title, Kind: "story", Story: entry, Error: "Full-story fetching is available in your own instance."}, http.StatusOK)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	select {
	case a.articleSlots <- struct{}{}:
		defer func() { <-a.articleSlots }()
	case <-ctx.Done():
		a.render(w, r, page{Title: entry.Title, Kind: "story", Story: entry, Error: "Could not fetch the full story. Try again later."}, http.StatusServiceUnavailable)
		return
	}
	body, rich, err := a.Fetcher.Article(ctx, entry.URL)
	if err != nil {
		a.render(w, r, page{Title: entry.Title, Kind: "story", Story: entry, Error: "Could not fetch the full story. You can still read the original."}, http.StatusBadGateway)
		return
	}
	if err = a.Store.saveArticle(ctx, current(r).User.ID, id, body, rich); errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/story/%d", id), http.StatusSeeOther)
}

func (a *App) saveKeyboard(w http.ResponseWriter, r *http.Request) {
	values := r.PostForm["enabled"]
	if len(values) > 1 || len(values) == 1 && values[0] != "true" {
		a.bad(w, r, "Choose whether to use keyboard shortcuts")
		return
	}
	_, err := a.Store.DB.ExecContext(r.Context(), "UPDATE users SET keyboard_shortcuts=? WHERE id=? AND suspended=0", len(values) == 1, current(r).User.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/account?notice=keyboard#reading-preferences", http.StatusSeeOther)
}
