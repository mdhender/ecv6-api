// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis

import (
	"fmt"
	"math"

	"github.com/mdhender/ecv6-api/internal/cerrs"
	"github.com/mdhender/ecv6-api/internal/prng"
)

// The Genesis Placement generator's provenance identity. Per ADR-0017 these are
// RECORDED PROVENANCE only — they no longer enter the seed path. The placement
// seed root is Derive(TagCluster) alone; the generator's selection and Knobs are
// the recorded inputs that make a run reproducible, not an id mixed into the
// seed. Kept as the integer generator/version handles the store's game_generator
// row records (pending the UUID reconciliation in the E1 §3 generator rows).
const (
	PlacementGeneratorID prng.Key = 1
	PlacementVersion     prng.Key = 1
)

// placementShuffleStream is the generator-owned sub-address, off the placement
// seed root, that supplies the hex shuffle. It is private to this version's
// addressing (frozen only per-version, ADR-0016), not a global surface.
const placementShuffleStream prng.Key = 1

// Errors returned by placement. They are recoverable, data-driven conditions —
// bad settings or an infeasible request — never panics.
const (
	// ErrInvalidSettings is returned when a PlacementSettings field is out of the
	// range the supplement allows.
	ErrInvalidSettings = cerrs.Error("invalid placement settings")
	// ErrInfeasible is returned when N systems cannot be placed within the radius
	// at the chosen spacing: the hex list is exhausted before N are placed. No
	// cluster is created. This is expected, GM-resolved behavior (change the seed,
	// lower S or N, or pick a sparser density) — not a bug.
	ErrInfeasible = cerrs.Error("placement infeasible: N systems do not fit at this density and spacing")
)

// Density is a stellar-density tier: how large an area the systems spread
// across, a radius knob at a fixed N. The tiers are monotonic from densest
// (extremely dense) to loosest (very sparse).
type Density string

// The five stellar-density tiers, densest to sparsest.
const (
	ExtremelyDense Density = "extremely dense"
	Dense          Density = "dense"
	Average        Density = "average"
	Sparse         Density = "sparse"
	VerySparse     Density = "very sparse"
)

// baselineHexes maps each tier to its baseline hex count H_D at N = 100 — the
// target map size a tier is calibrated to. These are frozen: changing one would
// change every existing map (see the supplement's "stability").
var baselineHexes = map[Density]int{
	ExtremelyDense: 4219,
	Dense:          4921,
	Average:        5677,
	Sparse:         6769,
	VerySparse:     7957,
}

// Placement bounds, from the supplement's "Settings".
const (
	MinSystems     = 10   // N floor
	MaxSystems     = 1000 // N ceiling
	DefaultSystems = 100  // N default
	DefaultDensity = Average
	MinSpacing     = 1 // S floor; no maximum
	DefaultSpacing = 2 // S default
)

// PlacementSettings are the GM's inputs to the placement stage: N systems, the
// stellar density D, and the minimum spacing S. Validate before use, or Place
// will. See the supplement's "Settings".
type PlacementSettings struct {
	N       int     // number of systems, MinSystems..MaxSystems
	Density Density // stellar density tier
	Spacing int     // minimum system spacing S, >= MinSpacing; not derived from density
}

// DefaultPlacementSettings returns the supplement's defaults: N = 100,
// average density, S = 2.
func DefaultPlacementSettings() PlacementSettings {
	return PlacementSettings{N: DefaultSystems, Density: DefaultDensity, Spacing: DefaultSpacing}
}

// Validate reports whether the settings lie in the allowed ranges, wrapping
// ErrInvalidSettings on the first violation. It never panics.
func (s PlacementSettings) Validate() error {
	if s.N < MinSystems || s.N > MaxSystems {
		return fmt.Errorf("%w: N = %d, want %d..%d", ErrInvalidSettings, s.N, MinSystems, MaxSystems)
	}
	if _, ok := baselineHexes[s.Density]; !ok {
		return fmt.Errorf("%w: density = %q", ErrInvalidSettings, s.Density)
	}
	if s.Spacing < MinSpacing {
		return fmt.Errorf("%w: S = %d, want >= %d", ErrInvalidSettings, s.Spacing, MinSpacing)
	}
	return nil
}

// Hex is an axial coordinate on the cluster grid. The origin is (0, 0). See the
// cluster core reference.
type Hex struct {
	Q int
	R int
}

// Distance returns the axial hex distance between two hexes, the metric every
// rule measures with (cluster.md "measuring distance"):
//
//	dist = (|dq| + |dr| + |dq + dr|) / 2
func Distance(a, b Hex) int {
	dq := a.Q - b.Q
	dr := a.R - b.R
	return (abs(dq) + abs(dr) + abs(dq+dr)) / 2
}

// hexCount is the number of hexes on a radius-R map: 1 + 3R(R+1).
func hexCount(r int) int { return 1 + 3*r*(r+1) }

// Radius returns the cluster radius R for N systems at density d — a pure
// function of N and d, with no randomness (supplement "the cluster radius" /
// "scaling with the number of systems"). It holds the tier's hexes-per-system
// fixed: the target hex count is T = round(H_D * N / 100), and R is the integer
// whose hex count 1 + 3R(R+1) is nearest T, breaking ties toward the smaller R.
//
// d must be a known density; an unknown density yields 0. Callers validate
// settings first, so this is not a normal path.
func Radius(n int, d Density) int {
	h, ok := baselineHexes[d]
	if !ok {
		return 0
	}
	t := int(math.Round(float64(h) * float64(n) / 100.0))

	// Closed form gives R directly; search a small window around it and pick the
	// nearest hex count, ties toward the smaller R. The window makes the result
	// robust to float rounding at the boundary between two radii.
	approx := int(math.Round((math.Sqrt((4*float64(t)-1)/3) - 1) / 2))
	lo := max(approx-3, 0)
	hi := approx + 3

	best := -1
	bestDiff := 0
	for r := lo; r <= hi; r++ {
		diff := abs(hexCount(r) - t)
		// Ascending r with a strict-less-than test keeps the smaller R on a tie.
		if best == -1 || diff < bestDiff {
			best, bestDiff = r, diff
		}
	}
	return best
}

// HexesWithin returns every hex whose distance from the origin is <= R, in a
// deterministic order (q outer, r inner). This order is not the placement order
// — the shuffle imposes that — but it is fixed so the shuffle input never
// depends on Go-map iteration.
func HexesWithin(r int) []Hex {
	out := make([]Hex, 0, hexCount(r))
	origin := Hex{0, 0}
	for q := -r; q <= r; q++ {
		for rr := -r; rr <= r; rr++ {
			h := Hex{q, rr}
			if Distance(origin, h) <= r {
				out = append(out, h)
			}
		}
	}
	return out
}

// PlacementResult is the output of the placement stage: the derived radius, the
// settings used, and the placed systems in the order they were placed.
type PlacementResult struct {
	Radius   int
	Settings PlacementSettings
	Systems  []Hex
}

// Place runs the Genesis Placement stage against a game's seeds and settings,
// returning the placed systems (supplement "placing the systems"). It is a pure
// function of (seeds, settings): same inputs always give the same result,
// independent of Go-map iteration order — it never iterates a map to draw. It
// does not persist anything.
//
// It derives R from N and density, builds every hex within R, shuffles that list
// off the placement seed root, then draws hexes in shuffled order, keeping a hex
// only if it is at least S from every already-placed system. It stops at N
// placed. If the list is exhausted first it returns ErrInfeasible and no result
// — no relaxing S, no growing R, no partial map. Invalid settings return
// ErrInvalidSettings.
func Place(seeds prng.Seeds, s PlacementSettings) (PlacementResult, error) {
	if err := s.Validate(); err != nil {
		return PlacementResult{}, err
	}

	r := Radius(s.N, s.Density)
	candidates := HexesWithin(r)

	// One stream off the placement seed root supplies the shuffle. Deriving the
	// root here — not at package scope — keeps the address explicit at the draw.
	// The root is Derive(TagCluster) alone: generator id/version are provenance,
	// not entropy (ADR-0017).
	root := seeds.Derive(prng.TagCluster)
	roller := root.Roller(placementShuffleStream)
	roller.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	placed := make([]Hex, 0, s.N)
	for _, h := range candidates {
		if farEnough(h, placed, s.Spacing) {
			placed = append(placed, h)
			if len(placed) == s.N {
				return PlacementResult{Radius: r, Settings: s, Systems: placed}, nil
			}
		}
	}

	return PlacementResult{}, fmt.Errorf("%w: placed %d of %d (N=%d, density=%q, S=%d, R=%d)",
		ErrInfeasible, len(placed), s.N, s.N, s.Density, s.Spacing, r)
}

// farEnough reports whether h is at least spacing hexes from every system in
// placed.
func farEnough(h Hex, placed []Hex, spacing int) bool {
	for _, p := range placed {
		if Distance(h, p) < spacing {
			return false
		}
	}
	return true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
