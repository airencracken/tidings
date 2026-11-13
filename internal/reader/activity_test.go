// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func activityRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow("SELECT count(*) FROM comfy_analytics_activity").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Tidings collects operational health only: no sign-in history and no record
// of which stories a member opened, through any sign-in path.
func TestSignInsAndStoryOpensAreNotRecorded(t *testing.T) {
	h := webHarness(t)
	feed := testSource(t, h.store, 2, "personal", false)
	id := entryID(t, h.store, feed)
	if _, err := h.store.Login(t.Context(), "alice", testPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.sharedSignIn(httptest.NewRecorder(), httptest.NewRequest("GET", "http://example.test/auth/shared/callback", nil), 2); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fmt.Sprintf("/story/%d", id), fmt.Sprintf("/story/%04d", id), "/", "/?view=all"} {
		if w := h.request(2, "GET", path, nil); w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
	if n := activityRows(t, h.store); n != 0 {
		t.Fatalf("%d per-member activity rows recorded", n)
	}
	summary, err := h.store.Analytics(t.Context(), time.Now())
	if err != nil || summary.Logins != 0 || summary.Views != 0 || summary.ActiveMembers != 0 || len(summary.LoginsByAccount) != 0 || len(summary.Popular) != 0 {
		t.Fatal(summary, err)
	}
	for _, uid := range []int64{0, 2, 3} {
		if w := h.request(uid, "GET", "/admin/metrics?owner=true", nil); w.Code == 200 || strings.Contains(w.Body.String(), "Instance overview") {
			t.Fatal("metrics exposed", uid, w.Code)
		}
	}
	if w := h.request(1, "GET", "/admin/metrics", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Sources with errors") || !strings.Contains(w.Body.String(), "Database size") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSharedSignInStillRejectsSuspendedAccounts(t *testing.T) {
	h := webHarness(t)
	request := httptest.NewRequest("GET", "http://example.test/auth/shared/callback", nil)
	var before int
	if err := h.store.DB.QueryRow("SELECT count(*) FROM sessions WHERE user_id=2").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.DB.Exec("UPDATE users SET suspended=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if _, err := h.app.sharedSignIn(response, request, 2); err == nil {
		t.Fatal("suspended account signed in")
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("suspended account received a session")
	}
	var sessions int
	if err := h.store.DB.QueryRow("SELECT count(*) FROM sessions WHERE user_id=2").Scan(&sessions); err != nil || sessions != before {
		t.Fatal(sessions, err)
	}
	if _, err := h.app.sharedSignIn(response, request, 99999); err == nil {
		t.Fatal("unknown account signed in")
	}
}

// Rows stored by earlier releases, including in restored backups, are deleted
// the next time the database opens. Other data is untouched.
func TestOpeningDatabaseDeletesStoredMemberActivity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reader")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err = s.DB.Exec(`INSERT INTO users(username,password) VALUES('kept','hash');
	INSERT INTO comfy_analytics_activity(day,kind,member,item,count,last_at) VALUES(?,'login','1','',3,?),(?,'story','1','4812',1,?),(?,'story','2','17',2,?)`,
		now/86400, now, now/86400, now, now/86400-1, now); err != nil {
		t.Fatal(err)
	}
	if n := activityRows(t, s); n != 3 {
		t.Fatal(n)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if n := activityRows(t, reopened); n != 0 {
		t.Fatalf("%d stored activity rows survived", n)
	}
	var users int
	if err = reopened.DB.QueryRow("SELECT count(*) FROM users WHERE username='kept'").Scan(&users); err != nil || users != 1 {
		t.Fatal("unrelated data changed", users, err)
	}
	// Reopening an already clean database is a no-op.
	if err = forgetMemberActivity(t.Context(), reopened.DB); err != nil {
		t.Fatal(err)
	}
}
