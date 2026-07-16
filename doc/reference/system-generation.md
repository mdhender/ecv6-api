# System generation

Contributor reference for how the engine implements **system-contents
generation** at setup — *how the back end fills a system*, not the player-facing
rules for *what a system is*. This stage implements the **Genesis System
Contents** generator; the rules it implements (planet count, per-orbit planet
type, habitability, the home-system template) live upstream and are the source of
truth:

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
> [`internal/genesis`](../../internal/genesis) (`GenerateContents`, `HomeTemplate`)
> and its output persists via `internal/store` (`planet` and `home_template`
> tables, migration 5; `SaveSystemContents`/`GetSystemContents`). The rules it
> implements are grounded upstream (CLAUDE.md rule 3) in the supplement linked
> above; the godoc on the code cites it. Deposits (the next stage) and the
> per-player home-system copy are ticketed separately (see
> [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md) and
> epic [mdhender/ecv6-api#65](https://github.com/mdhender/ecv6-api/issues/65)).

## Systems vary

Genesis System Contents produces **varied, stochastic** systems: the number of
planets, which orbits they occupy, and each planet's type and habitability are
rolled per system (with a fixed layout for the smallest systems and a habitability
top-up for the largest). This retires the earlier interim behavior, in which every
system was identical and no randomness was drawn. See the supplement for the exact
dice, orbits, clamps, and the min-habitability rule.

A separate **fixed home-system template** is applied when a player joins: the
generator overwrites the chosen system's ordinary contents with the template. The
template's values are the supplement's, not this repo's.

## The stage seam

Generation is staged and each stage's generator is chosen independently
([ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md)).
System contents runs **after placement** and **before deposits**:

- **In:** the placed systems (their `(q, r)` hexes) from the placement stage.
- **Out:** for every ordinary system, each occupied orbit's planet type and
  habitability; plus the fixed home-system template. Empty orbits carry no planet.

Deposits consumes `(planet type, orbit)` per planet, so this stage's output is the
compatibility surface between the two — keep it stable as both stages version.

## Determinism

Each Genesis stage draws from its **own seed root**, derived
`Derive(stageTag, generatorID, version)` — the stage's domain tag, then the
generator's identity and version — below which the generator owns its addressing
entirely ([ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md)).
The global domain-tag registry and the key-path hash encoding remain the frozen
surfaces; only the *root* addressing is global. See
[`doc/determinism.md`](../determinism.md) and `internal/prng`.

System Contents roots at `Derive(TagSystem, SysContentsGeneratorID,
SysContentsVersion)` with `(generatorID, version) = (1, 1)`. Below that root each
ordinary system draws from one stream addressed by its `(q, r)` —
`root.Roller(Key(q), Key(r))` — so a system's contents depend only on the game
seeds and its own coordinates, never on Go-map iteration or the order systems are
processed. Each system's single `Roller` is drawn from in a fixed order: planet
count, then the orbit shuffle (only for four-plus-planet systems), then
habitability per occupied orbit in ascending orbit order.

The per-system key-path shape below the root is a generator-internal decision, not
a global frozen surface; it freezes per generator version, on this generator's
schedule, once a game depends on it. The home-system template makes no draws, so
it reserves a fixed sentinel `(q, r)` address deliberately outside any legal
radius (`HomeTemplateQ`, `HomeTemplateR`), distinct from every real system, so the
deposit stage can roll the template's deposits without colliding with an ordinary
system.

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
