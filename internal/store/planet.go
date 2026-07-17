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

// SystemContents is a game's system-contents stage output: every ordinary
// system's planets. Planets carry their own (q, r), so a flat slice addresses
// every system's orbits. Home systems are not a template here (ADR-0017); they
// are ordinary planet rows produced on demand at founding.
type SystemContents struct {
	GameID  int64
	Planets []Planet
}

// SaveSystemContents persists a game's system-contents output — every planet row
// — in one transaction. Each planet references an existing system (game_id, q, r);
// a planet for an unknown system or a duplicate (game_id, q, r, orbit) violates a
// constraint and returns ErrConflict. It does not touch generator-selection rows
// or per-system provenance; persist those with SaveGenerator and
// PutSystemContentsGenerator.
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
	return nil
}

// GetSystemContents loads a game's system-contents output: its planets, ordered
// by (q, r) then orbit. A game whose system-contents stage has not run has no
// planet rows and returns ErrRecordNotFound.
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

	if len(c.Planets) == 0 {
		return SystemContents{}, ErrRecordNotFound
	}
	return c, nil
}

// SystemContentsGenerator is per-system contents provenance (ADR-0017 §3): the
// generator that produced one system's contents, keyed by its (game_id, q, r). It
// is an OVERRIDE of the game-level system_contents stage default recorded in
// game_generator (see GeneratorSelection): a row exists only for a system whose
// contents did not come from the stage generator — today only a founding home
// overwrite (E3). GeneratorID and Version mirror game_generator's columns.
type SystemContentsGenerator struct {
	GameID      int64
	Q           int
	R           int
	GeneratorID int64
	Version     int64
}

// PutSystemContentsGenerator records (or replaces) the generator that produced a
// system's contents, when it overrides the game-level stage default. It upserts
// on (game_id, q, r), so re-running a founding overwrite replaces the prior row
// (alpha regeneration is repeatable). The system (game_id, q, r) must exist or the
// foreign key fails with ErrConflict.
func (db *DB) PutSystemContentsGenerator(ctx context.Context, g SystemContentsGenerator) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	err = sqlitex.Execute(conn, `
		INSERT OR REPLACE INTO system_contents_generator (game_id, q, r, generator_id, version)
		VALUES (?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
		Args: []any{g.GameID, g.Q, g.R, g.GeneratorID, g.Version},
	})
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("put contents generator for system (%d,%d) game %d: %w", g.Q, g.R, g.GameID, ErrConflict)
		}
		return fmt.Errorf("put contents generator for system (%d,%d) game %d: %w", g.Q, g.R, g.GameID, err)
	}
	return nil
}

// GetSystemContentsGenerators loads a game's per-system contents-provenance
// overrides, ordered by (q, r). A game with no overrides (every system used the
// stage generator — the norm after cluster generation) returns an empty slice and
// a nil error; absence of overrides is not ErrRecordNotFound.
func (db *DB) GetSystemContentsGenerators(ctx context.Context, gameID int64) ([]SystemContentsGenerator, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Put(conn)

	var out []SystemContentsGenerator
	err = sqlitex.Execute(conn, `
		SELECT q, r, generator_id, version
		FROM system_contents_generator WHERE game_id = ? ORDER BY q, r`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			out = append(out, SystemContentsGenerator{
				GameID:      gameID,
				Q:           stmt.ColumnInt(0),
				R:           stmt.ColumnInt(1),
				GeneratorID: stmt.ColumnInt64(2),
				Version:     stmt.ColumnInt64(3),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get contents generators for game %d: %w", gameID, err)
	}
	return out, nil
}
