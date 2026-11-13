package reader

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// PublicOrigin validates shareable origins. Plain HTTP is for local development.
func PublicOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("set this instance's public HTTPS address before creating invitations (TIDINGS_BASE_URL)")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return "", errors.New("the public address must use HTTPS; HTTP is allowed only on a loopback IP")
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (a *App) invitations(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		a.problem(w, r, http.StatusForbidden, "", nil)
		return
	}
	a.renderInvitations(w, r, "", "")
}

func (a *App) renderInvitations(w http.ResponseWriter, r *http.Request, link, reference string) {
	list, err := a.Store.Invitations(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, page{Title: "Invitations", Kind: "invitations", Invite: link, InviteRef: reference, Invitations: list}, http.StatusOK)
}
