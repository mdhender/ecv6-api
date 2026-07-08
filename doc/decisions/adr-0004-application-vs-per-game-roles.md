# ADR-0004: Application roles vs per-game roles

- **Status:** accepted
- **Date:** 2026-07-07

## Context

The inherited v4 contract carried a flat `roles: [player, gm, admin]` array on
both `Account` and the `/me` `User` object (gap analysis
[../api/v4-gap-analysis.md](../api/v4-gap-analysis.md), **G6**). That conflates
two different things:

- **Application roles** — global, about *who* an account is across the whole
  server.
- **Per-game roles** — GM and player, which the adopted model makes facets of a
  **membership** in one game (`game_account_role.is_gm`, and "player" = holding a
  seat), not properties of the account (`CLAUDE.md` application model & domain
  vocabulary).

Treating `gm`/`player` as global account roles also violates the two-domain
split ([../control-and-ownership.md](../control-and-ownership.md)): GM/player are
game-scoped concepts, and the application domain should not carry them on an
account.

## Decision

**There are exactly two application roles: `admin` and `user`.** They are derived
from the account alone — `is_admin` true → `admin`, otherwise `user`. No other
global role exists.

**`gm` and `player` are not account properties.** They are per-game, derived from
the membership row for a given game: `is_gm` → GM; an active seat → player. They
are meaningful only in the scope of a specific game.

**`/me` returns account information only.** It projects the application-domain
account — id, email, `displayName`, application role (admin/user), active state —
and **must not** return any in-game data: no GM/player status, no game list, no
seat. Per-game GM/player status is surfaced only by game-scoped responses
(`listMyGames`, `listGameMembers`) via their `isGm` field, where it is correctly
scoped to a game.

Concretely: the `Role` enum's global `gm`/`player` values are dropped. The
application role is exposed as a **`roles` string array** carrying only
application-role values (`admin` / `user`) — never gm/player. Its exact spelling
(open string values, **not** a boolean `isAdmin` and **not** an
`enum`-constrained field) is fixed by
[ADR-0005](adr-0005-application-surface-decisions.md).

## Consequences

- **Clean domain separation.** The application domain knows only `admin`/`user`;
  GM/player live with membership. `/me` becomes a pure application-domain
  projection with no game coupling — it can be served without touching game
  state, reinforcing the boundary.
- **Hold the line on `/me`.** Expect recurring pressure to bundle game data into
  `/me` (the caller's games, current seat, GM status) for client convenience.
  Resist it: `/me` stays account-only. Per-game data belongs to the game-scoped
  endpoints (`/me/games`, `/games/{gameId}/members`), which keep it correctly
  scoped and let `/me` stay a pure application-domain read. Convenience is one
  extra call, not an eroded boundary.
- **Authorization shape follows.** Admin-only routes check the application role;
  game-scoped routes check membership (existence of an active seat) plus `is_gm`.
  No route reads a global `gm`/`player` role, because none exists.
- **Schema change.** Account/`/me` lose `roles[]`; `/me` gains `displayName`
  (ADR-0003) and the application role. Game-scoped member/my-game responses keep
  `isGm`.
- **Not a frozen surface.** These are role/projection choices, freely revisable;
  unrelated to the determinism contract.

Resolves G6. With G4 (auth) and G7/G8/G10 (identity) settled, the Phase 3
application `openapi.yaml` draft is unblocked.
