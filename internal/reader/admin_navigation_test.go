package reader

import (
	"strings"
	"testing"
)

func TestAdminNavigationAndOwnerOnlySettings(t *testing.T) {
	h := webHarness(t)
	for _, path := range []string{"/", "/sources", "/groups", "/account", "/admin"} {
		owner := h.request(1, "GET", path, nil)
		if owner.Code != 200 || !strings.Contains(owner.Body.String(), `href="/admin"`) || !strings.Contains(owner.Body.String(), `>Admin</a>`) {
			t.Fatalf("owner cannot discover administration from %s: %d", path, owner.Code)
		}
		member := h.request(2, "GET", path, nil)
		if strings.Contains(member.Body.String(), `href="/admin"`) || strings.Contains(member.Body.String(), "Admin settings") {
			t.Fatalf("member sees owner controls at %s", path)
		}
	}
	for _, path := range []string{"/admin", "/admin?owner=true", "/admin?role=admin"} {
		if response := h.request(2, "GET", path, nil); response.Code != 403 {
			t.Fatalf("member opened administration: %s: %d", path, response.Code)
		}
		if response := h.request(0, "GET", path, nil); response.Code != 303 || response.Header().Get("Location") != "/login" {
			t.Fatalf("guest opened administration: %d", response.Code)
		}
	}
	body := h.request(1, "GET", "/admin", nil).Body.String()
	for _, control := range []string{"<h1>Admin settings</h1>", "Suggested sources", "Feed images", "Feed fetching", "Household members", "Instance backups", `href="/account/invitations"`, `name="csrf"`} {
		if !strings.Contains(body, control) {
			t.Fatal("missing administration control", control)
		}
	}
	if strings.Contains(body, "Change your password") {
		t.Fatal("administration unexpectedly shows personal account forms")
	}
	if _, err := h.store.DB.Exec("UPDATE users SET suspended=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if response := h.request(1, "GET", "/admin", nil); response.Code != 303 {
		t.Fatal("suspended owner reached administration", response.Code)
	}
}
