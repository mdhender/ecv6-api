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
working API server, so treat inherited assumptions skeptically. There is **no
committed Go source yet** — `doc/api/openapi-v4.yaml` is copied in from the failed
v4 iteration and is **not yet reviewed**.

> **Current phase: planning and documentation.** Do **not** write code, scaffold
> packages, run migrations, or modify the API surface (including the v4 OpenAPI)
> until explicitly asked. We are settling docs and design first.

- Sibling repository: **`../docs`** (`github.com/mdhender/ecv6-docs`) — the Hugo
  documentation site. See [Relationship to the docs](#relationship-to-the-docs).
- Expected module path (follow the sibling's convention when running
  `go mod init`): `github.com/mdhender/ecv6-api`.
- Go 1.26+.

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

- **Game** — the top-level unit of play. Has an id (a short JSON-safe,
  space-free slug the GM chooses), a pair of `uint64` master seeds, and a current
  turn. The cluster and the players belong to a game.
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

> **Docs vs. implementation — reconciliation in progress.** Three concepts were
> being crushed into "player": the **account** (global login — email, secret),
> the **player** (a seat in *one* game — the per-game id and GM flag;
> = `game_account_role`), and the **faction** (the in-game entity commanded). A
> three-tier vocabulary — `account` / `player` / `faction`, with `player` and
> `faction` meaning the same thing in both repos — has been proposed to the docs
> team (`docs-prompt.md`) and is **pending their adoption**. This resolves the
> "sequential, unique-in-game, never-reused id": it's the **player (seat)** id,
> not the account id. Until the docs team confirms, treat the docs' single
> "player" as ambiguous.

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
- **Environment:** set `ECV6_ENV=claude` so you never clobber the user's
  environments. A dotenv loader will be added later (you'll be told); env vars use
  the `ECV6_` prefix.

## Commands & configuration

CLI is built with **`github.com/peterbourgon/ff/v4`** (commands + subcommands).
Environment variables use the **`ECV6_`** prefix, loaded by the dotenv package on
startup once it lands.

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
