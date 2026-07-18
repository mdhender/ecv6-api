# Cluster setup

Contributor reference for how the software runs and persists **turn-0 cluster
generation** — *how the back end fills a game's cluster and saves it*, not the
player-facing rules for what a cluster is. This is the workflow seam that drives
the Genesis generators (system contents and deposits, each documented separately)
and writes their output to the store. The rules the generators implement live
upstream and are the source of truth:

- [Cluster core](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md)
  — the shared schema (hex `(q, r)`, ten orbits, planet types, habitability, the
  three resources) every stage fills in.
- [Genesis family index](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/_index.md)
  — the staged generators and how a game records `(generator, version)` per stage.

Never restate the rules here; link them. This page is the workflow mechanism and
the store seam. See [`doc/README.md`](../README.md).

> **Implemented.** The workflow lives in [`internal/setup`](../../internal/setup)
> (`GenerateCluster`), over the world DTOs in `internal/domains` and the store
> accessors in `internal/store`. It is the single-hop domain-adapter pipeline
> settled in [ADR-0018](../decisions/adr-0018-project-shape-and-engine-store-boundary.md).
> This page describes the seam; symbol-level detail is godoc truth (package
> `setup`, `GenerateCluster`).

## The seam

`internal/setup.GenerateCluster` is the **turn-0 generator-invocation workflow**:
it resolves a game's seeds, drives the Genesis cluster generator off them, adapts
the result to store rows, and persists it in one pass. A future **E2** REST handler
is the caller.

It is **not** a CLI command, **not** a REST endpoint, and **not** the engine.
Turn-0 generation is a *generator* concern; the generator is not the engine, and
the engine — turn processing, from turn 1 onward — stays deferred
([ADR-0018](../decisions/adr-0018-project-shape-and-engine-store-boundary.md) §3).
Turn 0 is setup; play starts at turn 1.

## The pipeline

The workflow is the single-hop (Option A) pipeline of
[ADR-0018](../decisions/adr-0018-project-shape-and-engine-store-boundary.md) §4:

```
ensureSeeds → snapshotToDomain → genesis.GenesisCluster ClusterGenerator → domainToStore → persist
```

Three data shapes meet here — the **store** rows (`internal/store`, tuned for
persistence), the **domain** world DTOs (`internal/domains`, what the generator
produces and consumes), and the future **engine** turn state (not on this path).
The generator works in the domain shape directly, so a value is converted exactly
once at each store edge — loaded in, and mapped back out — never re-mapped inside.

- **`ensureSeeds`** resolves the game's master seeds (see [Seed policy](#seed-policy)).
- **`snapshotToDomain`** adapts the stored engine snapshot into the generator's
  domain input. At turn 0 the world is empty, so the snapshot is just the game's
  seeds and the generator fills an empty cluster from scratch.
- **`genesis.GenesisCluster`** is the monolithic `ClusterGenerator`: one call fills
  placement, system contents, and deposits into a `domains.Cluster` off those seeds
  and the GM's `worldgen.Knobs`.
- **`domainToStore`** (the `…ToStore` mappers in `mapping.go`) converts the
  generated cluster to store rows.
- **persist** writes them (see [Idempotency](#idempotency-and-no-partial-writes)).

The two generator stages this drives are documented on their own pages:
[System generation](system-generation.md) and [Deposits](deposit-generation.md).

## The adapters

Two adapters bracket the generator, one at each store edge:

- **`snapshotToDomain`** (in `snapshot.go`) is the **store → domain** (load)
  adapter. It is also the seam where a partial or in-play cluster would later load
  into a `domains.Cluster` before regeneration.
- **`domainToStore`** (in `mapping.go`) is the **domain → store** (persist)
  adapter. Each function is pure and total: it allocates store rows from an
  already-assembled cluster and never draws randomness or touches the database.

Adapters live in the workflow (`setup`) layer, never in the generator or the
store. An adapter is the only code that names both a component's shape and the
store's at once; keeping it uphill of both leaves keeps the generator and the
store-blind engine free of each other's types
([ADR-0018](../decisions/adr-0018-project-shape-and-engine-store-boundary.md) §4,
Option A). The orchestrator (`GenerateCluster`) reads the snapshot, runs the
generator, adapts the result back, and persists it in one pass.

## Seed policy

Seeds are assigned at **setup**, not at game creation, and the policy lives in the
setup layer rather than the store accessors
([ADR-0013](../decisions/adr-0013-engine-game-state-placement.md)). `ensureSeeds`
is **assign-if-missing / reuse-if-present**:

- A game with no `game_engine_state` row is assigned two fresh master seeds drawn
  from `math/rand/v2`, saved with `current_turn = 0`.
- A game that already has seeds reuses them unchanged.

Because generation draws only off the game's derived seeds and the resolved knobs,
the same seeds and knobs always produce the same rows, independent of the machine
or Go-map iteration order — so reusing seeds makes regeneration byte-identical. The
per-stage determinism (seed roots, key paths) is documented on the stage pages and
in [`doc/determinism.md`](../determinism.md).

## Idempotency and no partial writes

Generation runs **entirely in memory first**. An invalid or infeasible request
surfaces (as `genesis.ErrInvalidSettings` / `genesis.ErrInfeasible`) before any
write, so a bad run never disturbs existing data — an overshoot is the GM's
problem, with no engine fallback
([ADR-0014](../decisions/adr-0014-cluster-minimum-spacing-knob.md)).

The persist pass is **idempotent**: it clears any prior generation
(`DeleteGeneration`) and then writes child-after-parent so foreign keys resolve.
Regenerating a game replaces its rows outright — alpha data is disposable and
regeneration must be repeatable.

## Provenance

The pass records **three `game_generator` rows** per game, one per stage —
placement, system contents, deposits — each carrying the stage's generator
identity and version and the resolved knobs it ran as opaque JSON settings
([ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md),
[ADR-0017](../decisions/adr-0017-generator-identity-and-home-system-generation.md)).
Genesis is monolithic, so every row records a Genesis per-stage identity. There is
no home-system template: a home system is ordinary rows produced on demand at
founding (E3).

## See also

- [System generation](system-generation.md) — the system-contents stage this
  workflow drives.
- [Deposits](deposit-generation.md) — the deposits stage this workflow drives.
- [architecture.md](../architecture.md) — package layout, boundaries, request
  lifecycle.
- [ADR-0018](../decisions/adr-0018-project-shape-and-engine-store-boundary.md) —
  project shape, the generator≠engine boundary, and the single-hop domain-adapter
  model (Option A).
- [ADR-0013](../decisions/adr-0013-engine-game-state-placement.md) — setup-layer
  seed placement.
- [Cluster core](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md)
  — the shared schema the generators fill.
