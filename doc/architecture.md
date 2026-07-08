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

The application server (`internal/server`) handles a request in a fixed order; the
authoritative, code-level description is that package's godoc, and the
cross-cutting wire rules are in [api/conventions.md](api/conventions.md).

1. **Correlate.** The outer middleware assigns a correlation id (reusing an
   inbound `X-Request-Id`, else minting one), echoes it in the response header,
   installs a request-scoped logger, and guards the request with panic recovery.
2. **Route.** The standard-library `net/http` mux ([ADR-0011](decisions/adr-0011-standard-library-http-server.md))
   dispatches on method + path under the unversioned base path `/api`
   ([ADR-0006](decisions/adr-0006-openapi-version-tooling-and-routing.md)). An unrouted path falls
   through to the JSON 404 envelope.
3. **Authenticate / authorize.** Public routes skip this. Otherwise `requireAuth`
   resolves the opaque bearer session ([ADR-0002](decisions/adr-0002-api-authentication-model.md))
   to a fresh account, and admin routes additionally require the admin role;
   finer per-game checks (active GM, self-drop) live in the handler.
4. **Decode and validate.** The handler decodes the request DTO (generated from
   the OpenAPI spec) with a size cap and validates required fields, rendering a
   `400` envelope on bad input.
5. **Domain call → persist.** The handler translates wire ↔ domain and calls
   `internal/store`; it contains no game logic (the engine is not on this path).
6. **Respond.** Success is rendered as an indented JSON envelope (or a bare
   `204`/`202` where the spec calls for it); any failure goes through the single
   error envelope, which carries the same correlation id.
