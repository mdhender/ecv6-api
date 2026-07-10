// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestDB opens an isolated in-memory store for a test.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenTemporary(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAccountCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	// Create; email is lowercased on write.
	id, err := db.CreateAccount(ctx, Account{Email: "Alice@Example.COM", DisplayName: "Alice", HashedSecret: "h1", IsAdmin: true, IsActive: true})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if id != 1 {
		t.Errorf("first account id = %d, want 1", id)
	}

	got, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("email = %q, want lowercased alice@example.com", got.Email)
	}
	if !got.IsAdmin || !got.IsActive || got.DisplayName != "Alice" || got.HashedSecret != "h1" {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	// Lookup by email is case-insensitive.
	byEmail, err := db.GetAccountByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatalf("GetAccountByEmail: %v", err)
	}
	if byEmail.ID != id {
		t.Errorf("GetAccountByEmail id = %d, want %d", byEmail.ID, id)
	}

	// Duplicate email (any case) conflicts.
	if _, err := db.CreateAccount(ctx, Account{Email: "alice@example.com", HashedSecret: "h2"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate email error = %v, want ErrConflict", err)
	}

	// Update mutable fields.
	got.DisplayName, got.IsAdmin, got.HashedSecret = "Alice B", false, "h2"
	if err := db.UpdateAccount(ctx, got); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	after, _ := db.GetAccount(ctx, id)
	if after.DisplayName != "Alice B" || after.IsAdmin || after.HashedSecret != "h2" {
		t.Errorf("after update: %+v", after)
	}

	// A second account, then a listing ordered by id.
	if _, err := db.CreateAccount(ctx, Account{Email: "bob@example.com", HashedSecret: "h3", IsActive: false}); err != nil {
		t.Fatalf("CreateAccount bob: %v", err)
	}
	list, err := db.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(list) != 2 || list[0].Email != "alice@example.com" || list[1].Email != "bob@example.com" {
		t.Errorf("ListAccounts = %+v", list)
	}
}

func TestAccountNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := db.GetAccount(ctx, 404); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetAccount error = %v, want ErrRecordNotFound", err)
	}
	if _, err := db.GetAccountByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetAccountByEmail error = %v, want ErrRecordNotFound", err)
	}
	if err := db.UpdateAccount(ctx, Account{ID: 404, Email: "x@y.z", HashedSecret: "h"}); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("UpdateAccount error = %v, want ErrRecordNotFound", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	acct, err := db.CreateAccount(ctx, Account{Email: "user@example.com", HashedSecret: "h", IsActive: true})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)

	s := Session{ID: "sess-1", AccountID: acct, HashedToken: "tok-1", IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := db.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Round-trip by id.
	got, err := db.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.AccountID != acct || got.Actor != 0 || got.Revoked() {
		t.Errorf("session round-trip: %+v", got)
	}
	if !got.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Errorf("expiresAt = %v, want %v", got.ExpiresAt, now.Add(time.Hour))
	}

	// Active lookup by token succeeds while unexpired and unrevoked.
	if _, err := db.GetActiveSessionByToken(ctx, "tok-1", now); err != nil {
		t.Fatalf("GetActiveSessionByToken (fresh): %v", err)
	}
	// An expired session is not "active".
	if _, err := db.GetActiveSessionByToken(ctx, "tok-1", now.Add(2*time.Hour)); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("expired active lookup = %v, want ErrRecordNotFound", err)
	}

	// Listing returns the one active session.
	active, err := db.ListActiveSessionsByAccount(ctx, acct, now)
	if err != nil {
		t.Fatalf("ListActiveSessionsByAccount: %v", err)
	}
	if len(active) != 1 || active[0].ID != "sess-1" {
		t.Errorf("active sessions = %+v", active)
	}

	// Revoke it; it disappears from active views but the row remains readable.
	if err := db.RevokeSession(ctx, "sess-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := db.GetActiveSessionByToken(ctx, "tok-1", now); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("revoked active lookup = %v, want ErrRecordNotFound", err)
	}
	if r, err := db.GetSession(ctx, "sess-1"); err != nil || !r.Revoked() {
		t.Errorf("GetSession after revoke: %+v err=%v", r, err)
	}
	if active, _ := db.ListActiveSessionsByAccount(ctx, acct, now); len(active) != 0 {
		t.Errorf("active after revoke = %d, want 0", len(active))
	}

	// Revoking an unknown session is ErrRecordNotFound.
	if err := db.RevokeSession(ctx, "nope", now); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("RevokeSession(unknown) = %v, want ErrRecordNotFound", err)
	}
}

func TestSessionRevokeAllAndExcept(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	acct, _ := db.CreateAccount(ctx, Account{Email: "u@example.com", HashedSecret: "h"})
	now := time.Now().UTC().Truncate(time.Second)

	for i, id := range []string{"a", "b", "c"} {
		s := Session{ID: id, AccountID: acct, HashedToken: "t" + id, IssuedAt: now.Add(time.Duration(i) * time.Second), ExpiresAt: now.Add(time.Hour)}
		if err := db.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession %s: %v", id, err)
		}
	}

	// Revoke all but "b" (the "keep the current session" case).
	n, err := db.RevokeAccountSessionsExcept(ctx, acct, "b", now)
	if err != nil {
		t.Fatalf("RevokeAccountSessionsExcept: %v", err)
	}
	if n != 2 {
		t.Errorf("revoked (except) = %d, want 2", n)
	}
	if active, _ := db.ListActiveSessionsByAccount(ctx, acct, now); len(active) != 1 || active[0].ID != "b" {
		t.Errorf("active after except-revoke = %+v", active)
	}

	// Revoke the remainder.
	n, err = db.RevokeAccountSessions(ctx, acct, now)
	if err != nil {
		t.Fatalf("RevokeAccountSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("revoked (all) = %d, want 1", n)
	}
}

func TestSessionImpersonationActor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	admin, _ := db.CreateAccount(ctx, Account{Email: "admin@example.com", HashedSecret: "h", IsAdmin: true})
	subject, _ := db.CreateAccount(ctx, Account{Email: "subject@example.com", HashedSecret: "h"})
	now := time.Now().UTC().Truncate(time.Second)

	s := Session{ID: "imp", AccountID: subject, HashedToken: "itok", Actor: admin, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := db.CreateSession(ctx, s); err != nil {
		t.Fatalf("CreateSession (impersonation): %v", err)
	}
	got, err := db.GetSession(ctx, "imp")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.AccountID != subject || got.Actor != admin {
		t.Errorf("impersonation session = %+v, want account=%d actor=%d", got, subject, admin)
	}
}

func TestPurgeExpiredSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	acct, _ := db.CreateAccount(ctx, Account{Email: "u@example.com", HashedSecret: "h"})
	now := time.Now().UTC().Truncate(time.Second)

	// One expired, one live.
	_ = db.CreateSession(ctx, Session{ID: "old", AccountID: acct, HashedToken: "told", IssuedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)})
	_ = db.CreateSession(ctx, Session{ID: "new", AccountID: acct, HashedToken: "tnew", IssuedAt: now, ExpiresAt: now.Add(time.Hour)})

	n, err := db.PurgeExpiredSessions(ctx, now)
	if err != nil {
		t.Fatalf("PurgeExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("purged = %d, want 1", n)
	}
	// The expired row is physically gone; the live one remains.
	if _, err := db.GetSession(ctx, "old"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("purged session still present: %v", err)
	}
	if _, err := db.GetSession(ctx, "new"); err != nil {
		t.Errorf("live session removed: %v", err)
	}
}

func TestGameCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	// Empty status defaults to draft.
	id, err := db.CreateGame(ctx, Game{Name: "Alpha Campaign", Description: "first", IsActive: true})
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	g, err := db.GetGame(ctx, id)
	if err != nil {
		t.Fatalf("GetGame: %v", err)
	}
	// Names are stored upper-cased (issue #72).
	if g.Status != "draft" || g.Name != "ALPHA CAMPAIGN" || g.Description != "first" || !g.IsActive {
		t.Errorf("game = %+v", g)
	}

	// Advance status and toggle visibility.
	g.Status, g.IsActive, g.Name = "recruiting", false, "Alpha"
	if err := db.UpdateGame(ctx, g); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}
	after, _ := db.GetGame(ctx, id)
	if after.Status != "recruiting" || after.IsActive || after.Name != "ALPHA" {
		t.Errorf("after update: %+v", after)
	}

	// An invalid status is rejected by the CHECK constraint.
	if _, err := db.CreateGame(ctx, Game{Name: "Bad", Status: "bogus"}); !errors.Is(err, ErrConflict) {
		t.Errorf("invalid status error = %v, want ErrConflict", err)
	}

	// Second game, then filtered and unfiltered listings.
	if _, err := db.CreateGame(ctx, Game{Name: "Beta", Status: "active"}); err != nil {
		t.Fatalf("CreateGame Beta: %v", err)
	}
	all, _ := db.ListGames(ctx, "")
	if len(all) != 2 {
		t.Errorf("ListGames(all) = %d, want 2", len(all))
	}
	activeOnly, _ := db.ListGames(ctx, "active")
	if len(activeOnly) != 1 || activeOnly[0].Name != "BETA" {
		t.Errorf("ListGames(active) = %+v", activeOnly)
	}
}

// TestGameNameUnique covers issue #72: a game name is unique across all games
// (active or not) and normalized to upper-case, so a duplicate create or rename
// — in any case — is ErrConflict.
func TestGameNameUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := db.CreateGame(ctx, Game{Name: "ec01"}); err != nil {
		t.Fatalf("CreateGame ec01: %v", err)
	}
	// Different case, and inactive — still collides after normalization.
	if _, err := db.CreateGame(ctx, Game{Name: "EC01", IsActive: false}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate create error = %v, want ErrConflict", err)
	}

	// Renaming a distinct game onto an existing name also collides.
	other, err := db.CreateGame(ctx, Game{Name: "ec02"})
	if err != nil {
		t.Fatalf("CreateGame ec02: %v", err)
	}
	g, _ := db.GetGame(ctx, other)
	g.Name = "Ec01"
	if err := db.UpdateGame(ctx, g); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate rename error = %v, want ErrConflict", err)
	}
}

func TestGameNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	if _, err := db.GetGame(ctx, 7); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("GetGame error = %v, want ErrRecordNotFound", err)
	}
	if err := db.UpdateGame(ctx, Game{ID: 7, Name: "x", Status: "draft"}); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("UpdateGame error = %v, want ErrRecordNotFound", err)
	}
}

func TestMemberSeating(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	game, _ := db.CreateGame(ctx, Game{Name: "G", Status: "recruiting"})
	a1, _ := db.CreateAccount(ctx, Account{Email: "a1@example.com", HashedSecret: "h"})
	a2, _ := db.CreateAccount(ctx, Account{Email: "a2@example.com", HashedSecret: "h"})

	// First two seats get sequential player_ids 1, 2.
	m1, err := db.AddMember(ctx, game, a1, true)
	if err != nil {
		t.Fatalf("AddMember a1: %v", err)
	}
	if m1.PlayerID != 1 || !m1.IsGM || !m1.IsActive {
		t.Errorf("first member = %+v", m1)
	}
	m2, err := db.AddMember(ctx, game, a2, false)
	if err != nil {
		t.Fatalf("AddMember a2: %v", err)
	}
	if m2.PlayerID != 2 {
		t.Errorf("second player_id = %d, want 2", m2.PlayerID)
	}

	// Seating the same account twice conflicts (UNIQUE game_id, account_id).
	if _, err := db.AddMember(ctx, game, a1, false); !errors.Is(err, ErrConflict) {
		t.Errorf("re-seat error = %v, want ErrConflict", err)
	}

	// An unknown game/account is rejected by the foreign keys (as ErrConflict).
	if _, err := db.AddMember(ctx, 999, a1, false); !errors.Is(err, ErrConflict) {
		t.Errorf("AddMember(bad game) = %v, want ErrConflict", err)
	}

	// Lookups by seat and by account.
	if got, err := db.GetMember(ctx, game, 2); err != nil || got.AccountID != a2 {
		t.Errorf("GetMember = %+v err=%v", got, err)
	}
	if got, err := db.GetMemberByAccount(ctx, game, a1); err != nil || got.PlayerID != 1 {
		t.Errorf("GetMemberByAccount = %+v err=%v", got, err)
	}
}

func TestMemberUpdateAndNeverReuse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	game, _ := db.CreateGame(ctx, Game{Name: "G"})
	a1, _ := db.CreateAccount(ctx, Account{Email: "a1@example.com", HashedSecret: "h"})
	a2, _ := db.CreateAccount(ctx, Account{Email: "a2@example.com", HashedSecret: "h"})

	m1, _ := db.AddMember(ctx, game, a1, false)

	// Drop the seat (soft delete) and promote to GM in one update.
	m1.IsActive, m1.IsGM = false, true
	if err := db.UpdateMember(ctx, m1); err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}
	got, _ := db.GetMember(ctx, game, m1.PlayerID)
	if got.IsActive || !got.IsGM {
		t.Errorf("after update: %+v", got)
	}

	// A new seat must NOT reuse player_id 1, even though seat 1 is now inactive.
	m2, err := db.AddMember(ctx, game, a2, false)
	if err != nil {
		t.Fatalf("AddMember a2: %v", err)
	}
	if m2.PlayerID != 2 {
		t.Errorf("new player_id = %d, want 2 (ids never reused)", m2.PlayerID)
	}

	// Updating an unknown seat is ErrRecordNotFound.
	if err := db.UpdateMember(ctx, Member{GameID: game, PlayerID: 99}); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("UpdateMember(unknown) = %v, want ErrRecordNotFound", err)
	}
}

func TestListMembersAndMyGames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)

	g1, _ := db.CreateGame(ctx, Game{Name: "One", IsActive: true})
	g2, _ := db.CreateGame(ctx, Game{Name: "Two", IsActive: true})
	acct, _ := db.CreateAccount(ctx, Account{Email: "a@example.com", HashedSecret: "h"})
	other, _ := db.CreateAccount(ctx, Account{Email: "b@example.com", HashedSecret: "h"})

	// acct is a GM in g1 and a (later dropped) player in g2; other fills a seat in g1.
	if _, err := db.AddMember(ctx, g1, acct, true); err != nil {
		t.Fatalf("seat acct g1: %v", err)
	}
	if _, err := db.AddMember(ctx, g1, other, false); err != nil {
		t.Fatalf("seat other g1: %v", err)
	}
	m, _ := db.AddMember(ctx, g2, acct, false)

	// ListMembers shows every seat in the game, including inactive ones.
	m.IsActive = false
	if err := db.UpdateMember(ctx, m); err != nil {
		t.Fatalf("drop acct in g2: %v", err)
	}
	g1Members, _ := db.ListMembers(ctx, g1)
	if len(g1Members) != 2 {
		t.Errorf("g1 members = %d, want 2", len(g1Members))
	}

	// ListMyGames shows only active seats, projected with the caller's seat.
	mine, err := db.ListMyGames(ctx, acct)
	if err != nil {
		t.Fatalf("ListMyGames: %v", err)
	}
	if len(mine) != 1 {
		t.Fatalf("my games = %d, want 1 (dropped g2 seat excluded)", len(mine))
	}
	if mine[0].GameID != g1 || mine[0].Name != "ONE" || !mine[0].IsGM {
		t.Errorf("my game = %+v", mine[0])
	}
}
