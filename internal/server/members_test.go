// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mdhender/ecv6-api/internal/api"
)

// decodeMember unmarshals a MemberResponse body or fails the test.
func decodeMember(t *testing.T, rec interface{ Bytes() []byte }) api.Member {
	t.Helper()
	var got api.MemberResponse
	if err := json.Unmarshal(rec.Bytes(), &got); err != nil {
		t.Fatalf("decode MemberResponse: %v", err)
	}
	return got.Member
}

func TestAddGameMember(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	userID := seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	game := seedGame(t, s, "G", string(api.Recruiting), true)

	// Admin seats a user as an ordinary player; playerId is minted server-side.
	rec := do(t, s, http.MethodPost, "/api/games/"+itoa(game)+"/members", adminTok, api.AddMemberRequest{AccountId: userID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	m := decodeMember(t, rec.Body)
	if m.AccountId != userID || m.IsGm || !m.IsActive || m.PlayerId < 1 {
		t.Errorf("member = %+v, want accountId=%d isGm=false isActive=true playerId>=1", m, userID)
	}

	// Seating the same account again is a 409.
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(game)+"/members", adminTok, api.AddMemberRequest{AccountId: userID}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate add: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAddGameMemberAsGM(t *testing.T) {
	s := newTestServer(t)
	gmID := seedAccount(t, s, "gm@example.com", "gm-pass-111", false, true)
	p1 := seedAccount(t, s, "p1@example.com", "p1-pass-111", false, true)
	p2 := seedAccount(t, s, "p2@example.com", "p2-pass-111", false, true)
	game := seedGame(t, s, "G", string(api.Recruiting), true)
	seedMember(t, s, game, gmID, true) // active GM

	gmTok := tokenFor(t, s, "gm@example.com", "gm-pass-111")

	// The active GM can add a player.
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(game)+"/members", gmTok, api.AddMemberRequest{AccountId: p1}); rec.Code != http.StatusCreated {
		t.Fatalf("GM add player: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// An ordinary (non-GM) player cannot add members.
	p1Tok := tokenFor(t, s, "p1@example.com", "p1-pass-111")
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(game)+"/members", p1Tok, api.AddMemberRequest{AccountId: p2}); rec.Code != http.StatusForbidden {
		t.Errorf("player add: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// A non-member cannot add members.
	p2Tok := tokenFor(t, s, "p2@example.com", "p2-pass-111")
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(game)+"/members", p2Tok, api.AddMemberRequest{AccountId: p2}); rec.Code != http.StatusForbidden {
		t.Errorf("outsider add: status = %d, want 403", rec.Code)
	}
}

func TestAddGameMemberValidation(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	game := seedGame(t, s, "G", string(api.Recruiting), true)

	// Missing accountId is 400.
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(game)+"/members", adminTok, api.AddMemberRequest{}); rec.Code != http.StatusBadRequest {
		t.Errorf("missing accountId: status = %d, want 400", rec.Code)
	}

	// Unknown account is 400.
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(game)+"/members", adminTok, api.AddMemberRequest{AccountId: 999999}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown account: status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// Unknown game is 404.
	uID := seedAccount(t, s, "u@example.com", "u-pass-1111", false, true)
	if rec := do(t, s, http.MethodPost, "/api/games/999999/members", adminTok, api.AddMemberRequest{AccountId: uID}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown game: status = %d, want 404", rec.Code)
	}

	// Non-numeric game id is 400.
	if rec := do(t, s, http.MethodPost, "/api/games/abc/members", adminTok, api.AddMemberRequest{AccountId: uID}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad game id: status = %d, want 400", rec.Code)
	}
}

func TestAddGameMemberLifecycleWindow(t *testing.T) {
	s := newTestServer(t)
	gmID := seedAccount(t, s, "gm@example.com", "gm-pass-111", false, true)
	pID := seedAccount(t, s, "p@example.com", "p-pass-1111", false, true)
	gm2ID := seedAccount(t, s, "gm2@example.com", "gm2-pass-11", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// A GM adding a player outside recruiting is a 409.
	active := seedGame(t, s, "Active", string(api.Active), true)
	seedMember(t, s, active, gmID, true)
	gmTok := tokenFor(t, s, "gm@example.com", "gm-pass-111")
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(active)+"/members", gmTok, api.AddMemberRequest{AccountId: pID}); rec.Code != http.StatusConflict {
		t.Errorf("GM add player when active: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	// An admin bypasses the recruiting window for players.
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(active)+"/members", adminTok, api.AddMemberRequest{AccountId: pID}); rec.Code != http.StatusCreated {
		t.Errorf("admin add player when active: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// A GM may be added in any non-archived status.
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(active)+"/members", adminTok, api.AddMemberRequest{AccountId: gm2ID, IsGm: ptr(true)}); rec.Code != http.StatusCreated {
		t.Errorf("admin add GM when active: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// An archived game is frozen: even an admin cannot add.
	archived := seedGame(t, s, "Archived", string(api.Archived), true)
	if rec := do(t, s, http.MethodPost, "/api/games/"+itoa(archived)+"/members", adminTok, api.AddMemberRequest{AccountId: pID}); rec.Code != http.StatusConflict {
		t.Errorf("add to archived: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListGameMembersVisibility(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	memberID := seedAccount(t, s, "member@example.com", "member-pass", false, true)
	seedAccount(t, s, "out@example.com", "out-pass-111", false, true)
	game := seedGame(t, s, "G", string(api.Recruiting), true)
	seedMember(t, s, game, memberID, false)

	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	memberTok := tokenFor(t, s, "member@example.com", "member-pass")
	outTok := tokenFor(t, s, "out@example.com", "out-pass-111")

	// Admin sees the roster.
	rec := do(t, s, http.MethodGet, "/api/games/"+itoa(game)+"/members", adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.ListMembersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Members) != 1 || got.Members[0].AccountId != memberID {
		t.Errorf("roster = %+v, want one seat for account %d", got.Members, memberID)
	}

	// A seated member sees the roster.
	if rec := do(t, s, http.MethodGet, "/api/games/"+itoa(game)+"/members", memberTok, nil); rec.Code != http.StatusOK {
		t.Errorf("member list: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// A non-member gets 404 (indistinguishable from an unknown game).
	if rec := do(t, s, http.MethodGet, "/api/games/"+itoa(game)+"/members", outTok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("outsider list: status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	// An unknown game is 404.
	if rec := do(t, s, http.MethodGet, "/api/games/999999/members", adminTok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown game list: status = %d, want 404", rec.Code)
	}

	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodGet, "/api/games/"+itoa(game)+"/members", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anon list: status = %d, want 401", rec.Code)
	}
}

func TestListGameMembersIncludesDropped(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	aID := seedAccount(t, s, "a@example.com", "a-pass-1111", false, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	game := seedGame(t, s, "G", string(api.Recruiting), true)
	seat := seedMember(t, s, game, aID, false)

	// Drop the member, then confirm they remain in the roster as inactive.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(seat.PlayerID), adminTok, api.UpdateMemberRequest{IsActive: ptr(false)}); rec.Code != http.StatusOK {
		t.Fatalf("drop: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	rec := do(t, s, http.MethodGet, "/api/games/"+itoa(game)+"/members", adminTok, nil)
	var got api.ListMembersResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Members) != 1 || got.Members[0].IsActive {
		t.Errorf("roster = %+v, want one dropped (inactive) seat", got.Members)
	}
}

func TestUpdateGameMemberPromote(t *testing.T) {
	s := newTestServer(t)
	gmID := seedAccount(t, s, "gm@example.com", "gm-pass-111", false, true)
	pID := seedAccount(t, s, "p@example.com", "p-pass-1111", false, true)
	game := seedGame(t, s, "G", string(api.Recruiting), true)
	seedMember(t, s, game, gmID, true)
	seat := seedMember(t, s, game, pID, false)

	gmTok := tokenFor(t, s, "gm@example.com", "gm-pass-111")

	// The GM promotes the player to GM.
	rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(seat.PlayerID), gmTok, api.UpdateMemberRequest{IsGm: ptr(true)})
	if rec.Code != http.StatusOK {
		t.Fatalf("promote: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if m := decodeMember(t, rec.Body); !m.IsGm {
		t.Errorf("promoted member isGm = false, want true")
	}

	// The newly promoted account is now a GM (can list, and /me/games shows GM).
	pTok := tokenFor(t, s, "p@example.com", "p-pass-1111")
	rec = do(t, s, http.MethodGet, "/api/me/games", pTok, nil)
	var mine api.ListMyGamesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &mine)
	if len(mine.Games) != 1 || !mine.Games[0].IsGm {
		t.Errorf("my games = %+v, want one game with isGm true", mine.Games)
	}
}

func TestUpdateGameMemberPromoteRules(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	pID := seedAccount(t, s, "p@example.com", "p-pass-1111", false, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Demotion (isGm:false) is rejected 400.
	game := seedGame(t, s, "G", string(api.Recruiting), true)
	seat := seedMember(t, s, game, pID, true)
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(seat.PlayerID), adminTok, api.UpdateMemberRequest{IsGm: ptr(false)}); rec.Code != http.StatusBadRequest {
		t.Errorf("demote: status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// Empty patch is 400.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(seat.PlayerID), adminTok, api.UpdateMemberRequest{}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty patch: status = %d, want 400", rec.Code)
	}

	// A GM promoting outside recruiting is a 409; an admin bypasses that window.
	active := seedGame(t, s, "Active", string(api.Active), true)
	gmID := seedAccount(t, s, "gm@example.com", "gm-pass-111", false, true)
	p2ID := seedAccount(t, s, "p2@example.com", "p2-pass-111", false, true)
	seedMember(t, s, active, gmID, true)
	p2Seat := seedMember(t, s, active, p2ID, false)
	gmTok := tokenFor(t, s, "gm@example.com", "gm-pass-111")
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(active)+"/members/"+itoa(p2Seat.PlayerID), gmTok, api.UpdateMemberRequest{IsGm: ptr(true)}); rec.Code != http.StatusConflict {
		t.Errorf("GM promote when active: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(active)+"/members/"+itoa(p2Seat.PlayerID), adminTok, api.UpdateMemberRequest{IsGm: ptr(true)}); rec.Code != http.StatusOK {
		t.Errorf("admin promote when active: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateGameMemberDrop(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	selfID := seedAccount(t, s, "self@example.com", "self-pass-1", false, true)
	otherID := seedAccount(t, s, "other@example.com", "other-pass1", false, true)
	game := seedGame(t, s, "G", string(api.Recruiting), true)
	selfSeat := seedMember(t, s, game, selfID, false)
	otherSeat := seedMember(t, s, game, otherID, false)

	selfTok := tokenFor(t, s, "self@example.com", "self-pass-1")

	// A member may drop their own seat.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(selfSeat.PlayerID), selfTok, api.UpdateMemberRequest{IsActive: ptr(false)}); rec.Code != http.StatusOK {
		t.Fatalf("self drop: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// An ordinary member cannot drop another's seat. (self is now dropped, so not
	// even a member — still forbidden.)
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(otherSeat.PlayerID), selfTok, api.UpdateMemberRequest{IsActive: ptr(false)}); rec.Code != http.StatusForbidden {
		t.Errorf("drop another: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// A member cannot self-reactivate (needs an admin or active GM).
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(selfSeat.PlayerID), selfTok, api.UpdateMemberRequest{IsActive: ptr(true)}); rec.Code != http.StatusForbidden {
		t.Errorf("self reactivate: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// An admin can reactivate.
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(selfSeat.PlayerID), adminTok, api.UpdateMemberRequest{IsActive: ptr(true)}); rec.Code != http.StatusOK {
		t.Errorf("admin reactivate: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateGameMemberNotFound(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	game := seedGame(t, s, "G", string(api.Recruiting), true)

	// Unknown seat is 404.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/999999", adminTok, api.UpdateMemberRequest{IsActive: ptr(false)}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown seat: status = %d, want 404", rec.Code)
	}

	// Unknown game is 404.
	if rec := do(t, s, http.MethodPatch, "/api/games/999999/members/1", adminTok, api.UpdateMemberRequest{IsActive: ptr(false)}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown game: status = %d, want 404", rec.Code)
	}

	// Non-numeric player id is 400.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/abc", adminTok, api.UpdateMemberRequest{IsActive: ptr(false)}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad player id: status = %d, want 400", rec.Code)
	}
}

func TestUpdateGameMemberArchivedFrozen(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	aID := seedAccount(t, s, "a@example.com", "a-pass-1111", false, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	game := seedGame(t, s, "G", string(api.Archived), true)
	seat := seedMember(t, s, game, aID, false)

	// No update is accepted once archived, not even for an admin.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game)+"/members/"+itoa(seat.PlayerID), adminTok, api.UpdateMemberRequest{IsActive: ptr(false)}); rec.Code != http.StatusConflict {
		t.Errorf("update archived: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}
