// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
)

// The -update flag is defined in placement_test.go (same package). It also
// regenerates the system-contents golden fixture below.

const contentsGoldenPath = "testdata/system_contents_golden.json"

// contentsGolden is the on-disk shape of the frozen system-contents fixture.
type contentsGolden struct {
	Seed1   uint64           `json:"seed1"`
	Seed2   uint64           `json:"seed2"`
	Systems []goldenContents `json:"systems"`
}

type goldenContents struct {
	Q       int            `json:"q"`
	R       int            `json:"r"`
	Planets []goldenPlanet `json:"planets"`
}

type goldenPlanet struct {
	Orbit        int    `json:"orbit"`
	Type         string `json:"type"`
	Habitability int    `json:"habitability"`
}

// goldenHexes are the fixed (q, r) addresses pinned in the system-contents
// golden. They cover a range of coordinates, including negatives and the origin.
var goldenHexes = []genesis.Hex{
	{Q: 0, R: 0},
	{Q: 3, R: -3},
	{Q: -5, R: 2},
	{Q: 10, R: -7},
	{Q: -8, R: 15},
	{Q: 7, R: 1},
}

// TestGoldenSystemContents pins each fixed (q, r)'s full contents — planet count,
// occupied orbits, types, and habitability — for a fixed seed. This is the
// end-to-end reproducibility contract for the system-contents stage (seed root,
// per-(q, r) addressing, draw order, tables, clamp, and top-up).
func TestGoldenSystemContents(t *testing.T) {
	const s1, s2 uint64 = 0x0123456789abcdef, 0xfedcba9876543210

	res := genesis.GenerateContents(prng.New(s1, s2), goldenHexes)

	if *update {
		g := contentsGolden{Seed1: s1, Seed2: s2, Systems: toGoldenContents(res.Systems)}
		writeContentsGolden(t, g)
		t.Log("wrote", contentsGoldenPath)
	}

	want := readContentsGolden(t)
	got := toGoldenContents(res.Systems)
	if len(got) != len(want.Systems) {
		t.Fatalf("generated %d systems, golden has %d", len(got), len(want.Systems))
	}
	for i := range got {
		if got[i].Q != want.Systems[i].Q || got[i].R != want.Systems[i].R {
			t.Errorf("system %d hex = (%d,%d), want (%d,%d)", i, got[i].Q, got[i].R, want.Systems[i].Q, want.Systems[i].R)
			continue
		}
		if len(got[i].Planets) != len(want.Systems[i].Planets) {
			t.Errorf("system (%d,%d) has %d planets, golden has %d (frozen surface changed?)",
				got[i].Q, got[i].R, len(got[i].Planets), len(want.Systems[i].Planets))
			continue
		}
		for j := range got[i].Planets {
			if got[i].Planets[j] != want.Systems[i].Planets[j] {
				t.Errorf("system (%d,%d) planet %d = %+v, want %+v (frozen surface changed?)",
					got[i].Q, got[i].R, j, got[i].Planets[j], want.Systems[i].Planets[j])
			}
		}
	}
}

// TestContentsProperties exercises the invariants the supplement guarantees,
// across many systems and several seeds.
func TestContentsProperties(t *testing.T) {
	seedPairs := [][2]uint64{{1, 2}, {0xC0FFEE, 0xBEEF}, {42, 99}, {0xDEAD, 0xBEA7}}
	// A spread of hexes to hit many planet counts.
	var hexes []genesis.Hex
	for q := -6; q <= 6; q++ {
		for r := -6; r <= 6; r++ {
			hexes = append(hexes, genesis.Hex{Q: q, R: r})
		}
	}

	for _, sp := range seedPairs {
		res := genesis.GenerateContents(prng.New(sp[0], sp[1]), hexes)
		for _, sys := range res.Systems {
			checkSystem(t, sys)
		}
	}
}

// checkSystem asserts every per-system invariant from the supplement.
func checkSystem(t *testing.T, sys genesis.SystemContents) {
	t.Helper()
	n := len(sys.Planets)
	if n < 1 || n > 10 {
		t.Errorf("system (%d,%d) has %d planets, want 1..10", sys.Hex.Q, sys.Hex.R, n)
		return
	}

	// Fixed orbits for the smallest systems.
	switch n {
	case 1:
		assertOrbits(t, sys, []int{4})
	case 2:
		assertOrbits(t, sys, []int{4, 6})
	case 3:
		assertOrbits(t, sys, []int{3, 4, 8})
	}

	seen := map[int]bool{}
	ascending := true
	prev := 0
	for _, p := range sys.Planets {
		// No duplicate orbits.
		if seen[p.Orbit] {
			t.Errorf("system (%d,%d) has duplicate orbit %d", sys.Hex.Q, sys.Hex.R, p.Orbit)
		}
		seen[p.Orbit] = true
		if p.Orbit < prev {
			ascending = false
		}
		prev = p.Orbit

		// Type matches the orbit's schema.
		wantType := expectedType(p.Orbit)
		if p.Type != wantType {
			t.Errorf("system (%d,%d) orbit %d type = %q, want %q", sys.Hex.Q, sys.Hex.R, p.Orbit, p.Type, wantType)
		}
		// Habitability is in range after the base step (top-up cannot exceed 25 at
		// these totals, so the whole result stays 0..25).
		if p.Habitability < 0 || p.Habitability > 25 {
			t.Errorf("system (%d,%d) orbit %d habitability = %d, want 0..25", sys.Hex.Q, sys.Hex.R, p.Orbit, p.Habitability)
		}
		// Asteroid belts are always 0.
		if p.Type == genesis.AsteroidBelt && p.Habitability != 0 {
			t.Errorf("system (%d,%d) orbit %d asteroid belt habitability = %d, want 0", sys.Hex.Q, sys.Hex.R, p.Orbit, p.Habitability)
		}
	}
	if !ascending {
		t.Errorf("system (%d,%d) planets not in ascending orbit order: %+v", sys.Hex.Q, sys.Hex.R, sys.Planets)
	}

	// Large systems reach the minimum total habitability, and orbit 1 is never
	// increased by the top-up.
	if n >= 5 {
		total := 0
		for _, p := range sys.Planets {
			total += p.Habitability
		}
		if total < 9 {
			t.Errorf("system (%d,%d) with %d planets has total habitability %d, want >= 9", sys.Hex.Q, sys.Hex.R, n, total)
		}
	}
}

// TestOrbit1NeverToppedUp verifies the top-up never increases orbit 1. Because a
// base orbit-1 habitability is 3d2-3 clamped to 0..25 (so 0..3), and the top-up
// leaves it untouched, an orbit-1 planet must always stay within its base range.
// We assert directly by comparing against a base-only recomputation is awkward;
// instead we confirm orbit-1 habitability never exceeds its base maximum of 3.
func TestOrbit1NeverToppedUp(t *testing.T) {
	var hexes []genesis.Hex
	for q := -8; q <= 8; q++ {
		for r := -8; r <= 8; r++ {
			hexes = append(hexes, genesis.Hex{Q: q, R: r})
		}
	}
	res := genesis.GenerateContents(prng.New(0xABCDEF, 0x123456), hexes)
	found := false
	for _, sys := range res.Systems {
		for _, p := range sys.Planets {
			if p.Orbit == 1 {
				found = true
				// Base orbit 1 is 3d2 - 3, i.e. 0..3 after clamp. The top-up must
				// not have raised it above 3.
				if p.Habitability > 3 {
					t.Errorf("system (%d,%d) orbit 1 habitability = %d > 3: top-up touched orbit 1", sys.Hex.Q, sys.Hex.R, p.Habitability)
				}
			}
		}
	}
	if !found {
		t.Skip("no orbit-1 planet appeared in the sample; nothing to assert")
	}
}

// TestSmallSystemsNoTopUp confirms systems with fewer than five planets are
// unchanged by the top-up: their total habitability is whatever the base rolls
// produced, and may legitimately be below 9. We assert only that the base
// invariants hold (covered by checkSystem) and that a 1-planet asteroid-belt
// system stays at total 0 — the clearest witness that no top-up ran.
func TestSmallSystemsNoTopUp(t *testing.T) {
	var hexes []genesis.Hex
	for q := -10; q <= 10; q++ {
		for r := -10; r <= 10; r++ {
			hexes = append(hexes, genesis.Hex{Q: q, R: r})
		}
	}
	res := genesis.GenerateContents(prng.New(7, 7), hexes)
	sawZeroTotalSmall := false
	for _, sys := range res.Systems {
		if len(sys.Planets) >= 5 {
			continue
		}
		total := 0
		for _, p := range sys.Planets {
			total += p.Habitability
		}
		// A 1-planet system is always the orbit-4 asteroid belt -> total 0.
		if len(sys.Planets) == 1 {
			if sys.Planets[0].Orbit != 4 || sys.Planets[0].Type != genesis.AsteroidBelt || total != 0 {
				t.Errorf("1-planet system (%d,%d) = %+v, want single orbit-4 asteroid belt at 0", sys.Hex.Q, sys.Hex.R, sys.Planets)
			}
			sawZeroTotalSmall = true
		}
	}
	if !sawZeroTotalSmall {
		t.Skip("no 1-planet system in the sample")
	}
}

// TestContentsDeterministic confirms the same seeds and the same (q, r) reproduce
// identical contents, and that generating one system does not depend on another:
// generating a hex alone matches generating it within a batch, regardless of the
// batch's order.
func TestContentsDeterministic(t *testing.T) {
	const s1, s2 uint64 = 0xFEED, 0xFACE
	hexes := []genesis.Hex{{Q: 2, R: -3}, {Q: -7, R: 4}, {Q: 0, R: 0}, {Q: 5, R: 5}}

	a := genesis.GenerateContents(prng.New(s1, s2), hexes)
	b := genesis.GenerateContents(prng.New(s1, s2), hexes)
	if !sameContents(a.Systems, b.Systems) {
		t.Errorf("same seeds produced different contents")
	}

	// Order independence: reversing the input must give the same per-hex contents.
	reversed := make([]genesis.Hex, len(hexes))
	for i, h := range hexes {
		reversed[len(hexes)-1-i] = h
	}
	c := genesis.GenerateContents(prng.New(s1, s2), reversed)
	byHex := map[genesis.Hex][]genesis.Planet{}
	for _, sys := range c.Systems {
		byHex[sys.Hex] = sys.Planets
	}
	for _, sys := range a.Systems {
		if !samePlanets(byHex[sys.Hex], sys.Planets) {
			t.Errorf("hex %+v contents depend on processing order", sys.Hex)
		}
	}

	// Isolation: one hex generated alone matches its batch result.
	solo := genesis.GenerateContents(prng.New(s1, s2), []genesis.Hex{{Q: 5, R: 5}})
	if !samePlanets(solo.Systems[0].Planets, byHex[genesis.Hex{Q: 5, R: 5}]) {
		t.Errorf("solo generation of (5,5) differs from its batch result")
	}
}

// expectedType returns the planet type an orbit must hold, per the supplement's
// table. It mirrors the generator's table so the property test is independent of
// the generator's internal representation.
func expectedType(orbit int) genesis.PlanetType {
	switch orbit {
	case 4, 10:
		return genesis.AsteroidBelt
	case 6, 7, 8:
		return genesis.GasGiant
	default:
		return genesis.Rocky
	}
}

func assertOrbits(t *testing.T, sys genesis.SystemContents, want []int) {
	t.Helper()
	if len(sys.Planets) != len(want) {
		t.Errorf("system (%d,%d) has %d planets, want %d", sys.Hex.Q, sys.Hex.R, len(sys.Planets), len(want))
		return
	}
	for i, orbit := range want {
		if sys.Planets[i].Orbit != orbit {
			t.Errorf("system (%d,%d) planet %d orbit = %d, want %d", sys.Hex.Q, sys.Hex.R, i, sys.Planets[i].Orbit, orbit)
		}
	}
}

func sameContents(a, b []genesis.SystemContents) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Hex != b[i].Hex || !samePlanets(a[i].Planets, b[i].Planets) {
			return false
		}
	}
	return true
}

func samePlanets(a, b []genesis.Planet) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toGoldenContents(systems []genesis.SystemContents) []goldenContents {
	out := make([]goldenContents, len(systems))
	for i, sys := range systems {
		planets := make([]goldenPlanet, len(sys.Planets))
		for j, p := range sys.Planets {
			planets[j] = goldenPlanet{Orbit: p.Orbit, Type: string(p.Type), Habitability: p.Habitability}
		}
		out[i] = goldenContents{Q: sys.Hex.Q, R: sys.Hex.R, Planets: planets}
	}
	return out
}

func readContentsGolden(t *testing.T) contentsGolden {
	t.Helper()
	data, err := os.ReadFile(contentsGoldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	var g contentsGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func writeContentsGolden(t *testing.T, g contentsGolden) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(contentsGoldenPath), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	if err := os.WriteFile(contentsGoldenPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}
