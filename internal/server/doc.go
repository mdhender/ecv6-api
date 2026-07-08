// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package server is the application-domain HTTP server for Epimethean Challenge:
// accounts, authentication, sessions, games, and game membership (the
// game_account_role boundary). It knows nothing of the game engine — no faction,
// turn, orders, or cluster identity crosses this surface (ADR-0003). Wire DTOs
// come from the generated internal/api package (spec-first; the source of truth
// is doc/api/openapi.yaml, ADR-0006), and persistence goes through
// internal/store.
//
// # Routing
//
// Routes are served by the standard-library net/http ServeMux (ADR-0011), all
// hung under the unversioned base path /api (ADR-0006; no /v1 segment — see
// "Versioning" in doc/api/conventions.md). Handler builds the mux and wraps it in
// the middleware chain. Routes are registered on three groups that differ only in
// the credential middleware layered on top of the base chain:
//
//   - public — no credential check: GET /healthz, GET /version, POST /auth/login.
//   - authenticated (requireAuth) — a valid bearer session is required. Covers
//     /me and its sub-resources, logout, the game catalog reads, and the
//     membership endpoints (whose finer per-game authorization lives in the
//     handler).
//   - admin (requireAuth + requireAdmin) — the caller must additionally hold the
//     application admin role: account management, session administration, game
//     creation, and the /admin operational routes.
//
// An unrouted path (or a routed path with an unsupported method) falls through to
// a catch-all that renders the standard 404 envelope rather than net/http's
// plain-text default.
//
// # Middleware chain
//
// The base chain is applied once around the whole mux, outermost first —
// withRequestID, then withLogging, then withRecovery, then any group middleware,
// then the handler:
//
//   - withRequestID assigns each request a correlation id — reusing an inbound
//     X-Request-Id when the client supplied one, otherwise minting one — and
//     echoes it in the X-Request-Id response header.
//   - withLogging installs a request-scoped slog.Logger (tagged with the id) on
//     the context and logs one line per completed request (method, path, status,
//     bytes, duration).
//   - withRecovery turns a panic in a downstream handler into a logged 500 in the
//     standard envelope, so one bad handler cannot take the process down.
//
// Recovery sits inside logging so a recovered panic is still logged with its
// final (500) status.
//
// # Authentication
//
// Auth is an opaque, server-side bearer session (ADR-0002), not a JWT. POST
// /auth/login exchanges an account's email + secret for a token shown exactly
// once; every later request presents it as "Authorization: Bearer <token>".
// requireAuth hashes the presented token, resolves it to an active session
// (neither revoked nor expired), and re-reads the account on every request, so
// revoking a session or deactivating an account takes effect on the very next
// call. A missing, malformed, unknown, revoked, or expired credential — and a
// deactivated account — all yield the same opaque 401 so a caller cannot tell
// them apart. The resolved account and session are stashed on the request context
// (accountFromContext, sessionFromContext). Secrets are stored only as PBKDF2
// hashes (secret.go); tokens only as SHA-256 hashes (token.go). An impersonation
// session additionally carries the acting admin as the auditable actor and sets
// the Impersonated-Subject response header (ADR-0002).
//
// # Response and error conventions
//
// Every response is rendered through the helpers in respond.go so the surface
// stays uniform (doc/api/conventions.md):
//
//   - Success bodies go through writeJSON: indented application/json with the
//     charset, or a bare 204/202 with no body where the spec calls for it.
//   - Failures go through writeError, which emits the single error envelope
//     shared by every endpoint, of the form
//     {"error":{"code":"<stable_code>","message":"<human text>","requestId":"<id>"}}.
//
// code is a stable, machine-readable value (the constants in respond.go);
// message is human-facing and may change; requestId is the same correlation id
// as the X-Request-Id header, so a client can quote it and an operator can find
// the matching log line. Request bodies are decoded with decodeJSON, which caps
// the body size and renders a 400 envelope on malformed input.
//
// # Lifecycle
//
// New builds a Server around an already-open store (cmd/ec opens it; the server
// never creates a persistent database). Run starts net/http, serves until the run
// context is cancelled (SIGINT/SIGTERM) or an admin shutdown is requested, then
// drains in-flight requests gracefully.
package server
