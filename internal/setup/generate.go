// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package setup runs a game's turn-0 world generation: it drives the pure
// Genesis stages (internal/genesis) off the game's master seeds and, in later E1
// steps, maps and persists the result into the store (internal/store). It is the
// orchestration seam between generation and persistence — the piece the read
// model (E2) depends on to find non-empty tables.
package setup

import (
	"context"
	"fmt"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
	"github.com/mdhender/ecv6-api/internal/store"
)

// clusterResult bundles the three Genesis stage outputs for one game, in
// dependency order (placement feeds contents feeds deposits). It is the hand-off
// from the pure pipeline to the mapping and persistence steps (E1 §2–§4, not yet
// wired).
type clusterResult struct {
	Placement genesis.PlacementResult
	Contents  genesis.ContentsResult
	Deposits  genesis.DepositsResult
}

// GenerateCluster runs turn-0 cluster generation for a game. It loads the game's
// master seeds, then runs the three Genesis stages off those seeds:
//
//	Place → GenerateContents → GenerateDeposits
//
// Because generation draws only off the game's derived seeds, the same seeds and
// settings always produce the same cluster, independent of the machine or map
// iteration order.
//
// Settings are validated before any stage runs, so invalid or infeasible
// settings surface as genesis.ErrInvalidSettings / genesis.ErrInfeasible with no
// writes (ADR-0014: an overshoot is the GM's problem, no engine fallback). A game
// whose master seeds are unassigned (no game_engine_state row; see E1 §0 and
// ADR-0013) returns store.ErrRecordNotFound.
//
// Mapping the genesis output to store structs and persisting it (E1 §2–§4) attach
// at the seam marked below; today GenerateCluster runs generation and reports
// failures, but writes nothing.
func GenerateCluster(ctx context.Context, db *store.DB, gameID int64, placement genesis.PlacementSettings, deposits genesis.DepositSettings) error {
	es, err := db.GetEngineState(ctx, gameID)
	if err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}

	root := prng.New(es.Seed1, es.Seed2)
	if _, err := generate(root, placement, deposits); err != nil {
		return fmt.Errorf("generate cluster for game %d: %w", gameID, err)
	}

	// TODO(E1 §2–§4): map the genesis result to store structs (mirror
	// Quantity → initial/current, assign deposit_no), build the three
	// game_generator selection rows, and persist in one idempotent pass
	// (SaveCluster → SaveGenerator ×3 → SaveSystemContents → SaveDeposits).
	return nil
}

// generate runs the three pure Genesis stages off a game's root seeds, threading
// each stage's output into the next. Only Place can fail — on invalid or
// infeasible settings — and it validates up front, so a failure means nothing
// downstream ran; GenerateContents and GenerateDeposits are total. Each stage
// derives its own per-stage seed root from root via its frozen stage tag
// (TagCluster / TagSystem / TagDeposit), so the caller hands the same root to
// all three.
func generate(root prng.Seeds, placement genesis.PlacementSettings, deposits genesis.DepositSettings) (clusterResult, error) {
	placed, err := genesis.Place(root, placement)
	if err != nil {
		return clusterResult{}, err
	}
	contents := genesis.GenerateContents(root, placed.Systems)
	dep := genesis.GenerateDeposits(root, contents, deposits)
	return clusterResult{Placement: placed, Contents: contents, Deposits: dep}, nil
}
