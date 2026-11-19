// SPDX-License-Identifier: AGPL-3.0-or-later

package reader

import (
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
