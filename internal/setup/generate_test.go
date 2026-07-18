// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package setup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/store"
	"github.com/mdhender/ecv6-api/internal/worldgen"
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

// newSeededGame is newTestGame plus assigned master seeds, ready to generate.
func newSeededGame(t *testing.T, seed1, seed2 uint64) (*store.DB, int64) {
	t.Helper()
	db, gameID := newTestGame(t)
	if err := db.SaveEngineState(context.Background(), store.EngineState{GameID: gameID, Seed1: seed1, Seed2: seed2}); err != nil {
		t.Fatalf("SaveEngineState: %v", err)
	}
	return db, gameID
}

// TestGenerateClusterAssignsSeeds asserts the assign-if-missing policy (ADR-0013):
// generating for a game with no game_engine_state row succeeds, drawing fresh
// master seeds; afterwards the engine state exists at current_turn 0 with (almost
// surely) non-zero seeds, and a cluster was persisted.
func TestGenerateClusterAssignsSeeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newTestGame(t)

	if err := GenerateCluster(ctx, db, gameID, mappingKnobs()); err != nil {
		t.Fatalf("GenerateCluster(no seeds) error = %v, want success", err)
	}

	es, err := db.GetEngineState(ctx, gameID)
	if err != nil {
		t.Fatalf("GetEngineState after generate: %v", err)
	}
	if es.CurrentTurn != 0 {
		t.Errorf("assigned engine state CurrentTurn = %d, want 0", es.CurrentTurn)
	}
	if es.Seed1 == 0 && es.Seed2 == 0 {
		t.Errorf("assigned seeds are both zero (%d, %d); want fresh non-zero seeds", es.Seed1, es.Seed2)
	}
	if _, err := db.GetCluster(ctx, gameID); err != nil {
		t.Errorf("no cluster persisted after assign-if-missing generate: %v", err)
	}
}

// TestGenerateClusterReusesAssignedSeeds asserts the reuse-if-present half of the
// policy: once seeds are assigned, regeneration reuses them (does not re-draw), so
// the world reproduces byte-for-byte.
func TestGenerateClusterReusesAssignedSeeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newTestGame(t)
	knobs := mappingKnobs()

	if err := GenerateCluster(ctx, db, gameID, knobs); err != nil {
		t.Fatalf("first GenerateCluster: %v", err)
	}
	assigned, err := db.GetEngineState(ctx, gameID)
	if err != nil {
		t.Fatalf("GetEngineState after first generate: %v", err)
	}
	first, err := db.GetDeposits(ctx, gameID)
	if err != nil {
		t.Fatalf("GetDeposits (first): %v", err)
	}

	if err := GenerateCluster(ctx, db, gameID, knobs); err != nil {
		t.Fatalf("second GenerateCluster (regenerate): %v", err)
	}
	reused, err := db.GetEngineState(ctx, gameID)
	if err != nil {
		t.Fatalf("GetEngineState after second generate: %v", err)
	}
	if reused.Seed1 != assigned.Seed1 || reused.Seed2 != assigned.Seed2 {
		t.Errorf("seeds changed on regenerate: (%d, %d) then (%d, %d); want reuse",
			assigned.Seed1, assigned.Seed2, reused.Seed1, reused.Seed2)
	}
	second, err := db.GetDeposits(ctx, gameID)
	if err != nil {
		t.Fatalf("GetDeposits (second): %v", err)
	}
	if !reflect.DeepEqual(first.Deposits, second.Deposits) {
		t.Error("regeneration with reused seeds produced different deposits")
	}
}

// TestGenerateClusterInfeasible asserts infeasible settings surface through
// GenerateCluster and leave no rows behind.
func TestGenerateClusterInfeasible(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newSeededGame(t, 1, 2)

	knobs := worldgen.DefaultKnobs()
	knobs.Placement = worldgen.PlacementKnobs{Count: 100, Density: worldgen.ExtremelyDense, Spacing: 40}
	if err := GenerateCluster(ctx, db, gameID, knobs); !errors.Is(err, genesis.ErrInfeasible) {
		t.Fatalf("GenerateCluster(infeasible) error = %v, want ErrInfeasible", err)
	}
	if _, err := db.GetCluster(ctx, gameID); !errors.Is(err, store.ErrRecordNotFound) {
		t.Errorf("cluster written despite infeasible settings: %v", err)
	}
}

// TestGenerateClusterInvalidNoWrites asserts invalid settings fail up front with
// ErrInvalidSettings and write nothing.
func TestGenerateClusterInvalidNoWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newSeededGame(t, 1, 2)

	knobs := mappingKnobs()
	knobs.Placement.Count = 1 // below MinSystems
	if err := GenerateCluster(ctx, db, gameID, knobs); !errors.Is(err, genesis.ErrInvalidSettings) {
		t.Fatalf("GenerateCluster(invalid) error = %v, want ErrInvalidSettings", err)
	}
	if _, err := db.GetCluster(ctx, gameID); !errors.Is(err, store.ErrRecordNotFound) {
		t.Errorf("cluster written despite invalid settings: %v", err)
	}
}

// TestGenerateClusterPersists runs the full pass and confirms every stage's output
// landed: the cluster and its systems, the planets, the deposits, and the three
// generator-selection rows with their recorded settings.
func TestGenerateClusterPersists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newSeededGame(t, 0xC0FFEE, 0xBEEF)

	knobs := mappingKnobs()
	if err := GenerateCluster(ctx, db, gameID, knobs); err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}

	cluster, err := db.GetCluster(ctx, gameID)
	if err != nil {
		t.Fatalf("GetCluster: %v", err)
	}
	if cluster.N != int(knobs.Placement.Count) || len(cluster.Systems) != int(knobs.Placement.Count) {
		t.Errorf("cluster N=%d, %d systems; want %d each", cluster.N, len(cluster.Systems), int(knobs.Placement.Count))
	}

	contents, err := db.GetSystemContents(ctx, gameID)
	if err != nil {
		t.Fatalf("GetSystemContents: %v", err)
	}
	if len(contents.Planets) == 0 {
		t.Error("no planets persisted")
	}

	deposits, err := db.GetDeposits(ctx, gameID)
	if err != nil {
		t.Fatalf("GetDeposits: %v", err)
	}
	if len(deposits.Deposits) == 0 {
		t.Error("no deposits persisted")
	}

	// All three generator-selection rows exist; the deposits row records the
	// resolved abundance knobs as its settings JSON.
	for _, stage := range []string{store.StagePlacement, store.StageSystemContents, store.StageDeposits} {
		g, err := db.GetGenerator(ctx, gameID, stage)
		if err != nil {
			t.Fatalf("GetGenerator(%s): %v", stage, err)
		}
		if g.Version != 1 {
			t.Errorf("generator %s version = %d, want 1", stage, g.Version)
		}
	}
	dep, err := db.GetGenerator(ctx, gameID, store.StageDeposits)
	if err != nil {
		t.Fatalf("GetGenerator(deposits): %v", err)
	}
	if !strings.Contains(dep.Settings, "\"fuel\"") {
		t.Errorf("deposits generator settings = %q, want resolved deposit knobs", dep.Settings)
	}
}

// TestGenerateClusterRegenerates confirms regeneration replaces cleanly: running
// twice for the same game does not raise ErrConflict and leaves the same row counts.
func TestGenerateClusterRegenerates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newSeededGame(t, 42, 99)
	knobs := mappingKnobs()

	if err := GenerateCluster(ctx, db, gameID, knobs); err != nil {
		t.Fatalf("first GenerateCluster: %v", err)
	}
	first, err := db.GetDeposits(ctx, gameID)
	if err != nil {
		t.Fatalf("GetDeposits (first): %v", err)
	}

	if err := GenerateCluster(ctx, db, gameID, knobs); err != nil {
		t.Fatalf("second GenerateCluster (regenerate): %v", err)
	}
	second, err := db.GetDeposits(ctx, gameID)
	if err != nil {
		t.Fatalf("GetDeposits (second): %v", err)
	}
	if len(first.Deposits) != len(second.Deposits) {
		t.Errorf("regeneration changed deposit count: %d then %d", len(first.Deposits), len(second.Deposits))
	}
}

// TestGenerateClusterDeterministic confirms the persisted rows are a pure function
// of the seeds and knobs: two games generated from the same seeds + knobs hold
// identical clusters, planets, and deposits.
func TestGenerateClusterDeterministic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	knobs := mappingKnobs()

	dbA, gameA := newSeededGame(t, 0x1234, 0x5678)
	dbB, gameB := newSeededGame(t, 0x1234, 0x5678)
	if err := GenerateCluster(ctx, dbA, gameA, knobs); err != nil {
		t.Fatalf("GenerateCluster A: %v", err)
	}
	if err := GenerateCluster(ctx, dbB, gameB, knobs); err != nil {
		t.Fatalf("GenerateCluster B: %v", err)
	}

	ca, _ := dbA.GetCluster(ctx, gameA)
	cb, _ := dbB.GetCluster(ctx, gameB)
	if ca.Radius != cb.Radius || ca.N != cb.N || ca.Density != cb.Density ||
		ca.Spacing != cb.Spacing || !reflect.DeepEqual(ca.Systems, cb.Systems) {
		t.Error("same seeds + knobs produced different clusters")
	}

	pa, _ := dbA.GetSystemContents(ctx, gameA)
	pb, _ := dbB.GetSystemContents(ctx, gameB)
	if !reflect.DeepEqual(pa.Planets, pb.Planets) {
		t.Error("same seeds + knobs produced different planets")
	}

	da, _ := dbA.GetDeposits(ctx, gameA)
	dbb, _ := dbB.GetDeposits(ctx, gameB)
	if !reflect.DeepEqual(da.Deposits, dbb.Deposits) {
		t.Error("same seeds + knobs produced different deposits")
	}
}
