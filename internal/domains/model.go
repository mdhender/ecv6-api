// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package domains holds the behavior-free world-data DTOs shared by the cluster
// generators (internal/genesis, internal/worldgen), the setup workflow
// (internal/setup), and the future store-blind engine. These are the domain
// shape in the turn-0 generation pipeline (store shape -> domain shape ->
// generator -> domain shape -> store shape -> persist): plain structs with no
// methods and no logic, importing nothing else in this module. Validation and
// behavior live in the logic packages, per Go idiom.
//
// The types use the same string vocabulary as the store (rocky|asteroid belt|gas
// giant, fuel|mtls|nmtl) so the domain -> store mapping stays trivial.
//
// The nil convention marks ownership of a layer: a NIL Planets/Deposits slice
// means "not filled yet"; a non-nil (possibly empty) slice means "this generator
// owns the layer." The orchestrator uses it to decide whether to run a finer
// generator.
package domains

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
