# ADR-0006: OpenAPI 3.0.3, oapi-codegen, and unversioned `/api` routes

- **Status:** accepted
- **Date:** 2026-07-07

## Context

The source-of-truth spec (`doc/api/openapi.yaml`) was inherited declaring
`openapi: 3.1.0` and a `/api/v1` server path — neither ratified for v6. Two
gap-analysis items forced the question ([../api/v4-gap-analysis.md](../api/v4-gap-analysis.md),
**G2** spec version, **G3** route versioning):

- **Version is coupled to tooling.** `oapi-codegen` (the generator v4 used, and
  the intended one here) parses through `getkin/kin-openapi`, a **3.0.x**
  library whose 3.1 support is partial/experimental. A `3.1.0` spec is therefore
  not safe to generate from — the 3.1 changes (JSON-Schema `type: [T, "null"]`,
  the `const` keyword, the `examples` form) are exactly where it breaks.
- **Route versioning** in the path (`/api/v1`) was inherited from the current
  spec; v4 itself served routes at bare root.

## Decision

- **The spec is OpenAPI 3.0.3.** `openapi.yaml` declares `3.0.3`; 3.1-only
  constructs are avoided. Nullable fields use 3.0 `nullable: true` (so the eventual
  `Turn.ordersDueAt`-style fields are written that way, not `type: [T, "null"]`).
- **Code is generated with `oapi-codegen`.** Server DTOs and handler stubs are
  generated from `openapi.yaml`; the spec stays the source of truth (spec-first —
  change the spec, regenerate, then implement).
- **Routes are unversioned under base path `/api`.** No `/v1` segment — e.g.
  `/api/healthz`, `/api/games`. The alpha carries no API version in the path; if
  versioning is ever needed it will be by another means (e.g. a header), decided
  then, not a path fork ([../api/conventions.md](../api/conventions.md)).

## Consequences

- Stays on the well-trodden path: oapi-codegen + 3.0.3 is the mature, documented
  combination, appropriate for a churn-friendly alpha.
- **Cost of a future 3.1 need.** If a 3.1-only feature ever becomes worth it,
  this is a tooling migration (e.g. to `ogen`, which is 3.1-native), not a spec
  tweak — a conscious, larger change. Recorded so that trade-off is explicit.
- Schema authors must stay within 3.0.3 — no `type` arrays for null, no
  JSON-Schema `const`; use `nullable`, `enum`, and single-type `example`.
- Unversioned routes keep the surface small; breaking changes during the alpha
  are made in place rather than forked.
- **Not a frozen surface.** Spec version, generator, and base path are all
  revisable; unrelated to the determinism contract (ADR-0001).

Resolves G2 and G3.
