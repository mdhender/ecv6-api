# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What this project is

The Go server for **Epimethean Challenge (EC)** — a 4X, science-fantasy game.
This repository holds the running system: the **API server**, the **data store**,
and the **game engine** that generates clusters, manages players, and processes
turns.

EC is a **distinct game**, reverse-engineered from the prose of the original
rulebook — not a port or clone of it. It deliberately diverges from the original
in **cluster generation, coordinate systems, turn structure, and order entry**,
and it is built around a **modern REST API** rather than the original's
snail-mail / play-by-email flow. When the original's rules and EC's docs disagree,
**EC's docs win** — do not "restore" original behavior from memory of the source
system.

This is a **greenfield** codebase — as of this writing there is no Go source yet.
When you add the first packages, update this file to describe the actual layout
rather than leaving it aspirational.

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
- **Player** — a person in a game. Scoped to one game. Has a sequential positive
  integer `id` (assigned in order, never reused), a lowercased unique `email`, an
  active/inactive state (never physically deleted — removing marks inactive), and
  a plaintext password (a JSON-safe, space-free shared secret).
- **GM** — the game master; the operator who generates and runs a game.

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

## Commands

_No build/test tooling exists yet. Once the module is initialized, the usual:_

```sh
go mod tidy
go build ./...
go test ./...
go vet ./...
```

Update this section with the real entry points (server binary, CLI, migrations)
as they land.

## Licensing

Code here is under the MIT `LICENSE` (© 2026 Michael Henderson).

The original game system and materials that EC is based on are the property of
James Colombo and are used as reference with permission; this repository grants
no rights in that original protected material. See the docs' `NOTICE.md` for the
full notice.

Note: refer to this game only as **EC** or **Epimethean Challenge** in all code
and documentation — never by the name of the original system it derives from.
