// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// toMemberDTO projects a store.Member (a game_account_role row) onto the wire
// Member schema. Only the application-visible fields are exposed — playerId (the
// seat's game-side key), accountId (the account holding the seat), and the GM and
// active flags. No engine-owned identity crosses the boundary (ADR-0003).
func toMemberDTO(m store.Member) api.Member {
	return api.Member{
		PlayerId:  m.PlayerID,
		AccountId: m.AccountID,
		IsGm:      m.IsGM,
		IsActive:  m.IsActive,
	}
}

// parsePlayerID reads the {playerId} path value as an int64, writing the standard
// 400 envelope and returning ok=false when it is missing or malformed.
func (s *Server) parsePlayerID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("playerId"), 10, 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "invalid player id")
		return 0, false
	}
	return id, true
}

// callerIsActiveGM reports whether account holds an active, GM-flagged seat in the
// game. An account with no seat (or a dropped/non-GM seat) is not an active GM. A
// store error other than "no such seat" is surfaced so the caller can 500.
func (s *Server) callerIsActiveGM(r *http.Request, gameID, accountID int64) (bool, error) {
	m, err := s.db.GetMemberByAccount(r.Context(), gameID, accountID)
	switch {
	case err == nil:
		return m.IsActive && m.IsGM, nil
	case errors.Is(err, store.ErrRecordNotFound):
		return false, nil
	default:
		return false, err
	}
}

// handleListGameMembers serves GET /api/games/{gameId}/members (openapi.yaml:
// listGameMembers). It returns the game's full roster — every seat, GMs and
// players, active and dropped alike. Visibility: an admin, or any account ever
// seated in the game (active or inactive). An unknown game, or one the caller may
// not see, is a 404 — the two are indistinguishable to a non-member.
func (s *Server) handleListGameMembers(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	gameID, ok := s.parseGameID(w, r)
	if !ok {
		return
	}

	if _, err := s.db.GetGame(r.Context(), gameID); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "game not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "members: get game", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list members")
		return
	}

	// Visibility: an admin sees any roster; a non-admin must have a seat in the
	// game — active or dropped. A caller who may not see it gets the same 404 as an
	// unknown game, so the roster's existence is not disclosed.
	if !account.IsAdmin {
		if _, err := s.db.GetMemberByAccount(r.Context(), gameID, account.ID); err != nil {
			if errors.Is(err, store.ErrRecordNotFound) {
				writeError(w, r, http.StatusNotFound, codeNotFound, "game not found")
				return
			}
			logger(r).ErrorContext(r.Context(), "members: resolve visibility", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list members")
			return
		}
	}

	members, err := s.db.ListMembers(r.Context(), gameID)
	if err != nil {
		logger(r).ErrorContext(r.Context(), "members: list", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list members")
		return
	}
	out := make([]api.Member, 0, len(members))
	for _, m := range members {
		out = append(out, toMemberDTO(m))
	}
	writeJSON(w, r, http.StatusOK, api.ListMembersResponse{Members: out})
}

// handleAddGameMember serves POST /api/games/{gameId}/members (openapi.yaml:
// addGameMember). It seats an account as a net-new player or a GM, minting an
// immutable playerId in the store (never client-supplied). Authorization and the
// lifecycle window (openapi.yaml):
//   - Requires an admin or the game's active GM (403 otherwise).
//   - An archived game is frozen: no seat may be added (409).
//   - Adding a player is allowed only while recruiting; an admin bypasses that
//     window. Adding a GM is allowed in any non-archived status.
//
// An unknown game is 404; an unknown referenced account is 400; an account already
// seated (active or dropped) is 409 — reactivate via the update endpoint instead.
func (s *Server) handleAddGameMember(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	gameID, ok := s.parseGameID(w, r)
	if !ok {
		return
	}
	var req api.AddMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AccountId < 1 {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "accountId is required")
		return
	}
	isGM := derefOr(req.IsGm, false)

	g, err := s.db.GetGame(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "game not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "members: get game for add", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not add member")
		return
	}

	// Authorization: an admin or the game's active GM.
	isAdmin := account.IsAdmin
	if !isAdmin {
		gm, err := s.callerIsActiveGM(r, gameID, account.ID)
		if err != nil {
			logger(r).ErrorContext(r.Context(), "members: resolve gm for add", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not add member")
			return
		}
		if !gm {
			writeError(w, r, http.StatusForbidden, codeForbidden, "you do not have permission to add members to this game")
			return
		}
	}

	// Lifecycle window. Archived is frozen for everyone; adding a player is
	// recruiting-only unless the caller is an admin.
	switch api.GameStatus(g.Status) {
	case api.Archived:
		writeError(w, r, http.StatusConflict, codeConflict, "an archived game is frozen")
		return
	case api.Recruiting:
		// both GMs and players may be added.
	default:
		if !isGM && !isAdmin {
			writeError(w, r, http.StatusConflict, codeConflict, "players may only be added while recruiting")
			return
		}
	}

	// The referenced account must exist. It is a body reference (not the path
	// resource), so an unknown account is a 400, reserving 404 for the game.
	if _, err := s.db.GetAccount(r.Context(), req.AccountId); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusBadRequest, codeBadRequest, "unknown account")
			return
		}
		logger(r).ErrorContext(r.Context(), "members: get account for add", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not add member")
		return
	}

	m, err := s.db.AddMember(r.Context(), gameID, req.AccountId, isGM)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, r, http.StatusConflict, codeConflict, "account is already a member of this game")
			return
		}
		logger(r).ErrorContext(r.Context(), "members: add", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not add member")
		return
	}
	writeJSON(w, r, http.StatusCreated, api.MemberResponse{Member: toMemberDTO(m)})
}

// handleUpdateGameMember serves PATCH /api/games/{gameId}/members/{playerId}
// (openapi.yaml: updateGameMember). Partial update of a seat; an omitted field is
// left unchanged. Drop and reactivate are folded into isActive (there is no
// DELETE). Rules (openapi.yaml):
//   - isActive true (reactivate): an admin or the game's active GM.
//   - isActive false (drop): the member themselves at any time, or an admin or
//     active GM dropping another.
//   - isGm: only true (promote a player to GM); demotion (false) is a 400. Requires
//     an admin or active GM, recruiting-only unless the caller is an admin.
//   - An archived game is frozen: no update is accepted (409).
//
// An unknown game or seat is a 404; an empty patch is a 400.
func (s *Server) handleUpdateGameMember(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	gameID, ok := s.parseGameID(w, r)
	if !ok {
		return
	}
	playerID, ok := s.parsePlayerID(w, r)
	if !ok {
		return
	}
	var req api.UpdateMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.IsActive == nil && req.IsGm == nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "at least one field is required")
		return
	}
	// isGm supports promotion only; demotion is not part of the surface.
	if req.IsGm != nil && !*req.IsGm {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "isGm may only be set to true")
		return
	}

	g, err := s.db.GetGame(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "game not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "members: get game for update", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update member")
		return
	}

	m, err := s.db.GetMember(r.Context(), gameID, playerID)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "member not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "members: get for update", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update member")
		return
	}

	// An archived game is frozen: nothing may change, not even for an admin.
	if api.GameStatus(g.Status) == api.Archived {
		writeError(w, r, http.StatusConflict, codeConflict, "an archived game is frozen")
		return
	}

	// Establish the caller's authority over this seat.
	isAdmin := account.IsAdmin
	isSelf := m.AccountID == account.ID
	isGM := false
	if !isAdmin {
		isGM, err = s.callerIsActiveGM(r, gameID, account.ID)
		if err != nil {
			logger(r).ErrorContext(r.Context(), "members: resolve gm for update", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update member")
			return
		}
	}
	canManage := isAdmin || isGM

	// isActive: reactivate needs a manager; drop is allowed for the member
	// themselves, or a manager acting on another.
	if req.IsActive != nil {
		if *req.IsActive {
			if !canManage {
				writeError(w, r, http.StatusForbidden, codeForbidden, "reactivating a member requires an admin or active GM")
				return
			}
		} else {
			if !canManage && !isSelf {
				writeError(w, r, http.StatusForbidden, codeForbidden, "you may only drop your own seat")
				return
			}
		}
		m.IsActive = *req.IsActive
	}

	// isGm: promotion only, by a manager, recruiting-only unless the caller is an
	// admin. (An omitted or already-checked false was handled above.)
	if req.IsGm != nil {
		if !canManage {
			writeError(w, r, http.StatusForbidden, codeForbidden, "promoting a member requires an admin or active GM")
			return
		}
		if !isAdmin && api.GameStatus(g.Status) != api.Recruiting {
			writeError(w, r, http.StatusConflict, codeConflict, "a player may only be promoted while recruiting")
			return
		}
		m.IsGM = *req.IsGm
	}

	if err := s.db.UpdateMember(r.Context(), m); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "member not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "members: update", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update member")
		return
	}
	writeJSON(w, r, http.StatusOK, api.MemberResponse{Member: toMemberDTO(m)})
}
