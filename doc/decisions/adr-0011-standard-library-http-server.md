# ADR-0011: The API server uses the standard library `net/http`, not a web framework

- **Status:** accepted
- **Date:** 2026-07-08

## Context

We are about to build the first HTTP server of the sixth iteration (epic #13,
the application-side API). [ADR-0006](adr-0006-openapi-version-tooling-and-routing.md)
already fixed the spec-first toolchain — OpenAPI 3.0.3 generated with
`oapi-codegen` into `internal/api`, configured with `std-http-server: true` and
`strict-server: true` — but it did not rule on the HTTP framework itself. Before
writing any handler code, we asked whether to adopt a framework (Echo v5) instead
of the standard library.

The investigation turned up three facts that decide it:

- **`oapi-codegen` is Echo-first, not stdlib-only.** Its default `server` target
  generates Echo boilerplate; `std-http-server`, `chi-server`, `gin`, and `fiber`
  are alternatives. So tooling is not a reason to avoid Echo — the earlier worry
  that the generator is "stdlib-only" was backwards.
- **…but its Echo generator targets Echo v4.** The generated Echo adapter imports
  `github.com/labstack/echo/v4`. Echo **v5** (GA June 2026) carries breaking API
  changes, and the generator has not moved to it. So *oapi-codegen + Echo v5* is
  not a clean combination today: you would generate v4-targeted code and then
  fight it onto v5, and hand-edits would trip `make verify`'s drift check.
- **`strict-server` already provides the framework's best parts.** The generated
  strict handlers give typed request/response objects, request binding, and
  validation independent of the router. That is most of what a framework like Echo
  would contribute, and we get it regardless of the router.

On Go 1.26 the standard library router (`http.ServeMux` with method+pattern and
path wildcards, since Go 1.22) is sufficient for this surface.

## Decision

**The API server is built on the standard library `net/http`, with no web
framework.** We keep `oapi-codegen`'s `std-http-server` + `strict-server` output
(ADR-0006) and write the small amount of glue — a middleware/route-group helper,
the error-envelope response wrapper, graceful shutdown — directly against
`net/http`.

We do **not** adopt Echo (v4 or v5). If route-group and per-group-middleware
ergonomics ever become painful enough to want a router, the sanctioned next step is
**chi** — a first-class `oapi-codegen` target that is 100% `net/http`-compatible
and a tiny dependency — evaluated then, before any full framework.

## Consequences

- **Minimal dependencies**, consistent with CLAUDE.md's "standard library first"
  stance. No framework tree to track, upgrade, or get pinned by.
- **We own a little plumbing**: chaining middleware, grouping the public /
  authenticated / admin routes, and rendering the error envelope are ours to write
  (~small, one-time). `net/http`'s `Server.Shutdown` covers graceful shutdown.
- **We sidestep the Echo v5 friction entirely** — no v4/v5 import mismatch with the
  generator, no dependence on a young major (v5's stabilization window only closed
  end of March 2026; v4 is maintained only through 2026-12-31).
- **`strict-server` carries the typed-handler load**, so choosing stdlib costs us
  little in handler ergonomics.
- **chi is the recorded escape hatch**: reaching for it later is a small amendment
  to this ADR, not a rewrite, because chi handlers are `net/http` handlers.
- **Not a frozen surface.** The router/framework choice is revisable; it does not
  touch the on-disk format or the determinism contract (ADR-0001). Complements
  ADR-0006 (tooling) rather than replacing it.
