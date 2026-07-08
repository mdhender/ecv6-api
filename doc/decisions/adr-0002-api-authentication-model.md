# ADR-0002: Opaque server-side session tokens (bearer transport)

- **Status:** accepted
- **Date:** 2026-07-07

## Context

The inherited v4 API (`doc/api/openapi-v4.yaml`) authenticates with **JWT**: a
short-lived stateless access token plus a revocable, server-stored refresh-token
*family*. Its stated justification was "REST requires bearer tokens." That
premise conflates two independent axes:

- **Transport** — `Authorization` header vs `Cookie`. REST constrains neither; a
  cookie is just a header the browser fills in.
- **Validation** — a self-contained token (JWT) the server verifies without a
  lookup, vs an opaque id the server resolves against a store. REST's
  statelessness constraint lives here, and it exists for horizontal scale and
  visibility.

Two facts decide the matter for EC:

- **We do not have the scale statelessness buys.** One node, one SQLite file, an
  alpha game server — a per-request indexed session lookup is free.
- **v4 is not actually stateless.** Its revocable refresh families are
  server-side session state already. v4 is a stateless access token bolted onto a
  stateful store — the *most* machinery of any option, not the least.

The gap analysis ([../api/v4-gap-analysis.md](../api/v4-gap-analysis.md), **G4**)
also flagged v4's real operational failure: an admin deactivating a compromised
account cannot kill its live sessions, because a stateless access token stays
valid until its TTL expires.

Our clients are **heterogeneous and player-written** — the `ec` CLI, scripts,
bots submitting orders, perhaps a browser later. We write none of them, so a
cookie's marquee benefit (`HttpOnly` storage, XSS-proof) is a property only a
browser client could opt into; it is not a guarantee the server can make. Cookies
would also impose CSRF handling and cookie-jar friction on the non-browser clients
that auto-attachment does not help.

This decision ratifies the authentication model so the rest of the application
surface (`/auth/*`, `/me/sessions/*`, `/admin/impersonation`, password change)
can be drafted on top of it.

## Decision

Authenticate with **opaque, server-side session tokens carried as
`Authorization: Bearer`**. Drop JWT and the access/refresh split.

- `POST /auth/login` exchanges **email + secret** for a single **session token**
  — a high-entropy random opaque string. Only a hash of the token is stored, as
  with the account secret.
- Every request presents the token as a bearer credential; the server resolves it
  to a session row (one indexed lookup) and to the account, re-reading account
  state so a deactivated account fails immediately.
- A **session** is a row in the session store: `(id, account_id, issued_at,
  expires_at, revoked_at, ...)`. The v4 `/me/sessions` surface maps directly onto
  these rows; per-session revoke is a soft-delete of the row.
- **Revocation is immediate** — deleting or revoking the row invalidates the token
  on its next use; there is no stateless-token TTL window to wait out.
- Ripple through the surface:
  - `/auth/refresh` is **removed** (no refresh token exists).
  - `/auth/logout` revokes the current session (or all of the account's).
  - `changeMyPassword` revokes the account's other sessions with a store
    `DELETE/UPDATE ... WHERE session <> current`.
  - `/admin/impersonation` mints a **short-lived session bound to the target
    account** with an `actor` (auditing) column naming the admin — no JWT `act`
    claim.

The concrete transport binding (token format, header spelling, session lifetime,
whether a login also issues a short rotation token) is fixed in
[../api/conventions.md](../api/conventions.md) and the `playerSecret` security
scheme in [../api/openapi.yaml](../api/openapi.yaml), which this ADR unblocks.

## Consequences

- **Not a frozen surface.** Unlike the determinism contract (ADR-0001), the
  session token is an internal credential with no cross-game compatibility
  meaning. Its format, storage, and lifetime can change at any time; rotating the
  scheme costs at most a forced re-login. Do **not** encode game state or
  reproducibility assumptions in it.
- The G4 revocation gap closes by construction: account deactivation and session
  revocation take effect on the next request, not after a token expires.
- Fewer moving parts than v4 — no signing keys, no claims, no access/refresh
  rotation dance, no reuse-detection state machine. One concept (the session)
  replaces two (access + refresh).
- The server is no longer "stateless" in the purist REST sense, but it never was:
  v4 already kept refresh state, and the single-node SQLite deployment gains
  nothing from statelessness. We trade a scale property we cannot use for
  immediate revocation and a smaller surface.
- Each authenticated request costs one indexed session lookup. At EC's scale this
  is invisible; if it ever mattered, a short in-memory cache keyed by token hash
  recovers it without changing the model.
- **Transport stays open at one end.** Bearer is the committed transport. If a
  first-party browser client later appears, the *same* session token may also be
  accepted from a cookie (adding CSRF handling then) without changing the store —
  the decision here is the validation model, not an exclusive transport.
- The application domain owns sessions entirely; the engine never sees a session
  or an `account_id`, consistent with the domain boundary
  ([../control-and-ownership.md](../control-and-ownership.md)).
