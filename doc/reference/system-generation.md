# System generation

Contributor reference for how the engine implements **system-contents
generation** at setup — *how the back end fills a system*, not the player-facing
rules for *what a system is*. This stage implements the **Genesis System
Contents** generator; the rules it implements (planet count, per-orbit planet
type, habitability) live upstream and are the source of truth:

- [Genesis System Contents](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/system-contents.md)
  — the generator this stage implements (draft, v1).
- [Genesis family index](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/_index.md)
  — the three staged generators and how a game records `(generator, version)` per
  stage.
- [Cluster core](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md)
  — the shared schema (hex `(q, r)`, ten orbits, planet types, habitability) every
  generator fills in.

Never restate the rules here; link them. This page is engine mechanism and the
stage seam. See [`doc/README.md`](../README.md).

> **Implemented.** The pure generator lives in
> [`internal/genesis`](../../internal/genesis) (`GenerateContents`) and its output
> persists via `internal/store` (`planet` table, migration 5;
> `SaveSystemContents`/`GetSystemContents`). Per-system contents provenance —
> which generator produced a system's contents — lives in the
> `system_contents_generator` table
> ([ADR-0017](../decisions/adr-0017-generator-identity-and-home-system-generation.md) §3;
> `PutSystemContentsGenerator`/`GetSystemContentsGenerators`). The rules it
> implements are grounded upstream (CLAUDE.md rule 3) in the supplement linked
> above; the godoc on the code cites it. Deposits (the next stage) and the
> on-demand home-system generator (E3) are ticketed separately (see
> [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md) and
> epic [mdhender/ecv6-api#65](https://github.com/mdhender/ecv6-api/issues/65)).

## Systems vary

Genesis System Contents produces **varied, stochastic** systems: the number of
planets, which orbits they occupy, and each planet's type and habitability are
rolled per system (with a fixed layout for the smallest systems and a habitability
top-up for the largest). This retires the earlier interim behavior, in which every
system was identical and no randomness was drawn. See the supplement for the exact
dice, orbits, clamps, and the min-habitability rule.

There is **no fixed home-system template**
([ADR-0017](../decisions/adr-0017-generator-identity-and-home-system-generation.md)).
A home system is produced on demand at founding: the GM picks an already-placed
system and a **home-system generator** rebuilds it, replacing that system's
ordinary contents, before a faction is assigned (E3). The layout the generator
produces is the supplement's business, not this repo's.

## The stage seam

Generation is staged and each stage's generator is chosen independently
([ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md)).
System contents runs **after placement** and **before deposits**:

- **In:** the placed systems (their `(q, r)` hexes) from the placement stage.
- **Out:** for every ordinary system, each occupied orbit's planet type and
  habitability. Empty orbits carry no planet.

Deposits consumes `(planet type, orbit)` per planet, so this stage's output is the
compatibility surface between the two — keep it stable as both stages version.

## Determinism

Each Genesis stage draws from its **own seed root**, derived `Derive(stageTag)` —
the stage's domain tag alone — below which the generator owns its addressing
entirely. Generator id and version are recorded provenance, not seed inputs
([ADR-0017](../decisions/adr-0017-generator-identity-and-home-system-generation.md)
amends [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md)).
The global domain-tag registry and the key-path hash encoding remain the frozen
surfaces; only the *root* addressing is global. See
[`doc/determinism.md`](../determinism.md) and `internal/prng`.

System Contents roots at `Derive(TagSystem)`. Below that root each ordinary system
draws from one stream addressed by its `(q, r)` —
`root.Roller(Key(q), Key(r))` — so a system's contents depend only on the game
seeds and its own coordinates, never on Go-map iteration or the order systems are
processed. Each system's single `Roller` is drawn from in a fixed order: planet
count, then the orbit shuffle (only for four-plus-planet systems), then
habitability per occupied orbit in ascending orbit order.

The per-system key-path shape below the root is a generator-internal decision, not
a global frozen surface; it freezes per generator version, on this generator's
schedule, once a game depends on it. A home-system generator (E3) roots at the
**same** per-`(q, r)` seed as any other system generator for that system, so a
rebuilt home system stays deterministic without any special sentinel address.

## See also

- [Deposits](deposit-generation.md) — the next stage, which turns this stage's
  planet types and orbits into resource deposits.
- [`doc/determinism.md`](../determinism.md) — streams, key paths, domain tags,
  frozen surfaces.
- [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md) —
  why the rulebook splits into a core and generator supplements, and how each
  generator gets a seed root.
- [Genesis System Contents](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/system-contents.md)
  — the rules this stage implements.
