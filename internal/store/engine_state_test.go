// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"testing"
)

// TestEngineStateRoundTrip saves a game's engine state and reads it back,
// asserting every field survives — including a seed with the high bit set, which
// exercises the uint64<->int64 bit-pattern reinterpret across the INTEGER column.
func TestEngineStateRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "engine-state")

	want := EngineState{
		GameID:      gameID,
		Seed1:       0xFEEDFACEDEADBEEF, // high bit set: negative as int64
		Seed2:       0x0123456789ABCDEF,
		CurrentTurn: 0,
	}
	if err := db.SaveEngineState(ctx, want); err != nil {
		t.Fatalf("SaveEngineState: %v", err)
	}

	got, err := db.GetEngineState(ctx, gameID)
	if err != nil {
		t.Fatalf("GetEngineState: %v", err)
	}
	if got != want {
		t.Fatalf("engine state round-trip:\n got %+v\nwant %+v", got, want)
	}
}

// TestSaveEngineStateUpsert asserts a second save for the same game overwrites the
// first, so setup-time regeneration is repeatable.
func TestSaveEngineStateUpsert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "engine-state-upsert")

	if err := db.SaveEngineState(ctx, EngineState{GameID: gameID, Seed1: 1, Seed2: 2}); err != nil {
		t.Fatalf("first SaveEngineState: %v", err)
	}
	want := EngineState{GameID: gameID, Seed1: 111, Seed2: 222, CurrentTurn: 3}
	if err := db.SaveEngineState(ctx, want); err != nil {
		t.Fatalf("second SaveEngineState: %v", err)
	}

	got, err := db.GetEngineState(ctx, gameID)
	if err != nil {
		t.Fatalf("GetEngineState: %v", err)
	}
	if got != want {
		t.Fatalf("upsert did not overwrite:\n got %+v\nwant %+v", got, want)
	}
}

// TestGetEngineStateNotFound asserts a game with no engine-state row (never set up)
// reports ErrRecordNotFound.
func TestGetEngineStateNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "engine-state-missing")

	if _, err := db.GetEngineState(ctx, gameID); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("GetEngineState: got %v, want ErrRecordNotFound", err)
	}
}

// TestSaveEngineStateUnknownGame asserts a seed for a nonexistent game trips the
// foreign key and surfaces as ErrConflict.
func TestSaveEngineStateUnknownGame(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	err := db.SaveEngineState(ctx, EngineState{GameID: 99999, Seed1: 1, Seed2: 2})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("SaveEngineState for unknown game: got %v, want ErrConflict", err)
	}
}
