// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

// migrations is the ordered list of schema migrations. sqlitemigration records
// the count applied in PRAGMA user_version, which is the database's version.
//
// Never edit or reorder a migration once databases exist in the wild — append a
// new one, because changing an applied migration silently diverges existing
// databases. During alpha, data is disposable, so squashing early migrations
// into a new baseline is allowed.
var migrations = []string{
	migration0001,
	migration0002,
	migration0003,
	migration0004,
	migration0005,
}

// migration0001 lays down the application-domain tables: accounts, sessions,
// games, and the game_account_role membership bridge (the boundary between the
// application and game domains). Game-engine tables (faction, cluster, turns,
// and the timebound-fact tables) arrive in later migrations.
//
// Conventions, grounded in the docs:
//
//   - Soft deletes are the norm (is_active), so ids are never reused — a removed
//     account/game/seat keeps its row and its id (doc/model.md). Sessions are the
//     exception: they carry a revoked_at timestamp instead of is_active, because
//     revocation records *when* a session died (ADR-0002).
//   - Accounts are identified by a lowercased, unique email; the seat (a row in
//     game_account_role) is identified by player_id, sequential and unique within
//     its game and never reused (ADR-0003). account_id is application-only; the
//     engine addresses control by player_id (doc/control-and-ownership.md).
//   - Timestamps are stored as INTEGER Unix seconds (UTC), never NULL except
//     sessions.revoked_at, whose NULL means "still active".
const migration0001 = `
CREATE TABLE accounts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT    NOT NULL UNIQUE,        -- stored lowercased (ADR-0003)
    display_name  TEXT    NOT NULL DEFAULT '',
    hashed_secret TEXT    NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,     -- application role: admin vs user (ADR-0004)
    is_active     INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE sessions (
    id               TEXT    NOT NULL PRIMARY KEY,               -- opaque, public session id
    account_id       INTEGER NOT NULL REFERENCES accounts (id),  -- the effective identity
    hashed_token     TEXT    NOT NULL UNIQUE,                    -- only the hash is stored (ADR-0002)
    actor_account_id INTEGER REFERENCES accounts (id),           -- admin behind an impersonation, else NULL
    issued_at        INTEGER NOT NULL,                           -- Unix seconds (UTC)
    expires_at       INTEGER NOT NULL,
    revoked_at       INTEGER                                     -- NULL = active; set = soft-deleted
);

CREATE INDEX sessions_by_account ON sessions (account_id);

CREATE TABLE games (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    status      TEXT    NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'recruiting', 'active', 'paused', 'complete', 'archived')),
    description TEXT    NOT NULL DEFAULT '',
    is_active   INTEGER NOT NULL DEFAULT 1        -- admin visibility, orthogonal to status
);

CREATE TABLE game_account_role (
    game_id    INTEGER NOT NULL REFERENCES games (id),
    player_id  INTEGER NOT NULL,                  -- seat id: sequential in-game, never reused (ADR-0003)
    account_id INTEGER NOT NULL REFERENCES accounts (id),
    is_gm      INTEGER NOT NULL DEFAULT 0,
    is_active  INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (game_id, player_id),
    UNIQUE (game_id, account_id)                  -- an account holds at most one seat per game
)`

// migration0002 lays down the first game-engine table: per-game engine state,
// kept separate from the application-domain games row so the two domains stay
// separate (ADR-0013). One row per game.
//
//   - seed1/seed2 are the game's two uint64 master seeds — the root of all
//     determinism (doc/determinism.md, internal/prng). SQLite has no unsigned
//     type, so the uint64 bit pattern is stored in an INTEGER and reinterpreted
//     on read; the sign is meaningless.
//   - current_turn is the engine's clock: turn 0 is setup, play starts at 1
//     (doc/storing-state-as-timebound-facts.md).
const migration0002 = `
CREATE TABLE game_engine_state (
    game_id      INTEGER NOT NULL PRIMARY KEY REFERENCES games (id),
    seed1        INTEGER NOT NULL,                 -- uint64 master seed (bit pattern; ADR-0013)
    seed2        INTEGER NOT NULL,                 -- uint64 master seed (bit pattern)
    current_turn INTEGER NOT NULL DEFAULT 0        -- turn 0 = setup; play starts at 1
)`

// migration0003 makes a game's name distinct across ALL games — active or
// inactive (issue #72). Names are stored upper-cased by the create/update paths
// (as accounts lowercase email), so a plain case-sensitive unique index enforces
// the rule; "ec01" and "EC01" normalize to the same stored value and collide.
// A duplicate INSERT/UPDATE surfaces as ErrConflict (see isConstraint).
const migration0003 = `
CREATE UNIQUE INDEX games_name_unique ON games (name)`

// migration0004 lays down the cluster-generation tables: the cluster and its
// systems (the placement stage's output), plus the per-game record of which
// generator, version, and settings each generation stage ran. Generation output
// is start-of-life state, decided once at setup and immutable thereafter (the
// cluster core reference), so these carry no turn axis and no soft-delete flag —
// regenerating during alpha replaces the rows outright.
//
// Grounding: the schema and vocabulary (axial (q, r), a cluster of N systems)
// are the cluster core reference; the placement settings (N, density, spacing)
// and the three staged generators are the Genesis Placement supplement and
// ADR-0016. See internal/genesis.
//
//   - cluster — one row per game: the derived radius R and the placement settings
//     actually used (N, density tier, spacing S). game_id is the primary key, so a
//     game has at most one cluster.
//   - system — the placed hexes, addressed by axial (q, r) within a game. Ten
//     orbits and their planets arrive in a later stage's migration; this table is
//     just the placement output.
//   - game_generator — the (stage, generator, version, settings) a game records
//     for each of the three stages (ADR-0016: a game records three generator pairs).
//     settings is opaque JSON so a stage can carry its own shape (deposits will add
//     abundance knobs) without a schema change. Only the placement row is written
//     today; the CHECK enumerates all three stages the schema accommodates.
const migration0004 = `
CREATE TABLE cluster (
    game_id INTEGER NOT NULL PRIMARY KEY REFERENCES games (id),
    radius  INTEGER NOT NULL,                 -- R(N, D), derived from N and density (no randomness)
    n       INTEGER NOT NULL,                 -- number of systems requested and placed
    density TEXT    NOT NULL,                 -- stellar-density tier used
    spacing INTEGER NOT NULL                  -- minimum system spacing S used
);

CREATE TABLE system (
    game_id INTEGER NOT NULL REFERENCES games (id),
    q       INTEGER NOT NULL,                 -- axial coordinate
    r       INTEGER NOT NULL,                 -- axial coordinate
    PRIMARY KEY (game_id, q, r)
);

CREATE TABLE game_generator (
    game_id      INTEGER NOT NULL REFERENCES games (id),
    stage        TEXT    NOT NULL
                 CHECK (stage IN ('placement', 'system_contents', 'deposits')),
    generator_id INTEGER NOT NULL,            -- generator identity within the stage
    version      INTEGER NOT NULL,            -- generator version (immutable once a game depends on it)
    settings     TEXT    NOT NULL DEFAULT '{}', -- opaque, stage-specific JSON
    PRIMARY KEY (game_id, stage)
)`

// migration0005 lays down the system-contents stage's output: the planets that
// occupy each system's orbits, plus the fixed home-system template. Like the
// placement tables (migration0004) this is start-of-life state, decided once at
// setup and immutable thereafter (the cluster core reference), so it carries no
// turn axis and no soft-delete flag — regenerating during alpha replaces the rows.
//
// Grounding: the schema and vocabulary (ten orbits, planet types, per-planet
// habitability) are the cluster core reference; which planet occupies which orbit
// and its habitability are the Genesis System Contents supplement (ADR-0016). See
// internal/genesis.
//
//   - planet — one row per occupied orbit of a system, keyed by the system's
//     (game_id, q, r) and its orbit. Empty orbits carry NO row. type and orbit are
//     schema (constrained by CHECK); habitability is the generator's per-planet
//     value, in 0..25. It references the system it belongs to via (game_id, q, r).
//   - home_template — the one fixed home-system template per game, keyed by orbit.
//     The template is generated once; the per-player copy onto a chosen system is a
//     later step and does not touch this table.
const migration0005 = `
CREATE TABLE planet (
    game_id      INTEGER NOT NULL,
    q            INTEGER NOT NULL,                 -- axial coordinate of the system
    r            INTEGER NOT NULL,                 -- axial coordinate of the system
    orbit        INTEGER NOT NULL CHECK (orbit BETWEEN 1 AND 10),
    type         TEXT    NOT NULL
                 CHECK (type IN ('rocky', 'asteroid belt', 'gas giant')),
    habitability INTEGER NOT NULL CHECK (habitability BETWEEN 0 AND 25),
    PRIMARY KEY (game_id, q, r, orbit),
    FOREIGN KEY (game_id, q, r) REFERENCES system (game_id, q, r)
);

CREATE TABLE home_template (
    game_id      INTEGER NOT NULL REFERENCES games (id),
    orbit        INTEGER NOT NULL CHECK (orbit BETWEEN 1 AND 10),
    type         TEXT    NOT NULL
                 CHECK (type IN ('rocky', 'asteroid belt', 'gas giant')),
    habitability INTEGER NOT NULL CHECK (habitability BETWEEN 0 AND 25),
    PRIMARY KEY (game_id, orbit)
)`
