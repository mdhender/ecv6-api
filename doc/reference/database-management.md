# Database management reference

Reference for the EC database file and the `ecdb` commands that manage it. For a
task walkthrough, see the how-to
[Create, migrate, and verify a database](../how-to/create-and-verify-a-database.md).

## The database file

| Property | Value |
| --- | --- |
| Filename | `ec.db` (fixed) |
| Location | a folder chosen by the operator; commands take that folder as `PATH` |
| Sidecar files | `ec.db-wal`, `ec.db-shm` when opened with WAL journaling |
| Identity | SQLite `application_id` pragma = `0x0EC0DB`; distinguishes an EC database from any other SQLite file |
| Version | SQLite `user_version` pragma = the number of migrations applied |

A database is **current** when its version equals the *expected version* — the
number of migrations the running binary knows about.

## Who may create a database

Only `ecdb create` creates a database file. The server, `ec`, opens an existing
database and applies migrations on startup, but never creates one: if the file is
missing, `ec` fails. Create the database with `ecdb` before the first `ec` start.

## Commands

Each command takes `PATH`, the folder that holds (or will hold) `ec.db`. All
diagnostic output is written to standard error via `slog`.

### `ecdb create [--overwrite] PATH`

Creates `PATH/ec.db` and applies all migrations. `PATH` must be an existing folder.

- Fails if `PATH` does not exist or is not a folder.
- Fails if `ec.db` already exists, unless `--overwrite` is given.
- `--overwrite` deletes the existing `ec.db` and its `-wal`/`-shm`/`-journal`
  sidecar files before creating a new one. It is destructive and cannot be undone.

### `ecdb migration up PATH`

Applies any migrations the database is missing. Never creates a database.

- Fails if the folder or `ec.db` does not exist.
- Fails if a migration errors.
- Fails if the database version is newer than the binary's expected version (an
  old binary against a newer schema).
- A no-op when the database is already current.

### `ecdb migration version PATH`

Logs the database's version and the binary's expected version. Read-only.

- Fails if the folder or `ec.db` does not exist, or the file is not an EC database.

### `ecdb migration verify PATH`

Exits `0` if the database exists and its version equals the expected version;
exits `1` otherwise (missing database, version mismatch, or not an EC database).
Read-only. Intended as a scriptable guard.

## Migrations

Migrations are an ordered, **append-only** list compiled into the binary. Applying
them advances `user_version` to the length of the list. The list is never edited or
reordered once databases exist; a new migration is appended. During alpha, because
data is disposable, early migrations may instead be squashed into a new baseline.

### No down-migration command

**The project has decided against implementing a "migration down" command.**
Migrations are forward-only. To move a database backwards, recreate it
(`ecdb create --overwrite`) and, once seed loading exists, reload its data. During
alpha this is cheap because databases are disposable and rebuilt from data files;
a reversible-migration mechanism is not worth its complexity at this stage.

## Environments

On every run, `ecdb` loads `.env` files selected by the `ECDB_ENV` variable
(default `development`), via `internal/dotenv`. This affects only which
configuration is loaded, not the database location, which is always the `PATH`
argument.
