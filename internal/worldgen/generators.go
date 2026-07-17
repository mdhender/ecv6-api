// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package worldgen

import (
	"context"

	"github.com/mdhender/ecv6-api/internal/prng"
)

// The three generator interfaces share one uniform signature — (Knobs, Seeds),
// plus the unit of work for the finer layers — so a registry keyed by role stays
// homogeneous. Each takes a private root already scoped by the orchestrator (the
// game root for a cluster, the per-(q,r) root for a system, the per-orbit root
// for a planet), so a generator draws freely below its root and never touches the
// frozen key encoding for addressing (ADR-0017).

// ClusterGenerator builds the whole cluster from the game's root seeds. At
// minimum it places the systems (the map); per its Scope it may also fill
// orbits/planets/habitability and deposits — hence it receives the full Knobs and
// a placement-only generator ignores the layers it does not fill.
//
// Genesis-as-monolith is one ClusterGenerator with Produces() =
// ScopeCluster|ScopeSystem|ScopePlanet.
type ClusterGenerator interface {
	Generator
	GenerateCluster(ctx context.Context, knobs Knobs, seeds prng.Seeds) (*Cluster, error)
}

// SystemGenerator fills one already-placed system — its orbits and planets (with
// habitability), and per its Scope optionally their deposits — and returns the
// planets. A home-system generator is an ordinary SystemGenerator (ADR-0017) that
// advertises Flavor() = FlavorHome: at founding the GM picks a placed system, a
// SystemGenerator rebuilds it, then the faction is assigned.
type SystemGenerator interface {
	Generator
	GenerateSystem(ctx context.Context, knobs Knobs, seeds prng.Seeds, sys System) ([]Planet, error)
}

// PlanetGenerator fills one planet (e.g. its deposits) and returns an updated
// copy. It must not mutate its input.
type PlanetGenerator interface {
	Generator
	GeneratePlanet(ctx context.Context, knobs Knobs, seeds prng.Seeds, p Planet) (Planet, error)
}
