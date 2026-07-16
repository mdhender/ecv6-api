// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
)

// seedGame inserts a game so cluster/system/game_generator foreign keys resolve,
// and returns its id.
func seedGame(t *testing.T, db *DB, name string) int64 {
	t.Helper()
	id, err := db.CreateGame(context.Background(), Game{Name: name, IsActive: true})
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	return id
}

// TestClusterRoundTrip generates a cluster with the real placement generator,
// persists it, reloads it, and asserts the systems match — the persistence
// contract for placement output.
func TestClusterRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "roundtrip")

	settings := genesis.PlacementSettings{N: 40, Density: genesis.Average, Spacing: 2}
	res, err := genesis.Place(prng.New(0xABCD, 0x1234), settings)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	systems := make([]System, len(res.Systems))
	for i, h := range res.Systems {
		systems[i] = System{Q: h.Q, R: h.R}
	}
	want := Cluster{
		GameID:  gameID,
		Radius:  res.Radius,
		N:       settings.N,
		Density: string(settings.Density),
		Spacing: settings.Spacing,
		Systems: systems,
	}
	if err := db.SaveCluster(ctx, want); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}

	// Record the placement generator selection alongside it.
	blob, _ := json.Marshal(map[string]any{"n": settings.N, "density": string(settings.Density), "spacing": settings.Spacing})
	gen := GeneratorSelection{
		GameID:      gameID,
		Stage:       StagePlacement,
		GeneratorID: int64(genesis.PlacementGeneratorID),
		Version:     int64(genesis.PlacementVersion),
		Settings:    string(blob),
	}
	if err := db.SaveGenerator(ctx, gen); err != nil {
		t.Fatalf("SaveGenerator: %v", err)
	}

	got, err := db.GetCluster(ctx, gameID)
	if err != nil {
		t.Fatalf("GetCluster: %v", err)
	}
	if got.GameID != want.GameID || got.Radius != want.Radius || got.N != want.N ||
		got.Density != want.Density || got.Spacing != want.Spacing {
		t.Errorf("cluster metadata mismatch: got %+v, want %+v", got, want)
	}
	if len(got.Systems) != len(want.Systems) {
		t.Fatalf("loaded %d systems, want %d", len(got.Systems), len(want.Systems))
	}
	// GetCluster returns systems ordered by (q, r); compare as sets.
	wantSet := map[System]bool{}
	for _, s := range want.Systems {
		wantSet[s] = true
	}
	for _, s := range got.Systems {
		if !wantSet[s] {
			t.Errorf("loaded unexpected system %+v", s)
		}
	}

	gotGen, err := db.GetGenerator(ctx, gameID, StagePlacement)
	if err != nil {
		t.Fatalf("GetGenerator: %v", err)
	}
	if gotGen != gen {
		t.Errorf("generator round-trip mismatch: got %+v, want %+v", gotGen, gen)
	}
}

// TestSaveClusterConflict confirms a game holds at most one cluster.
func TestSaveClusterConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "conflict")

	c := Cluster{GameID: gameID, Radius: 5, N: 10, Density: "average", Spacing: 2,
		Systems: []System{{Q: 0, R: 0}, {Q: 3, R: -3}}}
	if err := db.SaveCluster(ctx, c); err != nil {
		t.Fatalf("first SaveCluster: %v", err)
	}
	if err := db.SaveCluster(ctx, c); !errors.Is(err, ErrConflict) {
		t.Errorf("second SaveCluster = %v, want ErrConflict", err)
	}
}

// TestGetClusterNotFound confirms a game without a cluster reports not found.
func TestGetClusterNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "empty")

	if _, err := db.GetCluster(ctx, gameID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetCluster on empty game = %v, want ErrRecordNotFound", err)
	}
	if _, err := db.GetGenerator(ctx, gameID, StagePlacement); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetGenerator on empty game = %v, want ErrRecordNotFound", err)
	}
}

// TestGeneratorStages confirms all three stage rows coexist per game and that a
// duplicate stage conflicts.
func TestGeneratorStages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "stages")

	for _, st := range []string{StagePlacement, StageSystemContents, StageDeposits} {
		g := GeneratorSelection{GameID: gameID, Stage: st, GeneratorID: 1, Version: 1, Settings: "{}"}
		if err := db.SaveGenerator(ctx, g); err != nil {
			t.Fatalf("SaveGenerator(%s): %v", st, err)
		}
	}
	dup := GeneratorSelection{GameID: gameID, Stage: StagePlacement, GeneratorID: 1, Version: 1}
	if err := db.SaveGenerator(ctx, dup); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate stage = %v, want ErrConflict", err)
	}

	gens, err := db.ListGenerators(ctx, gameID)
	if err != nil {
		t.Fatalf("ListGenerators: %v", err)
	}
	if len(gens) != 3 {
		t.Errorf("ListGenerators returned %d rows, want 3", len(gens))
	}

	// An unknown stage is rejected by the CHECK constraint as a conflict.
	bad := GeneratorSelection{GameID: gameID, Stage: "bogus", GeneratorID: 1, Version: 1}
	if err := db.SaveGenerator(ctx, bad); !errors.Is(err, ErrConflict) {
		t.Errorf("unknown stage = %v, want ErrConflict", err)
	}
}
