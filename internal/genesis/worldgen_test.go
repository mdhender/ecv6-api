// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
	"github.com/mdhender/ecv6-api/internal/worldgen"
)

// TestGenesisIdentity pins the adapter's catalog metadata: a monolithic cluster
// generator that fills all three layers, offered with no special flavor, behind a
// non-nil stable UUID.
func TestGenesisIdentity(t *testing.T) {
	var g genesis.GenesisCluster

	id := g.Identity()
	if id.ID == uuid.Nil {
		t.Error("Identity().ID is the nil UUID")
	}
	if id.Name != "Genesis" {
		t.Errorf("Identity().Name = %q, want %q", id.Name, "Genesis")
	}

	if want := worldgen.ScopeCluster | worldgen.ScopeSystem | worldgen.ScopePlanet; g.Produces() != want {
		t.Errorf("Produces() = %b, want %b", g.Produces(), want)
	}
	if g.Flavor() != worldgen.FlavorNone {
		t.Errorf("Flavor() = %b, want FlavorNone", g.Flavor())
	}
}

// TestGenerateClusterMatchesDirectStages confirms the adapter output maps 1:1 to
// an equivalent direct Place -> GenerateContents -> GenerateDeposits run on the
// same seeds and the mapped settings — the whole point of the adapter. It also
// covers the Knobs -> settings mapping (including a non-default abundance) and the
// nil-slice ownership convention.
func TestGenerateClusterMatchesDirectStages(t *testing.T) {
	seeds := prng.New(0xABCD, 0x1234)

	knobs := worldgen.DefaultKnobs()
	knobs.Placement.Count = 40
	knobs.Deposits.Metals = worldgen.RichResource // exercise a non-default map

	var g genesis.GenesisCluster
	got, err := g.GenerateCluster(context.Background(), knobs, seeds)
	if err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}

	// The equivalent direct run, with the settings the adapter should have produced.
	placed, err := genesis.Place(seeds, genesis.PlacementSettings{N: 40, Density: genesis.Average, Spacing: 2})
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	contents := genesis.GenerateContents(seeds, placed.Systems)
	depSettings := genesis.DefaultDepositSettings()
	depSettings.Metals.Abundance = genesis.AbundanceRich
	deposits := genesis.GenerateDeposits(seeds, contents, depSettings)

	if got.Radius != placed.Radius {
		t.Errorf("Radius = %d, want %d", got.Radius, placed.Radius)
	}
	if len(got.Systems) != len(contents.Systems) {
		t.Fatalf("cluster has %d systems, direct run has %d", len(got.Systems), len(contents.Systems))
	}
	for i, sys := range got.Systems {
		sc := contents.Systems[i]
		sd := deposits.Systems[i]
		if sys.Q != sc.Hex.Q || sys.R != sc.Hex.R {
			t.Errorf("system %d hex = (%d,%d), want (%d,%d)", i, sys.Q, sys.R, sc.Hex.Q, sc.Hex.R)
		}
		if sys.Planets == nil {
			t.Errorf("system %d planets slice is nil; Genesis owns the layer", i)
		}
		if len(sys.Planets) != len(sc.Planets) {
			t.Fatalf("system %d has %d planets, want %d", i, len(sys.Planets), len(sc.Planets))
		}
		for j, p := range sys.Planets {
			cp := sc.Planets[j]
			if p.Orbit != cp.Orbit || string(p.Type) != string(cp.Type) || p.Habitability != cp.Habitability {
				t.Errorf("system %d planet %d = %+v, want orbit %d type %q hab %d",
					i, j, p, cp.Orbit, cp.Type, cp.Habitability)
			}
			if p.Deposits == nil {
				t.Errorf("system %d planet %d deposits slice is nil; Genesis owns the layer", i, j)
			}
			pd := sd.Planets[j]
			if len(p.Deposits) != len(pd.Deposits) {
				t.Fatalf("system %d planet %d has %d deposits, want %d", i, j, len(p.Deposits), len(pd.Deposits))
			}
			for k, d := range p.Deposits {
				gd := pd.Deposits[k]
				if string(d.Resource) != string(gd.Resource) || d.Quantity != gd.Quantity || d.Yield != gd.Yield {
					t.Errorf("system %d planet %d deposit %d = %+v, want %+v", i, j, k, d, gd)
				}
			}
		}
	}
}

// TestGenerateClusterDeterministic confirms the same seeds and Knobs produce a
// byte-identical cluster, independent of run.
func TestGenerateClusterDeterministic(t *testing.T) {
	seeds := prng.New(0xFEED, 0xFACE)
	knobs := worldgen.DefaultKnobs()

	var g genesis.GenesisCluster
	a, err := g.GenerateCluster(context.Background(), knobs, seeds)
	if err != nil {
		t.Fatalf("GenerateCluster a: %v", err)
	}
	b, err := g.GenerateCluster(context.Background(), knobs, prng.New(0xFEED, 0xFACE))
	if err != nil {
		t.Fatalf("GenerateCluster b: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("same seeds + Knobs produced different clusters")
	}
}

// TestGenerateClusterSurfacesPlacementError confirms invalid or infeasible
// settings surface unchanged, with no partial cluster returned.
func TestGenerateClusterSurfacesPlacementError(t *testing.T) {
	seeds := prng.New(1, 2)
	knobs := worldgen.DefaultKnobs()
	knobs.Placement.Count = 1 // below MinSystems -> ErrInvalidSettings

	var g genesis.GenesisCluster
	got, err := g.GenerateCluster(context.Background(), knobs, seeds)
	if !errors.Is(err, genesis.ErrInvalidSettings) {
		t.Errorf("err = %v, want ErrInvalidSettings", err)
	}
	if got != nil {
		t.Errorf("cluster = %+v, want nil on error", got)
	}
}
