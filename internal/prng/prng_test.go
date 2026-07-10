// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package prng_test

import (
	"encoding/json"
	"flag"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdhender/ecv6-api/internal/prng"
)

// update regenerates testdata/golden.json from the current code. Run once when
// intentionally establishing the frozen surface:
//
//	go test ./internal/prng/ -update
//
// then eyeball the diff and commit. Never run it to "fix" a failing golden test:
// a failure means the addressing, hashing, or generator changed, which silently
// rewrites every live game.
var update = flag.Bool("update", false, "regenerate testdata/golden.json")

const goldenPath = "testdata/golden.json"

// drawsPerStream is how many uint64 each golden stream pins.
const drawsPerStream = 4

// golden is the on-disk shape of the frozen vectors.
type golden struct {
	Streams []streamVector `json:"streams"`
	Derives []deriveVector `json:"derives"`
}

type streamVector struct {
	Seed1 uint64     `json:"seed1"`
	Seed2 uint64     `json:"seed2"`
	Path  []prng.Key `json:"path"`
	Draws []uint64   `json:"draws"`
}

type deriveVector struct {
	Seed1 uint64     `json:"seed1"`
	Seed2 uint64     `json:"seed2"`
	Path  []prng.Key `json:"path"`
	// child seeds are exposed only via their observable behavior; we pin the
	// first draw of the child's own default stream so the vector stays black-box.
	WantChildDraw uint64 `json:"want_child_draw"`
}

// goldenInputs enumerates the addresses whose outputs we freeze. Extend by
// APPENDING; never change an existing entry's seeds or path.
func goldenInputs() golden {
	streamPaths := [][]prng.Key{
		{prng.TagCluster},
		{prng.TagSystem, 0, 0},
		{prng.TagSystem, 3, -7},
		{prng.TagSystem, 3, -7, 1}, // longer path must differ from the one above
		{prng.TagPlayer, 1},
		{prng.TagPlayer, 2},
	}
	derivePaths := [][]prng.Key{
		{prng.TagCluster},
		{prng.TagPlayer, 42},
	}
	const s1, s2 = 0x0123456789abcdef, 0xfedcba9876543210

	var g golden
	seeds := prng.New(s1, s2)
	for _, p := range streamPaths {
		st := seeds.Stream(p...)
		draws := make([]uint64, drawsPerStream)
		for i := range draws {
			draws[i] = st.Uint64()
		}
		g.Streams = append(g.Streams, streamVector{Seed1: s1, Seed2: s2, Path: p, Draws: draws})
	}
	for _, p := range derivePaths {
		child := seeds.Derive(p...)
		g.Derives = append(g.Derives, deriveVector{
			Seed1: s1, Seed2: s2, Path: p,
			WantChildDraw: child.Stream(prng.TagCluster).Uint64(),
		})
	}
	return g
}

func TestGolden(t *testing.T) {
	if *update {
		writeGolden(t, goldenInputs())
		t.Log("wrote", goldenPath)
	}

	want := readGolden(t)

	for _, v := range want.Streams {
		st := prng.New(v.Seed1, v.Seed2).Stream(v.Path...)
		for i, w := range v.Draws {
			if got := st.Uint64(); got != w {
				t.Errorf("Stream(%v) draw %d = %d, want %d (frozen surface changed?)", v.Path, i, got, w)
			}
		}
	}
	for _, v := range want.Derives {
		child := prng.New(v.Seed1, v.Seed2).Derive(v.Path...)
		if got := child.Stream(prng.TagCluster).Uint64(); got != v.WantChildDraw {
			t.Errorf("Derive(%v) child draw = %d, want %d (frozen surface changed?)", v.Path, got, v.WantChildDraw)
		}
	}
}

// TestOrderIndependence: an address's output depends only on the address, never
// on when it is computed relative to other draws.
func TestOrderIndependence(t *testing.T) {
	seeds := prng.New(1, 2)
	a := []prng.Key{prng.TagSystem, 5, 9}
	b := []prng.Key{prng.TagSystem, 8, 1}

	// Reference: draw A on its own.
	ref := drawN(seeds.Stream(a...), 3)

	// Draw B first, then A — A must be unchanged.
	seeds.Stream(b...) // exercised, discarded
	got := drawN(seeds.Stream(a...), 3)

	if !equal(ref, got) {
		t.Errorf("A's draws changed with order: %v vs %v", ref, got)
	}
}

// TestDistinctAddresses: distinct tags, distinct instances, and distinct path
// lengths all yield uncorrelated streams (first draws differ).
func TestDistinctAddresses(t *testing.T) {
	seeds := prng.New(7, 11)
	cases := map[string][]prng.Key{
		"cluster":      {prng.TagCluster},
		"system-0-0":   {prng.TagSystem, 0, 0},
		"system-0-0-0": {prng.TagSystem, 0, 0, 0}, // length is part of the address
		"system-1-0":   {prng.TagSystem, 1, 0},
		"player-1":     {prng.TagPlayer, 1},
		"player-2":     {prng.TagPlayer, 2},
	}
	seen := map[uint64]string{}
	for name, path := range cases {
		first := seeds.Stream(path...).Uint64()
		if other, ok := seen[first]; ok {
			t.Errorf("address %q collides with %q (first draw %d)", name, other, first)
		}
		seen[first] = name
	}
}

// Stream must satisfy math/rand/v2.Source (so rand.New can wrap it).
var _ rand.Source = (*prng.Stream)(nil)

func TestStreamWrapsRand(t *testing.T) {
	r := rand.New(prng.New(3, 4).Stream(prng.TagPlayer, 99))
	if n := r.IntN(6); n < 0 || n >= 6 {
		t.Errorf("IntN(6) out of range: %d", n)
	}
}

func drawN(s *prng.Stream, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = s.Uint64()
	}
	return out
}

func equal(a, b []uint64) bool {
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

func readGolden(t *testing.T) golden {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	return g
}

func writeGolden(t *testing.T, g golden) {
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
