# Create, migrate, and verify a database

This guide shows how to create a new EC database with `ecdb`, bring an existing one
up to the current schema, and confirm it is at the version your binaries expect.

It assumes `ecdb` and `ec` are built and on your `PATH` (if not, run
`make build` and add `dist/local` to your `PATH`, or invoke the binaries by their
full path). It also assumes you have a folder to hold the database: `ecdb` never
creates the folder, only the database inside it.

## Create the database

The database file is always named `ec.db`. You pass `ecdb create` the *folder*
that will hold it, not the filename:

```
$ mkdir -p games/example
$ ecdb create games/example
created database games/example/ec.db (version 1)
```

`ecdb` created `games/example/ec.db` and applied every migration. `version 1` is the
schema version it reached — the number of migrations applied. This message is
written to standard error.

The folder must already exist. Pointing `create` at a missing folder fails without
creating anything, printing the error to standard error and exiting non-zero:

```
$ ecdb create games/missing
ecdb: create: cannot access PATH games/missing: stat games/missing: no such file or directory
```

Creating a database is `ecdb`'s job alone. The server, `ec`, opens an existing
database and applies migrations, but it will never create one — so run `ecdb`
before the first `ec` start.

## Replace an existing database

If `ec.db` already exists, `create` refuses to touch it and exits non-zero:

```
$ ecdb create games/example
ecdb: create: database already exists (pass --overwrite to replace it): games/example/ec.db
```

To discard the old database and build a fresh one, pass `--overwrite`:

```
$ ecdb create --overwrite games/example
removed existing database games/example/ec.db
created database games/example/ec.db (version 1)
```

`--overwrite` deletes the existing `ec.db` and its `-wal`/`-shm`/`-journal`
sidecar files, then builds a fresh one. During alpha this is expected — databases
are disposable and rebuilt from data files.

> **Warning:** `--overwrite` is destructive and cannot be undone. Never point it
> at a database you care about. To keep a snapshot first, run
> [`ecdb backup`](../reference/database-management.md) before overwriting.

## Apply migrations to an existing database

When you rebuild `ecdb` and `ec` with new migrations, bring an existing database up
to the current schema with `migration up`:

```
$ ecdb migration up games/example
migrations applied to games/example/ec.db (version 1)
```

It applies every migration the database is missing and reports the version it
reached. Running it on an already-current database is a safe no-op.

`migration up` never creates a database. Pointing it at a missing file or folder
fails — run `ecdb create` first:

```
$ ecdb migration up games/missing
ecdb: migration up: cannot apply migrations to games/missing/ec.db: games/missing/ec.db: database not found
```

If the database's version is newer than the binary — you are running an old `ecdb`
against a schema built by a newer one — `migration up` fails instead of touching
it; rebuild your binaries. Migrations are forward-only: there is no "migration
down" (see the [database management reference](../reference/database-management.md)).

## Check the version

To read a database's schema version, use `migration version`. It prints just the
version — a plain integer on standard output, with nothing else — so you can
capture it in a script:

```
$ ecdb migration version games/example
1
```

That number is what the database records. It does not tell you whether the
database matches what your binary expects; for a pass/fail comparison against the
binary's expected version, use `migration verify` (below).

## Verify the version in a script

When you want a pass/fail check rather than numbers to eyeball — for example a
startup or deploy guard — use `migration verify`. It reports the result through
its exit status:

```
$ ecdb migration verify games/example
verified games/example/ec.db (version 1)
$ echo $?
0
```

It exits `0` only when the database exists and its version matches what the binary
expects. It exits `1` for every other case: the database is missing, its version
does not match, or the file is not an EC database. That makes it a guard you can
put in front of starting the server:

```
if ! ecdb migration verify games/example; then
    echo "database missing or out of date; run 'ecdb create' or rebuild" >&2
    exit 1
fi
```

## Start the server

With the database in place, hand the same folder to `ec serve`:

```
$ ec serve --data games/example
```

`ec` opens the existing `ec.db` (never creating one), applies any pending
migrations, and serves on `:8080`. Override the address with `--listen`, and set
the bcrypt cost for hashing account secrets with `--secret-cost N` (`EC_SECRET_COST`,
default `10`) — keep the default in production and lower it only in throwaway dev
or test setups. Run `ec serve --help` for the full flag list; each flag is also
settable through its `EC_`-prefixed environment variable.

## Serve a throwaway in-memory database

For a quick dev spin or a smoke test you do not need a database on disk at all.
`ec serve --memory` (`EC_MEMORY`) serves a fresh, migrated, in-memory database
that **never touches disk** and vanishes when the process exits:

```
$ ec serve --memory
```

Because an in-memory database is empty and process-local, `ecdb` cannot seed it
(that is a different process, with different memory). So `--memory` instead
**auto-seeds one well-known admin** at startup and logs the credentials at `WARN`:

```
level=WARN msg="in-memory server: seeded well-known admin" email=admin@ecv6.example.com secret=password
```

Log in with those well-known credentials — email `admin@ecv6.example.com`, secret
`password` — and the server is immediately usable, no `ecdb` setup required. These
credentials are hard-coded for testing convenience and are safe only because
in-memory mode holds no persistent data; **never** use it to serve real data.

`--memory` overrides an ambient `EC_DATA` sourced from a loaded `.env*`, so you do
not need to blank it first; it only conflicts with an **explicit** `--data` flag
passed on the command line (that combination is a usage error). A persistent
on-disk database is **never** auto-seeded — those are seeded deliberately with
[`ecdb admin create`](../reference/database-management.md).

> **Environments.** On every run, `ecdb` and `ec` load `.env` files selected by
> `ECDB_ENV` and `EC_ENV` respectively (default `development`). This changes only
> which configuration is loaded — e.g. `EC_SECRET_COST` — not where the database
> goes, which is always the folder you pass on the command line.
