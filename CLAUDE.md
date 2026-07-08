# CLAUDE.md

Guidance for Claude Code in this repository. This file is deliberately thin: it
carries the **rules you must follow** and a **map to the detailed docs**. Pull in
the referenced `/doc` pages as a task needs them rather than assuming their
contents.

## What this project is

The Go **API server** for **Epimethean Challenge (EC)** — a 4X, science-fantasy
game. No front-end client. It exposes a REST API over two domains — the
**application** (accounts, auth, membership) and the **game** (the deterministic
engine) — backed by **SQLite**. See [`doc/architecture.md`](doc/architecture.md).

EC is a **distinct game**, reverse-engineered from the original rulebook's prose —
not a port or clone. It diverges deliberately (cluster generation, coordinates,
turn structure, order entry) and is built around a modern REST API. **When the
original system and EC's docs disagree, EC's docs win** — never "restore" original
behavior from memory.

This is the **sixth iteration (v6)**; no earlier iteration launched a working
server, so treat inherited assumptions skeptically. The module
(`github.com/mdhender/ecv6-api`, Go 1.26+) now exists with `cmd/ec` (server),
`cmd/ecdb` (database admin), and `internal/{store,cli,dotenv,cerrs,phrases}`.
We are building the server out incrementally under the rules below.

The **rules of the game** live in the sibling **`../docs`** repo
(`github.com/mdhender/ecv6-docs`), the Hugo documentation site — the source of
truth. See [Relationship to the docs](#relationship-to-the-docs).

## Development rules

Strict. Do not deviate without explicit approval. Rules 1, 2, 4 apply to **all**
code; rule 3 applies to **game-engine features** specifically.

1. **All tests green before push.** Run the full suite (`go test ./...`) and
   confirm it passes before any `git push`. Never push with failing, skipped, or
   unrun tests. (No game rules exist yet, so there is little engine to test until
   rules land.)
2. **Fix all bugs before new features.** Known bugs take priority; do not start a
   feature while there are open, unfixed bugs.
3. **Every game-engine feature is grounded in the rulebook and documented.**
   Before implementing engine behavior, find the rule it implements in the
   rulebook (`../docs/content/reference/`). If it isn't there, stop and write (or
   request) it first — never build engine behavior the rulebook doesn't ground.
   Then document the *implementation* under `/doc` (Diataxis; see
   [`doc/README.md`](doc/README.md)).
4. **Bump the version before every push.** Bump `version.go`'s **Minor** (feature)
   or **Patch** (fix) so each pushed commit carries a unique `Version().Core()`
   (Major.Minor.Patch; build metadata doesn't count). If already bumped since the
   last push, leave it. **Pure-documentation commits are exempt** (only Markdown
   under `/doc`, `CLAUDE.md`, and the like; no Go source).

## Relationship to the docs

**`../docs` is the source of truth for the rules of the game** — observable
behavior (orders, costs, formulas, turn resolution) for players and referees. This
repo implements that behavior.

- Read the reference under `../docs/content/reference/` before inventing behavior:
  `games.md`, `turns.md`, `orders.md`, `players.md`, `account.md`, `faction.md`,
  `cluster.md`, `determinism.md`, `glossary.md`.
- **Rules change in `../docs` first, then engine code follows.** When code and
  docs conflict, the docs win — fix the code, don't quietly change the rule. If a
  rule is missing or the two disagree, flag it.
- **Implementation detail migrates out of the docs** into here (code + `/doc`):
  determinism internals, wire formats, schemas. The docs stay player/referee-facing.
- **Links vs. paths.** Naming `../docs` in prose ("go read this locally") is fine,
  but any **navigable link** in committed files must use the repository URL, not a
  `../docs` relative path (which breaks when rendered on GitHub):
  `https://github.com/mdhender/ecv6-docs/blob/main/content/...`.

## Documentation map (`/doc`)

Our docs are **contributor/operator-facing** ("how the software does it"). The
discipline: **never restate a rule — link to it.** Full index and conventions in
[`doc/README.md`](doc/README.md). Follow **Diataxis** (each page is exactly one of
tutorial / how-to / reference / explanation); use the `diataxis` skill when
writing docs. **godoc is the reference truth** (package `doc.go`, doc comments);
Markdown under `/doc` carries only cross-cutting narrative. In how-to/tutorial
examples use `games/example` as the database folder.

Pull these in as needed:

| Topic                                                              | Read                                                                                 |
|--------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| Package layout, boundaries, request lifecycle                      | [`doc/architecture.md`](doc/architecture.md)                                         |
| Concept ↔ Go type ↔ schema, store invariants                       | [`doc/model.md`](doc/model.md)                                                       |
| Who controls/owns what (account→player→faction→asset)              | [`doc/control-and-ownership.md`](doc/control-and-ownership.md)                       |
| Temporal state model (turn = the coordinate)                       | [`doc/storing-state-as-timebound-facts.md`](doc/storing-state-as-timebound-facts.md) |
| Determinism mechanism (seeds, streams, key paths, frozen surfaces) | [`doc/determinism.md`](doc/determinism.md)                                           |
| Why determinism looks that way (rationale, prior art)              | [`doc/counter-based-prng.md`](doc/counter-based-prng.md)                             |
| REST wire contract (source of truth)                               | [`doc/api/openapi.yaml`](doc/api/openapi.yaml)                                       |
| API auth, error envelope, versioning, idempotency                  | [`doc/api/conventions.md`](doc/api/conventions.md)                                   |
| Hard-to-reverse decisions                                          | [`doc/decisions/`](doc/decisions/) (ADRs)                                            |
| Database file, `ecdb` commands, migrations                         | [`doc/reference/database-management.md`](doc/reference/database-management.md)       |

## Key invariants (don't violate; details in the linked docs)

- **Determinism is the load-bearing contract.** Same master seeds → identical game
  on any machine, independent of draw or Go-map iteration order. The key-path
  encoding and the append-only domain-tag numbering are **frozen surfaces** — never
  change them while any game exists. Read [`doc/determinism.md`](doc/determinism.md)
  **before** touching anything that draws randomness.
- **Two domains stay separate.** The engine is unaware of the application side (no
  accounts, HTTP, or auth). The boundary is a small set of ids: `account_id` is
  app-only (the engine never sees it); `game_id` and `player_id` are game-consumed;
  `faction_id` is engine-owned. See
  [`doc/control-and-ownership.md`](doc/control-and-ownership.md).
- **Controllers control; factions own.** A player (or NPC) controls a faction; the
  faction owns the assets. A player leaving cuts only the control link.
- **Turn is the axis of time.** Turn 0 is setup; play starts at turn 1. A turn's
  report reflects state at the *start* of the turn. No audit logs / event streams —
  historical queries take an `asOf` turn.
- **Soft deletes preferred.** Almost all tables use `is_active = false`, not hard
  deletes.

## Go conventions

- Standard library first: `math/rand/v2` (PCG), `crypto/sha256`,
  `encoding/binary` (big-endian) — the determinism design assumes these.
- Return errors, wrap with `%w`; don't panic in library code.
- Keep the engine deterministic and side-effect-free where it can be — separate
  computing a turn's result from persisting it.
- `gofmt`, `go vet`, `go test ./...` before considering a change done.

## Data store

- **SQLite 3** via **`zombiezen.com/go/sqlite`** (ZombieZen) — **not** modernc.
  Migrations use ZombieZen's `sqlitemigration`, forward-only
  ([ADR-0007](doc/decisions/adr-0007-forward-only-migrations.md)).
- **Alpha, so data is disposable.** We rebuild from data files at will and may
  squash migration files. No alpha SQLite data is precious.
- **Data directories:** `games/claude/` is **yours** — destructive tests are fine
  there. Leave `games/alpha/` and `games/zephyr` (the user's) alone.
- **Use the `claude` environment** so you never clobber the user's setup:
  `ECDB_ENV=claude` for `cmd/ecdb`, `EC_ENV=claude` for `cmd/ec`.

## Commands & configuration

CLI is built with **`github.com/peterbourgon/ff/v4`**. Each binary has its own
env-var prefix bound to flags via `ff.WithEnvVarPrefix` — `cmd/ecdb` → `ECDB_`,
`cmd/ec` → `EC_` — and the `_ENV` selector (`ECDB_ENV`, `EC_ENV`) picks which
`.env*` files `internal/dotenv` loads on startup.

- **`cmd/ecdb`** — runs directly against the database, assuming sole access.
  **Creating the persistent database is `ecdb`'s job** (`ecdb create`). See
  [`doc/reference/database-management.md`](doc/reference/database-management.md).
- **`cmd/ec`** — starts/stops the server; runs migrations automatically on open,
  but **must never create a persistent database** (fail if the file is missing).
  It may spin up an in-memory database for testing.

Build/test: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt`; or the
`Makefile` targets (`make build`, `make test`, `make generate`, `make verify`).
The REST surface is **spec-first**: `doc/api/openapi.yaml` is the source of truth;
`make generate` produces `internal/api/openapi.gen.go` and `make verify` fails on
drift. `doc/api/openapi-v4.yaml` is the historical v4 baseline (see
[`doc/api/v4-gap-analysis.md`](doc/api/v4-gap-analysis.md)); the application
surface is drafted and engine endpoints are deferred — don't add engine endpoints
until asked.

## Domain vocabulary

Use the docs' ubiquitous language exactly (`game`, `turn`, `orders`, `cluster`,
`system`, `player`, `GM`, `faction`); definitions live in the rulebook glossary
and [`doc/model.md`](doc/model.md). The one split to remember: the docs' single
"player" maps in the implementation to **account** (global login), **player** (a
seat in one game = `game_account_role`, carries the GM flag), and **faction** (the
in-game entity commanded). This vocabulary is now adopted upstream. See
[`doc/control-and-ownership.md`](doc/control-and-ownership.md).

## Licensing

Code is MIT (`LICENSE`, © 2026 Michael Henderson). The original game system EC is
based on is the property of James Colombo, used as reference with permission; this
repo grants no rights in that material (see the docs' `NOTICE.md`). **Refer to the
game only as EC or Epimethean Challenge** — never by the name of the original
system.
