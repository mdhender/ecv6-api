// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package setup

import (
	"context"
	"errors"
	"fmt"
	rand "math/rand/v2"

	"github.com/mdhender/ecv6-api/internal/store"
)

// ensureSeeds returns the game's master seeds, assigning them if missing. This is
// the setup-layer seed-assignment policy (ADR-0013): seeds are assigned at setup,
// not at game creation, and the policy lives here rather than in the store
// accessors.
//
// If the game already has a game_engine_state row, ensureSeeds returns it
// unchanged (reuse-if-present) so regeneration reproduces the world byte-for-byte.
// If no row exists (store.ErrRecordNotFound), it draws two fresh master seeds from
// math/rand/v2, saves an EngineState with current_turn 0, and returns it
// (assign-if-missing). Any other error propagates.
func ensureSeeds(ctx context.Context, db *store.DB, gameID int64) (store.EngineState, error) {
	es, err := db.GetEngineState(ctx, gameID)
	if err == nil {
		return es, nil
	}
	if !errors.Is(err, store.ErrRecordNotFound) {
		return store.EngineState{}, fmt.Errorf("ensure seeds for game %d: %w", gameID, err)
	}

	es = store.EngineState{
		GameID:      gameID,
		Seed1:       rand.Uint64(),
		Seed2:       rand.Uint64(),
		CurrentTurn: 0,
	}
	if err := db.SaveEngineState(ctx, es); err != nil {
		return store.EngineState{}, fmt.Errorf("ensure seeds for game %d: %w", gameID, err)
	}
	return es, nil
}
