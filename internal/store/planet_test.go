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
// generators, persists the planets and home template, reloads them, and asserts
// they match — the persistence contract for the system-contents stage.
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
	home := make([]HomePlanet, len(contents.Home))
	for i, p := range contents.Home {
		home[i] = HomePlanet{Orbit: p.Orbit, Type: string(p.Type), Habitability: p.Habitability}
	}

	want := SystemContents{GameID: gameID, Planets: planets, Home: home}
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

	// Home template round-trips exactly, ordered by orbit.
	if len(got.Home) != len(want.Home) {
		t.Fatalf("loaded %d home planets, want %d", len(got.Home), len(want.Home))
	}
	for i := range want.Home {
		if got.Home[i] != want.Home[i] {
			t.Errorf("home orbit %d = %+v, want %+v", want.Home[i].Orbit, got.Home[i], want.Home[i])
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
		Home:    []HomePlanet{{Orbit: 1, Type: "rocky", Habitability: 0}},
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
