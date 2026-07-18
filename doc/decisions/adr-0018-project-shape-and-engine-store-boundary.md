# ADR-0018: Project shape — thin API over generators and an engine that never sees the store

- **Status:** accepted
- **Date:** 2026-07-18

## Context

The pieces this repository builds — three commands, an OpenAPI-described REST
surface, a set of generators, a turn engine, and adapters between them — have each
been decided in earlier ADRs and described in scattered godoc and `/doc` pages.
What has *not* been written down in one place is the **overall intent**: what we
are building and how the parts fit. New work (and new readers) keep re-deriving
the shape from the code. This ADR records that shape so the boundaries below are a
stated commitment, not folklore.

Nothing here is new policy. It names the arrangement the existing ADRs already
imply — the two-domain split ([ADR-0013](adr-0013-engine-game-state-placement.md)),
the standard-library HTTP server ([ADR-0011](adr-0011-standard-library-http-server.md)),
spec-first routing ([ADR-0006](adr-0006-openapi-version-tooling-and-routing.md)),
and the generator model
([ADR-0016](adr-0016-core-rulebook-and-generator-supplements.md),
[ADR-0017](adr-0017-generator-identity-and-home-system-generation.md)) — and fixes
the one boundary that ties them together: **the engine and the generators know the
game's shape, never the store's.**

## Decision

### 1. Three commands, one server

- **`cmd/ec`** runs the API server. It is the only long-lived process. It opens
  (never creates) the database, runs forward migrations
  ([ADR-0007](adr-0007-forward-only-migrations.md)), and serves the REST surface.
- **`cmd/ecdb`** is the database-management utility: it *creates* the persistent
  database and performs offline admin against it, assuming sole access
  ([ADR-0008](adr-0008-ecdb-create-filesystem-checks.md)).
- **`cmd/earl`** is a thin REST client used to exercise the API server by hand or
  in scripts — the HTTP verb and path become the command line
  (`earl get /me`), so it covers the whole surface without per-endpoint code.

All three share the `ff` + `<PREFIX>_` env convention (`EC_`, `ECDB_`, `EARL_`)
and an `_ENV` selector for `.env*` loading.

### 2. The API server is thin; work lives under `internal/`

The REST surface is **spec-first**: `doc/api/openapi.yaml` is the contract, and
generated code (`internal/api`) carries the DTOs and route wiring
([ADR-0006](adr-0006-openapi-version-tooling-and-routing.md)) over a
standard-library `net/http` mux ([ADR-0011](adr-0011-standard-library-http-server.md)).
**Handlers translate wire ↔ domain, validate, and call into `internal/` packages;
they contain no game logic.** The request lifecycle is fixed and lives in
[architecture.md](../architecture.md).

### 3. The game is not one package — it is generators plus an engine

Game behavior is split by responsibility, not welded into a single package:

- **Generators** build turn-0 state. Each is a `ClusterGenerator` /
  `SystemGenerator` / `PlanetGenerator` selected by the GM and driven by typed
  `Knobs` (`internal/worldgen`, `internal/genesis`;
  [ADR-0016](adr-0016-core-rulebook-and-generator-supplements.md),
  [ADR-0017](adr-0017-generator-identity-and-home-system-generation.md)).
- **The engine** advances play: it takes an existing game state, mutates it, and
  reports what it could not do.

Each of these components defines its **own** data structures, shaped for its
algorithm — the generated `*worldgen.Cluster` and its kin, and (as it lands) the
engine's own turn state. There is no single shared "domain" struct they all agree
on: a generator's cluster is not a store row, and the engine's working state is
neither. The store, in turn, has its own row shapes tuned for persistence. These
shapes are deliberately different, so something has to translate between them.

### 4. Domain adapters translate the store's shape ↔ each component's shape

That translation is the **domain adapters'** job — today the `…ToStore` mappers in
`internal/setup`. An adapter converts, in both directions, between the store's rows
and *one component's own structs* (store ↔ `worldgen`, and later store ↔ engine
state). Neither the generators nor the engine reference `internal/store` types in
their own logic; the adapter is the only code that names both shapes at once. The
adapters are pure and total: they allocate one shape from the other and never draw
randomness or touch the database. The setup orchestrator is what reads a snapshot
from the store, runs a generator, adapts the result back, and persists it in one
pass.

### 5. The engine takes a turn snapshot, mutates it, returns errors — and has no store

The engine's contract is deliberately narrow:

- It receives a **snapshot of the game as of a given turn** — the state the store
  holds, already adapted into the engine's shape.
- It **mutates that snapshot** and **returns any errors**. It computes; it does
  not persist. The caller adapts the mutated snapshot back and writes it.
- It has **no knowledge of the store** — no database handle, no SQL, no store
  types. Its inputs are exactly the game ids and game state it is meant to see
  ([ADR-0013](adr-0013-engine-game-state-placement.md)), which keeps it
  deterministic and testable without standing up a database.

The engine need not be a **single package**. It may prove better to split it —
for example, one package per command (order type) it resolves — and the
implementation is free to make that call. What is fixed is the **contract**, not
the packaging: **snapshot → adapt → mutate → adapt → update**. However it is
decomposed, every part draws from the same in-adapted snapshot, mutates the
engine's shape, and stays store-blind; the adapters at the edge are what turn
store rows into that snapshot and back.

Generators follow the same snapshot contract, with one constant: **their snapshot
is always as of turn 0.** Turn 0 is setup; play starts at turn 1, and only the
engine runs from turn 1 onward.

## Consequences

- **The store boundary is a stated invariant, not a habit.** A generator or
  engine change that reaches for a `store` type is now a boundary violation with an
  ADR to point at, not a matter of taste. The domain adapters are the single seam
  where a component's shape and the store's meet.
- **The engine is unit-testable in isolation.** Snapshot in, mutated snapshot and
  errors out — no database, no fixtures beyond the domain structs. This is the
  "compute separately from persist" posture (CLAUDE.md) made concrete.
- **`earl` keeps pace with the API for free.** Because it mirrors verb+path rather
  than coding each endpoint, new routes are reachable the moment the spec and
  handlers land — so the API server has a ready-made way to be driven end-to-end.
- **The adapter layer is load-bearing and will grow.** Every new domain concept
  the engine or a generator manipulates needs a mapper on both sides. Today these
  live in `internal/setup`; as the engine lands, engine-facing adapters will sit
  alongside (an `internal/domains` DTO home is anticipated but not yet built). This
  is accepted cost: the alternative — letting the engine read store rows — trades a
  little translation code for a coupling we have decided not to have.
- **No new frozen surfaces.** This ADR records structure, not wire or determinism
  encoding. Package placement and adapter shape may change by ordinary refactor;
  only the `internal/prng` addressing and the OpenAPI contract carry
  can't-change-once-live weight, and those are governed elsewhere.

## Background: adapters in Go (Option A)

This section records *why* the adapters in decision 4 are shaped the way they are,
so a future reader does not re-litigate it. Two things decide it: Go's import rules
and the store-blind invariant this ADR already commits to.

**The deciding constraint.** Go forbids import cycles, so the package graph must be
a DAG. And the engine must be store-blind — no store types, no SQL. Put those
together and the store ↔ engine mapping *cannot* live in the engine package: naming
store types there would make `engine` import `store`, breaking the invariant. It
also should not live in `store` — hanging `row.ToEngine()` methods on store types
teaches the persistence leaf about a package downstream of it. So the conversion
code has to sit in a package that imports **both** sides and is imported by nobody
below it:

```
store       (row types)        ─┐
worldgen    (generator types)  ─┤  leaves: mutually unaware
engine      (engine types)     ─┘  imports neither store nor worldgen
        ▲
        │ imports both
   setup / adapters  ── holds StoreToEngine / EngineToStore
        ▲
   server / handlers
```

We call this **Option A**: the adapter package depends on `store` and `engine`,
maps between them, and keeps `store` and `engine` as leaves that never import each
other. The invariant becomes compiler-enforced rather than a matter of discipline.
`internal/setup/mapping.go` already *is* this pattern for `store` ↔ `worldgen`; the
engine reuses it.

**The alternative, and its cost.** *Option B* is the hexagonal/DDD move: a neutral
`domains` package holds a shared model, the engine imports it, accepts a
`domains.GameState`, remaps internally to performant structs, mutates, and maps
back to `domains.GameState`. It is legal Go and store-blindness still holds (the
engine only touches neutral types). But count the mappings per turn:

- **Option A** — store row → engine's own (performant) type → back. **One map each
  way**; the engine's public types *are* the performant ones.
- **Option B** — store row → domain → engine-internal → mutate → engine-internal →
  domain → store row. **Two maps each way, every turn.**

That inner remap earns its keep only if the neutral model has *other* consumers —
the API layer serializing the same model, or several engines over one
representation. When the engine is the sole consumer of its own snapshot, Option B
pays a double conversion to keep the adapter at arm's length from engine types it
is already entitled to see.

**Idiom notes that fall out of this.**

- Conversion lives "uphill," never on the leaf types: free functions in the adapter
  package (`func StoreToEngine(store.Turn) engine.State`), not `store.Turn.ToEngine()`
  (dirties `store`) and not `engine.FromStore(...)` (makes `engine` import `store`).
- This is a *value*-mapping seam, not a behavioral port. "Accept interfaces" is the
  other classic Go boundary, but that is for a live data source the engine calls
  back into. The engine deliberately takes a snapshot value in and returns a mutated
  value out (decision 5), so mapping functions — not an interface the store
  implements — are the right tool.
- The engine owns its performant types directly and the adapter maps
  `store → engine` in one hop; no neutral tier is introduced just to keep the
  adapter away from types it may touch.
- One adapter package or several is free to choose (`internal/setup`, or a
  per-consumer split like `internal/enginestore`) — the same packaging freedom
  decision 5 grants the engine itself, bounded only by the no-cycle rule.

**We adopt Option A** as the more idiomatic approach here. The store-blind invariant
already selects it, and it avoids Option B's per-turn double conversion. A neutral
`domains` model stays a reserved, additive refactor for the day a second consumer of
that model actually appears — not a cost paid up front.

## Alternatives considered

- **One `internal/engine` package holding game logic *and* its persistence.**
  Rejected: it would put store types in the engine's hands and forfeit the
  isolation in decision 5 — the very coupling ADR-0013 keeps out of the schema
  would reappear in the code.
- **Handlers call the engine/store directly with per-endpoint translation.**
  Rejected: it scatters wire ↔ domain ↔ store translation across handlers and
  couples the REST surface to storage shape. The thin-handler + adapter split keeps
  each translation in exactly one place.
- **Skip the ADR; leave the shape implicit in godoc.** Rejected: the boundary in
  decisions 4–5 is exactly the kind of thing that erodes silently. Writing it down
  makes a future violation reviewable.
