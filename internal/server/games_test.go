// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// seedGame inserts a game directly through the store and returns its id.
func seedGame(t *testing.T, s *Server, name, status string, active bool) int64 {
	t.Helper()
	id, err := s.db.CreateGame(context.Background(), store.Game{
		Name:     name,
		Status:   status,
		IsActive: active,
	})
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	return id
}

// seedMember seats an account in a game (as a GM when isGM) and returns the seat.
func seedMember(t *testing.T, s *Server, gameID, accountID int64, isGM bool) store.Member {
	t.Helper()
	m, err := s.db.AddMember(context.Background(), gameID, accountID, isGM)
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	return m
}

func TestCreateGameAdminOnly(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	body := api.CreateGameRequest{Name: "Alpha Campaign", Description: ptr("first game")}

	// Non-admin is forbidden.
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")
	if rec := do(t, s, http.MethodPost, "/api/games", userTok, body); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin create: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodPost, "/api/games", "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon create: status = %d, want 401", rec.Code)
	}

	// Admin succeeds; the new game starts in draft.
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	rec := do(t, s, http.MethodPost, "/api/games", adminTok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got api.GameResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Game.Id == 0 {
		t.Errorf("created game missing id")
	}
	if got.Game.Name != "ALPHA CAMPAIGN" {
		t.Errorf("name = %q, want ALPHA CAMPAIGN (stored upper-cased)", got.Game.Name)
	}
	if got.Game.Status != api.Draft {
		t.Errorf("status = %q, want draft", got.Game.Status)
	}
	if got.Game.Description == nil || *got.Game.Description != "first game" {
		t.Errorf("description = %v, want first game", got.Game.Description)
	}
}

func TestCreateGameValidation(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Blank name is 400.
	if rec := do(t, s, http.MethodPost, "/api/games", adminTok, api.CreateGameRequest{Name: "   "}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: status = %d, want 400", rec.Code)
	}
}

// TestCreateGameDuplicateName covers issue #72: names are unique across all
// games and matched case-insensitively (stored upper-cased), so a second create
// with the same name — in any case — is 409.
func TestCreateGameDuplicateName(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	if rec := do(t, s, http.MethodPost, "/api/games", adminTok, api.CreateGameRequest{Name: "ec01"}); rec.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// Same name, different case — collides after upper-casing.
	if rec := do(t, s, http.MethodPost, "/api/games", adminTok, api.CreateGameRequest{Name: "EC01"}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListGamesVisibility(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	seedGame(t, s, "Visible", string(api.Recruiting), true)
	seedGame(t, s, "Hidden", string(api.Draft), false)

	// Non-admin sees only the active game.
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")
	rec := do(t, s, http.MethodGet, "/api/games", userTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("user list: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var userGot api.ListGamesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &userGot)
	if len(userGot.Games) != 1 || userGot.Games[0].Name != "VISIBLE" {
		t.Errorf("non-admin games = %+v, want only [VISIBLE]", userGot.Games)
	}

	// Admin sees both.
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	rec = do(t, s, http.MethodGet, "/api/games", adminTok, nil)
	var adminGot api.ListGamesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &adminGot)
	if len(adminGot.Games) != 2 {
		t.Errorf("admin games = %d, want 2", len(adminGot.Games))
	}

	// A status filter narrows the result.
	rec = do(t, s, http.MethodGet, "/api/games?status=draft", adminTok, nil)
	var filtered api.ListGamesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &filtered)
	if len(filtered.Games) != 1 || filtered.Games[0].Name != "HIDDEN" {
		t.Errorf("status=draft games = %+v, want only [HIDDEN]", filtered.Games)
	}

	// An invalid status filter is 400.
	if rec := do(t, s, http.MethodGet, "/api/games?status=bogus", adminTok, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad status filter: status = %d, want 400", rec.Code)
	}
}

func TestGetGameVisibility(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	visible := seedGame(t, s, "Visible", string(api.Active), true)
	hidden := seedGame(t, s, "Hidden", string(api.Draft), false)

	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Non-admin can read a visible game.
	if rec := do(t, s, http.MethodGet, "/api/games/"+itoa(visible), userTok, nil); rec.Code != http.StatusOK {
		t.Errorf("user get visible: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Non-admin is forbidden from a hidden game.
	if rec := do(t, s, http.MethodGet, "/api/games/"+itoa(hidden), userTok, nil); rec.Code != http.StatusForbidden {
		t.Errorf("user get hidden: status = %d, want 403", rec.Code)
	}
	// Admin can read the hidden game.
	if rec := do(t, s, http.MethodGet, "/api/games/"+itoa(hidden), adminTok, nil); rec.Code != http.StatusOK {
		t.Errorf("admin get hidden: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Unknown id is 404.
	if rec := do(t, s, http.MethodGet, "/api/games/999999", adminTok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", rec.Code)
	}
	// Non-numeric id is 400.
	if rec := do(t, s, http.MethodGet, "/api/games/abc", adminTok, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestUpdateGameByAdmin(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	id := seedGame(t, s, "Alpha", string(api.Draft), false)

	// Empty patch is 400.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(id), adminTok, api.UpdateGameRequest{}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty patch: status = %d, want 400", rec.Code)
	}

	// Advance status (forward, with skip), rename, and flip visibility at once.
	body := api.UpdateGameRequest{
		Status:   ptr(api.Active),
		Name:     ptr("Alpha Renamed"),
		IsActive: ptr(true),
	}
	rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(id), adminTok, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin patch: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.GameResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Game.Status != api.Active {
		t.Errorf("status = %q, want active", got.Game.Status)
	}
	if got.Game.Name != "ALPHA RENAMED" {
		t.Errorf("name = %q, want ALPHA RENAMED (stored upper-cased)", got.Game.Name)
	}
	if !got.Game.IsActive {
		t.Errorf("isActive = false, want true")
	}

	// Unknown id is 404.
	if rec := do(t, s, http.MethodPatch, "/api/games/999999", adminTok, api.UpdateGameRequest{Name: ptr("x")}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id patch: status = %d, want 404", rec.Code)
	}
}

func TestUpdateGameStatusTransitions(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Backward move (active -> draft) is rejected 409.
	g1 := seedGame(t, s, "G1", string(api.Active), true)
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(g1), adminTok, api.UpdateGameRequest{Status: ptr(api.Draft)}); rec.Code != http.StatusConflict {
		t.Errorf("backward move: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}

	// Un-pause (paused -> active) is allowed for an admin.
	g2 := seedGame(t, s, "G2", string(api.Paused), true)
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(g2), adminTok, api.UpdateGameRequest{Status: ptr(api.Active)}); rec.Code != http.StatusOK {
		t.Errorf("un-pause: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// An archived game is frozen for metadata and visibility.
	g3 := seedGame(t, s, "G3", string(api.Archived), true)
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(g3), adminTok, api.UpdateGameRequest{Name: ptr("nope")}); rec.Code != http.StatusConflict {
		t.Errorf("rename archived: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	// But an admin can move it out of archived.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(g3), adminTok, api.UpdateGameRequest{Status: ptr(api.Complete)}); rec.Code != http.StatusOK {
		t.Errorf("out of archived: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateGameByGM(t *testing.T) {
	s := newTestServer(t)
	gmID := seedAccount(t, s, "gm@example.com", "gm-pass-11", false, true)
	playerID := seedAccount(t, s, "player@example.com", "player-pass", false, true)
	seedAccount(t, s, "out@example.com", "out-pass-11", false, true)
	game := seedGame(t, s, "GM Game", string(api.Recruiting), true)
	seedMember(t, s, game, gmID, true)      // active GM
	seedMember(t, s, game, playerID, false) // ordinary player

	gmTok := tokenFor(t, s, "gm@example.com", "gm-pass-11")
	playerTok := tokenFor(t, s, "player@example.com", "player-pass")
	outTok := tokenFor(t, s, "out@example.com", "out-pass-11")

	// The active GM can advance status and edit metadata.
	rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game), gmTok, api.UpdateGameRequest{
		Status: ptr(api.Active),
		Name:   ptr("GM Game v2"),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("GM patch: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The GM cannot change isActive (admin-only).
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game), gmTok, api.UpdateGameRequest{IsActive: ptr(false)}); rec.Code != http.StatusForbidden {
		t.Errorf("GM isActive: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// An ordinary (non-GM) player cannot update the game.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game), playerTok, api.UpdateGameRequest{Name: ptr("nope")}); rec.Code != http.StatusForbidden {
		t.Errorf("player patch: status = %d, want 403", rec.Code)
	}

	// A non-member cannot update the game.
	if rec := do(t, s, http.MethodPatch, "/api/games/"+itoa(game), outTok, api.UpdateGameRequest{Name: ptr("nope")}); rec.Code != http.StatusForbidden {
		t.Errorf("outsider patch: status = %d, want 403", rec.Code)
	}
}

func TestListMyGames(t *testing.T) {
	s := newTestServer(t)
	accID := seedAccount(t, s, "me@example.com", "my-secret-1", false, true)
	tok := tokenFor(t, s, "me@example.com", "my-secret-1")

	// No memberships yet: an empty list, not an error.
	rec := do(t, s, http.MethodGet, "/api/me/games", tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty my games: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var empty api.ListMyGamesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if empty.Games == nil {
		t.Errorf("games = nil, want empty array")
	}
	if len(empty.Games) != 0 {
		t.Errorf("games = %d, want 0", len(empty.Games))
	}

	// Seat the account as a GM and confirm the game shows up projected.
	game := seedGame(t, s, "My Game", string(api.Active), true)
	seat := seedMember(t, s, game, accID, true)
	rec = do(t, s, http.MethodGet, "/api/me/games", tok, nil)
	var got api.ListMyGamesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Games) != 1 {
		t.Fatalf("my games = %d, want 1", len(got.Games))
	}
	mg := got.Games[0]
	if mg.Id != game || mg.Name != "MY GAME" || mg.PlayerId != seat.PlayerID || !mg.IsGm {
		t.Errorf("my game = %+v, want {id:%d name:MY GAME playerId:%d isGm:true}", mg, game, seat.PlayerID)
	}

	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodGet, "/api/me/games", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anon my games: status = %d, want 401", rec.Code)
	}
}
