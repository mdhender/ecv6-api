# API conventions

Cross-cutting rules for the REST surface. The per-endpoint contract lives in
[openapi.yaml](openapi.yaml); this page covers what applies across all of it. The
implementation is `internal/server` — see its package godoc for the middleware
chain, route groups, and how these conventions are enforced in one place.

## Authentication

Clients authenticate with an opaque, server-side **session token**
([ADR-0002](../decisions/adr-0002-api-authentication-model.md)). `POST /auth/login`
exchanges an account's **email + secret** for a session token; every subsequent
request presents it as `Authorization: Bearer <token>` (the `sessionToken`
security scheme in `openapi.yaml`). The token is **not** a JWT — it carries no
claims and is resolved against the session store on each request, so revoking a
session (or deactivating the account) takes effect immediately, on the next call.

Email is stored lowercased and matched case-insensitively; the secret is only
ever stored hashed.

## Request correlation

Every request carries a correlation id. The server reuses an inbound
`X-Request-Id` header when the client supplies one, otherwise it mints one, and it
echoes the id back in the `X-Request-Id` response header on every response. The
same id also appears in the error envelope (below) and in the server's per-request
log line, so a client can quote it and an operator can find the matching log.

## Errors

A single error envelope across all endpoints:

```json
{
  "error": {
    "code": "some_stable_code",
    "message": "human-readable text",
    "requestId": "e2b1c0d4f5a6..."
  }
}
```

`code` is stable and machine-readable; `message` is for humans and may change;
`requestId` is the request's correlation id (the same value as the `X-Request-Id`
response header) and is present whenever the request reached the server's
middleware. The codes in use today:

| Code             | Typical status | Meaning                                             |
|------------------|----------------|-----------------------------------------------------|
| `bad_request`    | 400            | Malformed body, missing/invalid field, bad path id  |
| `unauthorized`   | 401            | Missing, malformed, expired, or invalid credentials |
| `forbidden`      | 403            | Authenticated but not allowed                       |
| `not_found`      | 404            | No such resource (or hidden from the caller)        |
| `conflict`       | 409            | Conflicts with current state (e.g. duplicate email) |
| `internal_error` | 500            | Unexpected server-side failure                      |

Codes are additive: new ones may be appended as endpoints land, but an existing
code's meaning does not change.

## Versioning

Routes carry **no version segment** — there is no `/v1` in the path. For the
alpha the surface is unversioned and churn-friendly; if versioning is ever needed
it will be handled another way (e.g. a header), decided then, rather than by
forking the path.

## Idempotency

The application surface needs no idempotency-key mechanism: its writes are already
safe to retry. `PUT`/`PATCH` updates are naturally idempotent; the `DELETE`-style
session revocations return `204` whether or not anything was still active;
creating a resource that would collide (a duplicate account email, an
already-seated member) returns `409` rather than silently creating a second one.
No endpoint on this surface accepts or requires an `Idempotency-Key` header.

The open question is the **game engine**: order submission and turn processing
have a genuine idempotency requirement — resubmitting a turn's orders must not
double-apply. That model will be specified here when those endpoints land; the
engine surface is deferred and out of scope for the application server.
