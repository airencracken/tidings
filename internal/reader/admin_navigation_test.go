package reader

import (
	"math/rand"
	"net/http/httptest"
	"net/url"
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

func TestAdminActionsReturnToTheirPageWithoutOpenRedirects(t *testing.T) {
	h := webHarness(t)
	for _, target := range []string{"/admin", "/admin?notice=images"} {
		response := h.request(1, "POST", "/account/images", url.Values{"return_to": {target}})
		if response.Code != 303 || response.Header().Get("Location") != "/admin?notice=images#feed-images" {
			t.Fatal("admin action lost its page", response.Code, response.Header())
		}
	}
	random := rand.New(rand.NewSource(20261111))
	targets := []string{"", "https://evil.example/admin", "//evil.example/admin", "javascript:alert(1)", "/admin\r\nLocation: https://evil.example", "/admin/../account", "/account", "/admin/extra"}
	for range 100 {
		targets = append(targets, string(rune(random.Intn(128)))+"/admin")
	}
	for _, target := range targets {
		request := httptest.NewRequest("POST", "/account/images", strings.NewReader(url.Values{"return_to": {target}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if actual := adminReturn(request, "?notice=images#feed-images"); actual != "/account?notice=images#feed-images" {
			t.Fatal("unsafe administration return", target, actual)
		}
	}
	if response := h.request(2, "POST", "/account/images", url.Values{"enabled": {"true"}, "return_to": {"/admin"}}); response.Code != 403 {
		t.Fatal("member changed settings via admin return", response.Code)
	}
}
