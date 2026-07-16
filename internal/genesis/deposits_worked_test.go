// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package genesis

import "testing"

// TestWorkedExample reproduces the Genesis Deposits supplement's "Worked example:
// a three-planet system" through the pure arithmetic functions. The example
// supplies specific hypothetical rolls ("suppose ..."), so this test drives those
// exact rolls/inputs and asserts the documented outputs — the reproducibility
// contract the ticket calls out.
//
// Genesis Deposits, "Worked example: a three-planet system":
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/deposits.md
func TestWorkedExample(t *testing.T) {
	const baseline = DefaultEndowment // 4,891,250,000, average abundance
	const planets = 3

	// Endowments: each resource gets 3 ÷ 10 of its ten-planet baseline.
	fuelEndow := systemEndowment(baseline, planets)
	metalsEndow := systemEndowment(baseline, planets)
	nmtlEndow := systemEndowment(baseline, planets)
	if fuelEndow != 1_467_375_000 || metalsEndow != 1_467_375_000 || nmtlEndow != 1_467_375_000 {
		t.Fatalf("endowments = (%v, %v, %v), want 1,467,375,000 each", fuelEndow, metalsEndow, nmtlEndow)
	}

	// Share totals from the doc's share-roll table.
	const totalFuel, totalMetals, totalNmtl = 11, 10, 9

	// Planet amounts (a representative subset of the documented table).
	rockyFuel := planetAmount(fuelEndow, 6, totalFuel)
	astMetals := planetAmount(metalsEndow, 7, totalMetals)
	gasNmtl := planetAmount(nmtlEndow, 5, totalNmtl)

	assertClose(t, "rocky fuel amount", rockyFuel, 800_386_363.6363636)
	assertClose(t, "asteroid metals amount", astMetals, 1_027_162_500)
	assertClose(t, "gas non-metals amount", gasNmtl, 815_208_333.3333334)

	// Slot assignment reproduces the documented deposit counts.
	// Rocky: count 26; amounts fuel 6/11, metals 3/10, non-metals 1/9.
	rockySlots := assignSlots(
		processingOrder(Rocky),
		[3]float64{
			planetAmount(fuelEndow, 6, totalFuel),
			planetAmount(metalsEndow, 3, totalMetals),
			planetAmount(nmtlEndow, 1, totalNmtl),
		},
		26,
	)
	if rockySlots != [3]int{14, 8, 4} { // Fuel, Metals, Non-metals
		t.Errorf("rocky slots = %v, want [14 8 4]", rockySlots)
	}
	// Asteroid belt: count 35; order Metals, Non-metals, Fuel.
	astSlots := assignSlots(
		processingOrder(AsteroidBelt),
		[3]float64{
			planetAmount(metalsEndow, 7, totalMetals),
			planetAmount(nmtlEndow, 3, totalNmtl),
			planetAmount(fuelEndow, 1, totalFuel),
		},
		35,
	)
	if astSlots != [3]int{21, 10, 4} { // Metals, Non-metals, Fuel
		t.Errorf("asteroid slots = %v, want [21 10 4]", astSlots)
	}
	// Gas giant: count 33; order Non-metals, Fuel, Metals; metals amount 0.
	gasSlots := assignSlots(
		processingOrder(GasGiant),
		[3]float64{
			planetAmount(nmtlEndow, 5, totalNmtl),
			planetAmount(fuelEndow, 4, totalFuel),
			0,
		},
		33,
	)
	if gasSlots != [3]int{20, 13, 0} { // Non-metals, Fuel, Metals
		t.Errorf("gas slots = %v, want [20 13 0]", gasSlots)
	}

	// Selected deposits: base quantity, adjustment, final quantity.
	rockyBase := depositBaseQuantity(rockyFuel, 8, 98)
	assertClose(t, "rocky fuel base quantity", rockyBase, 65_337_662.33766234)
	if got := finalQuantity(rockyBase, 8+9-13); got != 67_951_168 {
		t.Errorf("rocky fuel final quantity = %d, want 67,951,168", got)
	}

	astBase := depositBaseQuantity(astMetals, 10, 147)
	assertClose(t, "asteroid metals base quantity", astBase, 69_875_000)
	if got := finalQuantity(astBase, 5+6-13); got != 68_477_500 {
		t.Errorf("asteroid metals final quantity = %d, want 68,477,500", got)
	}

	gasBase := depositBaseQuantity(gasNmtl, 6, 140)
	assertClose(t, "gas non-metals base quantity", gasBase, 34_937_500)
	if got := finalQuantity(gasBase, 7+7-13); got != 35_286_875 {
		t.Errorf("gas non-metals final quantity = %d, want 35,286,875", got)
	}

	// Yields (stored in tenths of a percent: 42 = 4.2%).
	// Rocky fuel: base 5%, roll +5%, habitability 24 -> 20% penalty -> 4.25% -> 4.2%.
	if got := yieldTenths(baseYieldPct(Fuel), 10+8-13, 24); got != 42 {
		t.Errorf("rocky fuel yield = %d tenths, want 42 (4.2%%)", got)
	}
	// Asteroid metals: base 12%, roll +3%, penalty 0 -> 12.36% -> 12.3%.
	if got := yieldTenths(baseYieldPct(Metals), 8+8-13, 0); got != 123 {
		t.Errorf("asteroid metals yield = %d tenths, want 123 (12.3%%)", got)
	}
	// Gas non-metals: base 9%, roll -1%, penalty 0 -> 8.91% -> 8.9%.
	if got := yieldTenths(baseYieldPct(NonMetals), 6+6-13, 8); got != 89 {
		t.Errorf("gas non-metals yield = %d tenths, want 89 (8.9%%)", got)
	}
}

// TestYieldRoundingAndMinimum checks the documented yield floor and the round-down
// behavior at exact tenths (Genesis Deposits, "Habitability penalty" and "Output
// guarantees").
func TestYieldRoundingAndMinimum(t *testing.T) {
	// The supplement's habitability example: metals base 12%, +10% roll, hab 22
	// (10% penalty) -> exactly 12% -> 120 tenths.
	if got := yieldTenths(baseYieldPct(Metals), 10, 22); got != 120 {
		t.Errorf("metals 12%% with +10%% and hab 22 = %d tenths, want 120", got)
	}
	// A heavy penalty that would drive yield to zero is floored to the 0.1% minimum.
	if got := yieldTenths(baseYieldPct(Fuel), -100, 25); got != 1 {
		t.Errorf("yield floor = %d tenths, want 1 (0.1%%)", got)
	}
}

// assertClose fails if got and want differ by more than a tiny relative epsilon.
// The documented decimals are abbreviated, so an exact float compare is wrong;
// the pipeline itself keeps full float64 precision.
func assertClose(t *testing.T, what string, got, want float64) {
	t.Helper()
	const eps = 1e-3
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > eps {
		t.Errorf("%s = %.6f, want ~%.6f", what, got, want)
	}
}
