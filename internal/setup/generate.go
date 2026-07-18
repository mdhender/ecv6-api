// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package setup runs a game's turn-0 world generation: it drives the Genesis
// cluster generator (internal/genesis, behind the internal/worldgen contract) off
// the game's master seeds, maps the result to store structs, and persists it
// (internal/store) in one idempotent pass. It is the orchestration seam between
// generation and persistence — the piece the read model (E2) depends on to find
// non-empty tables.
package setup

import (
	"context"
	"fmt"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
	"github.com/mdhender/ecv6-api/internal/store"
	"github.com/mdhender/ecv6-api/internal/worldgen"
)

// GenerateCluster runs turn-0 cluster generation for a game and persists it. It
// loads the game's master seeds, runs the Genesis cluster generator off those
// seeds and the given knobs, maps the generated cluster to store rows, and writes
// them in one pass:
//
//	DeleteGeneration → SaveCluster → SaveGenerator ×3 → SaveSystemContents → SaveDeposits
//
// Generation runs entirely in memory first, so an invalid or infeasible request
// surfaces as genesis.ErrInvalidSettings / genesis.ErrInfeasible before any write
// — a bad run never disturbs existing data (ADR-0014: an overshoot is the GM's
// problem, no engine fallback). Because generation draws only off the game's
// derived seeds, the same seeds and knobs always produce the same rows,
// independent of the machine or map-iteration order.
//
// Persistence is idempotent: DeleteGeneration clears any prior generation first,
// so regenerating a game replaces its rows outright (alpha data is disposable and
// regeneration must be repeatable). A game whose master seeds are unassigned (no
// game_engine_state row; see E1 §0 and ADR-0013) returns store.ErrRecordNotFound
// and writes nothing.
func GenerateCluster(ctx context.Context, db *store.DB, gameID int64, knobs worldgen.Knobs) error {
	es, err := db.GetEngineState(ctx, gameID)
	if err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}

	// Generate in memory; a bad request fails here, before any write.
	cluster, err := genesis.GenesisCluster{}.GenerateCluster(ctx, knobs, prng.New(es.Seed1, es.Seed2))
	if err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}

	// Map the generated cluster to store rows and build the generator-selection
	// rows (§2–§3).
	storeCluster := clusterToStore(gameID, knobs, cluster)
	contents := systemContentsToStore(gameID, cluster)
	deposits := depositsToStore(gameID, cluster)
	generators, err := generatorSelections(gameID, knobs)
	if err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}

	// Persist (§4). Clear any prior generation first so a re-run replaces cleanly,
	// then write child-after-parent so the foreign keys resolve.
	if err := db.DeleteGeneration(ctx, gameID); err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}
	if err := db.SaveCluster(ctx, storeCluster); err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}
	for _, g := range generators {
		if err := db.SaveGenerator(ctx, g); err != nil {
			return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
		}
	}
	if err := db.SaveSystemContents(ctx, contents); err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}
	if err := db.SaveDeposits(ctx, deposits); err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}
	return nil
}
