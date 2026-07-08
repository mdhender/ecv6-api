# V4 API gap analysis

> **Status: review artifact (provisional).** A working record for the API
> review, not a decision. It catalogues where the inherited v4 contract
> (`openapi-v4.yaml`) diverges from the vocabulary and design this repo has since
> adopted (`CLAUDE.md`, the docs repo, and the `/doc` model docs). Each gap is
> assigned a stable id (`G1…`) so the reconciliation plan can reference it. No
> gap here is resolved; the **Disposition** lines are recommendations and open
> questions, not settled outcomes.

## What this is

`doc/api/openapi-v4.yaml` is copied in from the failed v4 iteration and is
fully fleshed out; `doc/api/openapi.yaml` (the current source of truth) is
effectively empty — `paths: {}`, one nested `Error` envelope, one `playerSecret`
scheme, `/api/v1`, OpenAPI 3.1. So porting v4 "over" is almost entirely
*additive*: the target overwrites nothing. The gaps below are the places where
v4 **encodes a design we have decided against**, so porting means dropping or
rewriting that part rather than copying it.

Each gap carries an **Area**:

- **Application** — accounts, auth, sessions, health/version (the application
  domain: *who* may act).
- **Game** — the engine surface: turns, orders, and anything keyed by
  game/player/faction identity.
- **Cross-cutting** — wire conventions that touch both.

## Summary

| Id | Area | Kind | Gap |
| --- | --- | --- | --- |
| [G1](#g1--error-envelope-shape--requestid) | Cross-cutting | Mechanical | Flat `{code,message,requestId}` vs nested `{error:{…}}`; `requestId` at risk |
| [G2](#g2--openapi-30--31) | Cross-cutting | Mechanical | **Resolved (ADR-0006):** OpenAPI 3.0.3 + oapi-codegen; `nullable: true` stays |
| [G3](#g3--path-versioning--host) | Cross-cutting | Mechanical | **Resolved (ADR-0006):** unversioned routes under `/api` (no `/v1`) |
| [G4](#g4--sessionjwtrefreshimpersonation-stack-is-unratified) | Application | Design | **Resolved (ADR-0002):** opaque server-side session tokens, bearer transport; JWT/refresh dropped |
| [G5](#g5--accountid-as-the-game-side-key-boundary-leak) | Game | Design | **Partly resolved:** roster routes keyed by `playerId`; orders/turns addressing deferred to engine |
| [G6](#g6--flat-global-roles-conflate-app-and-per-game-roles) | Application | Design | **Resolved (ADR-0004):** app roles are only `admin`/`user`; gm/player are per-game; `/me` is account-only |
| [G7](#g7--game-code-vs-slug-id) | Cross-cutting | Design | **Resolved (ADR-0003):** game keyed by integer PK; `code`/slug dropped, `name` is the label |
| [G8](#g8--username-vs-email) | Application | Design | **Resolved (ADR-0003):** identify by lowercased `email`; `username` dropped, `displayName` kept |
| [G9](#g9--turn-identity-vs-operational-metadata) | Game | Design | **Reframed:** turn id == turn number (settled, not a gap); only operational metadata (label/status/due-date) is an open Phase-4 question |
| [G10](#g10--no-player-seat-id) | Game | Design | **Resolved (ADR-0003):** immutable sequential `player_id`; `handle` + rename dropped |
| [G11](#g11--no-cluster--system--faction-read-surface) | Game | Gap | No read surface for the map or factions at all |

---

## Mechanical gaps

Cheap to reconcile — format conversion, not design.

### G1 — Error envelope shape + `requestId`
*(Cross-cutting · Mechanical)*

- **v4:** flat `ErrorResponse { code, message, requestId? }`.
- **Adopted:** nested envelope `{ error: { code, message } }`
  ([`conventions.md`](conventions.md), [`openapi.yaml`](openapi.yaml)) — **no
  `requestId`**.
- **Lost / must change:** the flat shape (fine to drop). The **request-
  correlation id** disappears unless we deliberately add it to the current
  envelope.
- **Disposition:** **Resolved (drafted).** The nested envelope
  `{ error: { code, message, requestId? } }` is in [`openapi.yaml`](openapi.yaml);
  `requestId` is kept as an optional correlation field. Error-code catalogue is
  still TODO in [`conventions.md`](conventions.md).

### G2 — OpenAPI 3.0 → 3.1
*(Cross-cutting · Mechanical)*

- **v4:** OpenAPI 3.0.3; uses `nullable: true` (on `Turn.ordersDueAt`).
- **Disposition:** **Resolved — [ADR-0006](../decisions/adr-0006-openapi-version-tooling-and-routing.md).**
  Spec is **OpenAPI 3.0.3**, generated with **oapi-codegen** (the inherited
  `3.1.0` was reverted, since kin-openapi lacks full 3.1). So `nullable: true`
  is *correct* and stays; 3.1-only constructs are avoided.

### G3 — Path versioning & host
*(Cross-cutting · Mechanical)*

- **v4:** root paths (`/games…`, `/me…`), dev host `http://localhost:9987`.
- **Adopted:** base path `/api` with **no version segment** — no `/v1` in routes
  ([`conventions.md`](conventions.md)). The alpha is unversioned; if versioning is
  ever needed it will be by another means (e.g. a header), not a path fork.
- **Disposition:** **Resolved — [ADR-0006](../decisions/adr-0006-openapi-version-tooling-and-routing.md).**
  Routes are unversioned under base path **`/api`** (e.g. `/api/healthz`,
  `/api/games`); no `/v1`.

---

## Design gaps

Where v4 predates an adopted decision. These do **not** survive the port intact.

### G4 — Session/JWT/refresh/impersonation stack is unratified
*(Application · Design)*

- **v4:** `login → access + refresh` tokens, token rotation, session families
  (`/me/sessions`, per-family revoke, `/auth/refresh`, `/auth/logout`), and
  `/admin/impersonation` built on an `act` JWT claim.
- **Adopted:** the committed model is only a **per-player shared secret**
  (`playerSecret`, mechanism TBD — [`conventions.md`](conventions.md)); the app
  model names an account's **hashed secret** but says nothing about JWTs,
  refresh tokens, or sessions (`CLAUDE.md` application model).
- **Lost / must change:** none of the session machinery is ratified. Porting
  forces a decision — **re-adopt it explicitly, or drop it.** Impersonation is
  *pure* JWT (mints a token bearing another identity); under a shared-secret
  model it has no mechanism at all.
- **Disposition:** **Resolved — [ADR-0002](../decisions/adr-0002-api-authentication-model.md).**
  Adopt **opaque, server-side session tokens carried as `Authorization: Bearer`**;
  drop JWT and the access/refresh split. `/auth/refresh` is removed; `/auth/login`
  returns one session token; `/me/sessions` + per-session revoke become CRUD over
  the session table; `changeMyPassword` revokes other sessions with a store
  delete; `/admin/impersonation` mints a short-lived session bound to the target
  with an `actor` audit column (no JWT `act` claim). This closes the revocation
  gap by construction. The session token is **not** a frozen surface.

### G5 — `accountId` as the game-side key (boundary leak)
*(Game · Design)*

- **v4:** game-scoped routes address members and orders by `accountId` —
  `/games/{gameId}/members/{accountId}`.
- **Adopted:** **`account_id` is app-only; the engine never sees it.** The
  game-consumed ids are `game_id` and `player_id`; the engine knows control by
  `player_id`, never by account
  ([`control-and-ownership.md`](../control-and-ownership.md), "domain boundary
  by id").
- **Lost / must change:** v4's URL structure would bake an `account_id` leak
  into the engine-facing wire. Member/order addressing must be **rewritten to
  `player_id`**, not ported.
- **Disposition:** **Partly resolved.** The **membership** surface is corrected:
  with `player_id` now real (G10, ADR-0003), the roster route is keyed
  `/games/{gameId}/members/{playerId}` (not `{accountId}`), and `accountId`
  survives only as an explicitly app-side field on the `Member` body — no leak
  into an engine-facing path. **Remaining:** the **orders/turns** routes must
  likewise address by `player_id`; they are unwritten (Phase 4, engine), so this
  closes with G9/G11.

### G6 — Flat global `roles` conflate app and per-game roles
*(Application · Design)*

- **v4:** `roles: [player, gm, admin]` on both `User` and `Account`.
- **Adopted:** application roles are `admin` / `user`; `gm` and `player` are
  **per-game membership facets** (`is_gm`, and "player" = the seat), not global
  account roles (`CLAUDE.md` application model & domain vocabulary).
- **Lost / must change:** the flat array doesn't map — an account is not
  globally a "gm". It gets dropped or reinterpreted (app role from
  `is_admin`; gm/player derived per game from membership).
- **Disposition:** **Resolved — [ADR-0004](../decisions/adr-0004-application-vs-per-game-roles.md).**
  Application roles are only **`admin`/`user`** (from `is_admin`); **gm/player are
  per-game**, derived from the membership row (`is_gm`, active seat). The flat
  `roles[]` is dropped. **`/me` returns account information only** — no in-game
  gm/player data; per-game status appears only in game-scoped responses via
  `isGm`.

### G7 — Game `code` vs slug `id`
*(Cross-cutting · Design)*

- **v4:** `Game.code`, uppercase `^[A-Z][A-Z]+$` (e.g. `ALPHA`).
- **Adopted:** a game's id is a **GM-chosen, JSON-safe, space-free slug**
  (`CLAUDE.md` domain vocabulary). Note the app model separately calls the
  application record `(id, name, is_active)` with an integer PK — the
  slug-vs-integer identity question is itself unsettled and should be pinned
  during reconciliation.
- **Lost / must change:** `code` and its uppercase constraint are dropped/
  replaced by the slug; decide whether the wire exposes the slug, the integer
  PK, or both.
- **Disposition:** **Resolved — [ADR-0003](../decisions/adr-0003-api-resource-identity.md).**
  Game is keyed by its **integer PK** (`gameId`); v4's uppercase `code`/slug is
  dropped as redundant, and `name` is the human label. Follow-up: `CLAUDE.md`
  domain vocabulary (and upstream `games.md`) still call the game id a GM-chosen
  slug and must be reconciled.

### G8 — `username` vs `email`
*(Application · Design)*

- **v4:** `login` takes `username`/`password`; `User.username`.
- **Adopted:** accounts are keyed by **email** (stored lowercased, unique);
  authenticate with the account's **secret** (`CLAUDE.md` application model).
- **Lost / must change:** `username` is dropped in favour of `email`.
  `User.displayName` (present but never set in v4) can stay if we want a
  human label.
- **Disposition:** **Resolved — [ADR-0003](../decisions/adr-0003-api-resource-identity.md).**
  Identify by **`email`**, stored lowercased and unique; `username` dropped. A
  separate optional **`displayName`** is kept as a human label.

### G9 — Turn identity vs operational metadata
*(Game · Design)*

- **v4:** `Turn = { id, gameId, label, status, ordersDueAt }` — an int64 `id`
  scoped to its game, a display `label` ("Spring 901"), a `TurnStatus`
  (`open/locked/processing/published`), and a wall-clock `ordersDueAt`.
- **Adopted:** a turn is identified by its **number** — sequential per game,
  turn 0 = setup — and that number is the effective-date axis for `asOf` reads
  (`CLAUDE.md` temporal model;
  [`storing-state-as-timebound-facts.md`](../storing-state-as-timebound-facts.md)).
- **Not a conflict — the earlier framing was wrong.** "Turn id" and "turn
  number" are the **same value**: v4 addresses turns at
  `/games/{gameId}/turns/{turnId}`, i.e. by number scoped to the game — exactly
  the adopted model. There is no surrogate key; the "surrogate `turnId`" reading
  was an unsupported inference from v4 pairing `id` with a `label`. Turn identity
  needs no change, and no `turnId` appears in the drafted spec (turns weren't
  drafted).
- **What is actually open:** the **operational metadata** v4 attaches to a turn —
  `label`, `status`, and the wall-clock `ordersDueAt`. None conflicts with the
  temporal model (an orders-due date is application-side scheduling, not the
  effective-date axis), but whether our turns expose any of them is undecided.
- **Disposition:** turn **identity/addressing is settled** (by number) — no
  action. Whether a turn carries operational metadata is a Phase-4 feature
  choice, decided with the engine/turns surface.

### G10 — No player-seat id
*(Game · Design)*

- **v4:** membership carries a mutable `handle` (a name) and is keyed by
  `accountId`; there is **no immutable per-game player id**.
- **Adopted:** the docs' split makes the **player** a *seat in one game* with a
  "sequential, unique-in-game, never-reused id" — distinct from the account and
  from the faction (`CLAUDE.md` domain vocabulary; upstream `players.md`).
- **Lost / must change:** not a loss *from* v4 so much as something v4 never had
  and we now require. It is the prerequisite for fixing G5 (address the game
  surface by `player_id`, not `account_id`).
- **Disposition:** **Resolved — [ADR-0003](../decisions/adr-0003-api-resource-identity.md).**
  Introduce an immutable **`player_id`** (integer, sequential, unique-in-game,
  never reused) as the seat's sole identifier and the game-side key. `handle`,
  `player_N` defaulting, and the member-rename path are all removed. Enables G5
  (Phase 4).

### G11 — No cluster / system / faction read surface
*(Game · Gap)*

- **v4:** none — the engine surface is only turns and orders (all `501`).
- **Adopted:** the game has a cluster (hex map), systems, and factions the
  player commands (`CLAUDE.md` domain vocabulary & control-and-ownership).
- **Lost / must change:** nothing to port; flagged as an outright **absence** to
  design later, not a delta.
- **Disposition:** out of scope for the application pass; design with the engine
  surface.

---

## What survives cleanly

Worth lifting from v4 largely as-is, subject to the renames above:

- Account / game / member **CRUD shape** and the reusable `400/401/403/404/409`
  responses.
- The game **lifecycle state machine** — `draft → recruiting → active → paused →
  complete → archived`, forward-only with the admin-only `paused → active` and
  out-of-`archived` exceptions.
- The **validate / submit orders** split (two operations, dry-run vs commit).
- `generatedSecret` one-time return on account create; `schemaVersion` (SQLite
  `user_version`) on `/version`; the health/version probes.

## Next step

Plan the reconciliation. Suggested order: settle **G4** (auth model) and
**G7/G8/G10** (identity: game id, email, player id) first — they gate the rest —
then draft the **application** surface into `openapi.yaml`, and defer the
**game** surface (G5, G9, G11) to a second pass with the engine.
