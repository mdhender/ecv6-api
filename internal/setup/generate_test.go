// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package setup

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
	"github.com/mdhender/ecv6-api/internal/store"
)

// newTestGame opens an in-memory store and creates one game, returning the store
// and the new game id. Engine state (master seeds) is left unassigned so callers
// can seed it — or not — per test.
func newTestGame(t *testing.T) (*store.DB, int64) {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	gameID, err := db.CreateGame(ctx, store.Game{Name: "setup-test"})
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	return db, gameID
}

// TestGeneratePipeline runs the pure pipeline on the defaults and asserts every
// stage ran and produced coherent shapes: N systems placed, one contents entry
// and one deposits entry per placed system.
func TestGeneratePipeline(t *testing.T) {
	t.Parallel()
	placement := genesis.DefaultPlacementSettings()
	deposits := genesis.DefaultDepositSettings()

	res, err := generate(prng.New(0x0123456789abcdef, 0xfedcba9876543210), placement, deposits)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got := len(res.Placement.Systems); got != placement.N {
		t.Errorf("placed %d systems, want N=%d", got, placement.N)
	}
	if got := len(res.Contents.Systems); got != placement.N {
		t.Errorf("contents for %d systems, want %d", got, placement.N)
	}
	if got := len(res.Deposits.Systems); got != placement.N {
		t.Errorf("deposits for %d systems, want %d", got, placement.N)
	}
}

// TestGenerateDeterministic confirms the pipeline is a pure function of its
// inputs: same seeds + settings reproduce byte-identical placement, contents, and
// deposits; different seeds generally diverge.
func TestGenerateDeterministic(t *testing.T) {
	t.Parallel()
	placement := genesis.PlacementSettings{N: 40, Density: genesis.Average, Spacing: 2}
	deposits := genesis.DefaultDepositSettings()

	a, err := generate(prng.New(11, 22), placement, deposits)
	if err != nil {
		t.Fatalf("generate a: %v", err)
	}
	b, err := generate(prng.New(11, 22), placement, deposits)
	if err != nil {
		t.Fatalf("generate b: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("same seeds + settings produced different clusters")
	}

	c, err := generate(prng.New(33, 44), placement, deposits)
	if err != nil {
		t.Fatalf("generate c: %v", err)
	}
	if reflect.DeepEqual(a, c) {
		t.Error("different seeds produced identical clusters (suspicious)")
	}
}

// TestGenerateInvalidSettings asserts out-of-range settings fail up front with
// ErrInvalidSettings and no result.
func TestGenerateInvalidSettings(t *testing.T) {
	t.Parallel()
	bad := genesis.PlacementSettings{N: 1, Density: genesis.Average, Spacing: 2} // N below MinSystems
	if _, err := generate(prng.New(1, 2), bad, genesis.DefaultDepositSettings()); !errors.Is(err, genesis.ErrInvalidSettings) {
		t.Fatalf("generate(invalid) error = %v, want ErrInvalidSettings", err)
	}
}

// TestGenerateInfeasible pins the placement infeasible case (the supplement's
// N=100, extremely dense, S=40 example) through the pipeline: it fails cleanly
// before any downstream stage runs.
func TestGenerateInfeasible(t *testing.T) {
	t.Parallel()
	infeasible := genesis.PlacementSettings{N: 100, Density: genesis.ExtremelyDense, Spacing: 40}
	if _, err := generate(prng.New(1, 2), infeasible, genesis.DefaultDepositSettings()); !errors.Is(err, genesis.ErrInfeasible) {
		t.Fatalf("generate(infeasible) error = %v, want ErrInfeasible", err)
	}
}

// TestGenerateClusterSeedsUnassigned asserts that generating for a game whose
// master seeds were never written (no game_engine_state row) reports
// ErrRecordNotFound rather than generating off zero seeds.
func TestGenerateClusterSeedsUnassigned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newTestGame(t)

	err := GenerateCluster(ctx, db, gameID, genesis.DefaultPlacementSettings(), genesis.DefaultDepositSettings())
	if !errors.Is(err, store.ErrRecordNotFound) {
		t.Fatalf("GenerateCluster(no seeds) error = %v, want ErrRecordNotFound", err)
	}
}

// TestGenerateClusterRuns asserts that, with seeds assigned, GenerateCluster runs
// the pipeline without error. (Persistence is E1 §2–§4; this covers the seed-load
// and error envelope.)
func TestGenerateClusterRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newTestGame(t)
	if err := db.SaveEngineState(ctx, store.EngineState{GameID: gameID, Seed1: 0xC0FFEE, Seed2: 0xBEEF}); err != nil {
		t.Fatalf("SaveEngineState: %v", err)
	}

	if err := GenerateCluster(ctx, db, gameID, genesis.DefaultPlacementSettings(), genesis.DefaultDepositSettings()); err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}
}

// TestGenerateClusterInfeasible asserts infeasible settings surface through
// GenerateCluster (wrapped, but errors.Is still matches) once seeds are present.
func TestGenerateClusterInfeasible(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newTestGame(t)
	if err := db.SaveEngineState(ctx, store.EngineState{GameID: gameID, Seed1: 1, Seed2: 2}); err != nil {
		t.Fatalf("SaveEngineState: %v", err)
	}

	infeasible := genesis.PlacementSettings{N: 100, Density: genesis.ExtremelyDense, Spacing: 40}
	if err := GenerateCluster(ctx, db, gameID, infeasible, genesis.DefaultDepositSettings()); !errors.Is(err, genesis.ErrInfeasible) {
		t.Fatalf("GenerateCluster(infeasible) error = %v, want ErrInfeasible", err)
	}
}
