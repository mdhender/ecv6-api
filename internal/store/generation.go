// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite/sqlitex"
)

// DeleteGeneration removes all of a game's turn-0 generation output — its
// deposits, planets, per-system contents provenance, systems, cluster, and the
// three generator-selection rows — in one transaction. It is the replace step of
// idempotent regeneration: the setup orchestrator calls it before re-persisting,
// so a game can be regenerated repeatably (alpha data is disposable, CLAUDE.md).
//
// Deletes run child-before-parent so foreign keys never block: deposit → planet
// and system_contents_generator → system before system, system before cluster.
// Deleting a game that has generated nothing is a no-op, not an error.
func (db *DB) DeleteGeneration(ctx context.Context, gameID int64) (err error) {
	conn, err := db.Get(ctx)
	if err != nil {
		return err
	}
	defer db.Put(conn)

	endTx := sqlitex.Transaction(conn)
	defer endTx(&err)

	// Child-before-parent order; game_generator hangs off games alone.
	for _, table := range []string{
		"deposit",
		"planet",
		"system_contents_generator",
		"system",
		"cluster",
		"game_generator",
	} {
		if err = sqlitex.Execute(conn, `DELETE FROM `+table+` WHERE game_id = ?`, &sqlitex.ExecOptions{
			Args: []any{gameID},
		}); err != nil {
			return fmt.Errorf("delete %s for game %d: %w", table, gameID, err)
		}
	}
	return nil
}
