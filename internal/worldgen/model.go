// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package worldgen

// The generator-facing domain model — distinct from both internal/genesis
// (which has its own stage result structs) and internal/store (the persistence
// structs). It uses the same string vocabulary as the store (rocky|asteroid
// belt|gas giant, fuel|mtls|nmtl) so the domain -> store mapping stays trivial.
//
// The nil convention marks ownership of a layer: a NIL Planets/Deposits slice
// means "not filled yet"; a non-nil (possibly empty) slice means "this generator
// owns the layer." The orchestrator uses it to decide whether to run a finer
// generator.

// PlanetType is what occupies an orbit. Same values as the store.
type PlanetType string

const (
	Rocky        PlanetType = "rocky"
	AsteroidBelt PlanetType = "asteroid belt"
	GasGiant     PlanetType = "gas giant"
)

// Resource is a natural resource kind. Same values as the store.
type Resource string

const (
	Fuel      Resource = "fuel"
	Metals    Resource = "mtls"
	NonMetals Resource = "nmtl"
)

// Cluster is a whole generated cluster. Radius is DERIVED output (from count and
// density per ADR-0014), never a knob.
type Cluster struct {
	Radius  int
	Systems []System
}

// System is one placed system at axial hex (Q, R). Planets is nil until a
// generator fills the contents layer.
type System struct {
	Q, R    int
	Planets []Planet
}

// Planet is one body in an orbit. Deposits is nil until a generator fills the
// deposit layer.
type Planet struct {
	Orbit        int
	Type         PlanetType
	Habitability int
	Deposits     []Deposit
}

// Deposit is one natural-resource deposit on a planet. Quantity and Yield are the
// as-generated values; the domain -> store mapping mirrors them into
// initial_/current_ pairs.
type Deposit struct {
	Resource Resource
	Quantity int64
	Yield    int
}
