// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// SaveEngineState writes a game's engine root state (master seeds and current
// turn) to game_engine_state, inserting the row or overwriting the existing one
// for that game. It upserts rather than insert-only so setup-time regeneration is
// repeatable (alpha data is disposable). The uint64 seeds are stored as their
// INTEGER bit pattern (SQLite has no unsigned type). A game_id that does not
// reference an existing game violates the foreign key and returns ErrConflict.
func (db *DB) SaveEngineState(ctx context.Context, s EngineState) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	err = sqlitex.Execute(conn, `
		INSERT INTO game_engine_state (game_id, seed1, seed2, current_turn)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (game_id) DO UPDATE SET
			seed1 = excluded.seed1,
			seed2 = excluded.seed2,
			current_turn = excluded.current_turn`, &sqlitex.ExecOptions{
		Args: []any{s.GameID, int64(s.Seed1), int64(s.Seed2), s.CurrentTurn},
	})
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("save engine state for game %d: %w", s.GameID, ErrConflict)
		}
		return fmt.Errorf("save engine state for game %d: %w", s.GameID, err)
	}
	return nil
}

// GetEngineState loads a game's engine root state. It returns ErrRecordNotFound if
// the game has no engine-state row (seeds are assigned at setup, so a game that has
// not been set up has none). The stored INTEGER bit patterns are reinterpreted back
// to the uint64 master seeds.
func (db *DB) GetEngineState(ctx context.Context, gameID int64) (EngineState, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return EngineState{}, err
	}
	defer db.Put(conn)

	var (
		s     EngineState
		found bool
	)
	err = sqlitex.Execute(conn, `
		SELECT game_id, seed1, seed2, current_turn
		FROM game_engine_state WHERE game_id = ?`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			s = EngineState{
				GameID:      stmt.ColumnInt64(0),
				Seed1:       uint64(stmt.ColumnInt64(1)),
				Seed2:       uint64(stmt.ColumnInt64(2)),
				CurrentTurn: stmt.ColumnInt(3),
			}
			found = true
			return nil
		},
	})
	if err != nil {
		return EngineState{}, fmt.Errorf("get engine state for game %d: %w", gameID, err)
	}
	if !found {
		return EngineState{}, ErrRecordNotFound
	}
	return s, nil
}
