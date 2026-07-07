# Storing State as Timebound Facts

This is a design exploration, not an authoritative specification.

## The Coordinate Is the Turn

Game state is stored as **timebound facts**: rows that record what is true over a
range of game turns. There is one store and one coordinate — the **turn**.

The turn is a logical clock. It does not record wall-clock time; it guarantees
*ordering* — turn 12 comes after turn 8 and before turn 15 — and marks *when, in
game time,* a fact holds. Every timebound row describes its validity as a range of
turns, and every query asks what was true "as of" some turn.

That shared coordinate — the turn — is what makes the rest of this design work.

## Background: Valid-Time Temporal Tables

The technique here is not new.
It is **valid-time temporal modelling**, the same idea behind SQL:2011
*application-time periods*: a row carries the range over which its value is true,
and queries ask what was true "as of" some point.
The literature calls the range columns `valid_from` / `valid_to`; Martin Fowler
calls the pattern *Effectivity*.
(See the [Bibliography](bibliography.md) for Fowler, Snodgrass, and the SQL:2011
summary.)

The only twist for a turn-based engine is the axis.
Ordinary temporal tables measure validity in dates. Here, validity is measured in
**game turns**.
A fact is true across a range of turns, not a range of calendar time.

We name the range columns `start_at` and `end_at`.

## The Timebound Fact

A timebound fact is true over a half-open interval of turns:

```text
start_at <= as_of < end_at
```

`start_at` is the turn at which the fact becomes true. `end_at` is the turn at
which it stops being true — a turn *outside* the range.
The fact is in effect for every turn from `start_at` up to but not including
`end_at`.

Two query parameters use the same predicate:

- **`current_turn`** — the turn the engine has reached. "Is this fact true right
  now?" is `start_at <= current_turn < end_at`.
- **`as_of`** — any chosen turn, not necessarily the current one. "Was this fact
  true as of turn *k*?" is `start_at <= k < end_at`.

They are the same test. `current_turn` is just `as_of` pinned to the turn being
processed.

A fact that is true until the end of time uses a sentinel for `end_at`:
`MaxInt`. No `NULL` values. The predicate stays uniform — `as_of < MaxInt` is
always true — so the currently-true row needs no special-casing in queries.

It can feel unnatural that `end_at` names a turn where the fact is *not* true.
The payoff is chaining. When a value changes, the old row's `end_at` and the new
row's `start_at` are the *same* number. Intervals butt against each other with
no gap and no overlap, and no value ever has to be offset by one.

```text
health
entity | value | start_at | end_at
Biff   | 8hp   | 1        | 15        ← true for turns 1..14
Biff   | 3hp   | 15       | MaxInt    ← true for turns 15..
```

At exactly turn 15 the new row is true (`15 <= 15 < MaxInt`) and the old row is
not (`1 <= 15 < 15` is false). The boundary belongs to the new row, cleanly.

## Grain: One Attribute Per Table

The goal is to keep each table's context small: **one attribute per timebound
table** — a `health` table, a `location` table, a `defense_bonus` table.
Tight grain keeps intervals tight.
A unit that moves every turn but is never wounded churns rows in `location` and
leaves `health` untouched.

This is a goal, not an imperative.
Some attributes naturally travel together and may share a table; the design does
not forbid it.
But the default lean is narrow.

## Timebound Rows Are Interval State

This is the distinction the design rests on, and it is worth stating plainly.

A timebound row is not a single current value that gets overwritten in place.
It is **interval state**: `(Biff, 3hp, [15, MaxInt))` records that a value *holds*
across a span of turns.

When a value changes, the old interval is closed and a new one opened, so the row
for turns 1..14 keeps answering correctly after the change. Nothing is edited in
place, and history is retained by construction. That is what lets "what is Biff's
health as of turn 12?" be an indexed lookup rather than a recomputation.

## The CRUD Lifecycle

Because timebound rows are interval state, the usual CRUD operations take a
specific shape.

### Create

A fact comes into effect with no predecessor. Insert one open-ended row.

```text
-- Biff enters play at turn 1
INSERT health (Biff, 8hp, start_at=1, end_at=MaxInt)
```

### Read

State as of a turn is the row whose interval contains it, per table.

```text
-- Biff's health as of turn 12
SELECT value FROM health
WHERE entity = 'Biff' AND start_at <= 12 AND 12 < end_at
-- → 8hp
```

"Current" state is just `as_of = current_turn`, or equivalently the row with `end_at = MaxInt`.

### Update

A value changes.
This is never an in-place edit of `value`.
It is **close plus insert**: close the open row at the change turn, then insert the successor.

```text
-- Biff is wounded at turn 15; health 8hp → 3hp
UPDATE health SET end_at = 15
  WHERE entity = 'Biff' AND end_at = MaxInt
INSERT health (Biff, 3hp, start_at=15, end_at=MaxInt)
```

History is preserved.
The `8hp` interval still answers correctly for turns 1..14.

### Delete

A fact stops being true with no successor — a posture expires, an item is destroyed, an entity dies.
**Close the open row; insert nothing.**

```text
-- Skar's defensive posture, set during turn 12
INSERT defense_bonus (Skar, +18, start_at=12, end_at=MaxInt)

-- cleared when the next turn begins, turn 13
UPDATE defense_bonus SET end_at = 13
  WHERE entity = 'Skar' AND end_at = MaxInt
```

From turn 13 on, an as-of query for Skar's defense bonus returns nothing — the fact is no longer true — but every historical interval remains queryable.
This is a temporal delete, not a physical one.

Physical deletion of timebound rows does not happen in normal operation.
The only thing that removes rows is [compaction](#compaction) or a
[rewind](#rewinding-a-turn).

## State as of a Turn Is a Query

Given timebound tables, the state at a turn needs no stored blob.
It is the result of running the as-of predicate across every timebound table at
that turn.

```text
state as of k = for each table: the row where start_at <= k < end_at
```

You do not save state at discrete checkpoints and hope you picked the right ones.
You reconstruct state as of *any* turn with one query per table.
Many states for the price of one set of tables.

> **Reporting may want a different projection.**
> The timebound tables are shaped for one access pattern: "what is true as of turn
> *k*." Reporting has a different access pattern — summing, consolidating, and
> grouping by turn — and may justify its own projection in the data-warehouse
> tradition: aggregate tables keyed by turn, built as a separate reader of the
> same timebound facts. That is out of scope here and deserves its own essay; the
> point for now is only that the as-of tables are *one* projection, not the only
> one, and reporting need not be forced through them.

## Rewinding a Turn

To rewind the game to turn P is to discard everything that became true after P and
restore what was true at P. Because state lives entirely in the timebound tables,
this is done directly on those tables — there is nothing else to reconcile.

After a rewind to P:

- A timebound row with `start_at > P` came into effect after P.
  It should not exist.
  Discard it.
- A timebound row whose `end_at > P` was *closed* after P.
  Its closing turn is gone, so reopen it — set `end_at` back to `MaxInt`.

Rewinding to turn 14, before Biff's wound at 15, returns the `health` table to:

```text
health
entity | value | start_at | end_at
Biff   | 8hp   | 1        | MaxInt
```

The `3hp` row (opened at 15) is discarded; the `8hp` row (closed at 15) is reopened.
The tables now describe the world as it stood at turn 14.

## Compaction

History accumulates.
Every value change leaves a closed interval behind, and the timebound tables grow
without bound.

Compaction establishes a baseline at some floor turn B: materialize the full
as-of-B state as a set of rows starting at B, and discard closed intervals that
end at or before B.
The tables then answer as-of queries for any turn ≥ B.
The cost is that you can no longer reconstruct fine-grained state before the floor
— a deliberate trade of history for size.

## What This Is Not, and What Is Open

This design does not pin a storage engine for the timebound tables.
It assumes only that a fact can be addressed by turn.

It does not address persistence formats, indexing strategy, or how the timebound
tables are kept transactionally consistent with the engine's in-memory working
state during a turn run.

Several questions remain open:

- **Where does the baseline floor sit, and who advances it?**
  Compaction is a policy decision, not an engine mechanism.
- **How are the tables kept consistent if the engine crashes mid-turn?**
  If some of a turn's close-and-insert pairs are written but not all, the tables
  describe a half-resolved turn. Detecting and repairing that is its own problem.
- **Does every attribute belong in a timebound table?**
  Some derived values may be cheaper to recompute than to version.
- **How does the half-open interval interact with within-turn resolution?**
  When the engine resolves a whole turn at once, several facts may share a
  `start_at` — the turn — and an attribute can take only one value per turn.
  The model allows the shared boundary, but the consequences for as-of queries
  within a single turn deserve their own treatment.
