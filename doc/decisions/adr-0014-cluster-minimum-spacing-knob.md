# ADR-0014: Minimum system spacing is an independent GM knob

- **Status:** accepted
- **Date:** 2026-07-10

## Context

Cluster generation (phase E1, issue [#67](https://github.com/mdhender/ecv6-api/issues/67))
places `N` star systems on a hex map. Two settings shape the map: **number of
systems** `N` and **stellar density** `D`. The placement algorithm — build the hex
list within a radius, shuffle, draw, keep a hex only if it is at least the *minimum
system spacing* from every system already placed, fail if the list is exhausted — is
documented in the rulebook (`cluster.md`), but two numbers were not: the **radius** and
the **minimum system spacing**.

`cluster.md`'s *From the settings to a map* prose listed *both* radius and minimum
spacing among the things "the number of systems and stellar density" determine — i.e. it
framed minimum spacing as **derived from density**, and the glossary encoded the same
framing (its `Minimum system spacing` entry read "Set by the stellar density during
generation," and `Stellar density` read "It sets the minimum system spacing"). Modeling
the generator (see the `clustergen` simulation and the design brief) surfaced a question
that framing hid: what, mechanically, is the minimum-spacing threshold *for*?

The design intent for a spacing control is to **force players to invest in engine / jump
technology** by forcing systems apart. Measuring the per-system nearest-neighbor
distribution (`N = 100`, 300–400 trials) showed the density and spacing levers act on
different parts of that distribution:

- **Density (radius)** moves the **mean**. Going from `average` to `very sparse` raises
  mean nearest-neighbor from ~4.0 to ~4.8 — but the **low tail persists**: even at the
  sparsest setting ~7% of systems still have an *adjacent* (1-hex) neighbor and ~20% have
  one within 2 hexes. Random placement always scatters some systems close together, and a
  bigger radius also inflates the whole map (~40% more hexes) as a side effect.
- **Minimum spacing** moves the **floor**. It rejects any placement closer than `S`,
  truncating the low tail by construction (`S = 3` → 0% of systems within 2 hexes) while
  leaving the radius, hex count, and map size unchanged.

A player invests only as much jump tech as their **cheapest** reachable neighbor demands.
Density leaves that cheapest jump at 1 no matter how far it is cranked (luck of the
seed); only a spacing floor guarantees a minimum. So the two knobs are **not**
redundant — they control genuinely different things:

- **map size / exploration** → density only;
- **typical / average trip length** → both (density is the stronger lever);
- **guaranteed minimum jump (the tech gate)** → spacing only.

Because the intent (a tech gate) maps onto the one axis density cannot reach, folding
spacing into density would make the intended lever unreachable.

## Decision

**Minimum system spacing `S` is an independent GM setting, not a value derived from
density.**

- **Type / range:** integer hexes, **minimum `1`**, **default `2`**, **no maximum**.
- **Radius is unaffected.** `R` remains a pure function of `(N, D)`. `S` does not enter
  the radius calculation.
- **`S` acts only at placement.** It is the rejection threshold: a drawn hex is kept only
  if it is at least `S` hexes (axial distance) from every system already placed. `S = 1`
  is a no-op floor (distinct hexes are always ≥ 1 apart), i.e. pure uniform-random
  placement.
- **Overshoot fails cleanly, and that is the GM's problem.** Because `S` has no maximum, a
  GM can choose an `S` for which `N` systems do not fit within the radius. Placement then
  exhausts the hex list and **generation fails — no cluster is created.** There is **no
  engine fallback**: the engine does not auto-relax `S`, auto-grow the radius, or emit a
  partial map. The GM resolves it by changing the seed, lowering `S`, lowering `N`, or
  choosing a sparser density. This reuses the fail-if-exhausted behavior the rulebook
  already specifies.

The rulebook is updated to match: `cluster.md` adds `Minimum spacing` to the *Settings*
table and sources radius (derived) and spacing (GM input) differently in *From the
settings to a map*, and the **glossary** entries that stated the old derivation
(`Minimum system spacing`, `Stellar density`) are corrected. This ADR records the
decision and rationale; the rules themselves live in the docs (docs-first, CLAUDE.md
rule 3).

## Consequences

- **The tech-pressure lever exists and is orthogonal.** GMs can dial map size/average
  trip (density) and the guaranteed minimum jump (spacing) independently — e.g. a *dense*
  galaxy that still forbids adjacent expansion (`dense` + `S = 3`), which neither knob
  alone can express.
- **Determinism is preserved.** `S` is a scalar GM input applied before any draw; it does
  not touch the key-path encoding, domain-tag numbering, or seed derivation
  ([ADR-0001](adr-0001-counter-based-prng.md), [../determinism.md](../determinism.md)).
  Same `(seeds, N, D, S)` → same map.
- **Feasibility becomes a two-variable question.** The radius rule must give `N` systems
  generous headroom at the **default `S = 2`** (random-sequential placement jams well
  below optimal packing, so headroom must be real). Beyond the default, high `S` trades
  off against feasibility on the GM's authority — an accepted, documented failure mode,
  not a bug to engineer around.
- **The published density table is an `S = 1` characterization.** Its geometry columns
  (radius, hexes, hexes-per-system) are `S`-independent; its measured avg-nearest-neighbor
  is reported at `S = 1` to isolate the density knob. Real games at `S = 2` sit ~0.25 hex
  higher. Any table that mixes the two must say which `S` it assumes.
- **Not a frozen surface, but reproducibility-bearing.** `S`'s range and default are
  revisable while they affect no stored game, but once a game is generated its `(N, D, S)`
  and seeds are part of that game's reproducibility contract — changing how `S` is applied
  later would change existing maps. The stored cluster settings must include `S`.
- **Storage.** The `clusters` table (E1 deliverable) records `S` alongside the derived
  radius/spacing and the cluster's derived seeds, so a cluster is reproducible from its
  own row.

## Alternatives considered

- **Derive spacing from density (the original `cluster.md` framing).** Rejected: it
  couples the tech gate to map size, so the one thing the spacing control is *for* — a
  guaranteed minimum jump independent of how big/empty the galaxy is — cannot be set. It
  also leaves the cheap-neighbor tail in place at every density.
- **Cap `S` at a computed maximum (never allow infeasible settings).** Rejected: the cap
  would itself depend on `N`, `D`, and the random-placement jamming fraction — fragile to
  compute and surprising to GMs. Fail-if-exhausted is already specified, honest, and
  simple; letting the GM overshoot and recover is less machinery than policing the input.
- **Auto-relax `S` or auto-grow the radius on failure.** Rejected: silent fallback would
  break the "same settings → same map" contract (the delivered map would not match the
  requested settings) and hide a GM mistake. Failing loudly keeps generation deterministic
  and legible.
