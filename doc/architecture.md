# Architecture

> **Status: stub.** Fill in as the first packages land; this file describes the
> intended shape, not existing code.

## Layers

The server separates three concerns named in the CLAUDE.md:

- **API server** — the REST surface. Spec-first (the `api/openapi.yaml` contract
  lands once the v4 API is reconciled). Handlers translate wire ↔ domain and do
  no game logic.
- **Game engine** — deterministic, side-effect-free game logic: cluster
  generation, order processing, turn resolution. Computes results; does not
  persist them. See [determinism.md](determinism.md).
- **Data store** — persistence of games, players, clusters, turns, and orders.
  Enforces the model invariants in [model.md](model.md).

## Boundaries

- The engine computes a turn's result from inputs; the store persists it. Keep
  those separable so a scenario can be exercised without standing up a whole game
  (subsystems carry their own derived seeds).
- Handlers depend on the engine and store; the engine depends on neither.

## Request lifecycle

_TODO once endpoints exist: auth → validate against OpenAPI → domain call →
persist → response envelope._
