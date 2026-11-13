package reader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func imageFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 70, G: 110, B: 80, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFeedSuppliedImagesAndAdversarialURLs(t *testing.T) {
	for _, tc := range []struct{ name, xml, want string }{
		{"rss thumbnail", `<rss xmlns:media="http://search.yahoo.com/mrss/"><channel><item><link>https://example.org/story</link><media:thumbnail url="/picture.png"/></item></channel></rss>`, "https://example.org/picture.png"},
		{"media content", `<rss xmlns:media="http://search.yahoo.com/mrss/"><channel><item><link>https://example.org/story</link><description>Story text</description><media:content url="https://cdn.example.org/picture.jpg" medium="image"/></item></channel></rss>`, "https://cdn.example.org/picture.jpg"},
		{"atom enclosure", `<feed xmlns="http://www.w3.org/2005/Atom"><entry><link href="https://example.org/story"/><link rel="enclosure" type="image/png" href="/photo.png"/><content type="html">&lt;p&gt;Story text&lt;/p&gt;</content></entry></feed>`, "https://example.org/photo.png"},
		{"html picture skips tracker", `<rss><channel><item><link>https://example.org/story</link><description><![CDATA[<img src="/pixel" width="1"><p>Story text</p><img src="/photo.png">]]></description></item></channel></rss>`, "https://example.org/photo.png"},
		{"unsafe image", `<rss><channel><item><link>https://example.org/story</link><description><![CDATA[<img src="javascript:alert(1)"><img src="data:image/svg+xml,bad"><img src="https://name:password@example.org/picture">]]></description></item></channel></rss>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			feed, err := ParseFeed([]byte(tc.xml), "https://example.org/feed", time.Now())
			if err != nil || len(feed.Entries) != 1 || feed.Entries[0].ImageURL != tc.want {
				t.Fatal(feed, err)
			}
			if strings.Contains(tc.xml, "Story text") && !strings.Contains(feed.Entries[0].Body, "Story text") {
				t.Fatal("image metadata replaced story text")
			}
		})
	}
}

func TestImageCacheSharedPrivateAndExpired(t *testing.T) {
	cache := newImageCache(t.TempDir())
	data := imageFixture(t, 1600, 800)
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "" {
			t.Error("reader credentials forwarded")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}
	const address = "https://example.org/photo.png"
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			preview, err := cache.get(t.Context(), address, client)
			if err != nil {
				t.Error(err)
				return
			}
			config, err := jpeg.DecodeConfig(bytes.NewReader(preview))
			if err != nil || config.Width != 1280 || config.Height != 640 {
				t.Error("preview not normalized", config, err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent readers fetched the same image repeatedly", calls.Load())
	}
	sum := sha256.Sum256([]byte(address))
	path := filepath.Join(cache.directory, hex.EncodeToString(sum[:])+".jpg")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache file is not private", info, err)
	}
	if _, err := newImageCache(filepath.Dir(filepath.Dir(cache.directory))).get(t.Context(), address, client); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("cache did not survive a new cache instance")
	}
	old := time.Now().Add(-imageCacheAge - time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.get(t.Context(), address, client); err != nil || calls.Load() != 2 {
		t.Fatal("expired image was not fetched again", calls.Load(), err)
	}
}

func TestImageLimitsFailuresAndPruning(t *testing.T) {
	for _, data := range [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), imageFixture(t, 1, 1), imageFixture(t, 8193, 2)} {
		if _, err := previewImage(data); err == nil {
			t.Fatal("unsafe or excessive image accepted")
		}
	}
	cache := newImageCache(t.TempDir())
	var calls atomic.Int32
	oversized := append(imageFixture(t, 20, 10), make([]byte, imageDownloadLimit)...)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(oversized)), Request: r}, nil
	})}
	if _, err := cache.get(t.Context(), "https://example.org/huge.png", client); err == nil {
		t.Fatal("image byte cap ignored")
	}
	if _, err := os.Stat(cache.directory); !os.IsNotExist(err) {
		t.Fatal("failed image published cache data", err)
	}
	if _, err := cache.get(t.Context(), "data:image/png,bad", client); err == nil || calls.Load() != 1 {
		t.Fatal("non-web image was fetched")
	}
	if err := os.MkdirAll(cache.directory, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		path := filepath.Join(cache.directory, fmt.Sprintf("%064x.jpg", i))
		if err := os.WriteFile(path, []byte("cached-data"), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cache.directory, "leave-this-alone"), []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cache.prune(12); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(cache.directory)
	if err != nil || len(files) != 2 {
		t.Fatal("cache cap not enforced or unrelated file removed", files, err)
	}
	if _, err := os.Stat(filepath.Join(cache.directory, fmt.Sprintf("%064x.jpg", 2))); err != nil {
		t.Fatal("newest cache entry was removed", err)
	}
}

func TestImagesRequireOptInAndStoryAuthorization(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "pictures", false)
	id := entryID(t, h.store, f)
	if _, err := h.store.DB.Exec("UPDATE entries SET image_url='https://example.org/photo.png' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	data := imageFixture(t, 20, 10)
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == f.URL {
			return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		calls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}}
	path := fmt.Sprintf("/story/%d/image", id)
	if w := h.request(2, "GET", path, nil); w.Code != 404 || calls.Load() != 0 {
		t.Fatal("images fetched without opt-in", w.Code, calls.Load())
	}
	for _, tc := range []struct {
		uid  int64
		csrf string
		want int
	}{{2, "", 403}, {1, "wrong", 403}, {1, "", 303}} {
		form := url.Values{"enabled": {"true"}}
		if tc.csrf != "" {
			form.Set("csrf", tc.csrf)
		}
		if w := h.request(tc.uid, "POST", "/account/images", form); w.Code != tc.want {
			t.Fatal("image setting permissions", tc, w.Code)
		}
	}
	h.app.Wait()
	if w := h.request(3, "GET", path, nil); w.Code != 404 || calls.Load() != 0 {
		t.Fatal("image bypassed story isolation", w.Code, calls.Load())
	}
	if w := h.request(2, "GET", path, nil); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	if w := h.request(2, "GET", path, nil); w.Code != 200 || calls.Load() != 1 {
		t.Fatal("readers did not share cache", w.Code, calls.Load())
	}
	if w := h.request(2, "GET", "/", nil); !strings.Contains(w.Body.String(), `src="`+path+`"`) || strings.Contains(w.Body.String(), `src="https://`) {
		t.Fatal("image bypasses Tidings")
	}
	if err := h.store.SetImagesEnabled(t.Context(), 2, false); err == nil {
		t.Fatal("member changed image setting directly")
	}
	if err := h.store.SetImagesEnabled(t.Context(), 1, false); err != nil {
		t.Fatal(err)
	}
	if w := h.request(2, "GET", path, nil); w.Code != 404 {
		t.Fatal("disabled images remained available", w.Code)
	}
	if _, err := h.store.DB.Exec("UPDATE instance_options SET images_enabled=2"); err == nil {
		t.Fatal("image setting schema accepts non-booleans")
	}
}

func TestEnablingImagesRefreshesOlderFeedMetadata(t *testing.T) {
	h := webHarness(t)
	f := testSource(t, h.store, 2, "older-parser", false)
	previousID := entryID(t, h.store, f)
	if _, err := h.store.DB.Exec("UPDATE entries SET image_url='' WHERE feed_id=?", f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB.Exec("UPDATE feeds SET etag='old-validator',modified='old-date',checked=? WHERE id=?", int64(0), f.ID); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var signalStart sync.Once
	var calls atomic.Int32
	h.app.Fetcher = &Fetcher{Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("If-None-Match") == "old-validator" {
			signalStart.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, context.Canceled
			}
			return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("older metadata was refreshed conditionally")
		}
		body := `<rss><channel><title>Pictures</title><item><guid>first</guid><link>https://example.org/story</link><description><![CDATA[<p>A fresh story.</p><img src="/preview.png">]]></description></item></channel></rss>`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}}
	h.app.startRefresh(0, true, true)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("initial fetch did not start")
	}
	if w := h.request(1, "POST", "/account/images", url.Values{"enabled": {"true"}}); w.Code != 303 {
		t.Fatal(w.Code, w.Body)
	}
	close(release)
	h.app.Wait()
	items, _, err := h.store.Entries(t.Context(), 2, Filter{View: "all"})
	found := false
	for _, item := range items {
		if item.ID == previousID && item.ImageURL == "https://example.org/preview.png" {
			found = true
		}
	}
	if err != nil || !found || len(items) != 1 || calls.Load() != 2 {
		t.Fatal("enabling images did not backfill publisher metadata", found, calls.Load(), err)
	}
}

func TestImageFetcherRejectsPrivateNetworks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := newImageCache(t.TempDir()).get(ctx, "http://127.0.0.1/image.png", NewFetcher().Client); err == nil || !strings.Contains(err.Error(), "public internet") {
		t.Fatal("image fetch bypassed public-network policy", err)
	}
}

func TestImageSchemaUpgradeIsAtomicAndDisabledByDefault(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "reader")
			st, err := OpenStore(directory)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = st.DB.Exec("INSERT INTO users(username,password,owner) VALUES('existing','unchanged',1);DROP TABLE article_copies;ALTER TABLE states DROP COLUMN skipped;ALTER TABLE subscription_tags DROP COLUMN position;ALTER TABLE household_tags DROP COLUMN position;ALTER TABLE users DROP COLUMN keyboard_shortcuts;ALTER TABLE entries DROP COLUMN body_html;ALTER TABLE feeds DROP COLUMN next_check;ALTER TABLE feeds DROP COLUMN poll_interval;ALTER TABLE feeds DROP COLUMN failures;ALTER TABLE entries DROP COLUMN image_url;DROP TABLE instance_options;PRAGMA user_version=3"); err != nil {
				t.Fatal(err)
			}
			if conflict {
				if _, err = st.DB.Exec("CREATE TABLE instance_options(unrelated TEXT)"); err != nil {
					t.Fatal(err)
				}
			}
			if err = st.Close(); err != nil {
				t.Fatal(err)
			}
			upgraded, err := OpenStore(directory)
			if !conflict {
				if err != nil {
					t.Fatal(err)
				}
				defer upgraded.Close()
				if enabled, err := upgraded.ImagesEnabled(t.Context()); err != nil || enabled {
					t.Fatal("upgrade enabled image downloads", enabled, err)
				}
				return
			}
			if err == nil {
				upgraded.Close()
				t.Fatal("conflicting migration accepted")
			}
			db, err := sql.Open("sqlite", filepath.Join(directory, "reader.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var version, columns, owners int
			if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM pragma_table_info('entries') WHERE name='image_url'").Scan(&columns); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM users WHERE username='existing' AND password='unchanged' AND owner=1").Scan(&owners); err != nil {
				t.Fatal(err)
			}
			if version != 3 || columns != 0 || owners != 1 {
				t.Fatal("failed migration partially changed the database", version, columns, owners)
			}
		})
	}
}
