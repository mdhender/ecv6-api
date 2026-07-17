// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis

import (
	"encoding/json"
	"math"
	"sort"

	"github.com/mdhender/ecv6-api/internal/prng"
)

// The Genesis Deposits generator's provenance identity. Per ADR-0017 these are
// RECORDED PROVENANCE only — they no longer enter the seed path. The deposits
// seed root is Derive(TagDeposit) alone; below it each system is addressed by its
// (q, r). Kept as the integer generator/version handles the store's
// game_generator row records (pending the UUID reconciliation in the E1 §3
// generator rows).
const (
	DepositsGeneratorID prng.Key = 1
	DepositsVersion     prng.Key = 1
)

// Resource is one of the three natural resources a deposit contains. The string
// values are the wire/store form (cluster core reference; Genesis Deposits
// supplement uses the same three codes).
type Resource string

// The three resources. Values match the abundance-setting codes fuel/mtls/nmtl.
const (
	Fuel      Resource = "fuel"
	Metals    Resource = "mtls"
	NonMetals Resource = "nmtl"
)

// Abundance is a resource's GM-provided abundance knob. It shifts a resource's
// final quantity and yield (each via an independent roll) but never its system
// endowment, planet shares, deposit counts, or deposit weights (Genesis
// Deposits, "Settings").
type Abundance string

// The three abundance settings. Named with an Abundance prefix so AbundanceAverage
// does not collide with the placement Density value Average.
const (
	AbundancePoor    Abundance = "poor"
	AbundanceAverage Abundance = "average"
	AbundanceRich    Abundance = "rich"
)

// DefaultEndowment is the default ten-planet baseline endowment Af/Am/An, shared
// by all three resources (Genesis Deposits, "Settings"):
//
//	Af = Am = An = 6.5 × 17.5 × 43,000,000 = 4,891,250,000
//
// GM entry of a different endowment is future work; the generator uses this
// default via DefaultDepositSettings.
const DefaultEndowment float64 = 6.5 * 17.5 * 43_000_000 // 4_891_250_000

// ResourceSettings is one resource's Genesis Deposits settings: its abundance
// knob and its ten-planet baseline endowment.
type ResourceSettings struct {
	Abundance Abundance
	Endowment float64
}

// DepositSettings holds the three resources' Genesis Deposits settings. These are
// GM-provided per game; DefaultDepositSettings supplies the documented defaults.
type DepositSettings struct {
	Fuel      ResourceSettings
	Metals    ResourceSettings
	NonMetals ResourceSettings
}

// DefaultDepositSettings returns the documented defaults: average abundance and
// the default endowment for all three resources (Genesis Deposits, "Settings").
func DefaultDepositSettings() DepositSettings {
	rs := ResourceSettings{Abundance: AbundanceAverage, Endowment: DefaultEndowment}
	return DepositSettings{Fuel: rs, Metals: rs, NonMetals: rs}
}

// resourceSettingsJSON is the on-disk shape of one resource's settings, stored in
// the deposits row of game_generator.settings (opaque, stage-specific JSON).
type resourceSettingsJSON struct {
	Abundance string  `json:"abundance"`
	Endowment float64 `json:"endowment"`
}

// depositSettingsJSON is the on-disk shape of DepositSettings, keyed by the
// resource codes fuel/mtls/nmtl.
type depositSettingsJSON struct {
	Fuel      resourceSettingsJSON `json:"fuel"`
	Metals    resourceSettingsJSON `json:"mtls"`
	NonMetals resourceSettingsJSON `json:"nmtl"`
}

// MarshalSettings encodes the deposit settings as the JSON stored in the deposits
// row of game_generator.settings.
func (s DepositSettings) MarshalSettings() (string, error) {
	j := depositSettingsJSON{
		Fuel:      resourceSettingsJSON{Abundance: string(s.Fuel.Abundance), Endowment: s.Fuel.Endowment},
		Metals:    resourceSettingsJSON{Abundance: string(s.Metals.Abundance), Endowment: s.Metals.Endowment},
		NonMetals: resourceSettingsJSON{Abundance: string(s.NonMetals.Abundance), Endowment: s.NonMetals.Endowment},
	}
	b, err := json.Marshal(j)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseDepositSettings decodes the JSON stored in game_generator.settings back
// into DepositSettings.
func ParseDepositSettings(s string) (DepositSettings, error) {
	var j depositSettingsJSON
	if err := json.Unmarshal([]byte(s), &j); err != nil {
		return DepositSettings{}, err
	}
	return DepositSettings{
		Fuel:      ResourceSettings{Abundance: Abundance(j.Fuel.Abundance), Endowment: j.Fuel.Endowment},
		Metals:    ResourceSettings{Abundance: Abundance(j.Metals.Abundance), Endowment: j.Metals.Endowment},
		NonMetals: ResourceSettings{Abundance: Abundance(j.NonMetals.Abundance), Endowment: j.NonMetals.Endowment},
	}, nil
}

// settings returns the ResourceSettings for a resource.
func (s DepositSettings) settings(r Resource) ResourceSettings {
	switch r {
	case Fuel:
		return s.Fuel
	case Metals:
		return s.Metals
	default:
		return s.NonMetals
	}
}

// Deposit is one generated deposit: its resource, its final whole-number
// quantity, and its final yield in tenths of a percentage point (Yield = 42
// means 4.2%). At generation a deposit's current values equal its initial values,
// so the pure generator emits one value per field; persistence mirrors it into
// initial and current columns.
type Deposit struct {
	Resource Resource
	Quantity int64 // final quantity, a positive whole number
	Yield    int   // final yield, in tenths of a percent (0.1% units); always >= 1
}

// PlanetDeposits is one planet's deposits, in creation order (the planet's
// resource type-order, then deposit index within each resource).
type PlanetDeposits struct {
	Orbit    int
	Deposits []Deposit
}

// SystemDeposits is one ordinary system's per-planet deposits, planets in
// ascending orbit order.
type SystemDeposits struct {
	Hex     Hex
	Planets []PlanetDeposits
}

// DepositsResult is the Genesis Deposits stage output: every ordinary system's
// deposits, in the same order as the system-contents systems handed in. There is
// no home-system template (ADR-0017): home systems, and their deposits, are
// generated on demand at founding, not here.
type DepositsResult struct {
	Systems []SystemDeposits
}

// baseYieldPct returns a resource's base yield, in whole percent (Genesis
// Deposits, "Base yield"): Fuel 5%, Metals 12%, Non-metals 9%.
func baseYieldPct(r Resource) float64 {
	switch r {
	case Fuel:
		return 5
	case Metals:
		return 12
	default:
		return 9
	}
}

// affinity is a planet type's affinity for a resource, which sets its shares roll
// (Genesis Deposits, "Distributing resources among planets").
type affinity int

const (
	affLow affinity = iota
	affNormal
	affHigh
)

// processingOrder returns the resource processing order for a planet type
// (Genesis Deposits, "Exact processing order"). This order is used for every
// per-planet, per-resource step: shares, slot assignment, and the equal-remainder
// and deposit-creation tie-break.
func processingOrder(t PlanetType) [3]Resource {
	switch t {
	case Rocky:
		return [3]Resource{Fuel, Metals, NonMetals}
	case AsteroidBelt:
		return [3]Resource{Metals, NonMetals, Fuel}
	default: // GasGiant
		return [3]Resource{NonMetals, Fuel, Metals}
	}
}

// affinityOf returns a planet type's affinity for a resource (Genesis Deposits,
// "Distributing resources among planets").
func affinityOf(t PlanetType, r Resource) affinity {
	switch t {
	case Rocky:
		switch r {
		case Fuel:
			return affHigh
		case Metals:
			return affNormal
		default:
			return affLow
		}
	case AsteroidBelt:
		switch r {
		case Fuel:
			return affLow
		case Metals:
			return affHigh
		default:
			return affNormal
		}
	default: // GasGiant
		switch r {
		case Fuel:
			return affNormal
		case Metals:
			return affLow
		default:
			return affHigh
		}
	}
}

// systemEndowment is a resource's system endowment for a system with planetCount
// planets (Genesis Deposits, "System endowments"):
//
//	system endowment = ten-planet baseline × P ÷ 10
//
// Kept fractional; not rounded. Abundance does not affect it.
func systemEndowment(baseline float64, planetCount int) float64 {
	return baseline * float64(planetCount) / 10
}

// planetAmount is a planet's fractional amount of a resource (Genesis Deposits,
// "Distributing resources among planets"):
//
//	planet resource amount = system endowment × planet shares ÷ total system shares
//
// totalShares must be > 0 (the caller guards the zero-total case: a resource with
// zero total shares does not occur in the system).
func planetAmount(endowment float64, shares, totalShares int) float64 {
	return endowment * float64(shares) / float64(totalShares)
}

// depositBaseQuantity is a deposit's fractional base quantity (Genesis Deposits,
// "Base quantity"):
//
//	deposit base quantity = planet resource amount × deposit weight ÷ total deposit weights
//
// Kept fractional; not rounded.
func depositBaseQuantity(amount float64, weight, totalWeight int) float64 {
	return amount * float64(weight) / float64(totalWeight)
}

// finalQuantity applies a deposit's quantity adjustment and rounds (Genesis
// Deposits, "Final quantity"). adjPct is the rolled adjustment in whole percent
// (relative: +4 means ×1.04):
//
//	adjusted     = base × (1 + adjPct/100)
//	final        = max(1, floor(adjusted))
func finalQuantity(base float64, adjPct int) int64 {
	adjusted := base * (1 + float64(adjPct)/100)
	return max(1, int64(math.Floor(adjusted)))
}

// yieldTenths computes a deposit's final yield in tenths of a percentage point
// (Genesis Deposits, "Deposit yields"). baseYield is the resource's base yield in
// whole percent, adjPct is the rolled yield adjustment in whole percent, and
// habitability drives the penalty:
//
//	habitability penalty = max(0, habitability - 20) × 5   (percentage points)
//	net                  = adjPct - penalty
//	unrounded yield %    = baseYield × (1 + net/100)
//	tenths               = max(1, floor(unrounded yield % × 10))
//
// The result is in 0.1% units, so it is always a multiple of 0.1% and never below
// the 0.1% minimum.
func yieldTenths(baseYield float64, adjPct, habitability int) int {
	penalty := 0
	if habitability > 20 {
		penalty = (habitability - 20) * 5
	}
	net := adjPct - penalty
	unrounded := baseYield * (1 + float64(net)/100)
	tenths := max(1, int(math.Floor(unrounded*10)))
	return tenths
}

// assignSlots distributes a planet's deposit count among its present resources
// (Genesis Deposits, "Assigning deposits to resources"). order is the planet's
// type-specific resource order; amounts holds each resource's planet amount (a
// resource is present when its amount > 0); count is the planet's total deposit
// count. It returns a slot count per resource, indexed the same as order, with a
// present resource always receiving at least one slot.
//
// One slot is reserved per present resource; the remainder is distributed in
// proportion to the amounts by the largest-remainder method, with equal remainders
// broken in favor of the earlier resource in order (never by float equality).
func assignSlots(order [3]Resource, amounts [3]float64, count int) [3]int {
	var slots [3]int

	present := make([]int, 0, 3) // indices into order that are present
	totalAmount := 0.0
	for i := range order {
		if amounts[i] > 0 {
			present = append(present, i)
			totalAmount += amounts[i]
		}
	}
	if len(present) == 0 {
		return slots // no resource occurs on this planet
	}

	// Reserve one slot per present resource; the rest are distributed.
	remaining := count - len(present)
	for _, i := range present {
		slots[i] = 1
	}
	if remaining <= 0 {
		return slots
	}

	// Whole-number portion of each present resource's exact share.
	type rem struct {
		idx  int     // index into order (also the tie-break priority)
		frac float64 // fractional remainder
	}
	rems := make([]rem, 0, len(present))
	assigned := 0
	for _, i := range present {
		exact := float64(remaining) * amounts[i] / totalAmount
		whole := int(math.Floor(exact))
		slots[i] += whole
		assigned += whole
		rems = append(rems, rem{idx: i, frac: exact - float64(whole)})
	}

	// Leftover slots go to the largest fractional remainders; equal remainders
	// favor the earlier resource in the planet's order (rems is built in order,
	// so a stable sort keeps that priority).
	leftover := remaining - assigned
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].frac > rems[b].frac })
	for k := 0; k < leftover && k < len(rems); k++ {
		slots[rems[k].idx]++
	}
	return slots
}

// GenerateDeposits runs the Genesis Deposits stage (stage 3) against a game's
// seeds, the system-contents output, and the deposit settings. It returns every
// ordinary system's per-planet deposits plus the home-system template's deposits,
// generated once. It is a pure function of (seeds, contents, settings) and does
// not persist anything.
//
// Genesis Deposits:
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/deposits.md
//
// Determinism. Deposits root at Derive(TagDeposit) — generator id/version are
// provenance, not entropy (ADR-0017). Each ordinary system draws from one Roller
// at root.Roller(Key(q), Key(r)). Within a system the seven phases are drawn
// strictly in the documented order (each phase completed system-wide before the
// next), addressing planets and resources by their deterministic order, never by
// Go-map iteration order.
func GenerateDeposits(seeds prng.Seeds, contents ContentsResult, settings DepositSettings) DepositsResult {
	root := seeds.Derive(prng.TagDeposit)

	systems := make([]SystemDeposits, len(contents.Systems))
	for i, sys := range contents.Systems {
		roller := root.Roller(prng.Key(sys.Hex.Q), prng.Key(sys.Hex.R))
		systems[i] = SystemDeposits{
			Hex:     sys.Hex,
			Planets: generateSystemDeposits(roller, sys.Planets, settings),
		}
	}

	return DepositsResult{Systems: systems}
}

// planetGen is a planet's mutable deposit-generation state, carried across the
// seven phases. Per-resource values are indexed the same as order (the planet's
// type-specific resource order), so no Go map is ever iterated for a draw.
type planetGen struct {
	planet   Planet
	order    [3]Resource
	shares   [3]int     // phase 2
	amount   [3]float64 // phase 2
	count    int        // phase 3
	slots    [3]int     // phase 4 (slot count per resource, same index as order)
	deposits []genDeposit
}

// genDeposit is one deposit under construction. resourceIdx indexes order.
type genDeposit struct {
	resourceIdx int
	resource    Resource
	weight      int     // phase 5 (2d6)
	baseQty     float64 // phase 5
	quantity    int64   // phase 6
	yield       int     // phase 7 (tenths of a percent)
}

// generateSystemDeposits runs the seven Genesis Deposits phases over one system's
// planets (already in ascending orbit order) using one Roller, completing each
// phase system-wide before the next (Genesis Deposits, "Exact processing order").
// It returns each planet's deposits in creation order.
func generateSystemDeposits(roller *prng.Roller, planets []Planet, settings DepositSettings) []PlanetDeposits {
	p := len(planets)
	gens := make([]planetGen, p)
	for i := range planets {
		gens[i] = planetGen{planet: planets[i], order: processingOrder(planets[i].Type)}
	}

	// Phase 1: system endowments (no rolls). Abundance does not affect these.
	endowment := map[Resource]float64{
		Fuel:      systemEndowment(settings.Fuel.Endowment, p),
		Metals:    systemEndowment(settings.Metals.Endowment, p),
		NonMetals: systemEndowment(settings.NonMetals.Endowment, p),
	}

	// Phase 2: planet shares, then distribute each endowment among the planets.
	totalShares := map[Resource]int{}
	for i := range gens {
		g := &gens[i]
		for k, r := range g.order {
			g.shares[k] = rollShares(roller, affinityOf(g.planet.Type, r))
			totalShares[r] += g.shares[k]
		}
	}
	for i := range gens {
		g := &gens[i]
		for k, r := range g.order {
			if totalShares[r] == 0 {
				g.amount[k] = 0 // resource does not occur in the system
				continue
			}
			g.amount[k] = planetAmount(endowment[r], g.shares[k], totalShares[r])
		}
	}

	// Phase 3: each planet's total deposit count.
	for i := range gens {
		gens[i].count = rollDepositCount(roller, gens[i].planet.Type)
	}

	// Phase 4: assign slots to resources and create the deposits (no rolls).
	for i := range gens {
		g := &gens[i]
		g.slots = assignSlots(g.order, g.amount, g.count)
		for k, r := range g.order {
			for n := 0; n < g.slots[k]; n++ {
				g.deposits = append(g.deposits, genDeposit{resourceIdx: k, resource: r})
			}
		}
	}

	// Phase 5: deposit weights (2d6) and base quantities. Weights are summed per
	// resource before base quantities are computed.
	for i := range gens {
		g := &gens[i]
		var weightTotal [3]int
		for di := range g.deposits {
			w := roller.RollN(2, 6)
			g.deposits[di].weight = w
			weightTotal[g.deposits[di].resourceIdx] += w
		}
		for di := range g.deposits {
			k := g.deposits[di].resourceIdx
			g.deposits[di].baseQty = depositBaseQuantity(g.amount[k], g.deposits[di].weight, weightTotal[k])
		}
	}

	// Phase 6: final quantity — an independent adjustment roll per deposit.
	for i := range gens {
		g := &gens[i]
		for di := range g.deposits {
			ab := settings.settings(g.deposits[di].resource).Abundance
			adj := rollAdjustment(roller, ab)
			g.deposits[di].quantity = finalQuantity(g.deposits[di].baseQty, adj)
		}
	}

	// Phase 7: yields — a new independent adjustment roll per deposit, plus the
	// planet's habitability penalty.
	for i := range gens {
		g := &gens[i]
		for di := range g.deposits {
			ab := settings.settings(g.deposits[di].resource).Abundance
			adj := rollAdjustment(roller, ab)
			g.deposits[di].yield = yieldTenths(baseYieldPct(g.deposits[di].resource), adj, g.planet.Habitability)
		}
	}

	out := make([]PlanetDeposits, p)
	for i := range gens {
		g := &gens[i]
		deposits := make([]Deposit, len(g.deposits))
		for di, d := range g.deposits {
			deposits[di] = Deposit{Resource: d.resource, Quantity: d.quantity, Yield: d.yield}
		}
		out[i] = PlanetDeposits{Orbit: g.planet.Orbit, Deposits: deposits}
	}
	return out
}

// rollShares rolls a planet's shares of one resource by its affinity and clamps to
// 0..7 (Genesis Deposits, "Distributing resources among planets").
func rollShares(roller *prng.Roller, aff affinity) int {
	var v int
	switch aff {
	case affHigh:
		v = roller.RollN(1, 3) + 4
	case affNormal:
		v = roller.RollN(1, 3) + 1
	default: // affLow
		v = roller.RollN(1, 3) - 1
	}
	return clamp(v, 0, 7)
}

// rollDepositCount rolls a planet's total deposit count by its type (Genesis
// Deposits, "Deposit counts"): Rocky 4d10, Gas giant 20+2d10, Asteroid belt
// 30+1d10.
func rollDepositCount(roller *prng.Roller, t PlanetType) int {
	switch t {
	case Rocky:
		return roller.RollN(4, 10)
	case GasGiant:
		return 20 + roller.RollN(2, 10)
	default: // AsteroidBelt
		return 30 + roller.RollN(1, 10)
	}
}

// rollAdjustment rolls a quantity- or yield-adjustment percentage by a resource's
// abundance (Genesis Deposits, "Final quantity" / "Yield adjustment"): rich
// +3d20%, average (2d12-13)%, poor -3d20%. The result is a whole-percent relative
// adjustment.
func rollAdjustment(roller *prng.Roller, ab Abundance) int {
	switch ab {
	case AbundanceRich:
		return roller.RollN(3, 20)
	case AbundancePoor:
		return -roller.RollN(3, 20)
	default: // AbundanceAverage
		return roller.RollN(2, 12) - 13
	}
}
