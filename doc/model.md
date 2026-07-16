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

For how the concepts *relate* — control vs. ownership, controllers (player/NPC),
factions, and the asset chain — see
[control-and-ownership.md](control-and-ownership.md). This page is the narrower
type/schema mapping.

## Concept ↔ type ↔ schema

| Concept | Go type | Stored as | Invariants to enforce |
| --- | --- | --- | --- |
| Game | `store.Game` | `games` | integer PK, `name` the human label (ADR-0003 — the earlier "slug" is superseded); `status` lifecycle + `is_active` visibility. Engine-owned `seed1`/`seed2` (`uint64`) and current turn (`0` = setup) are **deferred** — they land with the engine, not the application schema |
| Turn | `TODO` | `TODO` | `0` = setup/no-turn (zero value); advances only on GM action; a report reflects the **start** of its turn |
| Player | `TODO` | `TODO` | `id` positive int, sequential, **never reused**; `email` lowercased, unique within game across active **and** inactive; active/inactive state, never physically deleted |
| Password | `TODO` | `TODO` | plaintext shared secret; JSON-safe, space-free |
| Cluster | `store.Cluster` | `cluster` | one per game (`game_id` PK); holds the placement stage's derived radius `R` and the settings used (`n`, `density`, `spacing`); `R` is a pure function of `N` and density (no randomness); generated once at setup, immutable (no turn axis). Placement lives in `internal/genesis` |
| System | `store.System` | `system` | addressed by axial `(q, r)`, PK `(game_id, q, r)`; the placement output. Contents (orbits/planets), drawn order-independently from a stream keyed by `(q, r)`, arrive in a later stage |
| Generator selection | `store.GeneratorSelection` | `game_generator` | one row per generation stage per game (`placement`, `system_contents`, `deposits`); records `(generator_id, version, settings)`, settings as opaque stage-specific JSON (ADR-0016). PK `(game_id, stage)` |
| Orders | `TODO` | `TODO` | plain text; applied together at turn processing; do not advance the current turn |

## Application domain (implemented)

The application-side persistence has landed in `internal/store` (migration 1). The
authoritative field-level reference is the godoc on each type; this table is the
concept map. All four tables soft-delete via `is_active`, except `sessions`, which
records *when* a session died in `revoked_at` (ADR-0002).

| Concept | Go type | Stored as | Invariants to enforce |
| --- | --- | --- | --- |
| Account | `store.Account` | `accounts` | integer PK; `email` lowercased and unique across active **and** inactive (ADR-0003); application role is `admin` when `is_admin`, else `user` (ADR-0004); only the secret **hash** is stored |
| Session | `store.Session` | `sessions` | opaque public `id`; only the token **hash** is stored, unique (ADR-0002); `revoked_at`/`expires_at` gate "active"; `actor_account_id` names the admin behind an impersonation (else NULL) |
| Game | `store.Game` | `games` | see the Game row above; `status` constrained to the lifecycle enum |
| Member (seat) | `store.Member` | `game_account_role` | the boundary table; `player_id` is sequential within its game and **never reused** (`MAX(player_id)+1`, spanning active + inactive), immutable once assigned (ADR-0003); one seat per account per game; `account_id` is application-only, the engine addresses control by `player_id` |

## Invariants worth calling out

- **Ids never reused.** A removed player keeps its id and continues to occupy its
  email; uniqueness spans active and inactive alike.
- **Email is identity, lowercased.** Lowercase on write; compare lowercased.
- **Derived data belongs to its game.** A subsystem stores its derived seeds with
  its own data. Sharing across games is a dev/test convenience only; production
  games never share data.
