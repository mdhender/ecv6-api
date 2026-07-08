# API conventions

> **Status: stub.** Cross-cutting rules for the REST surface. The per-endpoint
> contract lives in [openapi.yaml](openapi.yaml); this page covers what applies
> across all of it.

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

## Errors

A single error envelope across all endpoints:

```json
{ "error": { "code": "some_stable_code", "message": "human-readable text" } }
```

Codes are stable and machine-readable; messages are for humans. _Catalogue of
codes: TODO._

## Versioning

Routes carry **no version segment** — there is no `/v1` in the path. For the
alpha the surface is unversioned and churn-friendly; if versioning is ever needed
it will be handled another way (e.g. a header), decided then, rather than by
forking the path.

## Idempotency

_TODO._ Order submission and turn processing have natural idempotency
requirements (resubmitting a turn's orders should not double-apply). Specify the
model here once endpoints exist.
