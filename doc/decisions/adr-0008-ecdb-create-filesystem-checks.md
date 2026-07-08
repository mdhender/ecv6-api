# ADR-0008: `ecdb create` treats its filesystem pre-checks as best-effort, and refuses non-regular files

- **Status:** accepted
- **Date:** 2026-07-08

## Context

A code review of [`cmd/ecdb`](../../cmd/ecdb/main.go) raised two questions about
`ecdb create`, which builds a fresh `ec.db` in a data folder (optionally removing
an existing one under `--overwrite`).

1. **TOCTOU in the create path.** `cmdCreate` stats the folder, stats `ec.db`,
   optionally `removeDB`s it, then calls `store.Create` — which stats the path
   again itself. Between the check and the use, a concurrent process could
   create, delete, or swap the file, so the stat-based decision can act on stale
   information. (Time-Of-Check to Time-Of-Use.)

2. **`--overwrite` and non-regular files.** If something other than a regular
   file sits at the `ec.db` path (a directory, a device, a symlink to one),
   `cmdCreate` fails *before* consulting `--overwrite`, so `--overwrite` silently
   does not apply in that case.

Both were flagged as "acceptable, but decide and document," not as bugs to fix
blindly. The relevant constraint is `ecdb`'s stated contract: it "runs commands
directly against the database, assuming it is the **only** process touching it."

## Decision

- **The pre-checks in `cmdCreate` are best-effort, and `store.Create` is the
  authority.** We keep the early `os.Stat` checks only to produce friendlier,
  earlier error messages. We do **not** make the create path race-free (e.g. with
  an atomic `O_CREATE|O_EXCL` open), because `ecdb`'s single-writer contract means
  the race window should not occur in practice, and `store.Create` already
  re-checks existence and returns `os.ErrExist` as the real guard. A comment in
  `cmdCreate` records this so a future reader does not mistake the pre-check for a
  guarantee.

- **`--overwrite` deliberately does not apply to a non-regular file.** If the
  `ec.db` path is not a regular file, `ecdb create` fails even with `--overwrite`,
  rather than removing it. `--overwrite` is a convenience for replacing a real
  database file; it is not a license to `rm` a directory, device, or other object
  that happens to occupy that path. A comment records the intent.

## Consequences

- Create stays simple: no atomic-open dance, no lock file. The friendly
  pre-check errors are kept without pretending they are safety guarantees.
- The single-writer assumption is now explicit at this call site as well as in
  the package doc. If `ecdb` ever needs to tolerate concurrent writers, this
  decision must be revisited — the pre-checks alone would not make it safe.
- `--overwrite` has a bounded blast radius: the worst it can delete is a regular
  `ec.db` (and its `-wal`/`-shm`/`-journal` sidecars via `removeDB`), never a
  directory or device. An operator who has put something unexpected at that path
  gets a hard error and must clear it themselves.
- **Not a frozen surface.** These are CLI behavior choices, revisable at any time;
  they touch neither on-disk format nor the determinism contract (ADR-0001).
