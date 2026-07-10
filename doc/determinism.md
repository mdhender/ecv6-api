# Determinism (implementation)

EC must be **reproducible** (same master seeds → identical game on any machine)
and **order-independent** (an outcome depends only on its address, never on the
order draws are made or on Go-map iteration order).

The **mechanism** — master seeds, streams, key paths, domain tags, and the
SHA-256 → PCG derivation — is *implementation detail, not player-facing*. It has
been migrated out of the docs repo; **this page (plus the `seeds`/`prng` package)
is now the spec.** The docs keep only the player-facing promise: a game is
reproducible from its seeds
([ecv6-docs determinism](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/determinism.md)).

- Rationale — why the design looks this way, and its prior art:
  [counter-based-prng.md](counter-based-prng.md).

## Mechanism

- A game has two `uint64` master seeds, `seed1` and `seed2` — the root of all
  randomness.
- Randomness is drawn from **streams** addressed by a **key path**, a pure
  function of the seeds and the path:
  `stream = PCG( SHA-256(seed1, seed2, len(path), path...) )`, all big-endian;
  the first 16 bytes of the digest seed a PCG source (`math/rand/v2`).
- The first path element is a **domain tag** naming the purpose; the rest
  identify the instance (e.g. system `(q, r)`, player `id`). `Key` is `int64`.
- `Seeds.Stream(path ...Key)` returns the stream; `Seeds.Derive(path ...Key)`
  draws a child `(seed1, seed2)` for a subsystem, which then carries its own
  randomness.

## What lives in code

- **The `seeds`/`prng` package** implements `Seeds`, `Stream`, `Derive`, `Key`.
- **The domain-tag registry** is a single append-only constants block — the one
  place tags are defined. See
  [decisions/adr-0001-counter-based-prng.md](decisions/adr-0001-counter-based-prng.md).
- **Golden test vectors** pin `(seed1, seed2, path) → output` and are the
  enforcement mechanism: any change to addressing, hashing, or generator makes
  them fail. They live in `internal/prng/testdata/golden.json`; regenerate
  intentionally with `go test ./internal/prng/ -update`, never to silence a
  failing test.

## Frozen surfaces — do not change while any game exists

- **Key-path encoding**: element order, how ids/coordinates coerce to `Key`, the
  big-endian layout, and the length prefix.
- **Domain-tag numbering**: the block starts at `1` (`0` is invalid) and is
  **append-only**.

## How to add a domain tag safely

1. Append the new constant to the **end** of the domain-tag block. Never insert
   or reorder — `iota` would renumber existing tags and silently rewrite every
   live game.
2. Give it a distinct purpose and instance-addressing whose trailing path
   elements are unique within that purpose.
3. Add golden vectors for the new stream so its addressing is pinned going
   forward.

## Rules that fall out of this

- Address a draw by the item's own identity (coordinates, id), never by
  iteration order. **Never draw in Go-map iteration order** — it is
  nondeterministic.
- Two draws sharing an address share a stream and produce correlated results;
  keep tags distinct and instances unique.
