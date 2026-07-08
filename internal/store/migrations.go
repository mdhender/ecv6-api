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

// migration0001 lays down the application-domain tables documented in CLAUDE.md:
// accounts, games, and the game_account_role membership bridge. Game-engine
// tables arrive in later migrations. Soft deletes are the norm (is_active),
// so ids are never reused.
const migration0001 = `
CREATE TABLE accounts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT    NOT NULL UNIQUE,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    is_active     INTEGER NOT NULL DEFAULT 1,
    hashed_secret TEXT    NOT NULL
);

CREATE TABLE games (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    name      TEXT    NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE game_account_role (
    game_id    INTEGER NOT NULL REFERENCES games (id),
    account_id INTEGER NOT NULL REFERENCES accounts (id),
    is_gm      INTEGER NOT NULL DEFAULT 0,
    is_active  INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (game_id, account_id)
)`
