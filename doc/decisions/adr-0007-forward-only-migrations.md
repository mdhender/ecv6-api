# ADR-0007: Forward-only migrations (no `migration down`)

- **Status:** accepted
- **Date:** 2026-07-08

## Context

`ecdb` manages the SQLite schema through ZombieZen's `sqlitemigration`: an
append-only list of migrations compiled into the binary, with the count applied
recorded in `user_version` (see
[../reference/database-management.md](../reference/database-management.md)). We
provide `ecdb migration up` to apply missing migrations. `ec` applies them on
startup.

A common expectation is a matching `migration down` — reversible migrations that
step the schema backwards. That expectation forces a question now, before the
migration surface grows: do we commit to writing a tested inverse for every
migration?

Two forces weigh against it:

- **Alpha data is disposable.** We rebuild databases from data files at will and
  may squash migrations rather than preserve their history. There is no precious
  production data whose in-place downgrade must be preserved.
- **Reversibility is expensive and often unsafe.** Every migration would need a
  hand-authored, separately tested inverse, and down-migrations that drop columns
  or tables destroy data — rarely the correct answer even when they run cleanly.

## Decision

**Migrations are forward-only. There is no `migration down` (or any rollback)
command.** `ecdb` offers `create` (applies all migrations to a new database) and
`migration up` (applies missing migrations to an existing one). To move a database
backwards, recreate it with `ecdb create --overwrite` and reload its data.

## Consequences

- The migration list stays a plain append-only forward sequence — no inverse SQL
  to author, test, or keep in sync.
- The "go backwards" story depends on disposability and data-file reload. That is
  fine during alpha; it is the assumption to revisit if production data ever
  becomes precious.
- Reinforces the version check: an old binary against a newer schema **hard-fails**
  (`ErrVersionMismatch`) rather than silently downgrading.
- **Not a frozen surface.** This is a policy, not an on-disk contract. If
  reversibility is ever needed, it is a new command plus per-migration down
  scripts — a conscious, larger change — not a compatibility break.
