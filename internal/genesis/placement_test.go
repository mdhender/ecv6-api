// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdhender/ecv6-api/internal/genesis"
	"github.com/mdhender/ecv6-api/internal/prng"
)

// update regenerates testdata/placement_golden.json from the current code. Run
// once when intentionally establishing the reproducibility contract:
//
//	go test ./internal/genesis/ -update
//
// then eyeball the diff and commit. A failure without -update means the radius
// formula, the shuffle addressing, or the placement algorithm changed, which
// silently rewrites every live map.
var update = flag.Bool("update", false, "regenerate testdata/placement_golden.json")

const goldenPath = "testdata/placement_golden.json"

// TestRadiusCheckpoints pins the nine golden radii from the supplement: the five
// baseline tiers at N = 100 and the four worked examples. These are the frozen
// reproducibility contract for R(N, D).
func TestRadiusCheckpoints(t *testing.T) {
	cases := []struct {
		n    int
		d    genesis.Density
		want int
	}{
		// Baseline tiers at N = 100.
		{100, genesis.ExtremelyDense, 37},
		{100, genesis.Dense, 40},
		{100, genesis.Average, 43},
		{100, genesis.Sparse, 47},
		{100, genesis.VerySparse, 51},
		// Worked examples.
		{100, genesis.Average, 43},
		{10, genesis.ExtremelyDense, 11},
		{1000, genesis.VerySparse, 162},
		{1000, genesis.ExtremelyDense, 118},
	}
	for _, c := range cases {
		if got := genesis.Radius(c.n, c.d); got != c.want {
			t.Errorf("Radius(%d, %q) = %d, want %d", c.n, c.d, got, c.want)
		}
	}
}

// TestRadiusHexCountExactAtBaseline confirms the baseline tiers land on an exact
// hex count (T is exactly a hex count), which is why the radii are clean.
func TestRadiusHexCountExactAtBaseline(t *testing.T) {
	want := map[genesis.Density]int{
		genesis.ExtremelyDense: 4219,
		genesis.Dense:          4921,
		genesis.Average:        5677,
		genesis.Sparse:         6769,
		genesis.VerySparse:     7957,
	}
	for d, hexes := range want {
		r := genesis.Radius(100, d)
		if got := 1 + 3*r*(r+1); got != hexes {
			t.Errorf("density %q: R=%d hex count = %d, want %d", d, r, got, hexes)
		}
	}
}

func TestSettingsValidation(t *testing.T) {
	ok := genesis.DefaultPlacementSettings()
	if err := ok.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	bad := []genesis.PlacementSettings{
		{N: 9, Density: genesis.Average, Spacing: 2},    // N too low
		{N: 1001, Density: genesis.Average, Spacing: 2}, // N too high
		{N: 100, Density: "medium", Spacing: 2},         // unknown density
		{N: 100, Density: genesis.Average, Spacing: 0},  // S too low
	}
	for _, s := range bad {
		if err := s.Validate(); !errors.Is(err, genesis.ErrInvalidSettings) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalidSettings", s, err)
		}
		if _, err := genesis.Place(prng.New(1, 2), s); !errors.Is(err, genesis.ErrInvalidSettings) {
			t.Errorf("Place(%+v) = %v, want ErrInvalidSettings", s, err)
		}
	}
}

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b genesis.Hex
		want int
	}{
		{genesis.Hex{0, 0}, genesis.Hex{0, 0}, 0},
		{genesis.Hex{0, 0}, genesis.Hex{0, -1}, 1}, // "up"
		{genesis.Hex{0, 0}, genesis.Hex{3, -7}, 7},
		{genesis.Hex{2, 1}, genesis.Hex{-1, 1}, 3},
	}
	for _, c := range cases {
		if got := genesis.Distance(c.a, c.b); got != c.want {
			t.Errorf("Distance(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestHexesWithinCount confirms the candidate list is exactly the map's hexes.
func TestHexesWithinCount(t *testing.T) {
	for _, r := range []int{0, 1, 5, 43} {
		got := len(genesis.HexesWithin(r))
		if want := 1 + 3*r*(r+1); got != want {
			t.Errorf("HexesWithin(%d) count = %d, want %d", r, got, want)
		}
	}
}

// TestPlacementProperties exercises the invariants the supplement guarantees on
// a successful placement, across several settings.
func TestPlacementProperties(t *testing.T) {
	seeds := prng.New(0xC0FFEE, 0xBEEF)
	cases := []genesis.PlacementSettings{
		{N: 100, Density: genesis.Average, Spacing: 2},
		{N: 50, Density: genesis.Dense, Spacing: 1},
		{N: 200, Density: genesis.VerySparse, Spacing: 3},
		{N: 10, Density: genesis.ExtremelyDense, Spacing: 2},
	}
	for _, s := range cases {
		res, err := genesis.Place(seeds, s)
		if err != nil {
			t.Errorf("Place(%+v) unexpected error: %v", s, err)
			continue
		}
		if len(res.Systems) != s.N {
			t.Errorf("Place(%+v) placed %d systems, want %d", s, len(res.Systems), s.N)
		}
		if res.Radius != genesis.Radius(s.N, s.Density) {
			t.Errorf("Place(%+v) radius = %d, want %d", s, res.Radius, genesis.Radius(s.N, s.Density))
		}
		// Every placed hex is within the radius.
		for _, h := range res.Systems {
			if d := genesis.Distance(genesis.Hex{0, 0}, h); d > res.Radius {
				t.Errorf("system %v at distance %d exceeds radius %d", h, d, res.Radius)
			}
		}
		// No pair is closer than S; no duplicates.
		seen := map[genesis.Hex]bool{}
		for i, a := range res.Systems {
			if seen[a] {
				t.Errorf("duplicate system %v", a)
			}
			seen[a] = true
			for j := i + 1; j < len(res.Systems); j++ {
				if d := genesis.Distance(a, res.Systems[j]); d < s.Spacing {
					t.Errorf("systems %v and %v are %d apart, below S=%d", a, res.Systems[j], d, s.Spacing)
				}
			}
		}
	}
}

// TestPlacementInfeasible pins the supplement's failure example: N=100,
// extremely dense (R=37), S=40 — no cluster is created.
func TestPlacementInfeasible(t *testing.T) {
	s := genesis.PlacementSettings{N: 100, Density: genesis.ExtremelyDense, Spacing: 40}
	res, err := genesis.Place(prng.New(1, 2), s)
	if !errors.Is(err, genesis.ErrInfeasible) {
		t.Fatalf("Place(%+v) error = %v, want ErrInfeasible", s, err)
	}
	if res.Systems != nil || res.Radius != 0 {
		t.Errorf("infeasible placement returned a result: %+v", res)
	}
}

// TestPlacementDeterministic confirms same seeds + settings reproduce the same
// systems in the same order, and different seeds generally differ.
func TestPlacementDeterministic(t *testing.T) {
	s := genesis.PlacementSettings{N: 80, Density: genesis.Average, Spacing: 2}

	a, err := genesis.Place(prng.New(11, 22), s)
	if err != nil {
		t.Fatalf("Place a: %v", err)
	}
	b, err := genesis.Place(prng.New(11, 22), s)
	if err != nil {
		t.Fatalf("Place b: %v", err)
	}
	if !sameSystems(a.Systems, b.Systems) {
		t.Errorf("same seeds produced different placements")
	}

	c, err := genesis.Place(prng.New(33, 44), s)
	if err != nil {
		t.Fatalf("Place c: %v", err)
	}
	if sameSystems(a.Systems, c.Systems) {
		t.Errorf("different seeds produced identical placements (suspicious)")
	}
}

// placementGolden is the on-disk shape of the frozen placement fixture.
type placementGolden struct {
	Seed1    uint64         `json:"seed1"`
	Seed2    uint64         `json:"seed2"`
	Settings goldenSettings `json:"settings"`
	Radius   int            `json:"radius"`
	Systems  []goldenHex    `json:"systems"`
}

type goldenSettings struct {
	N       int    `json:"n"`
	Density string `json:"density"`
	Spacing int    `json:"spacing"`
}

type goldenHex struct {
	Q int `json:"q"`
	R int `json:"r"`
}

// TestGoldenPlacement pins the full placed-system vector for a fixed seed and
// small settings — the reproducibility contract for the placement algorithm end
// to end (radius, shuffle addressing, and draw order).
func TestGoldenPlacement(t *testing.T) {
	const s1, s2 uint64 = 0x0123456789abcdef, 0xfedcba9876543210
	settings := genesis.PlacementSettings{N: 20, Density: genesis.Average, Spacing: 2}

	res, err := genesis.Place(prng.New(s1, s2), settings)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}

	if *update {
		g := placementGolden{
			Seed1: s1, Seed2: s2,
			Settings: goldenSettings{N: settings.N, Density: string(settings.Density), Spacing: settings.Spacing},
			Radius:   res.Radius,
			Systems:  toGoldenHexes(res.Systems),
		}
		writeGolden(t, g)
		t.Log("wrote", goldenPath)
	}

	want := readGolden(t)
	if want.Radius != res.Radius {
		t.Errorf("radius = %d, want %d (frozen surface changed?)", res.Radius, want.Radius)
	}
	got := toGoldenHexes(res.Systems)
	if len(got) != len(want.Systems) {
		t.Fatalf("placed %d systems, golden has %d", len(got), len(want.Systems))
	}
	for i := range got {
		if got[i] != want.Systems[i] {
			t.Errorf("system %d = %+v, want %+v (frozen surface changed?)", i, got[i], want.Systems[i])
		}
	}
}

func toGoldenHexes(hs []genesis.Hex) []goldenHex {
	out := make([]goldenHex, len(hs))
	for i, h := range hs {
		out[i] = goldenHex{Q: h.Q, R: h.R}
	}
	return out
}

func sameSystems(a, b []genesis.Hex) bool {
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

func readGolden(t *testing.T) placementGolden {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	var g placementGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func writeGolden(t *testing.T, g placementGolden) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}
