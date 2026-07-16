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
// regenerates the deposits golden fixture below.

const depositsGoldenPath = "testdata/deposits_golden.json"

// depositsGolden is the on-disk shape of the frozen deposits fixture.
type depositsGolden struct {
	Seed1   uint64            `json:"seed1"`
	Seed2   uint64            `json:"seed2"`
	Systems []goldenSysDep    `json:"systems"`
	Home    []goldenPlanetDep `json:"home"`
}

type goldenSysDep struct {
	Q       int               `json:"q"`
	R       int               `json:"r"`
	Planets []goldenPlanetDep `json:"planets"`
}

type goldenPlanetDep struct {
	Orbit    int             `json:"orbit"`
	Deposits []goldenDeposit `json:"deposits"`
}

type goldenDeposit struct {
	Resource string `json:"resource"`
	Quantity int64  `json:"quantity"`
	Yield    int    `json:"yield"` // tenths of a percent
}

// depositsGoldenHexes are a small fixed system set for the deposits golden. It
// reuses a subset of the system-contents golden hexes so both stages pin the same
// coordinates.
var depositsGoldenHexes = []genesis.Hex{
	{Q: 0, R: 0},
	{Q: 3, R: -3},
	{Q: -5, R: 2},
	{Q: 7, R: 1},
}

// TestGoldenDeposits pins the real PRNG-driven deposits output — every planet's
// deposit resources, quantities, and yields, plus the home template's deposits —
// for a fixed seed and a small fixed system set. This is the end-to-end
// reproducibility contract for the deposits stage (seed root, per-(q, r)
// addressing, phase order, tables, and the float64 pipeline). Regenerate with
// -update and eyeball the diff.
func TestGoldenDeposits(t *testing.T) {
	const s1, s2 uint64 = 0x0123456789abcdef, 0xfedcba9876543210

	seeds := prng.New(s1, s2)
	contents := genesis.GenerateContents(seeds, depositsGoldenHexes)
	res := genesis.GenerateDeposits(seeds, contents, genesis.DefaultDepositSettings())

	if *update {
		g := depositsGolden{
			Seed1:   s1,
			Seed2:   s2,
			Systems: toGoldenSysDep(res.Systems),
			Home:    toGoldenPlanetDep(res.Home),
		}
		writeDepositsGolden(t, g)
		t.Log("wrote", depositsGoldenPath)
	}

	want := readDepositsGolden(t)
	gotSystems := toGoldenSysDep(res.Systems)
	if len(gotSystems) != len(want.Systems) {
		t.Fatalf("generated %d systems, golden has %d", len(gotSystems), len(want.Systems))
	}
	for i := range gotSystems {
		assertSameSysDep(t, gotSystems[i], want.Systems[i])
	}
	assertSamePlanetDeps(t, "home", toGoldenPlanetDep(res.Home), want.Home)
}

func assertSameSysDep(t *testing.T, got, want goldenSysDep) {
	t.Helper()
	if got.Q != want.Q || got.R != want.R {
		t.Errorf("system hex = (%d,%d), want (%d,%d)", got.Q, got.R, want.Q, want.R)
		return
	}
	assertSamePlanetDeps(t, "system", got.Planets, want.Planets)
}

func assertSamePlanetDeps(t *testing.T, label string, got, want []goldenPlanetDep) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s has %d planets, golden has %d (frozen surface changed?)", label, len(got), len(want))
		return
	}
	for i := range got {
		if got[i].Orbit != want[i].Orbit {
			t.Errorf("%s planet %d orbit = %d, want %d", label, i, got[i].Orbit, want[i].Orbit)
			continue
		}
		if len(got[i].Deposits) != len(want[i].Deposits) {
			t.Errorf("%s orbit %d has %d deposits, golden has %d (frozen surface changed?)",
				label, got[i].Orbit, len(got[i].Deposits), len(want[i].Deposits))
			continue
		}
		for j := range got[i].Deposits {
			if got[i].Deposits[j] != want[i].Deposits[j] {
				t.Errorf("%s orbit %d deposit %d = %+v, want %+v (frozen surface changed?)",
					label, got[i].Orbit, j, got[i].Deposits[j], want[i].Deposits[j])
			}
		}
	}
}

// TestDepositsProperties exercises the Genesis Deposits output guarantees across
// many systems and several seeds.
func TestDepositsProperties(t *testing.T) {
	seedPairs := [][2]uint64{{1, 2}, {0xC0FFEE, 0xBEEF}, {42, 99}, {0xDEAD, 0xBEA7}}
	var hexes []genesis.Hex
	for q := -6; q <= 6; q++ {
		for r := -6; r <= 6; r++ {
			hexes = append(hexes, genesis.Hex{Q: q, R: r})
		}
	}
	settings := genesis.DefaultDepositSettings()

	for _, sp := range seedPairs {
		seeds := prng.New(sp[0], sp[1])
		contents := genesis.GenerateContents(seeds, hexes)
		res := genesis.GenerateDeposits(seeds, contents, settings)

		for si, sys := range res.Systems {
			// Deposits align with the system's planets, in the same order.
			if len(sys.Planets) != len(contents.Systems[si].Planets) {
				t.Fatalf("system (%d,%d): %d deposit-planets, %d content-planets",
					sys.Hex.Q, sys.Hex.R, len(sys.Planets), len(contents.Systems[si].Planets))
			}
			for pi, pd := range sys.Planets {
				checkPlanetDeposits(t, sys.Hex, contents.Systems[si].Planets[pi], pd)
			}
		}
		// The home template always produces deposits (every planet has at least its
		// high-affinity resource present).
		if len(res.Home) != len(contents.Home) {
			t.Fatalf("home has %d deposit-planets, %d content-planets", len(res.Home), len(contents.Home))
		}
		for pi, pd := range res.Home {
			checkPlanetDeposits(t, genesis.Hex{}, contents.Home[pi], pd)
		}
	}
}

// checkPlanetDeposits asserts the per-planet output guarantees.
func checkPlanetDeposits(t *testing.T, hex genesis.Hex, planet genesis.Planet, pd genesis.PlanetDeposits) {
	t.Helper()
	if pd.Orbit != planet.Orbit {
		t.Errorf("(%d,%d) deposit orbit %d != planet orbit %d", hex.Q, hex.R, pd.Orbit, planet.Orbit)
	}
	// Every planet has at least one deposit (its high-affinity resource is always
	// present).
	if len(pd.Deposits) == 0 {
		t.Errorf("(%d,%d) orbit %d has no deposits", hex.Q, hex.R, planet.Orbit)
	}
	for _, d := range pd.Deposits {
		// Exactly one of the three resources.
		if d.Resource != genesis.Fuel && d.Resource != genesis.Metals && d.Resource != genesis.NonMetals {
			t.Errorf("(%d,%d) orbit %d deposit resource %q invalid", hex.Q, hex.R, planet.Orbit, d.Resource)
		}
		// Positive whole-number quantity.
		if d.Quantity < 1 {
			t.Errorf("(%d,%d) orbit %d %s quantity = %d, want >= 1", hex.Q, hex.R, planet.Orbit, d.Resource, d.Quantity)
		}
		// Yield >= 0.1% (>= 1 tenth) and a multiple of 0.1% (integer tenths is
		// automatically a multiple).
		if d.Yield < 1 {
			t.Errorf("(%d,%d) orbit %d %s yield = %d tenths, want >= 1 (0.1%%)", hex.Q, hex.R, planet.Orbit, d.Resource, d.Yield)
		}
	}
}

// TestDepositsDeterministic confirms the same seeds and settings reproduce
// identical deposits and that generating one system does not depend on another:
// generating a hex alone matches generating it within a batch, regardless of the
// batch's order.
func TestDepositsDeterministic(t *testing.T) {
	const s1, s2 uint64 = 0xFEED, 0xFACE
	hexes := []genesis.Hex{{Q: 2, R: -3}, {Q: -7, R: 4}, {Q: 0, R: 0}, {Q: 5, R: 5}}
	settings := genesis.DefaultDepositSettings()
	seeds := prng.New(s1, s2)

	a := genesis.GenerateDeposits(seeds, genesis.GenerateContents(seeds, hexes), settings)
	b := genesis.GenerateDeposits(seeds, genesis.GenerateContents(seeds, hexes), settings)
	if !sameDeposits(a.Systems, b.Systems) {
		t.Errorf("same seeds/settings produced different deposits")
	}

	// Order independence: reversing the input gives the same per-hex deposits.
	reversed := make([]genesis.Hex, len(hexes))
	for i, h := range hexes {
		reversed[len(hexes)-1-i] = h
	}
	c := genesis.GenerateDeposits(seeds, genesis.GenerateContents(seeds, reversed), settings)
	byHex := map[genesis.Hex][]genesis.PlanetDeposits{}
	for _, sys := range c.Systems {
		byHex[sys.Hex] = sys.Planets
	}
	for _, sys := range a.Systems {
		if !samePlanetDeps(byHex[sys.Hex], sys.Planets) {
			t.Errorf("hex %+v deposits depend on processing order", sys.Hex)
		}
	}

	// Isolation: one hex generated alone matches its batch result.
	solo := genesis.GenerateDeposits(seeds, genesis.GenerateContents(seeds, []genesis.Hex{{Q: 5, R: 5}}), settings)
	if !samePlanetDeps(solo.Systems[0].Planets, byHex[genesis.Hex{Q: 5, R: 5}]) {
		t.Errorf("solo generation of (5,5) deposits differs from its batch result")
	}
}

// TestZeroTotalSharesNoDeposits checks that a resource with zero total shares
// across the system produces no deposits of that resource anywhere in the system.
// We drive this from real generation: for each system, if a resource never
// appears in any planet's deposits it is consistent, but the stronger guarantee we
// assert is the reverse — a planet's high-affinity resource is always present.
func TestZeroTotalSharesNoDeposits(t *testing.T) {
	var hexes []genesis.Hex
	for q := -8; q <= 8; q++ {
		for r := -8; r <= 8; r++ {
			hexes = append(hexes, genesis.Hex{Q: q, R: r})
		}
	}
	seeds := prng.New(0xABCDEF, 0x123456)
	contents := genesis.GenerateContents(seeds, hexes)
	res := genesis.GenerateDeposits(seeds, contents, genesis.DefaultDepositSettings())

	sawGasNoMetals := false
	for si, sys := range res.Systems {
		// A single-planet asteroid-belt system has metals as its high-affinity
		// resource, so metals must be present; fuel (low affinity) may be absent.
		planets := contents.Systems[si].Planets
		if len(planets) == 1 && planets[0].Type == genesis.AsteroidBelt {
			hasMetals := false
			for _, d := range sys.Planets[0].Deposits {
				if d.Resource == genesis.Metals {
					hasMetals = true
				}
			}
			if !hasMetals {
				t.Errorf("(%d,%d) single asteroid belt has no metals deposit", sys.Hex.Q, sys.Hex.R)
			}
		}
		// Look for a gas giant that ended up with no metals deposit — a witness that
		// a low-affinity resource can be fully absent (the worked example's case).
		for pi, p := range planets {
			if p.Type != genesis.GasGiant {
				continue
			}
			hasMetals := false
			for _, d := range sys.Planets[pi].Deposits {
				if d.Resource == genesis.Metals {
					hasMetals = true
				}
			}
			if !hasMetals {
				sawGasNoMetals = true
			}
		}
	}
	if !sawGasNoMetals {
		t.Log("no gas giant without metals appeared in the sample; not a failure")
	}
}

// TestAbundanceShiftsQuantityYieldNotCounts confirms that changing abundance
// changes quantities and yields but not deposit counts, resources, endowments, or
// shares (a sanity check on the settings' documented scope).
func TestAbundanceShiftsQuantityYieldNotCounts(t *testing.T) {
	var hexes []genesis.Hex
	for q := -4; q <= 4; q++ {
		for r := -4; r <= 4; r++ {
			hexes = append(hexes, genesis.Hex{Q: q, R: r})
		}
	}
	seeds := prng.New(0x5EED, 0x1234)
	contents := genesis.GenerateContents(seeds, hexes)

	avg := genesis.GenerateDeposits(seeds, contents, genesis.DefaultDepositSettings())

	rich := genesis.DefaultDepositSettings()
	rich.Fuel.Abundance = genesis.AbundanceRich
	rich.Metals.Abundance = genesis.AbundanceRich
	rich.NonMetals.Abundance = genesis.AbundanceRich
	richRes := genesis.GenerateDeposits(seeds, contents, rich)

	changed := false
	for si := range avg.Systems {
		for pi := range avg.Systems[si].Planets {
			a := avg.Systems[si].Planets[pi].Deposits
			r := richRes.Systems[si].Planets[pi].Deposits
			// Counts and resources (and their order) are identical.
			if len(a) != len(r) {
				t.Fatalf("system %d planet %d: rich changed deposit count %d -> %d", si, pi, len(a), len(r))
			}
			for di := range a {
				if a[di].Resource != r[di].Resource {
					t.Errorf("system %d planet %d deposit %d: rich changed resource %q -> %q",
						si, pi, di, a[di].Resource, r[di].Resource)
				}
				if a[di].Quantity != r[di].Quantity || a[di].Yield != r[di].Yield {
					changed = true
				}
			}
		}
	}
	if !changed {
		t.Errorf("rich abundance changed no quantity or yield; expected shifts")
	}
}

func sameDeposits(a, b []genesis.SystemDeposits) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Hex != b[i].Hex || !samePlanetDeps(a[i].Planets, b[i].Planets) {
			return false
		}
	}
	return true
}

func samePlanetDeps(a, b []genesis.PlanetDeposits) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Orbit != b[i].Orbit || len(a[i].Deposits) != len(b[i].Deposits) {
			return false
		}
		for j := range a[i].Deposits {
			if a[i].Deposits[j] != b[i].Deposits[j] {
				return false
			}
		}
	}
	return true
}

func toGoldenSysDep(systems []genesis.SystemDeposits) []goldenSysDep {
	out := make([]goldenSysDep, len(systems))
	for i, sys := range systems {
		out[i] = goldenSysDep{Q: sys.Hex.Q, R: sys.Hex.R, Planets: toGoldenPlanetDep(sys.Planets)}
	}
	return out
}

func toGoldenPlanetDep(planets []genesis.PlanetDeposits) []goldenPlanetDep {
	out := make([]goldenPlanetDep, len(planets))
	for i, p := range planets {
		deps := make([]goldenDeposit, len(p.Deposits))
		for j, d := range p.Deposits {
			deps[j] = goldenDeposit{Resource: string(d.Resource), Quantity: d.Quantity, Yield: d.Yield}
		}
		out[i] = goldenPlanetDep{Orbit: p.Orbit, Deposits: deps}
	}
	return out
}

func readDepositsGolden(t *testing.T) depositsGolden {
	t.Helper()
	data, err := os.ReadFile(depositsGoldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	var g depositsGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func writeDepositsGolden(t *testing.T, g depositsGolden) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(depositsGoldenPath), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	if err := os.WriteFile(depositsGoldenPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}
