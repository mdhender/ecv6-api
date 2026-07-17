// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package setup

import (
	"context"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
	"github.com/mdhender/ecv6-api/internal/store"
	"github.com/mdhender/ecv6-api/internal/worldgen"
)

// mappingKnobs is a small, fast cluster for the mapping tests: fewer systems than
// the default so the round-trip stays quick while still exercising many planets
// and deposits.
func mappingKnobs() worldgen.Knobs {
	k := worldgen.DefaultKnobs()
	k.Placement.Count = 25
	return k
}

// genCluster builds a generated cluster for the mapping tests off a fixed seed.
func genCluster(t *testing.T, knobs worldgen.Knobs) *worldgen.Cluster {
	t.Helper()
	c, err := genesis.GenesisCluster{}.GenerateCluster(context.Background(), knobs, prng.New(0xABCD, 0x1234))
	if err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}
	return c
}

// TestClusterToStore checks the placement-level mapping: derived radius from the
// cluster, N/density/spacing from the Knobs actually run, and one store.System per
// generated system in the same order.
func TestClusterToStore(t *testing.T) {
	t.Parallel()
	knobs := mappingKnobs()
	c := genCluster(t, knobs)

	got := clusterToStore(42, knobs, c)

	if got.GameID != 42 {
		t.Errorf("GameID = %d, want 42", got.GameID)
	}
	if got.Radius != c.Radius {
		t.Errorf("Radius = %d, want %d (cluster's derived radius)", got.Radius, c.Radius)
	}
	if got.N != int(knobs.Placement.Count) {
		t.Errorf("N = %d, want %d", got.N, int(knobs.Placement.Count))
	}
	if got.Density != string(knobs.Placement.Density) {
		t.Errorf("Density = %q, want %q", got.Density, string(knobs.Placement.Density))
	}
	if got.Spacing != int(knobs.Placement.Spacing) {
		t.Errorf("Spacing = %d, want %d", got.Spacing, int(knobs.Placement.Spacing))
	}
	if len(got.Systems) != len(c.Systems) {
		t.Fatalf("mapped %d systems, want %d", len(got.Systems), len(c.Systems))
	}
	for i, s := range c.Systems {
		if got.Systems[i] != (store.System{Q: s.Q, R: s.R}) {
			t.Errorf("system %d = %+v, want (%d,%d)", i, got.Systems[i], s.Q, s.R)
		}
	}
}

// TestSystemContentsToStore checks that every planet of every system is flattened,
// carrying its system's (q, r), with no home-template rows.
func TestSystemContentsToStore(t *testing.T) {
	t.Parallel()
	c := genCluster(t, mappingKnobs())

	got := systemContentsToStore(7, c)

	if got.GameID != 7 {
		t.Errorf("GameID = %d, want 7", got.GameID)
	}
	i := 0
	for _, s := range c.Systems {
		for _, p := range s.Planets {
			if i >= len(got.Planets) {
				t.Fatalf("mapped only %d planets, ran out at system (%d,%d)", len(got.Planets), s.Q, s.R)
			}
			want := store.Planet{Q: s.Q, R: s.R, Orbit: p.Orbit, Type: string(p.Type), Habitability: p.Habitability}
			if got.Planets[i] != want {
				t.Errorf("planet %d = %+v, want %+v", i, got.Planets[i], want)
			}
			i++
		}
	}
	if i != len(got.Planets) {
		t.Errorf("mapped %d planets, want %d", len(got.Planets), i)
	}
}

// TestDepositsToStore checks the deposit mapping: quantity/yield mirrored into
// initial == current, and DepositNo assigned 1-based per planet in creation order.
func TestDepositsToStore(t *testing.T) {
	t.Parallel()
	c := genCluster(t, mappingKnobs())

	got := depositsToStore(3, c)

	if got.GameID != 3 {
		t.Errorf("GameID = %d, want 3", got.GameID)
	}
	i := 0
	for _, s := range c.Systems {
		for _, p := range s.Planets {
			for no, d := range p.Deposits {
				if i >= len(got.Deposits) {
					t.Fatalf("mapped only %d deposits, ran out at system (%d,%d) orbit %d", len(got.Deposits), s.Q, s.R, p.Orbit)
				}
				want := store.Deposit{
					Q: s.Q, R: s.R, Orbit: p.Orbit, DepositNo: no + 1,
					Resource:        string(d.Resource),
					InitialQuantity: d.Quantity, CurrentQuantity: d.Quantity,
					InitialYield: d.Yield, CurrentYield: d.Yield,
				}
				if got.Deposits[i] != want {
					t.Errorf("deposit %d = %+v, want %+v", i, got.Deposits[i], want)
				}
				i++
			}
		}
	}
	if i != len(got.Deposits) {
		t.Errorf("mapped %d deposits, want %d", len(got.Deposits), i)
	}
	if i == 0 {
		t.Fatal("cluster produced no deposits; mapping was not exercised")
	}
}

// TestMappingPersists proves the mapped rows are mutually consistent and
// persistable: saving the cluster, then contents, then deposits satisfies every
// foreign key (deposit → planet → system), and each stage reloads with the mapped
// row count.
func TestMappingPersists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, gameID := newTestGame(t)

	knobs := mappingKnobs()
	c := genCluster(t, knobs)

	cluster := clusterToStore(gameID, knobs, c)
	contents := systemContentsToStore(gameID, c)
	deposits := depositsToStore(gameID, c)

	if err := db.SaveCluster(ctx, cluster); err != nil {
		t.Fatalf("SaveCluster: %v", err)
	}
	if err := db.SaveSystemContents(ctx, contents); err != nil {
		t.Fatalf("SaveSystemContents: %v", err)
	}
	if err := db.SaveDeposits(ctx, deposits); err != nil {
		t.Fatalf("SaveDeposits: %v", err)
	}

	gotCluster, err := db.GetCluster(ctx, gameID)
	if err != nil {
		t.Fatalf("GetCluster: %v", err)
	}
	if gotCluster.Radius != cluster.Radius || gotCluster.N != cluster.N ||
		gotCluster.Density != cluster.Density || gotCluster.Spacing != cluster.Spacing ||
		len(gotCluster.Systems) != len(cluster.Systems) {
		t.Errorf("reloaded cluster = %+v, want radius %d N %d density %q spacing %d, %d systems",
			gotCluster, cluster.Radius, cluster.N, cluster.Density, cluster.Spacing, len(cluster.Systems))
	}

	gotContents, err := db.GetSystemContents(ctx, gameID)
	if err != nil {
		t.Fatalf("GetSystemContents: %v", err)
	}
	if len(gotContents.Planets) != len(contents.Planets) {
		t.Errorf("reloaded %d planets, want %d", len(gotContents.Planets), len(contents.Planets))
	}

	gotDeposits, err := db.GetDeposits(ctx, gameID)
	if err != nil {
		t.Fatalf("GetDeposits: %v", err)
	}
	if len(gotDeposits.Deposits) != len(deposits.Deposits) {
		t.Errorf("reloaded %d deposits, want %d", len(gotDeposits.Deposits), len(deposits.Deposits))
	}
}
