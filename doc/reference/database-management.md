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

Each command takes `PATH`, the folder that holds (or will hold) `ec.db`. Status
and error messages are plain, human-readable text written to standard error; a
failing command prints `ecdb: <message>` and exits non-zero. Commands whose result
is a value write only that value to standard output so scripts can capture it:
`migration version` writes the schema version (a bare integer) and `backup` writes
the path of the file it wrote.

Flags always precede `PATH` (e.g. `ecdb backup --output-path DIR PATH`): the
command parser does not accept flags after a positional argument.

### `ecdb create [--overwrite] PATH`

Creates `PATH/ec.db` and applies all migrations. `PATH` must be an existing folder.

- Fails if `PATH` does not exist or is not a folder.
- Fails if `ec.db` already exists, unless `--overwrite` is given.
- `--overwrite` deletes the existing `ec.db` and its `-wal`/`-shm`/`-journal`
  sidecar files before creating a new one. It is destructive and cannot be undone.

### `ecdb backup [--output-path DIR] [--version-stamp] PATH`

Writes a consistent, defragmented single-file copy of `PATH/ec.db` and prints the
copy's full path to standard output.

- **Verifies the source first, and it must be exactly current** — the database's
  version must equal the binary's expected version (the same check as
  `migration verify`). This is deliberate: `backup` refuses to snapshot a database
  that is missing, not an EC database, or not current, so you never capture a stale
  or foreign file. To snapshot *before* applying new migrations, back up while the
  database is still current (before upgrading the binary that introduces them).
- **The backup file name is chosen by `ecdb`, never the caller.** It is
  `ec.db.<timestamp-utc>`, a filesystem-safe, sortable UTC stamp — for example
  `ec.db.20260708T190345Z`. Choosing the name (rather than accepting one) also keeps
  a backup from being mistaken for a live `ec.db` and, say, migrated by accident.
- **`--version-stamp`** appends the migration (schema) version to that name, giving
  `ec.db.<timestamp-utc>-<version>` — for example `ec.db.20260708T190345Z-1`. The
  version is the database's `user_version` (equal to the binary's expected version,
  since the source must be current), *not* the application's release version. Off by
  default. Use it when you want a backup's schema to be obvious from its name, such
  as before an upgrade.
- **`--output-path DIR`** selects the folder the backup is written into. It
  defaults to `PATH` (beside the source database) and must be an existing folder,
  not a file name.
- **Fails if the destination file already exists.** There is no overwrite flag; a
  name collision (two backups within the same second) is an error, not a clobber.
- The source is opened read-only and never modified. The copy is made with SQLite's
  `VACUUM INTO`, so it is defragmented and carries the same `application_id` and
  `user_version`.

There is no `restore` command; restoring is a deliberate, documented procedure —
see the how-to [Restore a database from a backup](../how-to/restore-a-database-from-a-backup.md).
Backup and in-place compaction are kept separate
([ADR-0010](../decisions/adr-0010-backup-and-compaction-are-separate.md)).

### `ecdb compact PATH`

Reclaims free space in `PATH/ec.db` by running SQLite's `VACUUM` **in place**,
rewriting the file to release pages freed by deletions (EC prefers soft deletes, so
freed space accrues) and to defragment. Reports the before/after size on standard
error, e.g. `compacted games/example/ec.db (4136960 -> 32768 bytes, reclaimed
4104192)`; it writes nothing to standard output.

- **Requires only an EC database — any version.** Unlike `backup`, `compact` does
  not check the schema version: compaction changes layout, not schema, so it is
  safe on a database that is behind or ahead of the binary. It fails only if the
  file is missing or is not an EC database.
- **Rewrites in place, transactionally.** `VACUUM` is crash-safe — a failure leaves
  the original intact — but it needs roughly twice the database size in free disk
  space while it runs, and exclusive access (stop the server first).
- **Does not take a backup first.** `compact` and `backup` are orthogonal
  ([ADR-0010](../decisions/adr-0010-backup-and-compaction-are-separate.md)). To keep
  a safety net, back up before compacting:

  ```
  $ ecdb backup --version-stamp games/example
  $ ecdb compact games/example
  ```

### `ecdb migration up PATH`

Applies any migrations the database is missing. Never creates a database. Running
it against a new build's schema is part of
[Upgrade `ec` and `ecdb`](../how-to/upgrade-ec-and-ecdb.md).

- Fails if the folder or `ec.db` does not exist.
- Fails if a migration errors.
- Fails if the database version is newer than the binary's expected version (an
  old binary against a newer schema).
- A no-op when the database is already current.

### `ecdb migration version PATH`

Prints the database's schema version as a bare integer to standard output.
Read-only. It does not print the binary's expected version; use `migration verify`
to compare the two.

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

## Logging

Every `ecdb` command accepts `--logging-level`, which sets the minimum level of
the diagnostic log:

| Value | Effect |
| --- | --- |
| `DEBUG` | everything, including per-command trace lines |
| `INFO` | the default |
| `WARN` | warnings and errors only |
| `ERROR` | errors only |

Names are case-insensitive. The level is also settable via `ECDB_LOGGING_LEVEL`
(the same flag through the `ECDB_` env prefix); an explicit `--logging-level` flag
wins over the environment. The default, `INFO`, matches `slog`'s own default. An
unrecognized name is a usage error (`ecdb: unknown logging level "…"`, exit 1).

`ERROR` is a floor: no value turns error logging off.

The log is separate from a command's other output and is written to standard
error, for the developer or agent. It does not affect results (which go to
standard output — see [`migration version`](#ecdb-migration-version-path)) or the
`ecdb: <message>` error reports the shell sees on failure. See
[ADR-0009](../decisions/adr-0009-output-channels-stdout-stderr-slog.md) for the
stdout / stderr / `slog` split. The same flag and env-prefix convention applies to
the `ec` server (`EC_LOGGING_LEVEL`).

## Environments

On every run, `ecdb` loads `.env` files selected by the `ECDB_ENV` variable
(default `development`), via `internal/dotenv`. This affects only which
configuration is loaded, not the database location, which is always the `PATH`
argument.
