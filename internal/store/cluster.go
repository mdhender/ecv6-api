// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Generation-stage names, as stored in game_generator.stage and constrained by
// the migration's CHECK. A game records one row per stage (ADR-0016).
const (
	StagePlacement      = "placement"
	StageSystemContents = "system_contents"
	StageDeposits       = "deposits"
)

// System is one placed system within a cluster, addressed by its axial
// coordinates (q, r) (the cluster core reference). Orbits and planets are added
// by a later generation stage; a System here is just the placement output.
type System struct {
	Q int
	R int
}

// Cluster is a game's generated map: the derived radius and the placement
// settings that produced it, plus the placed systems. One per game, generated
// once at setup and immutable thereafter. Radius R is a pure function of N and
// Density (no randomness); Spacing is the minimum system spacing S. See
// internal/genesis and the Genesis Placement supplement.
type Cluster struct {
	GameID  int64
	Radius  int
	N       int
	Density string
	Spacing int
	Systems []System
}

// GeneratorSelection records which generator, version, and settings a game ran
// for one generation stage (ADR-0016: a game records three such rows). Settings
// is opaque, stage-specific JSON, so later stages need no schema change.
type GeneratorSelection struct {
	GameID      int64
	Stage       string
	GeneratorID int64
	Version     int64
	Settings    string // JSON text
}

// SaveCluster persists a generated cluster — its cluster row and all its system
// rows — in one transaction. It does not touch generator-selection rows; persist
// those with SaveGenerator. Saving a cluster for a game that already has one
// violates the primary key and returns ErrConflict.
func (db *DB) SaveCluster(ctx context.Context, c Cluster) (err error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	endTx := sqlitex.Transaction(conn)
	defer endTx(&err)

	err = sqlitex.Execute(conn, `
		INSERT INTO cluster (game_id, radius, n, density, spacing)
		VALUES (?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
		Args: []any{c.GameID, c.Radius, c.N, c.Density, c.Spacing},
	})
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("save cluster for game %d: %w", c.GameID, ErrConflict)
		}
		return fmt.Errorf("save cluster for game %d: %w", c.GameID, err)
	}

	for _, s := range c.Systems {
		err = sqlitex.Execute(conn, `
			INSERT INTO system (game_id, q, r) VALUES (?, ?, ?)`, &sqlitex.ExecOptions{
			Args: []any{c.GameID, s.Q, s.R},
		})
		if err != nil {
			if isConstraint(err) {
				return fmt.Errorf("save system (%d,%d) for game %d: %w", s.Q, s.R, c.GameID, ErrConflict)
			}
			return fmt.Errorf("save system (%d,%d) for game %d: %w", s.Q, s.R, c.GameID, err)
		}
	}
	return nil
}

// GetCluster loads a game's cluster and its systems, ordered by (q, r). It
// returns ErrRecordNotFound if the game has no cluster.
func (db *DB) GetCluster(ctx context.Context, gameID int64) (Cluster, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Cluster{}, err
	}
	defer db.Put(conn)

	var (
		c     Cluster
		found bool
	)
	err = sqlitex.Execute(conn, `
		SELECT game_id, radius, n, density, spacing
		FROM cluster WHERE game_id = ?`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			c = Cluster{
				GameID:  stmt.ColumnInt64(0),
				Radius:  stmt.ColumnInt(1),
				N:       stmt.ColumnInt(2),
				Density: stmt.ColumnText(3),
				Spacing: stmt.ColumnInt(4),
			}
			found = true
			return nil
		},
	})
	if err != nil {
		return Cluster{}, fmt.Errorf("get cluster for game %d: %w", gameID, err)
	}
	if !found {
		return Cluster{}, ErrRecordNotFound
	}

	err = sqlitex.Execute(conn, `
		SELECT q, r FROM system WHERE game_id = ? ORDER BY q, r`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			c.Systems = append(c.Systems, System{Q: stmt.ColumnInt(0), R: stmt.ColumnInt(1)})
			return nil
		},
	})
	if err != nil {
		return Cluster{}, fmt.Errorf("get systems for game %d: %w", gameID, err)
	}
	return c, nil
}

// SaveGenerator records a game's generator selection for one stage. A second
// save for the same (game, stage) violates the primary key and returns
// ErrConflict. An empty Settings is stored as the JSON empty object.
func (db *DB) SaveGenerator(ctx context.Context, g GeneratorSelection) error {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	settings := g.Settings
	if settings == "" {
		settings = "{}"
	}
	err = sqlitex.Execute(conn, `
		INSERT INTO game_generator (game_id, stage, generator_id, version, settings)
		VALUES (?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
		Args: []any{g.GameID, g.Stage, g.GeneratorID, g.Version, settings},
	})
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("save %s generator for game %d: %w", g.Stage, g.GameID, ErrConflict)
		}
		return fmt.Errorf("save %s generator for game %d: %w", g.Stage, g.GameID, err)
	}
	return nil
}

// GetGenerator returns a game's generator selection for one stage, or
// ErrRecordNotFound if that stage has not been recorded.
func (db *DB) GetGenerator(ctx context.Context, gameID int64, stage string) (GeneratorSelection, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return GeneratorSelection{}, err
	}
	defer db.Put(conn)

	var (
		g     GeneratorSelection
		found bool
	)
	err = sqlitex.Execute(conn, `
		SELECT game_id, stage, generator_id, version, settings
		FROM game_generator WHERE game_id = ? AND stage = ?`, &sqlitex.ExecOptions{
		Args: []any{gameID, stage},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			g = scanGenerator(stmt)
			found = true
			return nil
		},
	})
	if err != nil {
		return GeneratorSelection{}, fmt.Errorf("get %s generator for game %d: %w", stage, gameID, err)
	}
	if !found {
		return GeneratorSelection{}, ErrRecordNotFound
	}
	return g, nil
}

// ListGenerators returns all recorded generator selections for a game, ordered
// by stage. A game with no generation recorded returns an empty slice.
func (db *DB) ListGenerators(ctx context.Context, gameID int64) ([]GeneratorSelection, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Put(conn)

	var gens []GeneratorSelection
	err = sqlitex.Execute(conn, `
		SELECT game_id, stage, generator_id, version, settings
		FROM game_generator WHERE game_id = ? ORDER BY stage`, &sqlitex.ExecOptions{
		Args:       []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error { gens = append(gens, scanGenerator(stmt)); return nil },
	})
	if err != nil {
		return nil, fmt.Errorf("list generators for game %d: %w", gameID, err)
	}
	return gens, nil
}

// scanGenerator reads a generator selection from the current row, whose columns
// are, in order: game_id, stage, generator_id, version, settings.
func scanGenerator(stmt *sqlite.Stmt) GeneratorSelection {
	return GeneratorSelection{
		GameID:      stmt.ColumnInt64(0),
		Stage:       stmt.ColumnText(1),
		GeneratorID: stmt.ColumnInt64(2),
		Version:     stmt.ColumnInt64(3),
		Settings:    stmt.ColumnText(4),
	}
}
