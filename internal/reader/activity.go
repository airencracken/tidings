// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"github.com/airencracken/comfylib/analytics"
	"net/http"
)

func (a *App) trackStory(next http.HandlerFunc) http.HandlerFunc {
	return analytics.Track(a.Store.DB, next, func(r *http.Request) []analytics.Event {
		u := current(r).User
		if u.ID == 0 {
			return nil
		}
		item := analytics.NumericID(r.PathValue("id"))
		if item == "" {
			return nil
		}
		return []analytics.Event{{Kind: analytics.Story, Member: analytics.MemberID(u.ID), Item: item}}
	})
}
