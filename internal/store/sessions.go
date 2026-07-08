// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// CreateSession persists a new session. The caller (the auth layer) supplies the
// opaque id, the account, the token hash, and the issue/expiry times; Actor is
// set only for an impersonation session and is stored as NULL when zero. The raw
// token is never passed here — only its hash is stored (ADR-0002).
func (db *DB) CreateSession(ctx context.Context, s Session) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	var actor any
	if s.Actor != 0 {
		actor = s.Actor
	}
	err = sqlitex.Execute(conn, `
		INSERT INTO sessions (id, account_id, hashed_token, actor_account_id, issued_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
		Args: []any{s.ID, s.AccountID, s.HashedToken, actor, unixSeconds(s.IssuedAt), unixSeconds(s.ExpiresAt)},
	})
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("create session: %w", ErrConflict)
		}
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetSession returns the session with the given id regardless of state (it may
// be revoked or expired), or ErrRecordNotFound.
func (db *DB) GetSession(ctx context.Context, id string) (Session, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Session{}, err
	}
	defer db.Put(conn)
	return getSessionWhere(conn, "id = ?", id)
}

// GetSessionByToken resolves a presented credential to its session by token
// hash, regardless of state, or ErrRecordNotFound. Callers that need a usable
// session must still check Revoked and expiry; GetActiveSessionByToken does both.
func (db *DB) GetSessionByToken(ctx context.Context, hashedToken string) (Session, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Session{}, err
	}
	defer db.Put(conn)
	return getSessionWhere(conn, "hashed_token = ?", hashedToken)
}

// GetActiveSessionByToken resolves a presented credential to a session that is
// neither revoked nor expired as of now. A missing, revoked, or expired session
// all return ErrRecordNotFound, so authentication cannot distinguish them.
func (db *DB) GetActiveSessionByToken(ctx context.Context, hashedToken string, now time.Time) (Session, error) {
	s, err := db.GetSessionByToken(ctx, hashedToken)
	if err != nil {
		return Session{}, err
	}
	if s.Revoked() || !s.ExpiresAt.After(now) {
		return Session{}, ErrRecordNotFound
	}
	return s, nil
}

// getSessionWhere reads the single session matching one WHERE predicate,
// returning ErrRecordNotFound when there is no match.
func getSessionWhere(conn *sqlite.Conn, where string, arg any) (Session, error) {
	var (
		s     Session
		found bool
	)
	err := sqlitex.Execute(conn, `
		SELECT id, account_id, hashed_token, actor_account_id, issued_at, expires_at, revoked_at
		FROM sessions WHERE `+where, &sqlitex.ExecOptions{
		Args:       []any{arg},
		ResultFunc: func(stmt *sqlite.Stmt) error { s = scanSession(stmt); found = true; return nil },
	})
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	if !found {
		return Session{}, ErrRecordNotFound
	}
	return s, nil
}

// scanSession reads a session from the current row, whose columns are, in order:
// id, account_id, hashed_token, actor_account_id, issued_at, expires_at,
// revoked_at. The nullable actor and revoked_at map to zero values.
func scanSession(stmt *sqlite.Stmt) Session {
	var actor int64
	if !stmt.ColumnIsNull(3) {
		actor = stmt.ColumnInt64(3)
	}
	var revoked time.Time
	if !stmt.ColumnIsNull(6) {
		revoked = unixOrZero(stmt.ColumnInt64(6))
	}
	return Session{
		ID:          stmt.ColumnText(0),
		AccountID:   stmt.ColumnInt64(1),
		HashedToken: stmt.ColumnText(2),
		Actor:       actor,
		IssuedAt:    unixOrZero(stmt.ColumnInt64(4)),
		ExpiresAt:   unixOrZero(stmt.ColumnInt64(5)),
		RevokedAt:   revoked,
	}
}

// ListActiveSessionsByAccount returns the account's sessions that are neither
// revoked nor expired as of now, ordered by issue time (newest first). It backs
// both the self and admin session listings.
func (db *DB) ListActiveSessionsByAccount(ctx context.Context, accountID int64, now time.Time) ([]Session, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Put(conn)

	var sessions []Session
	err = sqlitex.Execute(conn, `
		SELECT id, account_id, hashed_token, actor_account_id, issued_at, expires_at, revoked_at
		FROM sessions
		WHERE account_id = ? AND revoked_at IS NULL AND expires_at > ?
		ORDER BY issued_at DESC, id`, &sqlitex.ExecOptions{
		Args:       []any{accountID, unixSeconds(now)},
		ResultFunc: func(stmt *sqlite.Stmt) error { sessions = append(sessions, scanSession(stmt)); return nil },
	})
	if err != nil {
		return nil, fmt.Errorf("list sessions for account %d: %w", accountID, err)
	}
	return sessions, nil
}

// RevokeSession soft-deletes one session by id, stamping revoked_at with now. An
// unknown id returns ErrRecordNotFound; re-revoking an already-revoked session is
// a no-op that leaves the original revoked_at in place.
func (db *DB) RevokeSession(ctx context.Context, id string, now time.Time) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	err = sqlitex.Execute(conn,
		`UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		&sqlitex.ExecOptions{Args: []any{unixSeconds(now), id}})
	if err != nil {
		return fmt.Errorf("revoke session %q: %w", id, err)
	}
	if conn.Changes() == 0 {
		// Either the id is unknown or it was already revoked; distinguish so a
		// double-revoke is not mistaken for a missing session.
		if _, gerr := getSessionWhere(conn, "id = ?", id); gerr != nil {
			return gerr
		}
	}
	return nil
}

// RevokeAccountSessions soft-deletes every currently active session for an
// account and returns how many were revoked. Use it when deactivating or locking
// out an account.
func (db *DB) RevokeAccountSessions(ctx context.Context, accountID int64, now time.Time) (int64, error) {
	return db.revokeAccountSessions(ctx, accountID, "", now)
}

// RevokeAccountSessionsExcept soft-deletes every active session for an account
// other than the one named by exceptID, returning how many were revoked. It backs
// "log out my other sessions" after a secret change (ADR-0002).
func (db *DB) RevokeAccountSessionsExcept(ctx context.Context, accountID int64, exceptID string, now time.Time) (int64, error) {
	return db.revokeAccountSessions(ctx, accountID, exceptID, now)
}

// revokeAccountSessions revokes an account's active sessions, optionally sparing
// one id, returning the number revoked.
func (db *DB) revokeAccountSessions(ctx context.Context, accountID int64, exceptID string, now time.Time) (int64, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Put(conn)

	query := `UPDATE sessions SET revoked_at = ? WHERE account_id = ? AND revoked_at IS NULL`
	args := []any{unixSeconds(now), accountID}
	if exceptID != "" {
		query += ` AND id <> ?`
		args = append(args, exceptID)
	}
	if err := sqlitex.Execute(conn, query, &sqlitex.ExecOptions{Args: args}); err != nil {
		return 0, fmt.Errorf("revoke sessions for account %d: %w", accountID, err)
	}
	return int64(conn.Changes()), nil
}

// PurgeExpiredSessions hard-deletes session rows that expired at or before now,
// returning how many were removed. This is the only physical delete in the store
// — it reclaims dead session rows (the /admin/sessions/purge maintenance path);
// everything else soft-deletes.
func (db *DB) PurgeExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Put(conn)

	if err := sqlitex.Execute(conn,
		`DELETE FROM sessions WHERE expires_at <= ?`,
		&sqlitex.ExecOptions{Args: []any{unixSeconds(now)}}); err != nil {
		return 0, fmt.Errorf("purge expired sessions: %w", err)
	}
	return int64(conn.Changes()), nil
}
