// SPDX-License-Identifier: AGPL-3.0-or-later

package reader

import (
	"context"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/airencracken/comfylib/twofactor"
	"golang.org/x/crypto/bcrypt"
)

func (a *App) registerTwoFactor(mux *http.ServeMux) error {
	factor, err := twofactor.New(context.Background(), a.Store.DB, os.Getenv("TIDINGS_TOTP_KEY"),
		"Tidings", "/", twofactor.Sessions{
			CredentialSQL: `SELECT password FROM users WHERE id=? AND suspended=0`,
			TokenColumn:   "token", ExpiryColumn: "expires",
			Lifetime: 30 * 24 * time.Hour, Milliseconds: false,
		}, twofactor.Hooks{
			Request: func(r *http.Request) (int64, string, string) {
				u, csrf := current(r).User, current(r).CSRF
				if u.ID == 0 {
					return 0, "", csrf
				}
				session, err := r.Cookie(a.cookieName())
				if err != nil {
					return 0, "", csrf
				}
				return u.ID, session.Value, csrf
			},
			CookieName: func(r *http.Request) string { return a.cookieName() + "-twofactor" },
			Session: func(w http.ResponseWriter, r *http.Request, raw string) {
				a.cookie(w, a.cookieName(), raw, 30*86400)
			},
			CheckPassword: func(hash, plain string) bool {
				return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
			},
			Render: func(w http.ResponseWriter, r *http.Request, status int, title string, content template.HTML) {
				a.render(w, r, page{Title: title, Kind: "two-factor", FactorContent: content}, status)
			},
			Fail: func(w http.ResponseWriter, r *http.Request, err error) { a.fail(w, r, err) },
		})
	if err != nil {
		return err
	}
	a.factor = factor
	factor.Register(mux)
	return nil
}

func (a *App) finishFirstFactor(w http.ResponseWriter, r *http.Request, id int64, hash string) bool {
	target, handled, err := a.factor.Start(w, r, id, hash)
	if err != nil {
		a.fail(w, r, err)
	} else if handled {
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
	return handled
}
