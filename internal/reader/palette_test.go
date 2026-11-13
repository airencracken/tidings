// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airencracken/comfylib/ui"
)

func TestOwnerChoosesTheSitePalette(t *testing.T) {
	h := webHarness(t)
	if p, err := h.store.SitePalette(t.Context()); err != nil || p != ui.Sage {
		t.Fatal("default palette", p, err)
	}
	for _, path := range []string{"/login", "/join"} {
		if page := h.request(0, "GET", path, nil).Body.String(); !strings.Contains(page, `data-palette="sage"`) || !strings.Contains(page, `class="theme-picker"`) {
			t.Fatal("signed-out page has no palette or picker", path)
		}
	}
	admin := h.request(1, "GET", "/admin", nil).Body.String()
	if !strings.Contains(admin, `<option value="sage" selected>Sage</option>`) || !strings.Contains(admin, `<option value="slate" >Slate</option>`) {
		t.Fatal("admin palette choices", admin)
	}
	if w := h.request(1, "POST", "/admin/palette", url.Values{"palette": {"slate"}}); w.Code != 303 || w.Header().Get("Location") != "/admin?notice=palette#site-palette" {
		t.Fatal(w.Code, w.Header().Get("Location"))
	}
	if page := h.request(2, "GET", "/", nil).Body.String(); !strings.Contains(page, `data-palette="slate"`) || !strings.Contains(page, `data-palette-default="slate"`) {
		t.Fatal("site palette not applied")
	}
	// Members cannot change it, and unknown or repeated names are refused.
	if w := h.request(2, "POST", "/admin/palette", url.Values{"palette": {"sage"}}); w.Code != 403 {
		t.Fatal("member changed the palette", w.Code)
	}
	for _, bad := range []url.Values{{"palette": {"Sage"}}, {"palette": {"neon"}}, {"palette": {""}}, {"palette": {"sage", "slate"}}, {}, {"palette": {`slate" onload="x`}}} {
		if w := h.request(1, "POST", "/admin/palette", bad); w.Code != 400 {
			t.Errorf("%v accepted: %d", bad, w.Code)
		}
	}
	if p, _ := h.store.SitePalette(t.Context()); p != ui.Slate {
		t.Fatal("rejected request changed the palette", p)
	}
	// A suspended owner cannot save, and a stale stored name falls back.
	if _, err := h.store.DB.Exec("UPDATE users SET suspended=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetSitePalette(t.Context(), 1, "sage"); err == nil {
		t.Fatal("suspended owner saved a palette")
	}
	if _, err := h.store.DB.Exec("UPDATE site_palette SET palette='retired'"); err != nil {
		t.Fatal(err)
	}
	if p, err := h.store.SitePalette(t.Context()); err != nil || p != ui.Sage {
		t.Fatal("stale palette", p, err)
	}
}

func TestSharedPageHelpersAreUsed(t *testing.T) {
	h := webHarness(t)
	account := h.request(2, "GET", "/account", nil).Body.String()
	if !strings.Contains(account, "Tidings JSON file, up to "+ui.Size(MaxRestoreBytes)) {
		t.Fatal("restore limit copy")
	}
	sources := h.request(2, "GET", "/sources", nil).Body.String()
	if !strings.Contains(sources, "OPML file, up to "+ui.Size(MaxOPMLBytes)) {
		t.Fatal("OPML limit copy")
	}
	// Limits are binary and labelled in IEC units; the copy follows the limit.
	for limit, want := range map[int64]string{MaxOPMLBytes: "1 MiB", MaxRestoreBytes: "16 MiB", MaxFeedBytes: "4 MiB", imageDownloadLimit: "5 MiB"} {
		if got := ui.Size(limit); got != want {
			t.Errorf("limit %d shows as %q, want %q", limit, got, want)
		}
	}
	if MaxOPMLBytes != 1<<20 || MaxRestoreBytes != 16<<20 || MaxFeedBytes != 4<<20 || imageDownloadLimit != 5<<20 {
		t.Fatal("a size limit is no longer a binary value")
	}
	if _, err := ParseOPML([]byte(strings.Repeat(" ", MaxOPMLBytes+1))); err == nil || !strings.Contains(err.Error(), ui.Size(MaxOPMLBytes)) {
		t.Fatal(err)
	}
	if w := h.request(1, "POST", "/account/invite", nil); !strings.Contains(w.Body.String(), `readonly data-comfy-copy value=`) {
		t.Fatal("invitation link has no copy button")
	}
	if strings.Contains(account, "/static/theme.js") {
		t.Fatal("app theme script still loaded")
	}
	if w := h.request(0, "GET", "/static/theme.js", nil); w.Code != 404 {
		t.Fatal("app theme script still served", w.Code)
	}
	if page := h.request(2, "GET", "/api/v1/missing", nil); page.Code != 404 {
		t.Fatal(page.Code)
	}
}

// A 0.8.0 database (schema 5, no site_palette table) gains the table when it
// opens, keeps its data, and starts on Tidings' default palette.
func TestSitePaletteReachesUpgradedDatabases(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reader")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`DROP TABLE site_palette;
	ALTER TABLE states DROP COLUMN skipped;
	ALTER TABLE subscription_tags DROP COLUMN position; ALTER TABLE household_tags DROP COLUMN position;
	INSERT INTO users(id,username,password,owner) VALUES(1,'host','x',1);
	PRAGMA user_version=5`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var version, tables int
	if err = upgraded.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 7 {
		t.Fatal(version, err)
	}
	if err = upgraded.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='site_palette'").Scan(&tables); err != nil || tables != 1 {
		t.Fatal("site_palette missing after upgrade", tables, err)
	}
	if p, err := upgraded.SitePalette(t.Context()); err != nil || p != ui.Sage {
		t.Fatal(p, err)
	}
	if err = upgraded.SetSitePalette(t.Context(), 1, "slate"); err != nil {
		t.Fatal(err)
	}
	if p, _ := upgraded.SitePalette(t.Context()); p != ui.Slate {
		t.Fatal(p)
	}
	if _, err = upgraded.DB.Exec("INSERT INTO site_palette(id,palette) VALUES(2,'sage')"); err == nil {
		t.Fatal("second palette row accepted")
	}
}
