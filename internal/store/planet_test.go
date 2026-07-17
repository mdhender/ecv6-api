// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
)

// TestSystemContentsRoundTrip generates a cluster and its contents with the real
// generators, persists the planets, reloads them, and asserts they match — the
// persistence contract for the system-contents stage.
func TestSystemContentsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "contents-roundtrip")

	seeds := prng.New(0xABCD, 0x1234)
	settings := genesis.PlacementSettings{N: 30, Density: genesis.Average, Spacing: 2}
	placement, err := genesis.Place(seeds, settings)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	// Persist the cluster/systems first so planet foreign keys resolve.
	systems := make([]System, len(placement.Systems))
	for i, h := range placement.Systems {
		systems[i] = System{Q: h.Q, R: h.R}
	}
	if err := db.SaveCluster(ctx, Cluster{
		GameID: gameID, Radius: placement.Radius, N: settings.N,
		Density: string(settings.Density), Spacing: settings.Spacing, Systems: systems,
	}); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}

	contents := genesis.GenerateContents(seeds, placement.Systems)

	var planets []Planet
	for _, sys := range contents.Systems {
		for _, p := range sys.Planets {
			planets = append(planets, Planet{
				Q: sys.Hex.Q, R: sys.Hex.R, Orbit: p.Orbit,
				Type: string(p.Type), Habitability: p.Habitability,
			})
		}
	}
	want := SystemContents{GameID: gameID, Planets: planets}
	if err := db.SaveSystemContents(ctx, want); err != nil {
		t.Fatalf("SaveSystemContents: %v", err)
	}

	got, err := db.GetSystemContents(ctx, gameID)
	if err != nil {
		t.Fatalf("GetSystemContents: %v", err)
	}
	if got.GameID != want.GameID {
		t.Errorf("game_id = %d, want %d", got.GameID, want.GameID)
	}

	// Planets are returned ordered by (q, r, orbit); compare as a set.
	if len(got.Planets) != len(want.Planets) {
		t.Fatalf("loaded %d planets, want %d", len(got.Planets), len(want.Planets))
	}
	wantSet := map[Planet]bool{}
	for _, p := range want.Planets {
		wantSet[p] = true
	}
	for _, p := range got.Planets {
		if !wantSet[p] {
			t.Errorf("loaded unexpected planet %+v", p)
		}
	}
}

// TestSaveSystemContentsConflict confirms saving the same planet twice violates
// the primary key and returns ErrConflict.
func TestSaveSystemContentsConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "contents-conflict")

	if err := db.SaveCluster(ctx, Cluster{
		GameID: gameID, Radius: 5, N: 10, Density: "average", Spacing: 2,
		Systems: []System{{Q: 0, R: 0}},
	}); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}

	c := SystemContents{
		GameID:  gameID,
		Planets: []Planet{{Q: 0, R: 0, Orbit: 4, Type: "asteroid belt", Habitability: 0}},
	}
	if err := db.SaveSystemContents(ctx, c); err != nil {
		t.Fatalf("first SaveSystemContents: %v", err)
	}
	if err := db.SaveSystemContents(ctx, c); !errors.Is(err, ErrConflict) {
		t.Errorf("second SaveSystemContents = %v, want ErrConflict", err)
	}
}

// TestSavePlanetUnknownSystem confirms a planet for a system that was never
// placed is rejected by the foreign key as ErrConflict.
func TestSavePlanetUnknownSystem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "contents-fk")

	// No cluster/system rows exist, so any planet violates the foreign key.
	c := SystemContents{
		GameID:  gameID,
		Planets: []Planet{{Q: 99, R: 99, Orbit: 4, Type: "asteroid belt", Habitability: 0}},
	}
	if err := db.SaveSystemContents(ctx, c); !errors.Is(err, ErrConflict) {
		t.Errorf("SaveSystemContents with unknown system = %v, want ErrConflict", err)
	}
}

// TestGetSystemContentsNotFound confirms a game whose contents stage has not run
// reports not found.
func TestGetSystemContentsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "contents-empty")

	if _, err := db.GetSystemContents(ctx, gameID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetSystemContents on empty game = %v, want ErrRecordNotFound", err)
	}
}

// TestSystemContentsGeneratorOverride exercises the per-system contents-provenance
// surface (ADR-0017 §3): no overrides after cluster generation, then a founding
// home overwrite records one, and re-running it replaces the row.
func TestSystemContentsGeneratorOverride(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "contents-provenance")

	// Two placed systems for the overrides to reference.
	if err := db.SaveCluster(ctx, Cluster{
		GameID: gameID, Radius: 5, N: 10, Density: "average", Spacing: 2,
		Systems: []System{{Q: 0, R: 0}, {Q: 1, R: -1}},
	}); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}

	// A freshly generated cluster has no overrides — every system used the stage
	// generator, so the table is empty and that is not an error.
	got, err := db.GetSystemContentsGenerators(ctx, gameID)
	if err != nil {
		t.Fatalf("GetSystemContentsGenerators (empty): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("fresh game has %d overrides, want 0", len(got))
	}

	// A founding home overwrite of (0,0) records a per-system override.
	ov := SystemContentsGenerator{GameID: gameID, Q: 0, R: 0, GeneratorID: 7, Version: 2}
	if err := db.PutSystemContentsGenerator(ctx, ov); err != nil {
		t.Fatalf("PutSystemContentsGenerator: %v", err)
	}
	got, err = db.GetSystemContentsGenerators(ctx, gameID)
	if err != nil {
		t.Fatalf("GetSystemContentsGenerators: %v", err)
	}
	if len(got) != 1 || got[0] != ov {
		t.Fatalf("overrides = %+v, want [%+v]", got, ov)
	}

	// Re-running the overwrite (same (q,r)) replaces the row rather than conflicting.
	ov2 := SystemContentsGenerator{GameID: gameID, Q: 0, R: 0, GeneratorID: 9, Version: 3}
	if err := db.PutSystemContentsGenerator(ctx, ov2); err != nil {
		t.Fatalf("PutSystemContentsGenerator (replace): %v", err)
	}
	got, err = db.GetSystemContentsGenerators(ctx, gameID)
	if err != nil {
		t.Fatalf("GetSystemContentsGenerators (after replace): %v", err)
	}
	if len(got) != 1 || got[0] != ov2 {
		t.Errorf("overrides after replace = %+v, want [%+v]", got, ov2)
	}
}

// TestPutSystemContentsGeneratorUnknownSystem confirms an override for a system
// that was never placed is rejected by the foreign key as ErrConflict.
func TestPutSystemContentsGeneratorUnknownSystem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "provenance-fk")

	ov := SystemContentsGenerator{GameID: gameID, Q: 42, R: 42, GeneratorID: 1, Version: 1}
	if err := db.PutSystemContentsGenerator(ctx, ov); !errors.Is(err, ErrConflict) {
		t.Errorf("PutSystemContentsGenerator for unknown system = %v, want ErrConflict", err)
	}
}
