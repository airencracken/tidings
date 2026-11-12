package reader

import (
	"context"
	"database/sql"
	"errors"
	"github.com/airencracken/comfylib/analytics"
	"net/http"
	"strings"
	"time"
)

type Invitation struct {
	Token   string
	Expires int64
	Status  string
}

func (s *Store) Invitations(ctx context.Context) ([]Invitation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT token,expires,CASE WHEN revoked=1 THEN 'Revoked' WHEN used=1 THEN 'Used' WHEN expires<=? THEN 'Expired' ELSE 'Available' END FROM invites ORDER BY expires DESC,rowid DESC LIMIT 100`, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var v Invitation
		if err = rows.Scan(&v.Token, &v.Expires, &v.Status); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Members(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,username,owner,suspended FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Username, &u.Owner, &u.Suspended); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Authorization and the last-active-owner invariant are checked in the same transaction.
func (s *Store) ManageMember(ctx context.Context, actor, target int64, action, confirmation, currentPassword string) error {
	if action != "suspend" && action != "resume" && action != "remove" {
		return errors.New("unknown member action")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash []byte
	if err = tx.QueryRowContext(ctx, "SELECT password FROM users WHERE id=? AND owner=1 AND suspended=0", actor).Scan(&hash); err != nil {
		return sql.ErrNoRows
	}
	if err = checkPassword(hash, currentPassword); err != nil {
		return errors.New("current password is incorrect")
	}
	var name string
	var owner, suspended bool
	if err = tx.QueryRowContext(ctx, "SELECT username,owner,suspended FROM users WHERE id=?", target).Scan(&name, &owner, &suspended); err != nil {
		return err
	}
	if action == "remove" && confirmation != name {
		return errors.New("type the member's username to confirm removal")
	}
	if owner && !suspended && action != "resume" {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE owner=1 AND suspended=0").Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			return errors.New("keep at least one active owner")
		}
	}
	if action == "remove" {
		if err := analytics.ForgetMember(ctx, tx, analytics.MemberID(target)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM users WHERE id=?", target)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE users SET suspended=? WHERE id=?", action == "suspend", target)
	}
	if err != nil {
		return err
	}
	if action != "resume" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", target); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (a *App) memberAction(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	if !u.Owner {
		http.Error(w, "Only owners can manage members", 403)
		return
	}
	id, err := pathID(r)
	if err == nil {
		err = a.Store.ManageMember(r.Context(), u.ID, id, r.PostFormValue("action"), r.PostFormValue("confirm"), r.PostFormValue("current"))
	}
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	http.Redirect(w, r, adminReturn(r, ""), 303)
}
func (a *App) revokeInvite(w http.ResponseWriter, r *http.Request) {
	if !current(r).User.Owner {
		http.Error(w, "Only owners can revoke invitations", 403)
		return
	}
	if _, err := a.Store.DB.ExecContext(r.Context(), "UPDATE invites SET revoked=1 WHERE token=?", r.PostFormValue("invitation")); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/account/invitations", 303)
}
func (s *Store) SetTopic(ctx context.Context, u User, fid int64, raw string, household bool) error {
	topic, err := category(raw)
	if err != nil {
		return err
	}
	q := "UPDATE subscriptions SET category=? WHERE user_id=? AND feed_id=?"
	args := []any{topic, u.ID, fid}
	if household {
		q = "UPDATE group_feeds SET category=? WHERE feed_id=? AND EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1 AND suspended=0)"
		args = []any{topic, fid, u.ID}
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
func (s *Store) RenameGroup(ctx context.Context, u User, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 || strings.ContainsAny(name, "\r\n\x00") {
		return errors.New("group name must be 1–60 characters on one line")
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE feed_groups SET name=? WHERE id=? AND (user_id=? OR (user_id IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=? AND owner=1 AND suspended=0)))`, name, id, u.ID, u.ID)
	if err != nil {
		return errors.New("could not rename group; choose a unique name")
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
func (a *App) sourceTopic(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = a.Store.SetTopic(r.Context(), current(r).User, id, r.PostFormValue("category"), scope(r))
	}
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	to := "/sources"
	if scope(r) {
		to += "?scope=household"
	}
	http.Redirect(w, r, to, 303)
}
func (a *App) renameGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		err = a.Store.RenameGroup(r.Context(), current(r).User, id, r.PostFormValue("name"))
	}
	if err != nil {
		a.bad(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/groups", 303)
}
