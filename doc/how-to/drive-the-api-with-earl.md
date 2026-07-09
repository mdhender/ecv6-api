# Drive the API with earl

`earl` is a command-line client for the EC API. It mirrors the REST surface: the
verb and path you would send become the command line, so any endpoint is one
command away. This guide shows how to log in, make requests, act as another
account, and log out.

It assumes a database with an admin account already exists (see
[`ecdb admin create`](../reference/database-management.md)) and a server is
running (see
[Create, migrate, and verify a database](create-and-verify-a-database.md#start-the-server)).
Build the client with `go build -o earl ./cmd/earl`, or run it in place with
`go run ./cmd/earl`.

> **Quick start with no database.** To try earl against a throwaway server
> without creating anything on disk, run
> [`ec serve --memory`](create-and-verify-a-database.md#serve-a-throwaway-in-memory-database).
> It stands up a fresh in-memory database and auto-seeds a well-known admin —
> email `admin@ecv6.example.com`, secret `password` — so you can log in
> immediately. Set `EARL_EMAIL=admin@ecv6.example.com` and `EARL_SECRET=password`
> below.

## Point earl at the server

earl reads the same `EARL_`-prefixed variables from your `.env` files that the
flags accept. Set the server and your credentials once so later commands need no
repetition:

```
EARL_BASE_URL=http://localhost:8080/api
EARL_EMAIL=penny@example.com
EARL_SECRET=happy.cat.happy.nap
```

`--base-url` and `--email` override `EARL_BASE_URL` / `EARL_EMAIL` per command;
`--secret` overrides `EARL_SECRET`. `EARL_ENV` selects which `.env` files load
(default `development`), exactly as with `ec` and `ecdb`.

## Check the server without logging in

Public endpoints need no token:

```
$ earl get /healthz
{
  "status": "ok",
  "version": "0.19.0-alpha+..."
}
```

## Log in

`earl login` exchanges your email and secret for a bearer token and saves it.
With `EARL_EMAIL`/`EARL_SECRET` set, it takes no arguments:

```
$ earl login
logged in as penny@example.com at http://localhost:8080/api (token expires ...)
```

Every later request attaches this token automatically. `earl whoami` is a
shortcut for `get /me` that confirms who you are:

```
$ earl whoami
{
  "account": { "email": "penny@example.com", "id": 1, "roles": ["admin"], ... }
}
```

## Make requests

Use the HTTP verb as the command and the path as its argument. Send a body with
`-d` — inline JSON, `@file` to read a file, or `@-` to read standard input. Flags
may come before or after the path:

```
$ earl get /accounts
$ earl post /accounts -d '{"email":"tester@example.com","secret":"hunter2hunter2","isActive":true}'
$ earl patch /games/1 -d @game.json
$ echo '{"name":"Test Game"}' | earl post /games -d @-
```

Responses print to standard output — indented at a terminal, compact when piped,
so they pass straight into `jq`:

```
$ earl get /accounts | jq '.accounts[] | {id, email}'
```

A non-2xx response prints the server's error envelope to standard error and exits
non-zero, so `earl` composes in scripts:

```
$ earl get /me --no-auth
earl: GET /me -> 401 Unauthorized
{ "error": { "code": "unauthorized", "message": "authentication required", ... } }
```

Pass `--no-auth` to send a request without a token. For the full list of paths,
bodies, and status codes, see the [OpenAPI spec](../api/openapi.yaml) and the
[API conventions](../api/conventions.md).

## Act as another account

To exercise the API as a non-admin (or GM), impersonate that account. As an
admin, mint a token for it by id:

```
$ earl impersonate 2
penny@example.com is now impersonating tester@example.com (account 2); select it with --email tester@example.com
```

earl saves the impersonation token alongside your admin one, keyed by the
subject's email. Select which identity a command uses with `--email`:

```
$ earl --email tester@example.com whoami        # acts as the impersonated user
$ earl --email tester@example.com get /accounts # expect 403 — not an admin
$ earl whoami                                    # back to penny (EARL_EMAIL)
```

This is how you confirm role-gated behavior — admin-only routes, GM-only game
edits, a member's ability to drop only themselves — all from one admin login.
Impersonation targets must be active, non-admin, and not yourself.

## Log out

`earl logout` revokes the current session and forgets its token. Add `--all` to
revoke every session for the account, and `--email` to log out a specific saved
identity:

```
$ earl logout
logged out penny@example.com at http://localhost:8080/api

$ earl --email tester@example.com logout
```

## Where tokens live

earl stores tokens in `~/.config/earl/tokens.json` (honoring `XDG_CONFIG_HOME`;
override with `EARL_TOKENS`), mode `0600`, keyed by base URL and account email.
Because tokens are keyed by email, earl holds several identities for one server at
once — an admin and one or more impersonated users — and `--email` chooses between
them. When only one identity is saved for a server, it is used by default.
