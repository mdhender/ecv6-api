# System generation (testbed)

Contributor reference for how the engine generates **system contents** at setup.
This is implementation detail — *how the back end fills a system*, not the
player-facing rules for *what a system is*. The player/GM-facing rules (a system
is a hex at `(q, r)`; ten orbits; planet types; habitability) live in the game
docs' [cluster reference](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md).

> **Interim home.** These facts were extracted from the game docs and live here
> as Markdown until the engine cluster/system package exists; at that point they
> migrate to that package's godoc, which becomes the reference truth. See
> [`doc/README.md`](../README.md).

## Testbed status

This iteration of the engine is a **testbed**. The system generator is
deliberately simplistic so we can exercise the deterministic cluster surface
(placement, radius, spacing) without committing to final system-content rules.
The generation of *system contents* described here is expected to change; the
*cluster placement* rules it sits inside are documented player-side and are
stable.

## Every system is identical

In the testbed the generator gives **every system the same contents**: the same
planets in the same orbits with the same habitability, and — because they are
placed by a deterministic formula from planet type and orbit — the same
natural-resource deposits. Systems do not vary from one another at all. See
[Deposits](deposit-generation.md). Per-system variation is future
work.

### Fixed per-orbit layout

Every system has exactly this layout, orbit `1` (innermost) to `10` (outermost).
An orbit either holds one planet or is empty.

| Orbit | Planet        | Habitability |
| ----- | ------------- | -----------: |
| 1     | Rocky         | 0            |
| 2     | Rocky         | 1            |
| 3     | Rocky         | 20           |
| 4     | Asteroid belt | 0            |
| 5     | *(empty)*     | 0            |
| 6     | Gas giant     | 10           |
| 7     | Gas giant     | 0            |
| 8     | Gas giant     | 0            |
| 9     | Asteroid belt | 0            |
| 10    | *(empty)*     | 0            |

Habitability is a per-planet integer; higher is more habitable. In this layout
the rocky planet in orbit `3` (habitability `20`) is the most habitable.

## Reserved per-system stream

Each system has its own PRNG stream, addressed by the `TagSystem` domain tag and
the system's coordinates — key path `(TagSystem, q, r)`. See
[`doc/determinism.md`](../determinism.md) and `internal/prng`.

Because every system is currently identical — deposits included, since they are
formula-driven (see [Deposits](deposit-generation.md)) — **the
generator does not draw from this stream**. It is still created and keyed by
`(q, r)`; it is simply unused. The stream is reserved so that when system
contents begin to vary — deposits first — the variation draws from a stream whose
address shape is already frozen.

> The key-path **length** is part of the address, so a longer path such as
> `(TagSystem, q, r, orbit)` is a *different* stream from `(TagSystem, q, r)`.
> Freezing the `(q, r)` shape now does not commit us to any per-orbit or
> per-resource sub-addressing; that shape is chosen when deposits land. See
> [Deposits](deposit-generation.md).

## See also

- [Deposits](deposit-generation.md) — the deposits every planet carries, and how
  they are derived.
- [`doc/determinism.md`](../determinism.md) — streams, key paths, domain tags,
  frozen surfaces.
- [Cluster reference (game docs)](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md)
  — the player-facing system and cluster rules.
