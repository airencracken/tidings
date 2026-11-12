package reader

import (
	"errors"
	"github.com/airencracken/comfylib/displaytime"
	"net/http"
)

func preferredTimezone(r *http.Request) string {
	if r == nil {
		return "UTC"
	}
	return displaytime.New(current(r).User.Timezone).Name()
}
func timezoneControl(r *http.Request, draft *string, problem, notice string) displaytime.Preference {
	name := preferredTimezone(r)
	if draft != nil {
		name = *draft
	}
	if r.URL.Query().Get("saved") == "timezone" {
		notice = "Your display timezone has been saved."
	}
	return displaytime.Preference{Action: "/account/timezone", CSRF: current(r).CSRF, Name: name, Error: problem, Notice: notice}
}
func (a *App) saveTimezone(w http.ResponseWriter, r *http.Request) {
	name, err := displaytime.ParseForm(r.PostForm)
	if err == nil {
		err = a.Store.SetTimezone(r.Context(), current(r).User.ID, name)
	}
	if errors.Is(err, displaytime.ErrTimezone) {
		a.render(w, r, page{Kind: "account", Title: "Your place here", TimezoneInput: &name, TimezoneError: err.Error()}, 422)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/account?saved=timezone#display-timezone", 303)
}
