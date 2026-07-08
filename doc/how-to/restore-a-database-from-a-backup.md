# Restore a database from a backup

This guide restores an EC database from a backup file made by
[`ecdb backup`](../reference/database-management.md). Restoring replaces the live
database with the backup, so it is a deliberate manual procedure — there is no
`ecdb restore` command
([ADR-0010](../decisions/adr-0010-backup-and-compaction-are-separate.md)).

> **Warning:** Restoring overwrites the current `ec.db` and cannot be undone. Make
> sure you have the right backup, and consider taking a fresh `ecdb backup` of the
> current database first (if it still verifies) so you can change your mind.

It assumes `ecdb` is built and on your `PATH`, and that you have a backup file. A
backup is named `ec.db.<timestamp-utc>` and lives wherever `--output-path` put it —
by default, beside the database it came from:

```
$ ls games/example
ec.db  ec.db.20260708T190345Z
```

## Stop everything using the database

`ecdb` assumes it is the only process touching the database, and a half-written
restore will corrupt a database another process is reading. Stop the `ec` server
(and anything else pointing at this folder) before you begin.

## Remove the current database and its sidecar files

Delete the live `ec.db` **and** its WAL/shared-memory sidecars. This step is not
optional: a stale `ec.db-wal` left beside the restored file would be replayed into
it and corrupt it.

```
$ rm -f games/example/ec.db games/example/ec.db-wal games/example/ec.db-shm games/example/ec.db-journal
```

## Copy the backup into place

A backup is a complete, standalone database. Copy it to `ec.db` in the folder.
Copy, do not move, so the backup file survives the restore:

```
$ cp games/example/ec.db.20260708T190345Z games/example/ec.db
```

## Verify the restored database

Confirm the restored file is a current EC database before you start the server:

```
$ ecdb migration verify games/example
verified games/example/ec.db (version 1)
```

If `verify` fails, do not start the server — the backup was not what you expected,
or was for a different schema version. Investigate before proceeding.

## Restart the server

With a verified database in place, start `ec` again. It opens the restored database
and runs any pending migrations as usual.
