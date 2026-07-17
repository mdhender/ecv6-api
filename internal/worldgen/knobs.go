// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package worldgen

// Knobs are well-defined GAME values, not per-generator settings: concrete typed
// values with no interface{}, no generics, no JSON blob. They bundle into a
// single aggregate ([Knobs], the "MetaKnob") passed uniformly to every generator,
// so the signature is identical across a role — which is what keeps a registry
// homogeneous — and a GM can save a reusable layout.

// SystemCount is N, the number of systems to place (10..1000).
type SystemCount int

// Density is a stellar-density tier: how large an area the systems spread over.
// Values match the Genesis placement supplement.
type Density string

const (
	ExtremelyDense Density = "extremely dense"
	Dense          Density = "dense"
	AverageDensity Density = "average"
	Sparse         Density = "sparse"
	VerySparse     Density = "very sparse"
)

// Spacing is the minimum system spacing in hexes (ADR-0014), >= 1.
type Spacing int

// Abundance is a per-resource richness tier. Values match the Genesis deposits
// supplement.
type Abundance string

const (
	PoorResource    Abundance = "poor"
	AverageResource Abundance = "average"
	RichResource    Abundance = "rich"
)

// PlacementKnobs are the knobs the cluster/map layer reads. The JSON tags are the
// stored shape recorded in game_generator.settings for the placement stage
// (ADR-0016: a game records the resolved knobs it ran).
type PlacementKnobs struct {
	Count   SystemCount `json:"count"`
	Density Density     `json:"density"`
	Spacing Spacing     `json:"spacing"`
}

// DepositKnobs are the per-resource abundance knobs the deposit layer reads. The
// JSON tags (keyed by the resource codes) are the stored shape recorded in
// game_generator.settings for the deposits stage.
type DepositKnobs struct {
	Fuel      Abundance `json:"fuel"`
	Metals    Abundance `json:"mtls"`
	NonMetals Abundance `json:"nmtl"`
}

// Knobs is the "MetaKnob": every layer's knobs in one value. A generator uses the
// fields for the layers it fills and ignores the rest. A GM "layout" is a saved
// Knobs.
//
// Defaults resolve at the EDGE, not in the signature: the API/CLI merges the GM's
// partial input onto [DefaultKnobs] once to produce a full Knobs, which is then
// both executed and recorded in game_generator.settings for reproducibility.
type Knobs struct {
	Placement PlacementKnobs
	Deposits  DepositKnobs
	// future layers append here
}

// DefaultKnobs returns the documented baseline: N = 100, average density,
// spacing 2, average abundance for every resource. Mirrors
// genesis.DefaultPlacementSettings and genesis.DefaultDepositSettings.
func DefaultKnobs() Knobs {
	return Knobs{
		Placement: PlacementKnobs{
			Count:   100,
			Density: AverageDensity,
			Spacing: 2,
		},
		Deposits: DepositKnobs{
			Fuel:      AverageResource,
			Metals:    AverageResource,
			NonMetals: AverageResource,
		},
	}
}
