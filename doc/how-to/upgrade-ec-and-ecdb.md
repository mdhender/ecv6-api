# Upgrade `ec` and `ecdb`

This guide upgrades a running install to a new build of the `ec` server and the
`ecdb` admin tool, bringing an existing database up to the new schema. The order
matters: you back up **before** swapping binaries, so keep the steps in sequence.

It assumes the current `ec` and `ecdb` are on your `PATH`, you have the new
binaries built (or downloaded), and the server is one you can stop briefly.

## Stop the server

`ecdb` assumes it is the only process touching the database, and migrations need
exclusive access. Stop `ec` before going further.

## Record the current versions

Note what you are upgrading from — you will want it if you roll back:

```
$ ec version
0.6.0
$ ecdb version
0.6.0
```

## Back up the database — before you swap binaries

Take a snapshot with the **current** `ecdb`, while the database still matches it.
`ecdb backup` refuses a database that is not at its expected version, so once the
new binaries are in place they will not back up the *old* schema — this is why the
backup comes first:

```
$ ecdb backup games/example
games/example/ec.db.20260708T190345Z
```

Keep the path it prints; that file is your rollback point.

## Keep the current binaries

Save the binaries you are replacing, tagged with their version, so a rollback is a
copy rather than a rebuild:

```
$ cp "$(command -v ec)" ec-0.6.0
$ cp "$(command -v ecdb)" ecdb-0.6.0
```

## Install the new binaries

Put the new `ec` and `ecdb` on your `PATH` (replace the old ones, or point `PATH`
at the new build's folder). Confirm the shell now resolves the new versions:

```
$ ec version
0.7.0
$ ecdb version
0.7.0
```

## Apply migrations

Bring the database up to the new schema with the new `ecdb`:

```
$ ecdb migration up games/example
migrations applied to games/example/ec.db (version 2)
```

This applies only the migrations the database is missing and is a safe no-op if the
new build added none. Confirm the database is now at the new schema version:

```
$ ecdb migration version games/example
2
```

`migration version` reports the database's own version whether or not it matches
the binary; `ecdb version` reports the binary's version. Use
[`ecdb migration verify`](../reference/database-management.md) if you want a
pass/fail check that the two agree.

## Start the new server

Start `ec` again. It opens the upgraded database and serves as usual.

## Roll back

If the new build misbehaves, return to the old one. Put the saved binaries back on
your `PATH`:

```
$ cp ec-0.6.0 "$(command -v ec)"
$ cp ecdb-0.6.0 "$(command -v ecdb)"
```

Because migrations are forward-only, the new schema may be ahead of what the old
`ec` accepts. If so, restore the database you backed up above — follow
[Restore a database from a backup](restore-a-database-from-a-backup.md) — then
start the old `ec`.
