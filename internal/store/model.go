// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"time"

	"github.com/mdhender/ecv6-api/internal/cerrs"
	"zombiezen.com/go/sqlite"
)

// Row-level store errors. ErrNotFound (defined in store.go) reports a missing
// database *file*; these report conditions on the rows within an open store.
const (
	// ErrRecordNotFound is returned when a requested row does not exist.
	ErrRecordNotFound = cerrs.Error("record not found")
	// ErrConflict is returned when a write violates a uniqueness constraint —
	// a duplicate account email, or an account seated twice in one game.
	ErrConflict = cerrs.Error("conflict")
)

// Account is an application-domain account (doc/model.md). Email is the identity,
// stored lowercased and unique across active and inactive accounts alike; the
// application role is admin when IsAdmin is set, otherwise user (ADR-0004).
// HashedSecret is the stored hash of the login secret — never the plaintext.
type Account struct {
	ID           int64
	Email        string
	DisplayName  string
	HashedSecret string
	IsAdmin      bool
	IsActive     bool
}

// Session is a server-side bearer session (ADR-0002). ID is the opaque public
// identifier used in URLs; HashedToken is the hash of the credential the client
// presents (the raw token is shown once, at login, and never stored). Actor is
// the admin behind an impersonation session, zero for an ordinary session.
// RevokedAt is the zero time while the session is active.
type Session struct {
	ID          string
	AccountID   int64
	HashedToken string
	Actor       int64
	IssuedAt    time.Time
	ExpiresAt   time.Time
	RevokedAt   time.Time
}

// Revoked reports whether the session has been revoked.
func (s Session) Revoked() bool { return !s.RevokedAt.IsZero() }

// Game is a game in the application catalog (ADR-0003). Status advances the
// lifecycle (draft, recruiting, active, paused, complete, archived); IsActive is
// the admin-only visibility flag, orthogonal to status. Engine-owned fields
// (master seeds, current turn) live with the game engine and are added when the
// engine lands, not here.
type Game struct {
	ID          int64
	Name        string
	Status      string
	Description string
	IsActive    bool
}

// EngineState is a game's engine-owned root state — the row of game_engine_state,
// kept separate from the application-domain Game so the two domains stay separate
// (ADR-0013). Seed1/Seed2 are the two uint64 master seeds that root all
// determinism (doc/determinism.md, internal/prng); SQLite has no unsigned type, so
// the accessors store and read the uint64 bit pattern via an INTEGER and the sign
// is meaningless. Seeds are assigned at setup time, not game creation. CurrentTurn
// is the engine clock: 0 is setup, play starts at 1.
type EngineState struct {
	GameID      int64
	Seed1       uint64
	Seed2       uint64
	CurrentTurn int
}

// Member is one seat in a game's roster — a row of game_account_role, the
// boundary table between the two domains (doc/control-and-ownership.md). PlayerID
// is the game-side key (sequential within GameID, never reused); AccountID links
// the seat to its account and is application-only. A dropped seat keeps its row
// with IsActive false.
type Member struct {
	GameID    int64
	PlayerID  int64
	AccountID int64
	IsGM      bool
	IsActive  bool
}

// MyGame projects a game together with the caller's seat in it, for the
// per-account "my games" listing (the MyGame wire schema). IsActive is the
// game's own active flag.
type MyGame struct {
	GameID   int64
	Name     string
	IsActive bool
	PlayerID int64
	IsGM     bool
}

// isConstraint reports whether err is a SQLite constraint violation (a UNIQUE,
// CHECK, NOT NULL, or foreign-key failure), which the store maps to ErrConflict
// for the callers it can occur on.
func isConstraint(err error) bool {
	return sqlite.ErrCode(err).ToPrimary() == sqlite.ResultConstraint
}

// unixOrZero converts a stored Unix-seconds column to a time.Time in UTC,
// returning the zero Time for a stored 0 (used for absent timestamps).
func unixOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// unixSeconds converts a time.Time to the Unix-seconds form stored in a column,
// mapping the zero Time to 0.
func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
