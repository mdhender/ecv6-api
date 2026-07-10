# Deposits (testbed)

Contributor reference for the **natural-resource deposits** planets carry in the
testbed: what each planet holds and in what amounts. *How* the engine derives
these values is an implementation detail — see [Derivation](#derivation) — and is
subject to change; the player-facing game docs describe only these qualities and
defer generation to this reference. The values are **deterministic** (a pure
function of planet type and orbit, no random draw) and, because every testbed
system has the same [fixed layout](system-generation.md), identical in every
system.

> **Scope.** Testbed values, engine-owned, **not yet grounded** in the rulebook
> (CLAUDE.md rule 3) — the open deposit blocker on E1 (mdhender/ecv6-api#67). It
> does not yet consume the cluster's abundance settings. Not a rule of the game;
> do not cite as one.

Three resources: fuel (`FUEL`), metals (`METL`), non-metals (`NMTL`). Each deposit
has a **quantity** (its starting amount) and a **yield percentage**.

## Deposits by orbit

The deposits every system carries, orbit by orbit — a golden reference for tests.
`n @ y%` is `n` deposits at yield `y%`; each carries the row's per-deposit
quantity. Empty orbits carry none.

| Orbit | Planet        | Qty / deposit |   FUEL |   METL |   NMTL |
|-------|---------------|--------------:|-------:|-------:|-------:|
| 1     | Rocky         |    33,333,334 | 1 @ 2% | 1 @ 3% | 1 @ 3% |
| 2     | Rocky         |    33,333,334 | 1 @ 2% | 1 @ 3% | 1 @ 3% |
| 3     | Rocky         |    33,333,334 | 1 @ 2% | 1 @ 3% | 1 @ 3% |
| 4     | Asteroid belt |    16,666,667 | 1 @ 2% | 3 @ 3% | 2 @ 3% |
| 5     | *(empty)*     |             — |      — |      — |      — |
| 6     | Gas giant     |    16,666,667 | 3 @ 3% | 1 @ 5% | 2 @ 6% |
| 7     | Gas giant     |    16,666,667 | 3 @ 3% | 1 @ 5% | 2 @ 6% |
| 8     | Gas giant     |    16,666,667 | 3 @ 2% | 1 @ 3% | 2 @ 3% |
| 9     | Asteroid belt |    16,666,667 | 1 @ 1% | 3 @ 2% | 2 @ 2% |
| 10    | *(empty)*     |             — |      — |      — |      — |

## System-wide totals

Summing every deposit across the ten orbits — identical in every system — gives
the system's starting resource totals. Totals are not round because the
per-planet `100,000,000` is divided with round-up (see [Quantity](#quantity)).

| Resource  | Deposits |  Total quantity |
|-----------|---------:|----------------:|
| FUEL      |       14 |     283,333,339 |
| METL      |       12 |     250,000,005 |
| NMTL      |       13 |     266,666,672 |
| **Total** |   **39** | **800,000,016** |

## Derivation

How the engine currently produces the values above. This is an implementation
detail: it is subject to change, draws no randomness, and does not yet consume the
cluster's abundance settings.

### Deposit counts by planet type

The planet type fixes how many deposits of each resource it carries.

| Planet type   | FUEL | METL | NMTL | Total deposits |
|---------------|-----:|-----:|-----:|---------------:|
| Rocky         |    1 |    1 |    1 |              3 |
| Asteroid belt |    1 |    3 |    2 |              6 |
| Gas giant     |    3 |    1 |    2 |              6 |

### Quantity

Every deposit on a planet starts at the same quantity: **100,000,000 divided by
the total number of deposits on that planet, rounded up.**

- Rocky (3 deposits): `⌈100,000,000 / 3⌉` = **33,333,334** each.
- Asteroid belt / Gas giant (6 deposits): `⌈100,000,000 / 6⌉` = **16,666,667** each.

### Yield percentage

Each deposit's yield is set by its resource, then reduced by orbit and planet type.
All halving rounds up (`⌈x / 2⌉`).

1. Base yield: **FUEL 3%**, **METL 5%**, **NMTL 6%**.
2. If the planet is in orbit **1, 2, 3, 8, 9, or 10**, halve the yield.
3. If the planet is an **asteroid belt**, halve the yield again.

Steps 2 and 3 are cumulative, so an asteroid belt in one of those orbits is halved
twice:

- **Orbit 4 (asteroid, not a halved orbit):** halved once — FUEL `⌈3/2⌉ = 2%`,
  METL `⌈5/2⌉ = 3%`, NMTL `⌈6/2⌉ = 3%`.
- **Orbit 9 (asteroid in a halved orbit):** halved twice — FUEL `⌈⌈3/2⌉/2⌉ = 1%`,
  METL `⌈⌈5/2⌉/2⌉ = 2%`, NMTL `⌈⌈6/2⌉/2⌉ = 2%`.
- **Orbit 8 (gas giant in a halved orbit):** halved once — FUEL 2%, METL 3%,
  NMTL 3%.

## Determinism

The values draw **no randomness**: given planet type and orbit, every value above
is fixed. The reserved per-system stream (`TagSystem`, keyed by `(q, r)` — see
[System generation](system-generation.md)) is therefore still **unused**.

## See also

- [System generation](system-generation.md) — the fixed testbed layout these
  deposits attach to, and the reserved per-system stream.
- [`doc/determinism.md`](../determinism.md) — streams, key paths, frozen surfaces.
- mdhender/ecv6-api#67 — E1 epic; the deposit blocker this addresses (pending
  grounding).
