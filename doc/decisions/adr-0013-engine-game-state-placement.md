# ADR-0013: Engine game-state lives in a separate table

- **Status:** accepted
- **Date:** 2026-07-09

## Context

Phase E0 (the determinism foundation, issue #66) needs to persist per-game
engine state: the two `uint64` master seeds (`seed1`, `seed2`) that root all
randomness (see [../determinism.md](../determinism.md) and the `internal/prng`
package) and the engine clock `current_turn` (turn 0 is setup; play starts at 1).

The `games` table (migration0001) is an **application-domain** row: name, status,
description, `is_active`. A load-bearing invariant is that the two domains stay
separate — the engine is unaware of the application side, and the boundary is a
small set of ids ([../control-and-ownership.md](../control-and-ownership.md)).

Two placements were on the table:

- **Extend `games`** with `seed1`, `seed2`, `current_turn` columns. Fewer joins,
  one row per game either way.
- **A separate engine-state table** keyed by `game_id`.

## Decision

**Engine state lives in a separate table, `game_engine_state`, keyed by
`game_id`** (one row per game, `PRIMARY KEY REFERENCES games (id)`). The `games`
row stays application-only.

```sql
CREATE TABLE game_engine_state (
    game_id      INTEGER NOT NULL PRIMARY KEY REFERENCES games (id),
    seed1        INTEGER NOT NULL,                 -- uint64 master seed (bit pattern)
    seed2        INTEGER NOT NULL,                 -- uint64 master seed (bit pattern)
    current_turn INTEGER NOT NULL DEFAULT 0        -- turn 0 = setup; play starts at 1
)
```

SQLite has no unsigned integer type, so each `uint64` seed is stored as its
**bit pattern** in an `INTEGER` (a signed `int64` on disk) and reinterpreted as
`uint64` on read; the sign is meaningless. This is a storage detail, not part of
the determinism frozen surface — the frozen surface is how seeds and a key path
are *hashed* (`internal/prng`), not how the seeds are warehoused.

## Consequences

- The domain boundary is visible in the schema: application code touches `games`,
  engine code touches `game_engine_state`, and neither table forces the other's
  concerns into its row.
- The reference is one-directional (`game_engine_state.game_id → games.id`): a
  game can exist before it is set up as an engine game (no engine row yet), which
  matches turn 0 being a distinct setup step.
- Slightly more ceremony to read seeds + turn (a join or a second query) — an
  acceptable cost for keeping the domains uncoupled.
- Engine-state grows here (later columns/tables) without widening the
  application row.
- **Scopes the engine to one game.** `game_id` is intended to be passed to the
  engine constructor, so an engine instance is always driven by a single game:
  it loads that game's row from `game_engine_state` and every table it touches is
  keyed by that `game_id`. Keeping this state out of the application `games` row
  keeps the engine's inputs to exactly the ids it is meant to see.
  - **Clarified by [ADR-0018](adr-0018-project-shape-and-engine-store-boundary.md):**
    "it loads … and every table it touches" conflates two roles. The engine is
    **store-blind** — it never opens `game_engine_state` or any store table. Loading
    the engine-state row and touching store tables is the *workflow's* job: the
    workflow reads the snapshot, adapts it to the engine shape, runs the engine, and
    adapts the result back to persist it (snapshot → adapt → mutate → adapt → update).
    Read this bullet as scoping the *game's engine state* to one `game_id`, not as the
    engine performing the load. The same store-blind rule governs turn-0 generation:
    the generator receives seeds and produces domain data, while `internal/setup`
    owns the store I/O.
- **Not a frozen surface.** Table placement and the `uint64`-as-`INTEGER` storage
  can change via a forward migration ([ADR-0007](adr-0007-forward-only-migrations.md));
  only the `internal/prng` addressing/hashing is frozen once a game exists.

## Note (2026-07-17): when and how seeds are assigned

E1 (issue #90) added the `game_engine_state` store accessors (`GetEngineState`,
`SaveEngineState`) and had to settle *when* a game's master seeds are written — a
gap this ADR left open (it only established that a game may exist before its engine
row does).

- **Assigned at setup time, not at game creation.** The application-side
  `CreateGame` never writes engine state; the setup orchestration (the turn-0
  cluster generation the GM triggers) is what calls `SaveEngineState`. This keeps
  the application domain unaware of determinism state, consistent with the
  one-directional reference above.
- **Unsupplied seeds default via `math/rand/v2`**, not `crypto/rand`. When the GM
  does not supply seeds, the setup layer draws them from Go's top-level
  `math/rand/v2` source (`rand.Uint64()`). Seeds are provenance, not a security
  secret, and this keeps the standard-library-first posture (CLAUDE.md); the store
  accessors themselves never invent seeds — that policy lives in the setup layer.
- **`SaveEngineState` upserts** (`INSERT … ON CONFLICT(game_id) DO UPDATE`) so
  setup-time regeneration is repeatable while alpha data is disposable.
