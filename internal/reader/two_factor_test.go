// SPDX-License-Identifier: AGPL-3.0-or-later

package reader

import (
	"errors"
	"strings"
	"testing"

	"github.com/airencracken/comfylib/twofactor/factortest"
)

func TestAuthenticatorSignIn(t *testing.T) {
	t.Setenv("TIDINGS_TOTP_KEY", strings.Repeat("ab", 32))
	h := webHarness(t)
	factortest.Run(t, factortest.Config{
		Handler: h.app, Origin: "https://tidings.example.test",
		Username: "alice", Password: testPassword, Home: "/",
	})
}

func TestSuspendedPasswordCannotAdvanceSignIn(t *testing.T) {
	s := testStore(t)
	if _, err := s.DB.Exec("UPDATE users SET suspended=1 WHERE username='alice'"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.checkCredentials(t.Context(), "alice", testPassword); !errors.Is(err, ErrCredentials) {
		t.Fatalf("suspended password check returned %v", err)
	}
}
