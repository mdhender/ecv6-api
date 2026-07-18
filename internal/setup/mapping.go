// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package setup

import (
	"encoding/json"
	"fmt"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/store"
	"github.com/mdhender/ecv6-api/internal/worldgen"
)

// The genesis → store mapping, productionized from the test-only fixtures. Its
// source is the worldgen domain model (a generated *worldgen.Cluster), not the
// genesis stage structs directly: the funnel is genesis → worldgen.Cluster (the
// generator adapter) → store (here). See the E1 reconciliation on issue #90.
//
// Each function is pure and total — it allocates store rows from an already
// assembled cluster and never draws randomness or touches the database. The
// orchestrator (GenerateCluster) persists the results in one pass.

// clusterToStore maps a generated cluster's placement-level facts to store.Cluster.
// Radius is the generator's derived output; N, density, and spacing are the GM's
// settings (the Knobs actually run), recorded so a game remembers what it asked
// for — they are not re-derived from the cluster. Systems carry only their (q, r);
// their contents map separately (systemContentsToStore / depositsToStore).
func clusterToStore(gameID int64, knobs worldgen.Knobs, c *worldgen.Cluster) store.Cluster {
	systems := make([]store.System, len(c.Systems))
	for i, s := range c.Systems {
		systems[i] = store.System{Q: s.Q, R: s.R}
	}
	return store.Cluster{
		GameID:  gameID,
		Radius:  c.Radius,
		N:       int(knobs.Placement.Count),
		Density: string(knobs.Placement.Density),
		Spacing: int(knobs.Placement.Spacing),
		Systems: systems,
	}
}

// systemContentsToStore flattens every system's planets into store.SystemContents.
// Each planet carries its system's (q, r), so one flat slice addresses every
// system's orbits. There are no home-template rows (ADR-0017): a home system is
// ordinary planet rows produced on demand at founding.
func systemContentsToStore(gameID int64, c *worldgen.Cluster) store.SystemContents {
	var planets []store.Planet
	for _, s := range c.Systems {
		for _, p := range s.Planets {
			planets = append(planets, store.Planet{
				Q:            s.Q,
				R:            s.R,
				Orbit:        p.Orbit,
				Type:         string(p.Type),
				Habitability: p.Habitability,
			})
		}
	}
	return store.SystemContents{GameID: gameID, Planets: planets}
}

// depositsToStore flattens every planet's deposits into store.Deposits. It mirrors
// each generated value into the initial/current pair — InitialQuantity ==
// CurrentQuantity and InitialYield == CurrentYield, equal at generation, with play
// later mutating the current values — and assigns DepositNo as the 1-based
// creation-order index within each planet. That index is a deterministic function
// of the generation order (reproducible from the game seeds), which is why the
// store does not hand out a surrogate id here. There are no home-template deposits
// (ADR-0017).
func depositsToStore(gameID int64, c *worldgen.Cluster) store.Deposits {
	var deposits []store.Deposit
	for _, s := range c.Systems {
		for _, p := range s.Planets {
			for i, d := range p.Deposits {
				deposits = append(deposits, store.Deposit{
					Q:               s.Q,
					R:               s.R,
					Orbit:           p.Orbit,
					DepositNo:       i + 1,
					Resource:        string(d.Resource),
					InitialQuantity: d.Quantity,
					CurrentQuantity: d.Quantity,
					InitialYield:    d.Yield,
					CurrentYield:    d.Yield,
				})
			}
		}
	}
	return store.Deposits{GameID: gameID, Deposits: deposits}
}

// generatorSelections builds the three game_generator rows a game records for its
// Genesis run (ADR-0016): one per stage — placement, system_contents, deposits —
// each carrying the stage's generator identity and version and the resolved knobs
// it ran as opaque JSON settings. Genesis is monolithic (it fills all three
// stages), so every row records a Genesis per-stage identity; the system_contents
// stage takes no knobs, so its settings are the empty object.
//
// GeneratorID/Version are the genesis integer stage identities the store's
// game_generator columns expect today. ADR-0017 makes generator_id the generator's
// UUID (Identity().ID); reconciling the column to that UUID is a later schema
// change, out of scope for the persist pass.
func generatorSelections(gameID int64, knobs worldgen.Knobs) ([]store.GeneratorSelection, error) {
	placementJSON, err := json.Marshal(knobs.Placement)
	if err != nil {
		return nil, fmt.Errorf("marshal placement knobs: %w", err)
	}
	depositsJSON, err := json.Marshal(knobs.Deposits)
	if err != nil {
		return nil, fmt.Errorf("marshal deposit knobs: %w", err)
	}
	return []store.GeneratorSelection{
		{
			GameID:      gameID,
			Stage:       store.StagePlacement,
			GeneratorID: int64(genesis.PlacementGeneratorID),
			Version:     int64(genesis.PlacementVersion),
			Settings:    string(placementJSON),
		},
		{
			GameID:      gameID,
			Stage:       store.StageSystemContents,
			GeneratorID: int64(genesis.SysContentsGeneratorID),
			Version:     int64(genesis.SysContentsVersion),
			Settings:    "{}",
		},
		{
			GameID:      gameID,
			Stage:       store.StageDeposits,
			GeneratorID: int64(genesis.DepositsGeneratorID),
			Version:     int64(genesis.DepositsVersion),
			Settings:    string(depositsJSON),
		},
	}, nil
}
