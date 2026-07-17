// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"testing"
)

// TestDeleteGenerationClearsAll populates every generation table for a game and
// confirms DeleteGeneration removes all of it: cluster, systems, planets, deposits,
// the generator-selection rows, and the per-system provenance override.
func TestDeleteGenerationClearsAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "delete-generation")

	if err := db.SaveCluster(ctx, Cluster{
		GameID: gameID, Radius: 5, N: 10, Density: "average", Spacing: 2,
		Systems: []System{{Q: 0, R: 0}},
	}); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}
	if err := db.SaveGenerator(ctx, GeneratorSelection{
		GameID: gameID, Stage: StagePlacement, GeneratorID: 1, Version: 1, Settings: "{}",
	}); err != nil {
		t.Fatalf("SaveGenerator: %v", err)
	}
	if err := db.SaveSystemContents(ctx, SystemContents{
		GameID:  gameID,
		Planets: []Planet{{Q: 0, R: 0, Orbit: 4, Type: "asteroid belt", Habitability: 0}},
	}); err != nil {
		t.Fatalf("SaveSystemContents: %v", err)
	}
	if err := db.SaveDeposits(ctx, Deposits{
		GameID: gameID,
		Deposits: []Deposit{{
			Q: 0, R: 0, Orbit: 4, DepositNo: 1, Resource: "mtls",
			InitialQuantity: 100, CurrentQuantity: 100, InitialYield: 120, CurrentYield: 120,
		}},
	}); err != nil {
		t.Fatalf("SaveDeposits: %v", err)
	}
	if err := db.PutSystemContentsGenerator(ctx, SystemContentsGenerator{
		GameID: gameID, Q: 0, R: 0, GeneratorID: 7, Version: 2,
	}); err != nil {
		t.Fatalf("PutSystemContentsGenerator: %v", err)
	}

	if err := db.DeleteGeneration(ctx, gameID); err != nil {
		t.Fatalf("DeleteGeneration: %v", err)
	}

	if _, err := db.GetCluster(ctx, gameID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("cluster survived delete: %v", err)
	}
	if _, err := db.GetSystemContents(ctx, gameID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("planets survived delete: %v", err)
	}
	if _, err := db.GetDeposits(ctx, gameID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("deposits survived delete: %v", err)
	}
	if _, err := db.GetGenerator(ctx, gameID, StagePlacement); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("generator row survived delete: %v", err)
	}
	overrides, err := db.GetSystemContentsGenerators(ctx, gameID)
	if err != nil {
		t.Fatalf("GetSystemContentsGenerators: %v", err)
	}
	if len(overrides) != 0 {
		t.Errorf("%d provenance overrides survived delete, want 0", len(overrides))
	}
}

// TestDeleteGenerationEmptyGame confirms deleting a game that has generated nothing
// is a no-op, not an error.
func TestDeleteGenerationEmptyGame(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "delete-generation-empty")

	if err := db.DeleteGeneration(ctx, gameID); err != nil {
		t.Errorf("DeleteGeneration on empty game = %v, want nil", err)
	}
}
