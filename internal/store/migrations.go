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
