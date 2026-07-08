# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What this project is

The Go server for **Epimethean Challenge (EC)** — a 4X, science-fantasy game.
This repository is an **API server** with **no front-end client**: it exposes a
RESTful API over two domains — the **application** (accounts, authentication,
authorization) and the **game** (the deterministic engine) — backed by a
**SQLite data store**. See [Architecture](#architecture--two-domains).

EC is a **distinct game**, reverse-engineered from the prose of the original
rulebook — not a port or clone of it. It deliberately diverges from the original
in **cluster generation, coordinate systems, turn structure, and order entry**,
and it is built around a **modern REST API** rather than the original's
snail-mail / play-by-email flow. When the original's rules and EC's docs disagree,
**EC's docs win** — do not "restore" original behavior from memory of the source
system.

This is the **sixth iteration (v6)**; no earlier iteration ever launched a
working API server, so treat inherited assumptions skeptically. The Go module now
exists — `cmd/ec` (server), `cmd/ecdb` (database admin), and `internal/store` have
landed — and we are building the server out incrementally under the
[Development rules](#development-rules). `doc/api/openapi-v4.yaml` is copied in
from the failed v4 iteration and is **not yet reviewed**.

> **Current phase: building the server incrementally.** Follow the
> [Development rules](#development-rules) below. The API surface is the exception:
> `doc/api/openapi-v4.yaml` is still the **unreviewed** v4 baseline, so do not
> implement handlers against it or change the API surface until it has been
> reconciled and you are asked to.

- Sibling repository: **`../docs`** (`github.com/mdhender/ecv6-docs`) — the Hugo
  documentation site. See [Relationship to the docs](#relationship-to-the-docs).
- Expected module path (follow the sibling's convention when running
  `go mod init`): `github.com/mdhender/ecv6-api`.
- Go 1.26+.

## Development rules

These rules are strict. Do not deviate from them without explicit approval.

Rules 1, 2, and 4 are quality and release discipline for **all** code in this
repo. Rule 3 (documentation) is a requirement on **game-engine features**
specifically — the engine that implements the game's rules; the application,
datastore, and API areas should be documented too, but are not yet held to it.

No game rules exist yet (the rulebook is being built up rule by rule), so there is
nothing to test at present. Once rules land, "green tests" means `go test ./...`
passes.

1. **All tests must be green before we push.** Run the full test suite and confirm
   it passes before any `git push`. Never push with failing, skipped, or unrun
   tests.
2. **Fix all bugs before introducing new features.** Known bugs take priority over
   new work. Do not start a new feature while there are open, unfixed bugs.
3. **Every game-engine feature must be grounded in the rulebook and documented.**
   Before implementing an engine feature, identify the game rule it implements in
   the **rulebook** — the sibling ecv6-docs repo under `content/reference/`
   (locally `../docs/content/reference/`; linked as
   [ecv6-docs](https://github.com/mdhender/ecv6-docs/tree/main/content/reference)).
   If the rule isn't there, stop and write (or request) it first — never build
   engine behavior the rulebook doesn't ground. Then document the *implementation*
   under `/doc` (see [Documentation structure](#documentation-structure)),
   following Diataxis.
4. **Bump the version before every push.** Bump `version.go`'s **Minor** (new
   feature) or **Patch** (bug fix) so every commit pushed to the remote carries a
   unique version; if it was already bumped since the last push, leave it as is.
   Uniqueness is measured on `Version().Core()` (Major.Minor.Patch) — build
   metadata does not count. We iterate frequently, so heavy version churn is
   expected. **Pure-documentation commits are exempt** — a commit that changes
   only docs (Markdown under `/doc`, `CLAUDE.md`, and the like; no Go source) needs
   no bump. The version tracks code, not our internal documentation.

## Relationship to the docs

**`../docs` is the source of truth for the rules of the game.** It documents
observable behavior — orders, costs, formulas, turn resolution, outcomes — for
players and referees. This repository implements that behavior.

- When a rule is unclear, read the reference under `../docs/content/reference/`
  before inventing behavior. Key pages: `games.md`, `turns.md`, `orders.md`,
  `players.md`, `cluster.md`, `determinism.md`, and `glossary.md`.
- The docs deliberately avoid implementation detail. This repo is where code
  structure, schemas, and package layout live — do not push those back into the
  docs.
- If you implement a rule that the docs don't yet specify, or find a conflict
  between code and docs, flag it. The two repositories are meant to agree.

**Workflow:** rules change in `../docs` first, then engine code follows. When
code and docs conflict, **the docs win** — fix the code, don't quietly change
the rule.

**Implementation detail migrates out of the docs.** The docs are player/referee-
facing; anything that isn't — determinism internals, seeding math, wire formats,
schemas — is pulled out of the docs repo and into this one (code plus `/doc`) as
we work. Determinism is the running example: its mechanism is not player-facing,
so it belongs here, and the docs' determinism page should shrink to the
player-facing promise (a game is reproducible from its seeds).

**Referencing the docs repo — path vs. URL.** Reading locally, use the sibling
checkout at `../docs`. But any **navigable link** in committed docs (this file,
`/doc/**`, ADRs) must use the repository URL, not a `../docs` relative path,
which breaks when the file is rendered on GitHub outside the local worktree:

> `https://github.com/mdhender/ecv6-docs/blob/main/content/...`

Naming the `../docs` sibling path in prose as a "go read this locally"
instruction is fine; it's live *links* that must be absolute.

## Documentation structure

This repo's docs are **contributor/operator-facing** ("how the software does
it"), the mirror of the docs repo's player/referee-facing rules. The discipline:
**never restate a rule — link to it.** Document only what the rules don't pin
down: wire formats, Go types, storage schema, package boundaries, engineering
contracts.

Four areas, by audience and home:

- **Game** — the rulebook: player/referee-facing rules. Lives in the sibling
  ecv6-docs repo (`content/reference/`), **not here**. Rule 3 above grounds engine
  features in it.
- **Engine** — how our code implements those rules (determinism, wire formats,
  types, invariants). Ours, under `/doc` and godoc.
- **Application** — running the server and managing the datastore. Ours, under
  `/doc`.
- **API** — the REST wire contract. Ours, spec-first under `/doc/api`.

Our docs (engine, application, API) follow **Diataxis**: every page is exactly one
of its four types — *tutorial* (learning by doing), *how-to* (achieving a stated
goal), *reference* (austere description of what is or does), or *explanation*
(context and why) — and never mixes them. Reference describes; explanation
discusses; split a page that tries to do both. When writing or restructuring docs,
use the `diataxis` skill.

In **how-to and tutorial** examples, use `games/example` as the database path (the
folder that holds `ec.db`), so runnable examples stay consistent across guides.

Mediums:

- **godoc is the reference truth.** Package overviews live in `doc.go`; type and
  function behavior lives in doc comments next to the code.
- **Markdown under `/doc` carries only cross-cutting narrative** that doesn't
  belong to a single package. (`/doc`, singular, so it doesn't read as the
  `../docs` repo.)
- **The REST API is spec-first.** `/doc/api/openapi.yaml` is the source of truth
  for the wire; handlers, clients, and request validation are driven from and
  checked against it. Change the spec first, then the code.
- **ADRs record hard-to-reverse decisions** under `/doc/decisions`, one short
  file each (Context / Decision / Consequences).

Layout:

```
/doc/architecture.md      package layout, boundaries, request lifecycle
/doc/model.md             concept ↔ Go type ↔ schema mapping + invariants
/doc/determinism.md       the PRNG mechanism spec: seeds, streams, key paths,
                          frozen surfaces, adding a tag, golden vectors
/doc/counter-based-prng.md  why the determinism design looks the way it does
/doc/api/openapi.yaml     the wire contract (source of truth)
/doc/api/conventions.md   auth, error envelope, versioning, idempotency
/doc/decisions/           adr-NNNN-*.md
/doc/how-to/              Diataxis how-to guides, e.g. create-and-verify-a-database.md
/doc/reference/           Diataxis reference docs, e.g. database-management.md
```

Determinism is a special case. Its mechanism (seeding math, key-path encoding,
domain-tag values) is **implementation detail, not player-facing**. It has been
migrated out of the docs and now lives here — `/doc/determinism.md` (spec),
`/doc/counter-based-prng.md` (rationale), plus code; the docs keep only the
player-facing promise. It remains a **frozen contract**: enforce it with the
append-only domain-tag registry (one constants block in code) and **golden test
vectors** (`(seed1, seed2, path) → output`) that fail CI on any addressing drift.
See [Determinism](#determinism--the-load-bearing-invariant).

## Domain vocabulary

Use these terms exactly as the docs define them; they are the ubiquitous language
of the codebase.

- **Game** — the top-level unit of play. Identified by its **integer PK** (the
  wire id `gameId`), with a human-facing `name` as its label; it also has a pair
  of `uint64` master seeds and a current turn. The cluster and the players belong
  to a game. (See [ADR-0003](doc/decisions/adr-0003-api-resource-identity.md); an
  earlier GM-chosen-slug id was dropped. Upstream `games.md` still to reconcile.)
- **Turn** — the unit a game advances by. **Turn 0** is setup (no turn, the zero
  value); play begins at **turn 1** and counts up. A turn's report describes the
  state at the *start* of the turn, before its orders are applied.
- **Orders** — plain-text instructions a player issues for a turn. Applied
  together when the GM *processes* the turn; the current turn does not advance
  until the GM advances it.
- **Cluster** — the map: a grid of flat-top hexes centered on the origin `(0,0)`,
  addressed by axial coordinates `(q, r)`. Generated once, at setup.
- **System** — the contents of a single occupied hex, addressed by its `(q, r)`.
- **Player** — a person in a game (docs sense). Scoped to one game. In the
  implementation this maps to an [Account](#application-model) joined to a game
  via a `game_account_role` membership — see the reconciliation note below.
- **GM** — the game master; the operator who generates and runs a game. In the
  implementation, a member whose `is_gm` is set.

> **Docs vs. implementation.** The docs' single "player" was split three ways:
> the **account** (global login — email, secret), the **player** (a seat in *one*
> game — the per-game id and GM flag; = `game_account_role`), and the **faction**
> (the in-game entity commanded), with `player` and `faction` meaning the same
> thing in both repos. This vocabulary is now **adopted upstream** (ecv6-docs has
> `account.md`, `players.md`, `faction.md`). It resolved the "sequential,
> unique-in-game, never-reused id": that's the **player (seat)** id, not the
> account id. The faction **lifecycle** (founding, independence, persistence) has
> since been adopted upstream too — with engine internals (NPC/controller) kept
> out of the player docs.

## Architecture — two domains

The server is split into two domains that do not know each other's internals:

- **Application** — owns account management, authentication, and authorization.
  It decides *who* may act, and on *which* games.
- **Game** (the *engine*) — owns game logic and is **unaware of the application
  side**. The server invokes the engine by passing in the game's data plus a
  command; the engine reads game state as needed, does its work, updates game
  state, and exits. It has no notion of accounts, HTTP, or auth.

**The data store belongs to neither domain — an unresolved tension.** Both the
application and the engine need persistence, but neither "owns" the store. Don't
assume this is settled; when a change forces the question, surface it rather than
quietly coupling a domain to storage.

## Application model

Concepts owned by the application domain. These are *additive* to the docs'
game vocabulary (above) and not final — more actions get flushed out when we
review the v4 API.

- **Account** — `(id, email, is_admin, is_active, hashed_secret)`. `id` is the
  integer PK; `email` is stored lowercased and is unique; the secret is **hashed**,
  never stored plainly.
- **Application roles:** `admin` and `user`. An **admin** can manage accounts and
  games.
- **Game (application record)** — `(id, name, is_active)`. An admin creates a
  game and assigns players.
- **Membership** — the bridge table `game_account_role`
  `(game_id, account_id, is_gm, is_active)`, unique on `(game_id, account_id)` so
  an account cannot join a game twice. A member with `is_gm` set may manage the
  game and its players (add/remove).
- **Soft deletes preferred.** Almost all tables use `is_active = false` rather
  than hard deletes; players are made inactive, not removed.

## Control and ownership

Full model: [`doc/control-and-ownership.md`](doc/control-and-ownership.md). The
essentials:

- **Controllers control; factions own.** A **controller** (a `player`, or an
  engine-driven `npc`) issues orders each turn for the faction(s) it controls; the
  **faction** owns the assets. A player never owns anything, so a player leaving
  cuts only the control link — the faction and its assets persist.
- **Chain:** `controller ─||──o< faction ─||──o< ship_or_colony ─┬─o< population └─|< inventory`.
  Every child has exactly one mandatory parent: nothing is un-owned.
- **Factions** are created by a player's setup-ritual API call after joining,
  never die (may hit zero assets), and are never orphaned — on a player leaving,
  the engine assigns an NPC.
- **Domain boundary by id:** `account_id` app-only (engine never sees it);
  `game_id`, `player_id` game-consumed; `faction_id` engine-owned. The engine
  knows control by `player_id`, never by account.

## Game state — a temporal model

Game state follows Martin Fowler's
[temporal patterns](https://martinfowler.com/eaaDev/timeNarrative.html), with one
adaptation: **the game turn is the "effective date"**, not a wall-clock
timestamp. There are **no audit logs and no event streaming** — the turn number
is the axis of time.

- Historical queries take an **`asOf` turn**: "the state as of turn N."
- This matches the docs' rule that a turn's report reflects the state at the
  *start* of the turn.

## Determinism — the load-bearing invariant

The engine is **deterministic**: the same master seeds reproduce the same game,
on any machine. This is not a nice-to-have; it is the core contract. The spec now
lives here — read [`doc/determinism.md`](doc/determinism.md) (the mechanism) and
[`doc/counter-based-prng.md`](doc/counter-based-prng.md) (the rationale) in full
before touching anything that draws random numbers. The docs repo keeps only the
player-facing promise.

The mechanism (counter-based / spawn-keyed PRNG):

- A game has two `uint64` master seeds, `seed1` and `seed2` — the root of all
  randomness.
- Randomness is drawn from **streams** addressed by a **key path**, not from one
  shared advancing sequence. `Seeds.Stream(path ...Key)` where `Key` is `int64`.
  A stream is a pure function of the seeds and its address.
- The first element of every path is a **domain tag** naming the stream's
  purpose; the rest identify the instance (e.g. a system's `(q, r)`, a player's
  `id`).
- Derivation: SHA-256 over `seed1`, `seed2`, the path length, then each path
  element — all big-endian `uint64` — and the first 16 bytes seed a PCG source
  (`math/rand/v2`). `Seeds.Derive(path ...Key)` draws a child `(seed1, seed2)`
  for a subsystem.

**Frozen surfaces — do not change these while any game exists** (they are a
compatibility surface, like a save-file format):

- The **key-path encoding**: element order, how ids/coordinates coerce to `Key`,
  and the length prefix.
- The **domain-tag numbering**: one enumerated block starting at `1` (`0` is
  invalid), **append-only**. Never reorder or insert a constant — `iota` would
  renumber the rest and silently rewrite every existing game. Add new tags only
  at the end.

**Rules that follow from this:**

- Address a draw by the item's own identity (its coordinates or id), never by
  iteration order. **Never range over a Go map when the iteration order would
  determine the order or addressing of draws** — map order is nondeterministic.
- Two draws that share an address share a stream and produce correlated results.
  Keep domain tags distinct and instance addresses unique within a domain.
- A subsystem stores its derived seeds with its own data, so it can reproduce its
  randomness standalone — convenient for tests. But derived data belongs to the
  game that created it; production games never share data.

## Go conventions

- Standard library first. Prefer `math/rand/v2` (PCG), `crypto/sha256`,
  `encoding/binary` (big-endian) as the determinism design assumes.
- Return errors, wrap with `%w` for context; don't panic in library code.
- Keep the engine deterministic and side-effect-free where it can be — separate
  computing a turn's result from persisting it.
- Format with `gofmt`; vet with `go vet`. Run `go test ./...` before considering
  a change done.

## Data store

- **SQLite 3** via **`github.com/zombiezen/go-sqlite`** (ZombieZen) — **not**
  `modernc`. Migrations use ZombieZen's **`sqlitemigration`** package.
- **Alpha, so data is disposable.** We rebuild from data **files** at will and may
  **compress/squash migration files** rather than preserve their history. No alpha
  SQLite data is precious.
- **Data directories:** `data/claude/` is **yours** — destructive tests are fine
  there. The user works in `data/alpha/` and `data/ec01/`; leave those alone.
- **Environment:** set the binary's `*_ENV` variable to `claude` so you never
  clobber the user's environments — `ECDB_ENV=claude` for `cmd/ecdb`, `EC_ENV=claude`
  for `cmd/ec`. Each binary uses its **own** env-var prefix (see
  [Commands & configuration](#commands--configuration)); the dotenv loader is wired
  in.

## Commands & configuration

CLI is built with **`github.com/peterbourgon/ff/v4`** (commands + subcommands).
Each binary uses its **own** env-var prefix — `cmd/ecdb` → **`ECDB_`**, `cmd/ec` →
**`EC_`** — bound to flags via `ff.WithEnvVarPrefix`. The selector for which
`.env*` files load is that same prefix plus `_ENV` (`ECDB_ENV`, `EC_ENV`), read
directly before flag parsing. Files are loaded by the `internal/dotenv` package on
startup.

Two binaries:

- **`cmd/ecdb`** — runs commands **directly against the database**, assuming it is
  the *only* process touching it. **Database creation is `ecdb`'s job** (rebuilt
  from data files).
- **`cmd/ec`** — starts and stops the **server**. It **runs migrations
  automatically whenever it opens the database.** **Crucially, `ec` must never
  create a new *persistent* database** — if the persistent DB file is missing it
  must fail, not create one. It *may* spin up an *in-memory* database for testing.
  (Creating the persistent database is `ecdb`'s responsibility.)

_No Go source or module exists yet; when it lands, the usual `go build ./...`,
`go test ./...`, `go vet ./...`, `gofmt`. Update this section with real entry
points as they arrive._

## Licensing

Code here is under the MIT `LICENSE` (© 2026 Michael Henderson).

The original game system and materials that EC is based on are the property of
James Colombo and are used as reference with permission; this repository grants
no rights in that original protected material. See the docs' `NOTICE.md` for the
full notice.

Note: refer to this game only as **EC** or **Epimethean Challenge** in all code
and documentation — never by the name of the original system it derives from.
