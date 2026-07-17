// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package worldgen

import "github.com/google/uuid"

// Generator is the base every generator carries: who it is, which layers it
// fills, and what intent it is offered for. The orchestrator uses Produces to
// compose generators (run a finer one only for a layer a coarser one left empty)
// and to reject incoherent selections up front; the catalog uses Flavor to scope
// which picker offers it.
type Generator interface {
	// Identity is the catalog card — selection handle and recorded provenance.
	Identity() Identity
	// Produces is the set of layers this generator fills.
	Produces() Scope
	// Flavor is the intent modifier — where the generator is offered, orthogonal
	// to the layers it fills. Zero (FlavorNone) is an ordinary generator.
	Flavor() Flavor
}

// Identity is a generator's catalog card. The UUID is a machine handle: a client
// sends it to select the implementation, and it is stored in
// game_generator.generator_id as provenance. The human GM sees Name/Description,
// not the raw id.
//
// Per ADR-0017 the id and version are SELECTION + PROVENANCE only — they never
// enter the PRNG key path.
type Identity struct {
	ID          uuid.UUID // machine handle: a client sends it to select the implementation
	Name        string    // short label the GM sees in the picker
	Description string    // prose shown to the GM when choosing
	Version     string    // provenance; recorded per game (ADR-0016)
}

// Scope is the set of container layers a generator fills — which contents-slice
// of the domain model it populates. It makes "may go further" composable: the
// orchestrator runs a finer generator only for a layer a coarser one left empty.
// Combine values with bitwise OR.
//
// Each bit names the container whose contents are generated, mirroring the domain
// model: ScopeCluster fills Cluster.Systems, ScopeSystem fills System.Planets,
// ScopePlanet fills Planet.Deposits. The bit lines up with the interface of the
// same name (ClusterGenerator produces ScopeCluster, and so on).
type Scope uint8

const (
	ScopeCluster Scope = 1 << iota // the cluster's contents (its systems — the map)
	ScopeSystem                    // a system's contents (orbits + planets + habitability)
	ScopePlanet                    // a planet's contents (deposits)
)

// Has reports whether s fills the given layer.
func (s Scope) Has(layer Scope) bool { return s&layer != 0 }

// Flavor is an intent modifier orthogonal to Scope: it says where a generator is
// offered in the GM's workflow (catalog placement and selection filtering), not
// which layers it fills. It is layer-agnostic — a Flavor combines with whatever
// Scope a generator produces, so FlavorHome + ScopeSystem is a home-system
// generator and FlavorHome + ScopePlanet a home-world generator, with no new
// constants. A Flavor is a presentation/validation hint, never entropy (ADR-0017).
// Combine values with bitwise OR.
type Flavor uint8

// FlavorNone is the zero value: an ordinary generator with no special placement.
const FlavorNone Flavor = 0

const (
	FlavorHome Flavor = 1 << iota // builds/rebuilds a faction home (founding, E3)
	// future flavors append here
)

// Has reports whether f carries the given flavor.
func (f Flavor) Has(flavor Flavor) bool { return f&flavor != 0 }
