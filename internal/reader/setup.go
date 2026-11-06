// SPDX-License-Identifier: AGPL-3.0-or-later

package reader

import (
	"context"

	"github.com/airencracken/comfylib/setup"
	"golang.org/x/crypto/bcrypt"
)

// HasOwner includes suspended owners so setup never reopens an existing instance.
func (s *Store) HasOwner(ctx context.Context) (bool, error) {
	var present bool
	err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE owner=1)").Scan(&present)
	return present, err
}

// CreateFirstOwner creates the browser setup account with an atomic owner guard.
func (s *Store) CreateFirstOwner(ctx context.Context, owner setup.Owner) error {
	if err := ValidateCredentials(owner.Username, owner.Password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(owner.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var present bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE owner=1)").Scan(&present); err != nil {
		return err
	}
	if present {
		return setup.ErrComplete
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO users(username,password,owner) VALUES(?,?,1)", owner.Username, hash); err != nil {
		return err
	}
	return tx.Commit()
}
