// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
)

// TestDepositsRoundTrip generates a cluster, its contents, and its deposits with
// the real generators, persists everything, reloads the deposits, and asserts they
// match — the persistence contract for the deposits stage. It also confirms
// current values equal initial values at generation.
func TestDepositsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "deposits-roundtrip")

	seeds := prng.New(0xABCD, 0x1234)
	pSettings := genesis.PlacementSettings{N: 20, Density: genesis.Average, Spacing: 2}
	placement, err := genesis.Place(seeds, pSettings)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	// Persist cluster/systems, then planets, so deposit FKs resolve.
	systems := make([]System, len(placement.Systems))
	for i, h := range placement.Systems {
		systems[i] = System{Q: h.Q, R: h.R}
	}
	if err := db.SaveCluster(ctx, Cluster{
		GameID: gameID, Radius: placement.Radius, N: pSettings.N,
		Density: string(pSettings.Density), Spacing: pSettings.Spacing, Systems: systems,
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
	if err := db.SaveSystemContents(ctx, SystemContents{GameID: gameID, Planets: planets}); err != nil {
		t.Fatalf("SaveSystemContents: %v", err)
	}

	// Generate and persist deposits. Genesis fills the ordinary systems; there is
	// no home template (ADR-0017).
	depRes := genesis.GenerateDeposits(seeds, contents, genesis.DefaultDepositSettings())
	want := depositsToStore(gameID, depRes)
	if err := db.SaveDeposits(ctx, want); err != nil {
		t.Fatalf("SaveDeposits: %v", err)
	}

	got, err := db.GetDeposits(ctx, gameID)
	if err != nil {
		t.Fatalf("GetDeposits: %v", err)
	}
	if got.GameID != want.GameID {
		t.Errorf("game_id = %d, want %d", got.GameID, want.GameID)
	}

	// Planet deposits round-trip as a set (returned ordered by q,r,orbit,deposit_no).
	if len(got.Deposits) != len(want.Deposits) {
		t.Fatalf("loaded %d planet deposits, want %d", len(got.Deposits), len(want.Deposits))
	}
	wantSet := map[Deposit]bool{}
	for _, d := range want.Deposits {
		wantSet[d] = true
	}
	for _, d := range got.Deposits {
		if !wantSet[d] {
			t.Errorf("loaded unexpected deposit %+v", d)
		}
		if d.CurrentQuantity != d.InitialQuantity || d.CurrentYield != d.InitialYield {
			t.Errorf("deposit %+v: current != initial at generation", d)
		}
	}
}

// TestSaveDepositsConflict confirms saving the same deposit twice violates the
// primary key and returns ErrConflict.
func TestSaveDepositsConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "deposits-conflict")

	if err := db.SaveCluster(ctx, Cluster{
		GameID: gameID, Radius: 5, N: 10, Density: "average", Spacing: 2,
		Systems: []System{{Q: 0, R: 0}},
	}); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}
	if err := db.SaveSystemContents(ctx, SystemContents{
		GameID:  gameID,
		Planets: []Planet{{Q: 0, R: 0, Orbit: 4, Type: "asteroid belt", Habitability: 0}},
	}); err != nil {
		t.Fatalf("SaveSystemContents: %v", err)
	}

	d := Deposits{
		GameID: gameID,
		Deposits: []Deposit{{
			Q: 0, R: 0, Orbit: 4, DepositNo: 1, Resource: "mtls",
			InitialQuantity: 100, CurrentQuantity: 100, InitialYield: 120, CurrentYield: 120,
		}},
	}
	if err := db.SaveDeposits(ctx, d); err != nil {
		t.Fatalf("first SaveDeposits: %v", err)
	}
	if err := db.SaveDeposits(ctx, d); !errors.Is(err, ErrConflict) {
		t.Errorf("second SaveDeposits = %v, want ErrConflict", err)
	}
}

// TestSaveDepositUnknownPlanet confirms a deposit for a planet that was never
// generated is rejected by the foreign key as ErrConflict.
func TestSaveDepositUnknownPlanet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "deposits-fk")

	d := Deposits{
		GameID: gameID,
		Deposits: []Deposit{{
			Q: 99, R: 99, Orbit: 4, DepositNo: 1, Resource: "mtls",
			InitialQuantity: 100, CurrentQuantity: 100, InitialYield: 120, CurrentYield: 120,
		}},
	}
	if err := db.SaveDeposits(ctx, d); !errors.Is(err, ErrConflict) {
		t.Errorf("SaveDeposits with unknown planet = %v, want ErrConflict", err)
	}
}

// TestSaveDepositRejectsZeroDepositNo confirms deposit_no is 1-based: a 0 (the old
// 0-based convention, or an unset field) violates the CHECK and returns ErrConflict.
func TestSaveDepositRejectsZeroDepositNo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "deposits-zero")

	if err := db.SaveCluster(ctx, Cluster{
		GameID: gameID, Radius: 5, N: 10, Density: "average", Spacing: 2,
		Systems: []System{{Q: 0, R: 0}},
	}); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}
	if err := db.SaveSystemContents(ctx, SystemContents{
		GameID:  gameID,
		Planets: []Planet{{Q: 0, R: 0, Orbit: 4, Type: "asteroid belt", Habitability: 0}},
	}); err != nil {
		t.Fatalf("SaveSystemContents: %v", err)
	}

	d := Deposits{
		GameID: gameID,
		Deposits: []Deposit{{
			Q: 0, R: 0, Orbit: 4, DepositNo: 0, Resource: "mtls",
			InitialQuantity: 100, CurrentQuantity: 100, InitialYield: 120, CurrentYield: 120,
		}},
	}
	if err := db.SaveDeposits(ctx, d); !errors.Is(err, ErrConflict) {
		t.Errorf("SaveDeposits with deposit_no 0 = %v, want ErrConflict", err)
	}
}

// TestGetDepositsNotFound confirms a game whose deposits stage has not run reports
// not found.
func TestGetDepositsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "deposits-empty")

	if _, err := db.GetDeposits(ctx, gameID); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetDeposits on empty game = %v, want ErrRecordNotFound", err)
	}
}

// TestDepositSettingsRoundTrip persists the deposit settings as generator JSON and
// reads them back, confirming the abundance knobs and endowments survive.
func TestDepositSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	gameID := seedGame(t, db, "deposits-settings")

	settings := genesis.DefaultDepositSettings()
	settings.Metals.Abundance = genesis.AbundanceRich
	settings.NonMetals.Endowment = 1_000_000
	js, err := settings.MarshalSettings()
	if err != nil {
		t.Fatalf("MarshalSettings: %v", err)
	}
	if err := db.SaveGenerator(ctx, GeneratorSelection{
		GameID: gameID, Stage: StageDeposits, GeneratorID: 1, Version: 1, Settings: js,
	}); err != nil {
		t.Fatalf("SaveGenerator: %v", err)
	}

	g, err := db.GetGenerator(ctx, gameID, StageDeposits)
	if err != nil {
		t.Fatalf("GetGenerator: %v", err)
	}
	back, err := genesis.ParseDepositSettings(g.Settings)
	if err != nil {
		t.Fatalf("ParseDepositSettings: %v", err)
	}
	if back != settings {
		t.Errorf("round-tripped settings = %+v, want %+v", back, settings)
	}
}

// depositsToStore flattens a genesis DepositsResult into the store's Deposits,
// with current == initial at generation and DepositNo a 1-based per-planet index.
func depositsToStore(gameID int64, res genesis.DepositsResult) Deposits {
	d := Deposits{GameID: gameID}
	for _, sys := range res.Systems {
		for _, pd := range sys.Planets {
			for i, dep := range pd.Deposits {
				d.Deposits = append(d.Deposits, Deposit{
					Q: sys.Hex.Q, R: sys.Hex.R, Orbit: pd.Orbit, DepositNo: i + 1,
					Resource:        string(dep.Resource),
					InitialQuantity: dep.Quantity, CurrentQuantity: dep.Quantity,
					InitialYield: dep.Yield, CurrentYield: dep.Yield,
				})
			}
		}
	}
	return d
}
