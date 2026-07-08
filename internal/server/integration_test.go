// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdhender/ecv6-api/internal/api"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// TestEndToEndJourney walks the full application-domain journey over the real
// routed handler stack (srv.Handler()) against a real in-memory SQLite store —
// no mocks. It exercises the cross-cutting conventions (doc/api/conventions.md)
// end to end: every response is asserted for status code, the standard error
// envelope (code + requestId) on failures, and the X-Request-Id response header.
//
// The journey: admin bootstrap (seeded, the only step without an HTTP route) →
// admin login → create a user account → user login → admin creates a game →
// admin seats the user → user is promoted to GM → the GM adds a second player →
// list the roster → the user reads "my games". Interleaved negative cases assert
// the envelope and status codes for unauthenticated, forbidden, and not-found.
func TestEndToEndJourney(t *testing.T) {
	s := newTestServer(t)

	// --- Admin bootstrap ------------------------------------------------------
	// The first admin cannot be created over HTTP (creating an account is itself
	// admin-gated), so bootstrapping seeds one directly in the store. Everything
	// after this point goes through the HTTP surface.
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	// Unauthenticated access to a protected route is a 401 in the standard
	// envelope, with a correlation id.
	rec := do(t, s, http.MethodGet, "/api/me", "", nil)
	assertErrorEnvelope(t, rec, http.StatusUnauthorized, codeUnauthorized)

	// --- Admin login ----------------------------------------------------------
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// GET /me confirms the token resolves to the admin account.
	rec = do(t, s, http.MethodGet, "/api/me", adminTok, nil)
	assertOK(t, rec, http.StatusOK)
	assertRequestIDHeader(t, rec)
	me := decodeAccount(t, rec)
	if string(me.Email) != "admin@example.com" || !hasRole(me, "admin") {
		t.Fatalf("me = %+v, want admin@example.com with admin role", me)
	}

	// --- Admin creates a user account ----------------------------------------
	// Secret omitted, so the server generates one and returns it once.
	rec = do(t, s, http.MethodPost, "/api/accounts", adminTok, api.CreateAccountRequest{
		Email:    openapi_types.Email("player@example.com"),
		IsActive: ptr(true),
	})
	assertOK(t, rec, http.StatusCreated)
	var created api.CreateAccountResponse
	decodeInto(t, rec, &created)
	if created.GeneratedSecret == nil || *created.GeneratedSecret == "" {
		t.Fatalf("expected a generated secret in the create response")
	}
	userID := created.Account.Id
	userSecret := *created.GeneratedSecret

	// A non-admin cannot create accounts: log the user in and confirm 403.
	userTok := tokenFor(t, s, "player@example.com", userSecret)
	rec = do(t, s, http.MethodPost, "/api/accounts", userTok, api.CreateAccountRequest{
		Email: openapi_types.Email("intruder@example.com"),
	})
	assertErrorEnvelope(t, rec, http.StatusForbidden, codeForbidden)

	// --- Admin creates a game -------------------------------------------------
	rec = do(t, s, http.MethodPost, "/api/games", adminTok, api.CreateGameRequest{
		Name:        "Journey",
		Description: ptr("integration game"),
	})
	assertOK(t, rec, http.StatusCreated)
	game := decodeGame(t, rec)
	if game.Status != api.Draft {
		t.Fatalf("new game status = %q, want draft", game.Status)
	}
	gameID := game.Id

	// A non-admin cannot create a game.
	rec = do(t, s, http.MethodPost, "/api/games", userTok, api.CreateGameRequest{Name: "nope"})
	assertErrorEnvelope(t, rec, http.StatusForbidden, codeForbidden)

	// Move the game to recruiting so members can be seated (admin PATCH).
	rec = do(t, s, http.MethodPatch, "/api/games/"+itoa(gameID), adminTok, api.UpdateGameRequest{
		Status: ptrStatus(api.Recruiting),
	})
	assertOK(t, rec, http.StatusOK)
	if g := decodeGame(t, rec); g.Status != api.Recruiting {
		t.Fatalf("game status after patch = %q, want recruiting", g.Status)
	}

	// --- Admin seats the user, then promotes them to GM ----------------------
	rec = do(t, s, http.MethodPost, "/api/games/"+itoa(gameID)+"/members", adminTok, api.AddMemberRequest{
		AccountId: userID,
	})
	assertOK(t, rec, http.StatusCreated)
	member := decodeMember(t, rec.Body)
	if member.AccountId != userID || member.IsGm || !member.IsActive || member.PlayerId < 1 {
		t.Fatalf("seated member = %+v, want the user as an active non-GM with a minted playerId", member)
	}
	playerID := member.PlayerId

	// Seating the same account again conflicts (409) in the standard envelope.
	rec = do(t, s, http.MethodPost, "/api/games/"+itoa(gameID)+"/members", adminTok, api.AddMemberRequest{
		AccountId: userID,
	})
	assertErrorEnvelope(t, rec, http.StatusConflict, codeConflict)

	// Set GM: promote the seated player. Admin promotes via PATCH member.
	rec = do(t, s, http.MethodPatch, "/api/games/"+itoa(gameID)+"/members/"+itoa(playerID), adminTok, api.UpdateMemberRequest{
		IsGm: ptr(true),
	})
	assertOK(t, rec, http.StatusOK)
	if m := decodeMember(t, rec.Body); !m.IsGm {
		t.Fatalf("member after promotion = %+v, want isGm=true", m)
	}

	// --- The freshly-minted GM adds a second player --------------------------
	seedAccount(t, s, "player2@example.com", "player2-pass", false, true)
	// The user token now carries an active GM seat, so the user may add members.
	var player2ID int64
	{
		rec = do(t, s, http.MethodGet, "/api/accounts", adminTok, nil)
		assertOK(t, rec, http.StatusOK)
		var list api.ListAccountsResponse
		decodeInto(t, rec, &list)
		for _, a := range list.Accounts {
			if string(a.Email) == "player2@example.com" {
				player2ID = a.Id
			}
		}
	}
	if player2ID == 0 {
		t.Fatal("could not find player2 account id via GET /accounts")
	}
	rec = do(t, s, http.MethodPost, "/api/games/"+itoa(gameID)+"/members", userTok, api.AddMemberRequest{
		AccountId: player2ID,
	})
	assertOK(t, rec, http.StatusCreated)

	// --- List members ---------------------------------------------------------
	rec = do(t, s, http.MethodGet, "/api/games/"+itoa(gameID)+"/members", adminTok, nil)
	assertOK(t, rec, http.StatusOK)
	var roster api.ListMembersResponse
	decodeInto(t, rec, &roster)
	if len(roster.Members) != 2 {
		t.Fatalf("roster size = %d, want 2 (%+v)", len(roster.Members), roster.Members)
	}

	// A not-found game yields a 404 envelope.
	rec = do(t, s, http.MethodGet, "/api/games/999999/members", adminTok, nil)
	assertErrorEnvelope(t, rec, http.StatusNotFound, codeNotFound)

	// --- My games -------------------------------------------------------------
	rec = do(t, s, http.MethodGet, "/api/me/games", userTok, nil)
	assertOK(t, rec, http.StatusOK)
	var mine api.ListMyGamesResponse
	decodeInto(t, rec, &mine)
	if len(mine.Games) != 1 {
		t.Fatalf("my games size = %d, want 1 (%+v)", len(mine.Games), mine.Games)
	}
	if g := mine.Games[0]; g.Id != gameID || g.PlayerId != playerID || !g.IsGm {
		t.Fatalf("my game = %+v, want gameId=%d playerId=%d isGm=true", g, gameID, playerID)
	}

	// --- Session lifecycle: logout invalidates the token immediately ---------
	if code := doLogout(t, s, userTok, false); code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", code)
	}
	rec = do(t, s, http.MethodGet, "/api/me", userTok, nil)
	assertErrorEnvelope(t, rec, http.StatusUnauthorized, codeUnauthorized)
}

// --- assertion helpers -------------------------------------------------------

// assertOK fails unless the recorder has the wanted 2xx status, the JSON content
// type (for bodied responses), and the correlation-id response header — the
// invariants every successful response must satisfy.
func assertOK(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, want, rec.Body.String())
	}
	if want != http.StatusNoContent {
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
		}
	}
	assertRequestIDHeader(t, rec)
}

// assertRequestIDHeader asserts the response carries a non-empty correlation id.
func assertRequestIDHeader(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if id := rec.Header().Get(requestIDHeader); id == "" {
		t.Errorf("missing %s response header", requestIDHeader)
	}
}

// assertErrorEnvelope asserts the recorder holds the standard error envelope:
// the wanted status, the given stable code, a non-empty message, and a requestId
// that matches the X-Request-Id response header (doc/api/conventions.md).
func assertErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	var env api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, rec.Body.String())
	}
	if env.Error.Code != wantCode {
		t.Errorf("error code = %q, want %q", env.Error.Code, wantCode)
	}
	if env.Error.Message == "" {
		t.Errorf("error message is empty")
	}
	if env.Error.RequestId == nil || *env.Error.RequestId == "" {
		t.Fatalf("error envelope missing requestId")
	}
	if hdr := rec.Header().Get(requestIDHeader); hdr != *env.Error.RequestId {
		t.Errorf("envelope requestId %q != header %q", *env.Error.RequestId, hdr)
	}
}

// --- decode helpers ----------------------------------------------------------

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %T: %v; body=%s", v, err, rec.Body.String())
	}
}

func decodeAccount(t *testing.T, rec *httptest.ResponseRecorder) api.Account {
	t.Helper()
	var got api.AccountResponse
	decodeInto(t, rec, &got)
	return got.Account
}

func decodeGame(t *testing.T, rec *httptest.ResponseRecorder) api.Game {
	t.Helper()
	var got api.GameResponse
	decodeInto(t, rec, &got)
	return got.Game
}

func hasRole(a api.Account, role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func ptrStatus(s api.GameStatus) *api.GameStatus { return &s }
