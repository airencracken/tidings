// SPDX-License-Identifier: AGPL-3.0-or-later
package reader

import (
	"github.com/airencracken/comfylib/admin"
	"net/http"
	"time"
)

func (a *App) metrics(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		http.Error(w, "Only owners can view instance metrics", http.StatusForbidden)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	metrics, err := a.Store.Metrics(r.Context(), time.Now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, page{Kind: "metrics", Title: "Instance metrics", Overview: admin.View{Metrics: metrics}}, 200)
}
