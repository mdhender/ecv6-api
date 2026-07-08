# ADR-0005: Application surface sub-decisions

- **Status:** accepted
- **Date:** 2026-07-07

## Context

Before drafting the reconciled application surface into `openapi.yaml` (Phase 3
of the [gap analysis](../api/v4-gap-analysis.md)), four additive choices had to
be settled that the numbered gaps did not cover. They came out of the
completeness review of v4's application surface (email immutability, no admin
session control, admin-only recovery) plus the role-representation question left
open by [ADR-0004](adr-0004-application-vs-per-game-roles.md). These are surface
and policy choices, not architectural reversals.

## Decision

**1. Email is mutable — by self and by admin.** The admin account-update path
gains an `email` field. Self-service email and secret changes are **separate,
dedicated routes** (`POST /me/email`, `POST /me/secret`) rather than fields on
`PATCH /me` — this documents the "current secret required to change" rule in the
route shape itself. `POST /me/email` requires `currentSecret`; the new value is
lowercased and re-checked for uniqueness (ADR-0003). An email change does **not**
revoke other sessions (the secret is unchanged), whereas a secret change does.
`PATCH /me` is left to carry only non-sensitive profile fields (`displayName`).

**2. Admins get session management over any account.** Add admin-scoped session
routes mirroring the self-service `/me/sessions` surface — list an account's
sessions and revoke one or all of them — so an admin can terminate a compromised
or deactivated account's live sessions. "CRUD" here is **read + delete**: sessions
are *created* only by login (ADR-0002), never minted by an admin through this
surface (admin-acts-as is impersonation, a separate concern). Likely shape:
`GET /accounts/{accountId}/sessions`, `DELETE
/accounts/{accountId}/sessions/{sessionId}`, and a bulk
`DELETE /accounts/{accountId}/sessions`. This is what mechanically closes the G4
revocation gap for admins.

**3. Account recovery stays admin-only.** No public forgot/reset flow for the
alpha. A locked-out account is recovered by an admin setting a new secret via the
account-update path (ADR-0002 authenticates against it). Revisit if/when
self-service recovery is needed.

**4. Application role is exposed as `roles` string values `"admin"` / `"user"`.**
Not a boolean `isAdmin`, and not an `enum`-constrained field. The account/`/me`
schema carries a `roles` array of open strings that, today, holds exactly one
application-role value. Keeping it an open string array (rather than a boolean or
a closed enum) keeps the contract churn-friendly — a future application role is
an added value, not a schema-breaking change — and preserves v4's `roles` *shape*
while purging its per-game (`gm`/`player`) values per ADR-0004.

## Consequences

- **Surface additions over v4:** a self email-change route, an `email` field on
  admin account-update, and three admin session routes. These are net-new and
  will appear in the Phase 3 draft.
- **Uniqueness on email change** must be enforced on both the self and admin
  paths, same rule as create.
- **Role extensibility** is preserved by the open-string `roles` array; clients
  should treat unknown role values gracefully rather than assuming the closed
  `{admin, user}` set forever.
- **Recovery remains an admin burden** — acceptable for the alpha's admin-
  provisioned model; noted as a known limitation, not an oversight.
- **Not frozen surfaces.** All four are application-domain policy/shape choices,
  freely revisable; unrelated to the determinism contract.

Settles the Phase 3 pre-draft sub-decisions. Refines ADR-0004 (role spelling) and
ADR-0002 (admin session routes). The application `openapi.yaml` draft is now
fully unblocked.
