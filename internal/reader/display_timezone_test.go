package reader

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDisplayTimezoneAccountRoutesAndRendering(t *testing.T) {
	h := webHarness(t)
	if w := h.request(2, "GET", "/account", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `value="UTC"`) {
		t.Fatal("missing default preference", w.Code)
	}
	if w := h.request(2, "POST", "/account/timezone", url.Values{"timezone": {"Asia/Tokyo"}, "csrf": {"wrong"}}); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	if w := h.request(2, "POST", "/account/timezone", url.Values{"timezone": {"America/Los_Angeles"}, "user_id": {"3"}}); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	if zone, err := h.store.Timezone(t.Context(), 2); err != nil || zone != "America/Los_Angeles" {
		t.Fatal(zone, err)
	}
	if zone, _ := h.store.Timezone(t.Context(), 3); zone != "UTC" {
		t.Fatal("changed another account")
	}
	feed := testSource(t, h.store, 2, "personal", false)
	id := entryID(t, h.store, feed)
	stamp := time.Date(2026, 1, 1, 0, 15, 0, 0, time.UTC)
	if _, err := h.store.DB.Exec("UPDATE entries SET published=? WHERE id=?", stamp.Unix(), id); err != nil {
		t.Fatal(err)
	}
	w := h.request(2, "GET", fmt.Sprintf("/story/%d", id), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Dec 31, 2025 · 16:15 PST") {
		t.Fatal("local date disagreement", w.Code, w.Body.String())
	}
	for _, values := range []url.Values{{"timezone": {"Local"}}, {"timezone": {"UTC", "Asia/Tokyo"}}, {"timezone": {"<script>bad</script>"}}} {
		w := h.request(2, "POST", "/account/timezone", values)
		if w.Code != 422 || strings.Contains(w.Body.String(), "<script>bad</script>") {
			t.Fatal("invalid preference accepted or unescaped", w.Code)
		}
		if zone, _ := h.store.Timezone(t.Context(), 2); zone != "America/Los_Angeles" {
			t.Fatal("invalid save changed preference")
		}
	}
	if w := h.request(0, "POST", "/account/timezone", url.Values{"timezone": {"UTC"}}); w.Code != 303 {
		t.Fatal("guest set timezone", w.Code)
	}
	if _, err := h.store.DB.Exec("UPDATE users SET suspended=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if h.store.SetTimezone(t.Context(), 2, "UTC") == nil {
		t.Fatal("suspended account changed preference")
	}
}
