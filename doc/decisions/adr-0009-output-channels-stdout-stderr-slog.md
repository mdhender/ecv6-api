# ADR-0009: Three output channels — stdout for results, stderr for errors, `slog` for logging

- **Status:** accepted
- **Date:** 2026-07-08

## Context

`cmd/ecdb`'s output has flip-flopped between two styles. An early version emitted
`slog` lines for everything — status, results, and errors alike
(`time=… level=INFO msg="created database" path=… version=1`). PR #3 (`b5df0dc`)
replaced that with plain `fmt` output, and PR #4/#5 then had to reconcile the docs
with the new format. This is the second time output formatting has churned across
these commands, and code and docs drifted apart in between.

The churn comes from conflating three distinct concerns a program emits, which
have different audiences and belong on different channels:

1. **Results** — the expected output the caller asked for (`migration version`'s
   schema number). A script needs to capture this cleanly.
2. **Errors** — reports of unexpected conditions, for the operator running the
   command, that also decide the process's exit status.
3. **Logs** — leveled, filterable information written for the *developer or agent*,
   meant to (eventually) be captured to a persistent log.

`slog` is the right tool for the third and the wrong tool for the first two: it
decorates every line with a timestamp and level, which is noise on a result a
script must parse, and it tempts each command to invent its own key/value shape
(as the `version`/`expected` pair did). Conversely, plain `fmt` output has no
levels and no structure to persist and filter, so it is the wrong tool for logs.

On error handling we mostly follow Dave Cheney,
[*Let's talk about logging*](https://dave.cheney.net/2015/11/05/lets-talk-about-logging):
an error should be *handled*, not automatically stop the program. When we handle
an error and continue, we still want a record that it happened — that is what the
log is for. Even around a panic, the contributing detail is worth logging, with
the caveat that persistence on panic is a race (no guarantee the log was flushed
before the process dies).

## Decision

A program emits on three channels, each with one purpose:

- **stdout — expected output.** The result the command was asked to produce. It may
  be free text or structured, but it is *only* the result, with nothing a caller
  must strip. Written with the standard library's `fmt`. `ecdb migration version`
  prints a bare integer and nothing else, so `V=$(ecdb migration version PATH)`
  captures a clean value.

- **stderr — error reporting.** Unexpected conditions, for the operator. Formatted
  conventionally as `<name>: <message>` and built from `%w`-wrapped errors so the
  cause chain is visible. The command also returns a **non-zero exit status**.
  Written with `fmt`. The shared `internal/cli.Run` renders the final error and
  sets the exit code; commands return wrapped errors rather than printing and
  exiting themselves.

- **`slog` — logging.** Leveled information (DEBUG / INFO / ERROR) that the
  *developer or agent* might want to filter by level, written for them (not for the
  end user) and intended to be captured to a persistent log. This is where we
  record that an error was *handled and execution continued*, and where panic
  context is logged (best-effort — persistence on panic is a race). `slog` is a
  channel defined by audience and purpose, available to any binary — it is **not**
  restricted to the long-running server, though the server's request lifecycle and
  background events will be its heaviest users once that logging lands.

- **Anything that needs a logger accepts one; it does not reach for the global.**
  A component that needs a `context.Context` or a logger receives it as an explicit
  parameter, or holds it as a struct field, rather than calling the package-level
  `slog` functions (`slog.Info`, …) that use the default logger. This keeps the
  dependency visible and the component testable, and lets a caller scope or silence
  logging without global state. The standard library ships a global default logger
  and we live with that — in fact we install our configured logger *as* the default
  (`slog.SetDefault`) so any call site we miss is still correctly filtered — but the
  default is a backstop, not the wiring path. Correctness never depends on it.

These channels are not mutually exclusive: the same error can be **logged** via
`slog` (so there is a durable record it occurred) and, when it is terminal for the
command, **reported** on stderr with a non-zero exit. Logging an error and
surfacing it are different acts for different audiences.

### Selecting the log level

Every command accepts `--logging-level` (`DEBUG` | `INFO` | `WARN` | `ERROR`,
case-insensitive), also settable via the binary's env prefix
(`ECDB_LOGGING_LEVEL`, `EC_LOGGING_LEVEL`); the flag wins over the env var. A
single string flag is used rather than one boolean per level, so there are no
ambiguous combinations (`--debug --info`) and the set extends cleanly.

The default is **`INFO`**. This is not an arbitrary pick: it is `slog`'s own
default — `slog.HandlerOptions.Level` documents that a nil `Level` "assumes
`LevelInfo`" — so the no-flag path matches a bare `slog` handler. The level
constants are `LevelDebug=-4`, `LevelInfo=0`, `LevelWarn=4`, `LevelError=8`, so
`INFO` discards only `DEBUG`.

`ERROR` is the floor and cannot be disabled: the resolved `slog.Level` is clamped
to at most `LevelError`, and there is no in-set level above `ERROR` to turn it off.
An unknown or misspelled level name is a usage error — reported on stderr as
`<name>: unknown logging level %q` with a non-zero exit (error *reporting* per the
channels above), distinct from the log threshold itself.

The level governs only the `slog` channel — it never changes what a command writes
to stdout (results) or stderr (error reports); a quiet command stays quiet on
stdout regardless of level. The logger is constructed in `internal/cli` (over a
`slog.LevelVar` set to the resolved level), writes to stderr, and is passed into
the command tree as a parameter; it is also installed with `slog.SetDefault` so
that any call site that slips through to the package-level `slog` functions is
still filtered correctly (the backstop described above). Handler format
(text vs JSON) is out of scope here and defaults to text.

## Consequences

- Command output is scriptable by default: results on stdout stay clean because
  status, errors, and logs never land there.
- The result / error / log split is one rule across every binary, so new
  subcommands have an obvious pattern and the docs have one format to describe. The
  [database how-to](../how-to/create-and-verify-a-database.md) and
  [reference](../reference/database-management.md) already reflect the stdout/stderr
  behavior.
- Accepting context and loggers as parameters (or struct fields) makes the
  dependency explicit at each call site and keeps components testable; the global
  default is a filtered backstop for missed sites, not something correctness leans
  on.
- No `slog` logging exists yet. This ADR fixes the boundary now so that when it
  lands it flows through parameters and stays off stdout/stderr, rather than being
  bolted on and re-triggering the churn.
- Doc examples are generated from the real binary and regenerated when output
  changes — the exact text is not a stability guarantee (the lesson of PR #4/#5).
- **Not a frozen surface.** These are presentation and plumbing choices, revisable
  at any time; they touch neither on-disk format nor the determinism contract
  (ADR-0001). If a command's output ever needs to change, this ADR is the rule to
  check against instead of re-litigating stdout-vs-`slog` a third time.
