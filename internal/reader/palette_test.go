// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"net/url"
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
