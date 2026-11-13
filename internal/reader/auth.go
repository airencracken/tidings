package reader

import (
	"context"
	"database/sql"
	"errors"
	"github.com/airencracken/comfylib/ratelimit"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/airencracken/comfylib/token"
	"golang.org/x/crypto/bcrypt"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{2,40}$`)
var ErrCredentials = errors.New("check your username and password")

// ErrInvitation means an invitation link is invalid, expired, used or revoked.
var ErrInvitation = errors.New("invitation is invalid, expired, or already used")

func checkPassword(hash []byte, password string) error {
	return bcrypt.CompareHashAndPassword(hash, []byte(password))
}

func ValidateCredentials(username, password string) error {
	if !usernameRE.MatchString(username) {
		return errors.New("username must be 2–40 letters, numbers, underscores or hyphens")
	}
	if utf8.RuneCountInString(password) < 12 {
		return errors.New("password must be 12 or more characters")
	}
	if len(password) > 72 {
		return errors.New("password is too long; use a shorter phrase")
	}
	return nil
}

func (s *Store) CreateOwner(ctx context.Context, username, password string) error {
	if err := ValidateCredentials(username, password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "INSERT INTO users(username,password,owner) VALUES(?,?,1)", username, hash)
	return err
}

func (s *Store) SetPassword(ctx context.Context, username, password string) error {
	if err := ValidateCredentials(username, password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE users SET password=? WHERE username=?", hash, username)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE username=?)", username); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Login(ctx context.Context, username, password string) (string, error) {
	var id int64
	var hash []byte
	err := s.DB.QueryRowContext(ctx, "SELECT id,password FROM users WHERE username=? AND suspended=0", username).Scan(&id, &hash)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if err == sql.ErrNoRows {
		hash = []byte("$2a$10$7EqJtq98hPqEX7fNZaFWoO5Uj3G1QYtO.JcH6r1e1V9x1sJzHth5q")
	}
	compare := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err != nil || compare != nil {
		return "", ErrCredentials
	}
	raw, digest := token.New()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires<=?", time.Now().Unix()); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(token,user_id,expires) VALUES(?,?,?)", digest, id, time.Now().Add(30*24*time.Hour).Unix()); err != nil {
		return "", err
	}
	return raw, tx.Commit()
}

func (s *Store) Session(ctx context.Context, raw string) (User, error) {
	var u User
	if len(raw) != 64 {
		return u, sql.ErrNoRows
	}
	err := s.DB.QueryRowContext(ctx, "SELECT u.id,u.username,u.owner,u.group_optin,u.keyboard_shortcuts FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.token=? AND s.expires>? AND u.suspended=0", token.Hash(raw), time.Now().Unix()).Scan(&u.ID, &u.Username, &u.Owner, &u.GroupOptin, &u.KeyboardShortcuts)
	return u, err
}

func (s *Store) Invite(ctx context.Context) (string, error) {
	raw, digest := token.New()
	_, err := s.DB.ExecContext(ctx, "INSERT INTO invites(token,expires) VALUES(?,?)", digest, time.Now().Add(7*24*time.Hour).Unix())
	return raw, err
}

func (s *Store) Join(ctx context.Context, code, username, password string) error {
	if err := ValidateCredentials(username, password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE invites SET used=1 WHERE token=? AND used=0 AND revoked=0 AND expires>?", token.Hash(code), time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvitation
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO users(username,password) VALUES(?,?)", username, hash); err != nil {
		return errors.New("that username is already taken")
	}
	return tx.Commit()
}

type limiter struct {
	once   sync.Once
	shared *ratelimit.Window
}

func (l *limiter) allow(key string) bool {
	l.once.Do(func() {
		l.shared = ratelimit.NewWindow(ratelimit.WindowConfig{Attempts: 20, Interval: 15 * time.Minute, Capacity: 4096, RejectNew: true})
	})
	ok, _ := l.shared.Allow(key)
	return ok
}
