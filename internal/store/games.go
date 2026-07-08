// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// CreateGame inserts a new game and returns its assigned id. An empty Status
// defaults to "draft"; an unrecognized status is rejected by the CHECK
// constraint and surfaces as ErrConflict.
func (db *DB) CreateGame(ctx context.Context, g Game) (int64, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return 0, err
	}
	defer db.Put(conn)

	status := g.Status
	if status == "" {
		status = "draft"
	}
	err = sqlitex.Execute(conn, `
		INSERT INTO games (name, status, description, is_active)
		VALUES (?, ?, ?, ?)`, &sqlitex.ExecOptions{
		Args: []any{g.Name, status, g.Description, g.IsActive},
	})
	if err != nil {
		if isConstraint(err) {
			return 0, fmt.Errorf("create game %q: %w", g.Name, ErrConflict)
		}
		return 0, fmt.Errorf("create game: %w", err)
	}
	return conn.LastInsertRowID(), nil
}

// GetGame returns the game with the given id, or ErrRecordNotFound.
func (db *DB) GetGame(ctx context.Context, id int64) (Game, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Game{}, err
	}
	defer db.Put(conn)

	var (
		g     Game
		found bool
	)
	err = sqlitex.Execute(conn, `
		SELECT id, name, status, description, is_active
		FROM games WHERE id = ?`, &sqlitex.ExecOptions{
		Args: []any{id},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			g = scanGame(stmt)
			found = true
			return nil
		},
	})
	if err != nil {
		return Game{}, fmt.Errorf("get game %d: %w", id, err)
	}
	if !found {
		return Game{}, ErrRecordNotFound
	}
	return g, nil
}

// scanGame reads a game from the current row, whose columns are, in order: id,
// name, status, description, is_active.
func scanGame(stmt *sqlite.Stmt) Game {
	return Game{
		ID:          stmt.ColumnInt64(0),
		Name:        stmt.ColumnText(1),
		Status:      stmt.ColumnText(2),
		Description: stmt.ColumnText(3),
		IsActive:    stmt.ColumnBool(4),
	}
}

// UpdateGame writes the mutable fields (name, status, description, active) of the
// game identified by g.ID. An unrecognized status surfaces as ErrConflict; an
// unknown id returns ErrRecordNotFound. It does not police lifecycle transitions
// (forward-only status rules) — that policy belongs to the game handlers.
func (db *DB) UpdateGame(ctx context.Context, g Game) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	err = sqlitex.Execute(conn, `
		UPDATE games
		SET name = ?, status = ?, description = ?, is_active = ?
		WHERE id = ?`, &sqlitex.ExecOptions{
		Args: []any{g.Name, g.Status, g.Description, g.IsActive, g.ID},
	})
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("update game %d: %w", g.ID, ErrConflict)
		}
		return fmt.Errorf("update game %d: %w", g.ID, err)
	}
	if conn.Changes() == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// ListGames returns games ordered by id. A non-empty statusFilter restricts the
// result to games in that lifecycle status; an empty filter returns all games,
// active and inactive.
func (db *DB) ListGames(ctx context.Context, statusFilter string) ([]Game, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Put(conn)

	query := `SELECT id, name, status, description, is_active FROM games`
	var args []any
	if statusFilter != "" {
		query += ` WHERE status = ?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY id`

	var games []Game
	err = sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
		Args:       args,
		ResultFunc: func(stmt *sqlite.Stmt) error { games = append(games, scanGame(stmt)); return nil },
	})
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	return games, nil
}
