// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// AddMember seats an account in a game and returns the new Member. It mints the
// seat's player_id as one past the highest ever used in that game (active or
// inactive), so ids are sequential within the game and never reused (ADR-0003).
// The mint-and-insert runs in one transaction so concurrent seatings cannot
// collide on a player_id. An account already seated in the game (even inactive)
// returns ErrConflict; an unknown game or account returns ErrConflict via the
// foreign-key check.
func (db *DB) AddMember(ctx context.Context, gameID, accountID int64, isGM bool) (m Member, err error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Member{}, err
	}
	defer db.Put(conn)

	endTx := sqlitex.Transaction(conn)
	defer endTx(&err)

	// Next seat id = one past the current max for this game. COALESCE handles the
	// first seat (no rows yet). Because seats are soft-deleted, the max spans
	// active and inactive rows, guaranteeing ids are never reused.
	var nextID int64 = 1
	err = sqlitex.Execute(conn,
		`SELECT COALESCE(MAX(player_id), 0) + 1 FROM game_account_role WHERE game_id = ?`,
		&sqlitex.ExecOptions{
			Args:       []any{gameID},
			ResultFunc: func(stmt *sqlite.Stmt) error { nextID = stmt.ColumnInt64(0); return nil },
		})
	if err != nil {
		return Member{}, fmt.Errorf("add member: next player_id: %w", err)
	}

	err = sqlitex.Execute(conn, `
		INSERT INTO game_account_role (game_id, player_id, account_id, is_gm, is_active)
		VALUES (?, ?, ?, ?, 1)`, &sqlitex.ExecOptions{
		Args: []any{gameID, nextID, accountID, isGM},
	})
	if err != nil {
		if isConstraint(err) {
			return Member{}, fmt.Errorf("add member: account %d in game %d: %w", accountID, gameID, ErrConflict)
		}
		return Member{}, fmt.Errorf("add member: %w", err)
	}
	return Member{GameID: gameID, PlayerID: nextID, AccountID: accountID, IsGM: isGM, IsActive: true}, nil
}

// GetMember returns the seat identified by (gameID, playerID), or
// ErrRecordNotFound.
func (db *DB) GetMember(ctx context.Context, gameID, playerID int64) (Member, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Member{}, err
	}
	defer db.Put(conn)
	return getMemberWhere(conn, "game_id = ? AND player_id = ?", gameID, playerID)
}

// GetMemberByAccount returns the seat an account holds in a game (there is at
// most one), or ErrRecordNotFound. It answers "is this account a member of this
// game?" for authorization.
func (db *DB) GetMemberByAccount(ctx context.Context, gameID, accountID int64) (Member, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Member{}, err
	}
	defer db.Put(conn)
	return getMemberWhere(conn, "game_id = ? AND account_id = ?", gameID, accountID)
}

// getMemberWhere reads the single seat matching a two-argument WHERE predicate,
// returning ErrRecordNotFound when there is no match.
func getMemberWhere(conn *sqlite.Conn, where string, arg1, arg2 any) (Member, error) {
	var (
		m     Member
		found bool
	)
	err := sqlitex.Execute(conn, `
		SELECT game_id, player_id, account_id, is_gm, is_active
		FROM game_account_role WHERE `+where, &sqlitex.ExecOptions{
		Args:       []any{arg1, arg2},
		ResultFunc: func(stmt *sqlite.Stmt) error { m = scanMember(stmt); found = true; return nil },
	})
	if err != nil {
		return Member{}, fmt.Errorf("get member: %w", err)
	}
	if !found {
		return Member{}, ErrRecordNotFound
	}
	return m, nil
}

// scanMember reads a seat from the current row, whose columns are, in order:
// game_id, player_id, account_id, is_gm, is_active.
func scanMember(stmt *sqlite.Stmt) Member {
	return Member{
		GameID:    stmt.ColumnInt64(0),
		PlayerID:  stmt.ColumnInt64(1),
		AccountID: stmt.ColumnInt64(2),
		IsGM:      stmt.ColumnBool(3),
		IsActive:  stmt.ColumnBool(4),
	}
}

// UpdateMember writes the mutable seat fields (is_gm, is_active) of the seat
// identified by (m.GameID, m.PlayerID). player_id and account_id are immutable
// and never change. An unknown seat returns ErrRecordNotFound. It does not police
// the "promote-only" GM rule — that policy belongs to the member handlers.
func (db *DB) UpdateMember(ctx context.Context, m Member) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	err = sqlitex.Execute(conn, `
		UPDATE game_account_role
		SET is_gm = ?, is_active = ?
		WHERE game_id = ? AND player_id = ?`, &sqlitex.ExecOptions{
		Args: []any{m.IsGM, m.IsActive, m.GameID, m.PlayerID},
	})
	if err != nil {
		return fmt.Errorf("update member (game %d, player %d): %w", m.GameID, m.PlayerID, err)
	}
	if conn.Changes() == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// ListMembers returns every seat in a game — active and inactive — ordered by
// player_id, so a dropped member still appears (with IsActive false).
func (db *DB) ListMembers(ctx context.Context, gameID int64) ([]Member, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Put(conn)

	var members []Member
	err = sqlitex.Execute(conn, `
		SELECT game_id, player_id, account_id, is_gm, is_active
		FROM game_account_role WHERE game_id = ? ORDER BY player_id`, &sqlitex.ExecOptions{
		Args:       []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error { members = append(members, scanMember(stmt)); return nil },
	})
	if err != nil {
		return nil, fmt.Errorf("list members of game %d: %w", gameID, err)
	}
	return members, nil
}

// ListMyGames returns the active seats an account holds, each joined to its game,
// ordered by game id. It backs the per-account "my games" listing; only active
// seats are included, matching what a current participant should see.
func (db *DB) ListMyGames(ctx context.Context, accountID int64) ([]MyGame, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Put(conn)

	var games []MyGame
	err = sqlitex.Execute(conn, `
		SELECT g.id, g.name, g.is_active, r.player_id, r.is_gm
		FROM game_account_role r
		JOIN games g ON g.id = r.game_id
		WHERE r.account_id = ? AND r.is_active = 1
		ORDER BY g.id`, &sqlitex.ExecOptions{
		Args: []any{accountID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			games = append(games, MyGame{
				GameID:   stmt.ColumnInt64(0),
				Name:     stmt.ColumnText(1),
				IsActive: stmt.ColumnBool(2),
				PlayerID: stmt.ColumnInt64(3),
				IsGM:     stmt.ColumnBool(4),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list games for account %d: %w", accountID, err)
	}
	return games, nil
}
