# ADR-0001: Counter-based, spawn-keyed PRNG for determinism

- **Status:** accepted
- **Date:** 2026-07-07

## Context

EC must be **reproducible** (same master seeds → identical game on any machine)
and **order-independent** (an outcome depends only on its address, never on the
order draws are made or on Go-map iteration order). A single advancing RNG
sequence provides neither: it entangles every draw and reshuffles the world when
iteration order changes.

This decision ratifies the mechanism. It now lives in this repo as implementation
detail; the docs keep only the player-facing promise
([ecv6-docs determinism](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/determinism.md)):

- [determinism.md](../determinism.md) — the spec (seeds, streams, key paths,
  frozen surfaces).
- [counter-based-prng.md](../counter-based-prng.md) — the full rationale and
  prior art (Random123, NumPy `SeedSequence`, JAX `fold_in`, domain separation).

## Decision

Randomness is drawn from **streams addressed by a key path**, derived as a pure
function of the game's two `uint64` master seeds and the path:

- `stream = PCG( SHA-256(seed1, seed2, len(path), path...) )`, all big-endian.
- The first path element is an append-only **domain tag** (block starts at `1`,
  `0` invalid); remaining elements identify the instance (e.g. system `(q, r)`,
  player `id`).
- Hashing (SHA-256) is kept separate from generation (PCG, `math/rand/v2`) so
  either can change without disturbing the other.

The domain-tag registry is a single append-only constants block in code, and
**golden test vectors** pin `(seed1, seed2, path) → output` to fail CI on any
drift.

## Consequences

- **Frozen surface.** The key-path encoding and the domain-tag numbering become a
  compatibility surface the moment any game exists — like a save-file format.
  Tags are appended, never inserted or reordered (`iota` would silently rewrite
  every live game).
- Order independence and reproducibility fall out of the construction rather than
  being maintained by discipline.
- SHA-256 is heavier than purpose-built mixers, but EC draws thousands of numbers,
  not billions, so the cost is invisible and we get mixing quality for free.
- Adding a subsystem's randomness is safe as long as it appends a tag and gives
  its instances unique addresses; see [../determinism.md](../determinism.md).
