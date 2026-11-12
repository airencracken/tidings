// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSharedSignInRecordsOnlySuccessfulActiveAccounts(t *testing.T) {
	h := webHarness(t)
	a := h.app
	request := httptest.NewRequest("GET", "http://example.test/auth/shared/callback", nil)
	if _, err := a.sharedSignIn(httptest.NewRecorder(), request, 2); err != nil {
		t.Fatal(err)
	}
	report, err := h.store.Analytics(t.Context(), time.Now())
	if err != nil || report.Logins != 1 {
		t.Fatal(report, err)
	}
	if _, err = h.store.DB.Exec("UPDATE users SET suspended=1 WHERE id=?", 2); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if _, err := a.sharedSignIn(response, request, 2); err == nil {
		t.Fatal("inactive account signed in")
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("inactive account received a session")
	}
	report, err = h.store.Analytics(t.Context(), time.Now())
	if err != nil || report.Logins != 1 {
		t.Fatal("inactive sign-in counted", report, err)
	}
}
