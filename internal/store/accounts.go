// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"
	"strings"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// CreateAccount inserts a new account and returns its assigned id. Email is
// lowercased before storage (the identity invariant, ADR-0003); a duplicate
// email returns ErrConflict. The caller supplies the already-hashed secret.
func (db *DB) CreateAccount(ctx context.Context, a Account) (int64, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Put(conn)

	err = sqlitex.Execute(conn, `
		INSERT INTO accounts (email, display_name, hashed_secret, is_admin, is_active)
		VALUES (?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
		Args: []any{strings.ToLower(a.Email), a.DisplayName, a.HashedSecret, a.IsAdmin, a.IsActive},
	})
	if err != nil {
		if isConstraint(err) {
			return 0, fmt.Errorf("create account %q: %w", a.Email, ErrConflict)
		}
		return 0, fmt.Errorf("create account: %w", err)
	}
	return conn.LastInsertRowID(), nil
}

// GetAccount returns the account with the given id, or ErrRecordNotFound.
func (db *DB) GetAccount(ctx context.Context, id int64) (Account, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Account{}, err
	}
	defer db.Put(conn)
	return getAccountWhere(conn, "id = ?", id)
}

// GetAccountByEmail returns the account with the given email (matched
// case-insensitively), or ErrRecordNotFound. It is the lookup behind login.
func (db *DB) GetAccountByEmail(ctx context.Context, email string) (Account, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Account{}, err
	}
	defer db.Put(conn)
	return getAccountWhere(conn, "email = ?", strings.ToLower(email))
}

// getAccountWhere reads the single account matching one WHERE predicate (id or
// email), returning ErrRecordNotFound when there is no match.
func getAccountWhere(conn *sqlite.Conn, where string, arg any) (Account, error) {
	var (
		a     Account
		found bool
	)
	err := sqlitex.Execute(conn, `
		SELECT id, email, display_name, hashed_secret, is_admin, is_active
		FROM accounts WHERE `+where, &sqlitex.ExecOptions{
		Args: []any{arg},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			a = Account{
				ID:           stmt.ColumnInt64(0),
				Email:        stmt.ColumnText(1),
				DisplayName:  stmt.ColumnText(2),
				HashedSecret: stmt.ColumnText(3),
				IsAdmin:      stmt.ColumnBool(4),
				IsActive:     stmt.ColumnBool(5),
			}
			found = true
			return nil
		},
	})
	if err != nil {
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	if !found {
		return Account{}, ErrRecordNotFound
	}
	return a, nil
}

// UpdateAccount writes the mutable fields (email, display name, hashed secret,
// admin, active) of the account identified by a.ID. Email is lowercased; a
// duplicate email returns ErrConflict, and an unknown id returns
// ErrRecordNotFound.
func (db *DB) UpdateAccount(ctx context.Context, a Account) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	err = sqlitex.Execute(conn, `
		UPDATE accounts
		SET email = ?, display_name = ?, hashed_secret = ?, is_admin = ?, is_active = ?
		WHERE id = ?`, &sqlitex.ExecOptions{
		Args: []any{strings.ToLower(a.Email), a.DisplayName, a.HashedSecret, a.IsAdmin, a.IsActive, a.ID},
	})
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("update account %d: %w", a.ID, ErrConflict)
		}
		return fmt.Errorf("update account %d: %w", a.ID, err)
	}
	if conn.Changes() == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// ListAccounts returns every account, active and inactive, ordered by id.
func (db *DB) ListAccounts(ctx context.Context) ([]Account, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Put(conn)

	var accounts []Account
	err = sqlitex.Execute(conn, `
		SELECT id, email, display_name, hashed_secret, is_admin, is_active
		FROM accounts ORDER BY id`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			accounts = append(accounts, Account{
				ID:           stmt.ColumnInt64(0),
				Email:        stmt.ColumnText(1),
				DisplayName:  stmt.ColumnText(2),
				HashedSecret: stmt.ColumnText(3),
				IsAdmin:      stmt.ColumnBool(4),
				IsActive:     stmt.ColumnBool(5),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return accounts, nil
}
