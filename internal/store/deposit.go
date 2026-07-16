// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Deposit is one natural-resource deposit on a planet, the deposits stage's
// output (Genesis Deposits; see internal/genesis). It is addressed by its
// planet's (q, r) and orbit and a per-planet creation-order index DepositNo
// (0-based). Resource is one of 'fuel', 'mtls', or 'nmtl'. Quantities are
// positive whole numbers; yields are stored in tenths of a percentage point
// (InitialYield == 42 means 4.2%). At generation the current values equal the
// initial values; play may later change the current values.
type Deposit struct {
	Q               int
	R               int
	Orbit           int
	DepositNo       int
	Resource        string
	InitialQuantity int64
	CurrentQuantity int64
	InitialYield    int // tenths of a percent (0.1% units)
	CurrentYield    int // tenths of a percent (0.1% units)
}

// HomeDeposit is one deposit of a game's fixed home-system template (Genesis
// Deposits, "Home-system template"), addressed by orbit and a per-orbit
// creation-order index DepositNo. Its fields mirror Deposit; copying the template
// onto a chosen system when a player joins is a later step.
type HomeDeposit struct {
	Orbit           int
	DepositNo       int
	Resource        string
	InitialQuantity int64
	CurrentQuantity int64
	InitialYield    int // tenths of a percent (0.1% units)
	CurrentYield    int // tenths of a percent (0.1% units)
}

// Deposits is a game's deposits stage output: every planet's deposits plus the
// fixed home-system template's deposits. Planet deposits carry their own (q, r,
// orbit), so a flat slice addresses every planet's deposits.
type Deposits struct {
	GameID   int64
	Deposits []Deposit
	Home     []HomeDeposit
}

// SaveDeposits persists a game's deposits stage output — every planet deposit and
// every home-template deposit — in one transaction. Each planet deposit
// references an existing planet (game_id, q, r, orbit) and each home deposit an
// existing home_template orbit; a deposit for an unknown planet/orbit, or a
// duplicate (game_id, q, r, orbit, deposit_no), violates a constraint and returns
// ErrConflict. It does not touch generator-selection rows; persist those with
// SaveGenerator.
func (db *DB) SaveDeposits(ctx context.Context, d Deposits) (err error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	endTx := sqlitex.Transaction(conn)
	defer endTx(&err)

	for _, dep := range d.Deposits {
		err = sqlitex.Execute(conn, `
			INSERT INTO deposit (game_id, q, r, orbit, deposit_no, resource,
			                     initial_quantity, current_quantity, initial_yield, current_yield)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
			Args: []any{d.GameID, dep.Q, dep.R, dep.Orbit, dep.DepositNo, dep.Resource,
				dep.InitialQuantity, dep.CurrentQuantity, dep.InitialYield, dep.CurrentYield},
		})
		if err != nil {
			if isConstraint(err) {
				return fmt.Errorf("save deposit %d on planet (%d,%d) orbit %d for game %d: %w",
					dep.DepositNo, dep.Q, dep.R, dep.Orbit, d.GameID, ErrConflict)
			}
			return fmt.Errorf("save deposit %d on planet (%d,%d) orbit %d for game %d: %w",
				dep.DepositNo, dep.Q, dep.R, dep.Orbit, d.GameID, err)
		}
	}

	for _, dep := range d.Home {
		err = sqlitex.Execute(conn, `
			INSERT INTO home_template_deposit (game_id, orbit, deposit_no, resource,
			                                   initial_quantity, current_quantity, initial_yield, current_yield)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, &sqlitex.ExecOptions{
			Args: []any{d.GameID, dep.Orbit, dep.DepositNo, dep.Resource,
				dep.InitialQuantity, dep.CurrentQuantity, dep.InitialYield, dep.CurrentYield},
		})
		if err != nil {
			if isConstraint(err) {
				return fmt.Errorf("save home deposit %d on orbit %d for game %d: %w",
					dep.DepositNo, dep.Orbit, d.GameID, ErrConflict)
			}
			return fmt.Errorf("save home deposit %d on orbit %d for game %d: %w",
				dep.DepositNo, dep.Orbit, d.GameID, err)
		}
	}
	return nil
}

// GetDeposits loads a game's deposits stage output: its planet deposits, ordered
// by (q, r, orbit, deposit_no), and its home-template deposits, ordered by
// (orbit, deposit_no). A game whose deposits stage has not run has no
// home-template deposit rows and returns ErrRecordNotFound.
func (db *DB) GetDeposits(ctx context.Context, gameID int64) (Deposits, error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return Deposits{}, err
	}
	defer db.Put(conn)

	d := Deposits{GameID: gameID}
	err = sqlitex.Execute(conn, `
		SELECT q, r, orbit, deposit_no, resource,
		       initial_quantity, current_quantity, initial_yield, current_yield
		FROM deposit WHERE game_id = ? ORDER BY q, r, orbit, deposit_no`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			d.Deposits = append(d.Deposits, Deposit{
				Q:               stmt.ColumnInt(0),
				R:               stmt.ColumnInt(1),
				Orbit:           stmt.ColumnInt(2),
				DepositNo:       stmt.ColumnInt(3),
				Resource:        stmt.ColumnText(4),
				InitialQuantity: stmt.ColumnInt64(5),
				CurrentQuantity: stmt.ColumnInt64(6),
				InitialYield:    stmt.ColumnInt(7),
				CurrentYield:    stmt.ColumnInt(8),
			})
			return nil
		},
	})
	if err != nil {
		return Deposits{}, fmt.Errorf("get deposits for game %d: %w", gameID, err)
	}

	err = sqlitex.Execute(conn, `
		SELECT orbit, deposit_no, resource,
		       initial_quantity, current_quantity, initial_yield, current_yield
		FROM home_template_deposit WHERE game_id = ? ORDER BY orbit, deposit_no`, &sqlitex.ExecOptions{
		Args: []any{gameID},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			d.Home = append(d.Home, HomeDeposit{
				Orbit:           stmt.ColumnInt(0),
				DepositNo:       stmt.ColumnInt(1),
				Resource:        stmt.ColumnText(2),
				InitialQuantity: stmt.ColumnInt64(3),
				CurrentQuantity: stmt.ColumnInt64(4),
				InitialYield:    stmt.ColumnInt(5),
				CurrentYield:    stmt.ColumnInt(6),
			})
			return nil
		},
	})
	if err != nil {
		return Deposits{}, fmt.Errorf("get home deposits for game %d: %w", gameID, err)
	}

	if len(d.Home) == 0 {
		return Deposits{}, ErrRecordNotFound
	}
	return d, nil
}
