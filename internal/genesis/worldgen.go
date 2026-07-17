// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis

import (
	"context"

	"github.com/google/uuid"

	"github.com/mdhender/ecv6-api/internal/prng"
	"github.com/mdhender/ecv6-api/internal/worldgen"
)

// The adapter lives in package genesis, not internal/worldgen: the worldgen
// contract must not depend on any concrete generator, so the dependency points
// genesis -> worldgen (genesis reads the worldgen domain model and interfaces),
// never the reverse. Placing it here also lets it read the genesis stage result
// structs directly.

// genesisID is Genesis's stable catalog UUID — a selection + provenance handle
// only. Per ADR-0017 it never enters the PRNG key path; seeds derive purely from
// the game root and the frozen stage tags. It is fixed for the life of the
// catalog entry so a recorded game_generator row always resolves back to Genesis.
var genesisID = uuid.MustParse("f7c0a9d2-4e83-4b1a-9c6f-2a5d8e3b1c40")

// genesisVersion is Genesis's provenance version, recorded per game (ADR-0016).
// It is the string form of the integer version the stage constants carry
// (PlacementVersion / SysContentsVersion / DepositsVersion == 1), pending the
// UUID/version reconciliation of the store's game_generator row in E1 §3.
const genesisVersion = "1"

// GenesisCluster is the Genesis world generator behind the worldgen contract: one
// monolithic ClusterGenerator that fills all three layers itself (systems, their
// planets, and each planet's deposits) by running the three Genesis stages. It is
// the only generator E1 ships; per-layer SystemGenerator/PlanetGenerator options,
// and the home-system (FlavorHome) generator, arrive with founding (E3).
type GenesisCluster struct{}

// compile-time proof the adapter satisfies the cluster-generation contract.
var _ worldgen.ClusterGenerator = (*GenesisCluster)(nil)

// Identity returns Genesis's catalog card: its stable UUID, the label and prose
// the GM sees when choosing a generator, and its provenance version.
func (GenesisCluster) Identity() worldgen.Identity {
	return worldgen.Identity{
		ID:          genesisID,
		Name:        "Genesis",
		Description: "The reference cluster generator: places the systems, fills every orbit with planets and habitability, and rolls each planet's resource deposits.",
		Version:     genesisVersion,
	}
}

// Produces reports that Genesis fills all three container layers — the cluster's
// systems, each system's planets, and each planet's deposits — so the
// orchestrator needs no finer generator to complete a Genesis cluster.
func (GenesisCluster) Produces() worldgen.Scope {
	return worldgen.ScopeCluster | worldgen.ScopeSystem | worldgen.ScopePlanet
}

// Flavor reports FlavorNone: Genesis is an ordinary generator with no special
// catalog placement. The home-system generator (FlavorHome) is separate (E3).
func (GenesisCluster) Flavor() worldgen.Flavor { return worldgen.FlavorNone }

// GenerateCluster runs the three Genesis stages off the game's root seeds and
// assembles a fully populated worldgen.Cluster:
//
//	Place → GenerateContents → GenerateDeposits
//
// The knobs are mapped to the stage settings up front; ctx is accepted and
// ignored (the stages are pure and non-blocking). Only Place can fail — on
// invalid or infeasible settings — and it validates before drawing, so a failure
// means nothing downstream ran and no partial cluster is returned:
// genesis.ErrInvalidSettings / genesis.ErrInfeasible surface unchanged.
func (GenesisCluster) GenerateCluster(ctx context.Context, knobs worldgen.Knobs, seeds prng.Seeds) (*worldgen.Cluster, error) {
	_ = ctx

	placed, err := Place(seeds, placementSettings(knobs))
	if err != nil {
		return nil, err
	}
	contents := GenerateContents(seeds, placed.Systems)
	deposits := GenerateDeposits(seeds, contents, depositSettings(knobs))

	return assembleCluster(placed, contents, deposits), nil
}

// placementSettings maps the placement knobs to Genesis PlacementSettings. The
// Density enum values are identical strings in both packages, so the tier maps
// losslessly; Validate (inside Place) rejects any out-of-range value.
func placementSettings(k worldgen.Knobs) PlacementSettings {
	return PlacementSettings{
		N:       int(k.Placement.Count),
		Density: Density(k.Placement.Density),
		Spacing: int(k.Placement.Spacing),
	}
}

// depositSettings maps the deposit knobs to Genesis DepositSettings. The
// Abundance enum values are identical strings in both packages. Knobs carry no
// endowment, so each resource gets the documented DefaultEndowment (the ten-planet
// baseline); GM-entered endowments are future work.
func depositSettings(k worldgen.Knobs) DepositSettings {
	res := func(a worldgen.Abundance) ResourceSettings {
		return ResourceSettings{Abundance: Abundance(a), Endowment: DefaultEndowment}
	}
	return DepositSettings{
		Fuel:      res(k.Deposits.Fuel),
		Metals:    res(k.Deposits.Metals),
		NonMetals: res(k.Deposits.NonMetals),
	}
}

// assembleCluster joins the three stage results into the worldgen domain model.
// The stages emit their systems in one shared order (placement order), and the
// deposit stage emits each system's per-planet deposits in the same order as that
// system's planets, so contents and deposits zip by index. Every slice is built
// with make, so it is non-nil even when empty: Genesis owns all three layers, and
// a non-nil (possibly empty) slice is how the domain model marks an owned layer.
func assembleCluster(placed PlacementResult, contents ContentsResult, deposits DepositsResult) *worldgen.Cluster {
	cluster := &worldgen.Cluster{
		Radius:  placed.Radius,
		Systems: make([]worldgen.System, len(contents.Systems)),
	}
	for i, sc := range contents.Systems {
		sd := deposits.Systems[i]
		planets := make([]worldgen.Planet, len(sc.Planets))
		for j, p := range sc.Planets {
			pd := sd.Planets[j]
			deps := make([]worldgen.Deposit, len(pd.Deposits))
			for k, d := range pd.Deposits {
				deps[k] = worldgen.Deposit{
					Resource: worldgen.Resource(d.Resource),
					Quantity: d.Quantity,
					Yield:    d.Yield,
				}
			}
			planets[j] = worldgen.Planet{
				Orbit:        p.Orbit,
				Type:         worldgen.PlanetType(p.Type),
				Habitability: p.Habitability,
				Deposits:     deps,
			}
		}
		cluster.Systems[i] = worldgen.System{Q: sc.Hex.Q, R: sc.Hex.R, Planets: planets}
	}
	return cluster
}
