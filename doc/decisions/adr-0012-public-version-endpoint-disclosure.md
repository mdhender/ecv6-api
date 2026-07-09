# ADR-0012: The public `/version` endpoint stays unauthenticated and reports the full truth

- **Status:** accepted
- **Date:** 2026-07-09

## Context

The application API exposes `GET /api/version` (and `GET /api/healthz`) publicly.
`/version` reports two things: the server build — the full version string from
`Version().String()`, i.e. semver + pre-release + git build metadata, including a
`-dirty` marker when built from a modified tree — and the database schema version
(SQLite `user_version`, the migration count).

The [#38](https://github.com/mdhender/ecv6-api/issues/38) review of the `ec`
command raised [#59](https://github.com/mdhender/ecv6-api/issues/59): an anonymous
caller can read the exact build commit and schema version. The options weighed
were to gate `/version` behind auth, trim the build metadata for anonymous
callers, split a coarse public response from a detailed admin one, or leave it as
is.

Two facts frame the call:

- **`/version` is a client diagnostic.** When a client hits trouble, the first
  question is "what am I running against?" — the server build, and the schema
  version its requests must be compatible with. That is the endpoint's whole job.
- **The resource concern is already gone.** [ADR-0002](adr-0002-api-authentication-model.md)
  keeps `/version` and `/healthz` out of the authenticated surface, and
  [#45](https://github.com/mdhender/ecv6-api/issues/45) made `/version` serve a
  cached schema version — no per-request database round-trip. So the *only*
  remaining objection to leaving it public was information disclosure.

## Decision

**`/version` stays public (unauthenticated), alongside `/healthz`, and reports the
full truth to every caller.** We do not gate it behind auth, and we do not vary
its content by caller.

- `application`: the complete `Version().String()` — semver, pre-release, and git
  build metadata, including the `-dirty` marker.
- `database.schemaVersion`: the real SQLite `user_version` (migration count).

**We do not ship an "API version" field.** The REST surface is not yet
versioned or tracked, so a placeholder would be dishonest — a field that lies is
worse than an absent one. The honest compatibility signal a client needs is
`database.schemaVersion`; a real API version can be added later, backed by the
spec, once the surface stabilizes.

## Consequences

- **The diagnostic works when it is needed most.** A client can confirm what it is
  talking to without credentials — including when the client's problem *is*
  authentication. Gating `/version` behind auth would force a support call in
  exactly the case (can't log in) where self-service matters most; we explicitly
  reject that, and reject the auth-varying split for the same reason (the anonymous
  caller is the one who needs the useful answer).
- **We accept the disclosure trade-off with eyes open.** The repository is public,
  so the exposed commit fingerprints the exact source and therefore any issue known
  at that commit. We judge this acceptable for the current posture — alpha,
  self-hosted, low-value target — because hiding a version is weak defense
  (patching, not obscurity, is the mitigation) and the diagnostic value outweighs
  the marginal obscurity. A `-dirty` marker showing up in a production deployment is
  itself a useful alarm, not a leak to suppress.
- **Revisit trigger.** If the deployment posture changes — hosted/multi-tenant, or
  a higher-value target — reweigh trimming the build metadata (e.g. serving
  `Version().Core()` to anonymous callers). That is an amendment to this ADR, not a
  rewrite.
- **Not a frozen surface.** The response shape is revisable and touches neither the
  on-disk format nor the determinism contract ([ADR-0001](adr-0001-counter-based-prng.md)).
  This ADR complements [ADR-0002](adr-0002-api-authentication-model.md) by recording
  that `/version` (and `/healthz`) sit outside the authenticated surface by design.
