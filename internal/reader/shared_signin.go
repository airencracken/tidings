// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"fmt"
	"github.com/airencracken/comfylib/token"
	"github.com/airencracken/tidings/internal/sharedoidc"
	"github.com/airencracken/tidings/internal/suite"
	"net/http"
	"time"
)

func (a *App) registerSharedSignIn(mux *http.ServeMux) error {
	auth, err := sharedoidc.FromEnvironment("TIDINGS_", a.config.BaseURL, a.Store.DB, suite.AuthHooks{
		RateKey: a.clientKey,
		Current: func(r *http.Request) (int64, string) {
			u := current(r).User
			binding := ""
			if c, err := r.Cookie(a.cookieName()); err == nil {
				binding = c.Value
			}
			return u.ID, binding
		},
		CSRF: func(r *http.Request) (string, string) { return "csrf", current(r).CSRF },
		SignIn: func(w http.ResponseWriter, r *http.Request, id int64) (string, error) {
			raw, digest := token.New()
			result, err := a.Store.DB.ExecContext(r.Context(), "INSERT INTO sessions(token,user_id,expires) SELECT ?,id,? FROM users WHERE id=? AND suspended=0", digest, time.Now().Add(30*24*time.Hour).Unix(), id)
			if err != nil {
				return "", err
			}
			count, err := result.RowsAffected()
			if err != nil || count != 1 {
				return "", fmt.Errorf("account unavailable")
			}
			a.cookie(w, a.cookieName(), raw, 30*86400)
			return "/", nil
		},
	})
	if err != nil {
		return err
	}
	if auth != nil {
		auth.Register(mux)
	}
	return nil
}
