// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis

import (
	"sort"

	"github.com/mdhender/ecv6-api/internal/prng"
)

// The Genesis System Contents generator's provenance identity. Per ADR-0017
// these are RECORDED PROVENANCE only — they no longer enter the seed path. The
// system-contents seed root is Derive(TagSystem) alone; below it each system is
// addressed by its (q, r). Kept as the integer generator/version handles the
// store's game_generator row records (pending the UUID reconciliation in the E1
// §3 generator rows).
const (
	SysContentsGeneratorID prng.Key = 1
	SysContentsVersion     prng.Key = 1
)

// Habitability is clamped to this inclusive range in the base step (Genesis
// System Contents, "Planet type and base habitability").
const (
	minHabitability = 0
	maxHabitability = 25
)

// minTotalHabitability is the floor the large-system top-up drives total
// habitability to (Genesis System Contents, "Minimum habitability for larger
// systems"). Systems with at least this many planets receive the top-up.
const (
	minTotalHabitability = 9
	largeSystemPlanets   = 5
)

// PlanetType is one of the three planet types every generator fills orbits with
// (cluster core reference).
type PlanetType string

// The three planet types. The string values are the wire/store form.
const (
	Rocky        PlanetType = "rocky"
	AsteroidBelt PlanetType = "asteroid belt"
	GasGiant     PlanetType = "gas giant"
)

// Planet is the contents of one occupied orbit: its orbit number (1..10), type,
// and habitability. Empty orbits have no Planet. Habitability is in
// [0, 25] after the base step; the large-system top-up may raise it further but
// stays well within range at the totals it operates on. See the supplement.
type Planet struct {
	Orbit        int
	Type         PlanetType
	Habitability int
}

// SystemContents is one ordinary system's generated contents: its hex and the
// planets occupying its orbits, in ascending orbit order. Unoccupied orbits
// carry no Planet.
type SystemContents struct {
	Hex     Hex
	Planets []Planet
}

// ContentsResult is the output of the Genesis System Contents stage: every
// ordinary system's contents, in the same order as the placed systems handed in.
// This is the compatibility surface the deposit stage consumes (ADR-0016). There
// is no home-system template: home systems are generated on demand at founding
// (ADR-0017), not produced here.
type ContentsResult struct {
	Systems []SystemContents
}

// orbitSpec is one row of the per-orbit planet/habitability table: the planet
// type an orbit holds and the dice expression n*d(sides) + offset for its base
// habitability. Asteroid belts have n == 0 and roll nothing.
type orbitSpec struct {
	typ    PlanetType
	n      int
	sides  int
	offset int
}

// orbitTable is the per-orbit planet type and base-habitability dice, indexed by
// orbit number 1..10 (index 0 unused). It is the code image of the supplement's
// "Planet type and base habitability" table; the dice, orbits, and clamp live
// upstream and are the source of truth. Genesis System Contents:
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/system-contents.md
var orbitTable = [11]orbitSpec{
	1:  {Rocky, 3, 2, -3}, // 3d2 - 3
	2:  {Rocky, 3, 3, -3}, // 3d3 - 3
	3:  {Rocky, 5, 6, -5}, // 5d6 - 5
	4:  {AsteroidBelt, 0, 0, 0},
	5:  {Rocky, 5, 4, -5},    // 5d4 - 5
	6:  {GasGiant, 2, 8, -2}, // 2d8 - 2
	7:  {GasGiant, 2, 8, -4}, // 2d8 - 4
	8:  {GasGiant, 2, 8, -8}, // 2d8 - 8
	9:  {Rocky, 2, 8, -12},   // 2d8 - 12
	10: {AsteroidBelt, 0, 0, 0},
}

// GenerateContents runs the Genesis System Contents stage against a game's seeds
// and the placed systems from placement (PlacementResult.Systems), returning
// every ordinary system's contents. It is a pure function of (seeds, placed):
// each system's contents depend only on the game seeds and the system's own
// (q, r), never on another system or on the order they are processed. It does not
// persist anything.
//
// Each system draws from one stream addressed by its (q, r) below the stage seed
// root Derive(TagSystem) — generator id/version are provenance, not entropy
// (ADR-0017) — in the documented draw order: planet count, then the orbit shuffle
// (only for 4+ planets), then habitability per occupied orbit in ascending orbit
// order. See the supplement.
func GenerateContents(seeds prng.Seeds, placed []Hex) ContentsResult {
	root := seeds.Derive(prng.TagSystem)
	systems := make([]SystemContents, len(placed))
	for i, hex := range placed {
		systems[i] = generateSystem(root, hex)
	}
	return ContentsResult{Systems: systems}
}

// generateSystem generates one ordinary system's contents from the stage seed
// root and the system's hex. It builds a fresh Roller for the (q, r) address and
// draws from it in the frozen order: planet count, orbit shuffle (4+ only), then
// per-orbit habitability ascending. All randomness for a system comes from this
// one Roller, so the result is a pure function of (root, hex).
func generateSystem(root prng.Seeds, hex Hex) SystemContents {
	roller := root.Roller(prng.Key(hex.Q), prng.Key(hex.R))

	// Number of planets: 3d4 - 2, giving 1..10.
	count := roller.RollN(3, 4) - 2

	occupied := occupiedOrbits(roller, count)

	planets := make([]Planet, 0, count)
	for _, orbit := range occupied {
		spec := orbitTable[orbit]
		hab := 0
		if spec.typ != AsteroidBelt {
			hab = clamp(roller.RollN(spec.n, spec.sides)+spec.offset, minHabitability, maxHabitability)
		}
		planets = append(planets, Planet{Orbit: orbit, Type: spec.typ, Habitability: hab})
	}

	topUpHabitability(planets)
	return SystemContents{Hex: hex, Planets: planets}
}

// occupiedOrbits returns the orbits a system's planets occupy, in ascending
// order (Genesis System Contents, "Occupied orbits"). One-, two-, and
// three-planet systems use fixed orbits and draw nothing. For four or more
// planets it shuffles the ten orbit numbers off roller and takes the first
// count, so every orbit appears at most once, then sorts the selection ascending
// for processing. The shuffle is the only draw here and happens before any
// habitability roll.
func occupiedOrbits(roller *prng.Roller, count int) []int {
	switch count {
	case 1:
		return []int{4}
	case 2:
		return []int{4, 6}
	case 3:
		return []int{3, 4, 8}
	}
	orbits := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	roller.Shuffle(len(orbits), func(i, j int) {
		orbits[i], orbits[j] = orbits[j], orbits[i]
	})
	selected := append([]int(nil), orbits[:count]...)
	sort.Ints(selected)
	return selected
}

// topUpHabitability applies the minimum-habitability top-up for systems with at
// least five planets (Genesis System Contents, "Minimum habitability for larger
// systems"). It draws no dice — it is deterministic. Systems with fewer than five
// planets are left unchanged.
//
// It walks orbits in the sequence 3, 4, 5, 6, 7, 8, 9, 10, 2, 3, ... (orbit 1 is
// intentionally never touched), adding to any rocky planet or gas giant it lands
// on — 2 in orbit 3 or 6, otherwise 1 — until the system's total habitability
// reaches minTotalHabitability. Empty orbits and asteroid belts get no increase.
// The top-up is not re-clamped by the base 0..25 rule; at these totals it cannot
// exceed 25 in practice. The loop always terminates because a five-planet system
// cannot be all asteroid belts (only orbits 4 and 10 are belts).
func topUpHabitability(planets []Planet) {
	if len(planets) < largeSystemPlanets {
		return
	}

	byOrbit := make(map[int]*Planet, len(planets))
	total := 0
	for i := range planets {
		byOrbit[planets[i].Orbit] = &planets[i]
		total += planets[i].Habitability
	}

	for orbit := 3; total < minTotalHabitability; {
		if p, ok := byOrbit[orbit]; ok && (p.Type == Rocky || p.Type == GasGiant) {
			inc := 1
			if orbit == 3 || orbit == 6 {
				inc = 2
			}
			p.Habitability += inc
			total += inc
		}
		// Advance 3->4->...->10, then wrap to 2 (skipping orbit 1 forever).
		orbit++
		if orbit > 10 {
			orbit = 2
		}
	}
}

// clamp returns v confined to [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
