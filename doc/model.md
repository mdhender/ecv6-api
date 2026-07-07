# Domain model (implementation mapping)

The domain **concepts** are defined in the docs repo. This page maps each concept
to its Go type and stored representation, and records the **invariants the store
must guarantee** — invariants the docs state as rules and the schema has to
enforce.

Concept definitions:
[ecv6-docs `content/reference/`](https://github.com/mdhender/ecv6-docs/tree/main/content/reference)
(`games.md`, `players.md`, `cluster.md`, `turns.md`, `orders.md`, `glossary.md`).

> **Status: skeleton.** Types and schema are filled in as packages land. Keep the
> "Invariant" column authoritative even before the types exist — it is the
> checklist the store is tested against.

## Concept ↔ type ↔ schema

| Concept | Go type | Stored as | Invariants to enforce |
| --- | --- | --- | --- |
| Game | `TODO` | `TODO` | id is a JSON-safe, space-free slug; holds `seed1`,`seed2` (`uint64`) and a current turn (`0` = setup) |
| Turn | `TODO` | `TODO` | `0` = setup/no-turn (zero value); advances only on GM action; a report reflects the **start** of its turn |
| Player | `TODO` | `TODO` | `id` positive int, sequential, **never reused**; `email` lowercased, unique within game across active **and** inactive; active/inactive state, never physically deleted |
| Password | `TODO` | `TODO` | plaintext shared secret; JSON-safe, space-free |
| Cluster | `TODO` | `TODO` | one per game; derives its own seeds from the game's; generated once at setup |
| System | `TODO` | `TODO` | addressed by axial `(q, r)`; contents drawn from a stream keyed by `(q, r)`, order-independent |
| Orders | `TODO` | `TODO` | plain text; applied together at turn processing; do not advance the current turn |

## Invariants worth calling out

- **Ids never reused.** A removed player keeps its id and continues to occupy its
  email; uniqueness spans active and inactive alike.
- **Email is identity, lowercased.** Lowercase on write; compare lowercased.
- **Derived data belongs to its game.** A subsystem stores its derived seeds with
  its own data. Sharing across games is a dev/test convenience only; production
  games never share data.
