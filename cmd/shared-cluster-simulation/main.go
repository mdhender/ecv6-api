// Command shared-cluster-simulation measures the density metrics of the EC
// cluster placement algorithm (shuffle -> draw -> reject-if-closer-than-MD ->
// fail-if-exhausted). Given N systems and a minimum-distance knob MD, it reports
// per stellar-density tier: radius, hex count, hexes-per-system, generation
// success rate, and the nearest-neighbor distribution (mean/median/min), all in
// hex distance.
//
// WARNING — this is an offline measurement tool, shared by the engineering and
// docs teams to derive and re-verify the radius and minimum-spacing tables in
// the cluster reference. It is NOT the engine cluster generator and NOT part of
// the frozen determinism surface: it draws from math/rand/v2 PCG with an
// arbitrary -seed flag, not internal/prng. Do not wire it into the engine or
// treat its output as reproducible game state — the real generator is a separate
// stage that keys off the cluster's derived master seeds.
//
// Its reference output (below) is the provenance for the radius/spacing tables in
// the cluster reference:
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md
//
//	go run . -n 100 -md 1 -trials 300
package main

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"sort"
)

type hex struct{ q, r int }

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// dist is axial hex distance.
func dist(a, b hex) int {
	return (abs(a.q-b.q) + abs(a.r-b.r) + abs((a.q+a.r)-(b.q+b.r))) / 2
}

// hexesWithin returns every hex whose distance from the origin is <= R.
func hexesWithin(R int) []hex {
	out := make([]hex, 0, 1+3*R*(R+1))
	for q := -R; q <= R; q++ {
		for r := -R; r <= R; r++ {
			if dist(hex{0, 0}, hex{q, r}) <= R {
				out = append(out, hex{q, r})
			}
		}
	}
	return out
}

func hexCount(R int) int { return 1 + 3*R*(R+1) }

// nearestRadius returns the R whose hex count is closest to target — hold
// hexes-per-system constant by scaling the radius with N. Round-to-nearest so
// that at the baseline N the tier's original radius is recovered exactly.
func nearestRadius(target int) int {
	R := 1
	for hexCount(R+1) < target {
		R++
	}
	if target-hexCount(R) <= hexCount(R+1)-target {
		return R
	}
	return R + 1
}

// place runs one trial: shuffle the candidate hexes, then draw and keep any hex
// no closer than md to every hex already placed, until n are placed or exhausted.
func place(all []hex, n, md int, rng *rand.Rand) ([]hex, bool) {
	list := make([]hex, len(all))
	copy(list, all)
	rng.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
	placed := make([]hex, 0, n)
	for _, h := range list {
		ok := true
		for _, p := range placed {
			if dist(h, p) < md {
				ok = false
				break
			}
		}
		if ok {
			placed = append(placed, h)
			if len(placed) == n {
				return placed, true
			}
		}
	}
	return placed, false
}

func nearestNeighbors(sys []hex) []int {
	res := make([]int, len(sys))
	for i := range sys {
		best := 1 << 30
		for j := range sys {
			if i != j {
				if d := dist(sys[i], sys[j]); d < best {
					best = d
				}
			}
		}
		res[i] = best
	}
	return res
}

type tier struct {
	name       string
	baseRadius int // radius at the baseline N=100
}

func main() {
	n := flag.Int("n", 100, "number of systems")
	md := flag.Int("md", 1, "minimum distance (hexes) between systems")
	trials := flag.Int("trials", 300, "trials per tier")
	seed := flag.Uint64("seed", 0x5eed, "base seed")
	flag.Parse()

	tiers := []tier{
		{"extremely dense", 37},
		{"dense", 40},
		{"average", 43},
		{"sparse", 47},
		{"very sparse", 51},
	}
	const baseN = 100

	fmt.Printf("N=%d  MD=%d  trials=%d\n\n", *n, *md, *trials)
	fmt.Printf("%-16s %6s %7s %8s %8s %8s %8s %8s\n",
		"density", "radius", "hexes", "hex/sys", "success", "mean-nn", "med-nn", "min-nn")

	for ti, t := range tiers {
		hps := float64(hexCount(t.baseRadius)) / float64(baseN)
		R := nearestRadius(int(hps*float64(*n) + 0.5))
		all := hexesWithin(R)

		var sumNN float64
		var cntNN int
		var allNN []int
		globalMin := 1 << 30
		success := 0

		for tr := 0; tr < *trials; tr++ {
			rng := rand.New(rand.NewPCG(*seed+uint64(ti)*1_000_003, uint64(tr)))
			sys, ok := place(all, *n, *md, rng)
			if ok {
				success++
			}
			for _, d := range nearestNeighbors(sys) {
				sumNN += float64(d)
				cntNN++
				allNN = append(allNN, d)
				if d < globalMin {
					globalMin = d
				}
			}
		}
		sort.Ints(allNN)
		mean := sumNN / float64(cntNN)
		med := 0.0
		if len(allNN) > 0 {
			med = float64(allNN[len(allNN)/2])
		}
		fmt.Printf("%-16s %6d %7d %8.1f %7.0f%% %8.2f %8.0f %8d\n",
			t.name, R, len(all), float64(len(all))/float64(*n),
			100*float64(success)/float64(*trials), mean, med, globalMin)
	}
}

/*
Reference output (300 trials/tier):

$ go run ./cmd/shared-cluster-simulation -n 100 -md 1
N=100  MD=1  trials=300

density          radius   hexes  hex/sys  success  mean-nn   med-nn   min-nn
extremely dense      37    4219     42.2     100%     3.52        3        1
dense                40    4921     49.2     100%     3.75        3        1
average              43    5677     56.8     100%     4.03        4        1
sparse               47    6769     67.7     100%     4.42        4        1
very sparse          51    7957     79.6     100%     4.78        4        1

$ go run ./cmd/shared-cluster-simulation -n 100 -md 2
N=100  MD=2  trials=300

extremely dense      37    4219     42.2     100%     3.79        3        2
dense                40    4921     49.2     100%     4.02        4        2
average              43    5677     56.8     100%     4.27        4        2
sparse               47    6769     67.7     100%     4.64        4        2
very sparse          51    7957     79.6     100%     5.01        5        2

$ go run ./cmd/shared-cluster-simulation -n 1000 -md 1
N=1000  MD=1  trials=300

density          radius   hexes  hex/sys  success  mean-nn   med-nn   min-nn
extremely dense     118   42127     42.1     100%     3.40        3        1
dense               128   49537     49.5     100%     3.68        3        1
average             137   56719     56.7     100%     3.93        4        1
sparse              150   67951     68.0     100%     4.30        4        1
very sparse         162   79219     79.2     100%     4.64        4        1
*/
