# ADR-0003: API resource identity — game, account, and player

- **Status:** accepted
- **Date:** 2026-07-07

## Context

The gap analysis ([../api/v4-gap-analysis.md](../api/v4-gap-analysis.md)) flagged
three identity mismatches between the inherited v4 contract and this repo's
adopted model — **G7** (game identifier), **G8** (account identifier), and
**G10** (no per-game player id). They are entangled: each defines a primary key
that every account/game/member schema and URL depends on, so they are settled
together, ahead of drafting any application paths.

Two of them collide with prior text:

- The domain vocabulary in `CLAUDE.md` calls a game's id "a short JSON-safe,
  space-free slug the GM chooses," while the application model in the same file
  calls the game record `(id, name, is_active)` with `id` an **integer PK**. The
  slug-vs-PK question was never resolved.
- v4 identified a game member by a mutable `handle` (defaulting to `player_N`)
  keyed on `accountId`, with no immutable per-game player id — incompatible with
  the docs' **player = a seat in one game with a sequential, unique-in-game,
  never-reused id** (upstream `players.md`), and with the domain boundary that
  keeps `account_id` out of the engine
  ([../control-and-ownership.md](../control-and-ownership.md)).

## Decision

**Games are identified by their integer primary key.** The wire key is
`gameId` (`int64`); `name` is the human-facing label (e.g. "Alpha Campaign").
v4's uppercase `code`/slug is **dropped** — it was redundant with PK + name. The
application record is `(id, name, is_active)`.

**Accounts are identified by email.** `email` is stored **lowercased** and is
unique; it replaces v4's `username`. A separate optional **`displayName`** is
retained as a human label distinct from the email. The secret remains hashed
(ADR-0002 authenticates against it).

**A player is a seat with an immutable `player_id`.** It is an integer,
**sequential, unique within a game, and never reused** — assigned when an account
joins a game. It is the sole identifier of the seat; the seat carries **no human
label** (the *faction*, not the seat, carries an in-game name). Consequently:

- v4's `handle`, its `player_N` defaulting, and the member-**rename** path are all
  removed. `addGameMember` reduces to "assign an account → mint a `player_id`";
  `updateGameMember` reduces to `isActive` / `isGm`.
- `player_id` is the **game-side key**. Game-scoped routes are addressed by it
  (not by `account_id`), which is the prerequisite for closing the boundary leak
  in **G5** (Phase 4). `account_id` stays app-only; the engine knows control by
  `player_id`.

## Consequences

- **Vocabulary drift to fix.** `CLAUDE.md`'s domain vocabulary still defines the
  game id as a GM-chosen slug; it must be updated to "integer PK, with `name` as
  the human label." Upstream `games.md`
  ([ecv6-docs](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/games.md))
  may likewise reference a GM-chosen game id and should be reconciled under the
  docs-lead workflow. Tracked as a follow-up, not done in this ADR.
- **`player_id` is game-consumed and may address determinism streams** (draws
  keyed by "a player's id" — see [../determinism.md](../determinism.md)). Its
  "never reused, stable once assigned" property is therefore not just a roster
  nicety: reusing or renumbering a seat id would change reproducible draws. The
  store must enforce it.
- **Integer PKs are enumerable.** Exposing sequential `gameId` / `accountId` /
  `player_id` on the wire leaks counts and invites IDOR probing. Acceptable for
  the alpha — every route is authenticated and object-level checks already gate
  access (v4 returns 404, not 403, to hide non-visible resources) — but noted so a
  later move to opaque public ids is a conscious change, not a surprise.
- **Smaller member surface.** Dropping handles removes a field, a default rule, a
  uniqueness constraint, and an entire rename code path from `addGameMember` /
  `updateGameMember`.
- **Not frozen surfaces.** These are application/game identifiers, not the
  determinism compatibility contract; schemas and exposure can change (subject to
  the `player_id` stability note above). Contrast ADR-0001's frozen key-path and
  domain-tag surfaces.

Resolves G7, G8, G10. Unblocks Phase 2 (G6, roles) and the Phase 3 application
draft; G5 (re-key game routes to `player_id`) is now unblocked for the Phase 4
engine pass.
