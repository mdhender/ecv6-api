# ADR-0010: Backup and compaction are separate commands; backup never mutates the source

- **Status:** accepted
- **Date:** 2026-07-08

## Context

`ecdb` is gaining a database backup command
([issue #1](https://github.com/mdhender/ecv6-api/issues/1)) and, separately, we are
exploring an in-place compaction command
([issue #8](https://github.com/mdhender/ecv6-api/issues/8)). Both are built on the
same SQLite primitive: `backup` uses `VACUUM INTO 'dest'` to write a clean
single-file copy, and `compact` would run a plain `VACUUM` to rewrite a database in
place, reclaiming free pages (we prefer soft deletes, so freed space accrues) and
defragmenting.

Because they share the primitive, a natural suggestion arose: give `backup` a
`--compact` flag (default off) that runs `VACUUM` on the source before copying it.
The symmetry is appealing, so it is worth writing down why we are *not* doing it.

The key fact is that **`VACUUM INTO` already produces a compacted file.** It is a
vacuum: the destination it writes is defragmented and free-page-reclaimed by
construction. A `--compact` flag therefore cannot make the *backup* any smaller —
its only possible effect is to compact the *source* database in place before the
copy is taken.

## Decision

**Backup and compaction stay separate commands, and `backup` never modifies the
source database.** We will not add a `--compact` flag (or any other
source-mutating option) to `ecdb backup`.

- `ecdb backup` opens the source read-only and writes a compacted copy via
  `VACUUM INTO`. The backup is compact regardless; no flag is needed to make it so.
- Compacting a database in place is the job of the separate `ecdb compact` command
  (issue #8), run explicitly.
- If an operator wants both a snapshot and a compacted source, they compose the two
  commands (`ecdb backup …` then `ecdb compact …`).

## Consequences

- **`backup` keeps its safety promise.** It is the command you reach for *because*
  you are about to do something risky; it must not itself be destructive. An
  in-place `VACUUM` needs an exclusive lock and roughly 2× the database size in free
  space, and a crash mid-vacuum endangers the very database you were protecting.
  Keeping the source read-only removes that hazard from the safe operation.
- **Each verb has one job**, which keeps the CLI predictable and composable and
  avoids one command that silently performs two mutations. This matches the
  single-purpose stance behind the output channels in
  [ADR-0009](adr-0009-output-channels-stdout-stderr-slog.md).
- **No redundant work.** Compact-source-then-`VACUUM INTO` would rewrite the whole
  file twice to produce a backup that a single `VACUUM INTO` yields already.
- **The destructive verb owns the danger.** If we later want a one-step
  "back up, then compact" convenience, the right home is an option on `compact`
  (which is already destructive), not on `backup`. Should that need arise, revisit
  issue #8 rather than this decision.
- **Not a frozen surface.** These are CLI-shape choices, revisable at any time; they
  touch neither on-disk format nor the determinism contract (ADR-0001).

## Addendum (2026-07-08): the command chooses the backup file name

[Issue #10](https://github.com/mdhender/ecv6-api/issues/10) revisited how backups
are named, which surfaced a design point worth recording alongside the backup
decision above.

`ecdb backup` chooses the backup file name (`ec.db.<timestamp-utc>`); the caller
cannot supply one. This is partly ergonomic but mostly a **safety** measure: a
backup that shared the live `ec.db` name could be mistaken for the database and, for
example, migrated or served by accident. Keeping backups off the `ec.db` name
prevents that. The trade-off is that a backup's schema version is not obvious from
its name.

To recover that without weakening the invariant, `backup` gains an opt-in
`--version-stamp` flag (default off). When set, it appends the migration (schema)
version — `ec.db.<timestamp-utc>-<version>` — so a backup states which schema it
holds (useful when matching a backup to a binary during an upgrade or rollback; see
the [upgrade how-to](../how-to/upgrade-ec-and-ecdb.md)). The version is the
database's `user_version`, not the application's release version. The caller still
never supplies a name; the flag only toggles a fixed suffix the command applies
itself.
