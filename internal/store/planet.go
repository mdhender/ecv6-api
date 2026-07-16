// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Planet is one planet occupying an orbit of a system, the system-contents
// stage's output (Genesis System Contents; see internal/genesis). It is
// addressed by its system's axial coordinates (q, r) and its orbit (1..10). Type
// is one of 'rocky', 'asteroid belt', or 'gas giant'; Habitability is the
// per-planet rating in 0..25. Empty orbits carry no Planet row.
type Planet struct {
	Q            int
	R            int
	Orbit        int
	Type         string
	Habitability int
}

// HomePlanet is one orbit of a game's fixed home-system template (Genesis System
// Contents, "Home-system template"). The template is stored once per game, keyed
// by orbit; copying it onto a chosen system when a player joins is a later step.
type HomePlanet struct {
	Orbit        int
	Type         string
	Habitability int
}

// SystemContents is a game's system-contents stage output: every ordinary
// system's planets plus the fixed home-system template. Planets carry their own
// (q, r), so a flat slice addresses every system's orbits.
type SystemContents struct {
	GameID  int64
	Planets []Planet
	Home    []HomePlanet
}

// SaveSystemContents persists a game's system-contents output — every planet row
// and the home-system template — in one transaction. Each planet references an
// existing system (game_id, q, r); a planet for an unknown system, a duplicate
// (game_id, q, r, orbit), or a duplicate home-template orbit violates a
// constraint and returns ErrConflict. It does not touch generator-selection
// rows; persist those with SaveGenerator.
func (db *DB) SaveSystemContents(ctx context.Context, c SystemContents) (err error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	endTx := sqlitex.Transaction(conn)
	defer endTx(&err)

	for _, p := range c.Planets {
		err = sqlitex.Execute(conn, `
			INSERT INTO planet (game_id, q, r, orbit, type, habitability)
			VALUES (?, ?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
			Args: []any{c.GameID, p.Q, p.R, p.Orbit, p.Type, p.Habitability},
		})
		if err != nil {
			if isConstraint(err) {
				return fmt.Errorf("save planet (%d,%d) orbit %d for game %d: %w", p.Q, p.R, p.Orbit, c.GameID, ErrConflict)
			}
			return fmt.Errorf("save planet (%d,%d) orbit %d for game %d: %w", p.Q, p.R, p.Orbit, c.GameID, err)
		}
	}

	for _, h := range c.Home {
		err = sqlitex.Execute(conn, `
			INSERT INTO home_template (game_id, orbit, type, habitability)
			VALUES (?, ?, ?, ?)`, &sqlitex.ExecOptions{
			Args: []any{c.GameID, h.Orbit, h.Type, h.Habitability},
		})
		if err != nil {
			if isConstraint(err) {
				return fmt.Errorf("save home template orbit %d for game %d: %w", h.Orbit, c.GameID, ErrConflict)
			}
			return fmt.Errorf("save home template orbit %d for game %d: %w", h.Orbit, c.GameID, err)
		}
	}
	return nil
}

// GetSystemContents loads a game's system-contents output: its planets, ordered
// by (q, r) then orbit, and its home-system template, ordered by orbit. A game
// whose system-contents stage has not run has no home-template rows and returns
// ErrRecordNotFound.
func (db *DB) GetSystemContents(ctx context.Context, gameID int64) (SystemContents, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return SystemContents{}, err
	}
	defer db.Put(conn)

	c := SystemContents{GameID: gameID}
	err = sqlitex.Execute(conn, `
		SELECT q, r, orbit, type, habitability
		FROM planet WHERE game_id = ? ORDER BY q, r, orbit`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			c.Planets = append(c.Planets, Planet{
				Q:            stmt.ColumnInt(0),
				R:            stmt.ColumnInt(1),
				Orbit:        stmt.ColumnInt(2),
				Type:         stmt.ColumnText(3),
				Habitability: stmt.ColumnInt(4),
			})
			return nil
		},
	})
	if err != nil {
		return SystemContents{}, fmt.Errorf("get planets for game %d: %w", gameID, err)
	}

	err = sqlitex.Execute(conn, `
		SELECT orbit, type, habitability
		FROM home_template WHERE game_id = ? ORDER BY orbit`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			c.Home = append(c.Home, HomePlanet{
				Orbit:        stmt.ColumnInt(0),
				Type:         stmt.ColumnText(1),
				Habitability: stmt.ColumnInt(2),
			})
			return nil
		},
	})
	if err != nil {
		return SystemContents{}, fmt.Errorf("get home template for game %d: %w", gameID, err)
	}

	if len(c.Home) == 0 {
		return SystemContents{}, ErrRecordNotFound
	}
	return c, nil
}
