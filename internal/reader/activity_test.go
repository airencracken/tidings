// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestActivityCountsAuthorizedOpensAndSuccessfulLogins(t *testing.T) {
	h := webHarness(t)
	feed := testSource(t, h.store, 2, "personal", false)
	id := entryID(t, h.store, feed)
	if _, err := h.store.Login(t.Context(), "alice", testPassword); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fmt.Sprintf("/story/%d", id), fmt.Sprintf("/story/%d", id), fmt.Sprintf("/story/%04d", id)} {
		if w := h.request(2, "GET", path, nil); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	h.request(2, "HEAD", fmt.Sprintf("/story/%d", id), nil)
	h.request(1, "GET", fmt.Sprintf("/story/%d", id), nil)
	h.request(2, "GET", "/story/99999999", nil)
	h.request(2, "GET", "/", nil)
	result, err := h.store.Analytics(t.Context(), time.Now())
	if err != nil || result.Logins != 1 || result.Views != 1 {
		t.Fatal(result, err)
	}
	if _, err = h.store.Login(t.Context(), "alice", "wrong-password"); err == nil {
		t.Fatal("bad login accepted")
	}
	result, err = h.store.Analytics(t.Context(), time.Now())
	if err != nil || result.Logins != 1 {
		t.Fatal("bad login counted", result, err)
	}
	for _, uid := range []int64{0, 2, 3} {
		if w := h.request(uid, "GET", "/admin/metrics?owner=true", nil); w.Code == 200 || strings.Contains(w.Body.String(), "Recent sign-ins") {
			t.Fatal("analytics exposed", uid, w.Code)
		}
	}
	if w := h.request(1, "GET", "/admin/metrics", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "comfy-admin-dashboard") {
		t.Fatal(w.Code, w.Body.String())
	}
}
