// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// gameStatusRank maps each lifecycle status to its position on the forward-only
// track draft → recruiting → active → paused → complete → archived (ADR-0003;
// openapi.yaml updateGame). A status change is "forward" when the target ranks
// higher; skips are allowed. Backward moves are rejected except the two admin-only
// exceptions (paused → active, and moving out of archived).
var gameStatusRank = map[api.GameStatus]int{
	api.Draft:      0,
	api.Recruiting: 1,
	api.Active:     2,
	api.Paused:     3,
	api.Complete:   4,
	api.Archived:   5,
}

// toGameDTO projects a store.Game onto the wire Game schema. description is
// omitted when empty.
func toGameDTO(g store.Game) api.Game {
	dto := api.Game{
		Id:       g.ID,
		Name:     g.Name,
		Status:   api.GameStatus(g.Status),
		IsActive: g.IsActive,
	}
	if g.Description != "" {
		d := g.Description
		dto.Description = &d
	}
	return dto
}

// toMyGameDTO projects a store.MyGame (a game joined to the caller's seat) onto
// the wire MyGame schema.
func toMyGameDTO(m store.MyGame) api.MyGame {
	return api.MyGame{
		Id:       m.GameID,
		Name:     m.Name,
		IsActive: m.IsActive,
		PlayerId: m.PlayerID,
		IsGm:     m.IsGM,
	}
}

// parseGameID reads the {gameId} path value as an int64, writing the standard 400
// envelope and returning ok=false when it is missing or malformed.
func (s *Server) parseGameID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("gameId"), 10, 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "invalid game id")
		return 0, false
	}
	return id, true
}

// handleListGames serves GET /api/games (openapi.yaml: listGames). Authenticated;
// results are filtered to what the caller may see: an admin sees every game, a
// non-admin sees only games with isActive true (the admin-only visibility flag
// hides a game from non-admin listings). An optional status query parameter
// further restricts the result to one lifecycle status; an unknown status is 400.
func (s *Server) handleListGames(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var statusFilter string
	if raw := r.URL.Query().Get("status"); raw != "" {
		st := api.GameStatus(raw)
		if !st.Valid() {
			writeError(w, r, http.StatusBadRequest, codeBadRequest, "invalid status filter")
			return
		}
		statusFilter = raw
	}

	games, err := s.db.ListGames(r.Context(), statusFilter)
	if err != nil {
		logger(r).ErrorContext(r.Context(), "games: list", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list games")
		return
	}

	out := make([]api.Game, 0, len(games))
	for _, g := range games {
		if !account.IsAdmin && !g.IsActive {
			continue // hidden from non-admin listings
		}
		out = append(out, toGameDTO(g))
	}
	writeJSON(w, r, http.StatusOK, api.ListGamesResponse{Games: out})
}

// handleCreateGame serves POST /api/games (openapi.yaml: createGame). Admin only
// (enforced by the group's requireAdmin). name is required; the new game starts in
// draft, inactive-until-set, with the given description.
func (s *Server) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	var req api.CreateGameRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "name is required")
		return
	}

	g := store.Game{
		Name:        name,
		Status:      string(api.Draft),
		Description: derefOr(req.Description, ""),
	}
	id, err := s.db.CreateGame(r.Context(), g)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, r, http.StatusConflict, codeConflict, "could not create game")
			return
		}
		logger(r).ErrorContext(r.Context(), "games: create", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create game")
		return
	}
	g.ID = id

	writeJSON(w, r, http.StatusCreated, api.GameResponse{Game: toGameDTO(g)})
}

// handleGetGame serves GET /api/games/{gameId} (openapi.yaml: getGame).
// Authenticated. An unknown id is 404; a game hidden from the caller (isActive
// false, caller not an admin) is 403; otherwise the game is returned.
func (s *Server) handleGetGame(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	id, ok := s.parseGameID(w, r)
	if !ok {
		return
	}
	g, err := s.db.GetGame(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "game not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "games: get", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not read game")
		return
	}
	if !account.IsAdmin && !g.IsActive {
		writeError(w, r, http.StatusForbidden, codeForbidden, "you do not have access to this game")
		return
	}
	writeJSON(w, r, http.StatusOK, api.GameResponse{Game: toGameDTO(g)})
}

// handleUpdateGame serves PATCH /api/games/{gameId} (openapi.yaml: updateGame).
// Partial update with a per-field authorization policy (openapi.yaml):
//   - status advances forward-only (skips allowed) for an active GM or an admin;
//     backward moves are 409 except paused → active and moving out of archived,
//     which are admin-only.
//   - name / description: an active GM or an admin, and rejected once archived.
//   - isActive: admin only, and frozen while archived.
//
// A caller who is neither an admin nor the game's active GM may change nothing
// (403). An unknown id is 404.
func (s *Server) handleUpdateGame(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	id, ok := s.parseGameID(w, r)
	if !ok {
		return
	}
	var req api.UpdateGameRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Status == nil && req.Name == nil && req.Description == nil && req.IsActive == nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "at least one field is required")
		return
	}
	if req.Status != nil && !req.Status.Valid() {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "invalid status")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "name must not be empty")
		return
	}

	g, err := s.db.GetGame(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "game not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "games: get for update", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update game")
		return
	}

	// Establish the caller's authority over this game: an admin, or the game's
	// active GM. A non-admin's GM status comes from their active, GM-flagged seat.
	isAdmin := account.IsAdmin
	isGM := false
	if !isAdmin {
		m, err := s.db.GetMemberByAccount(r.Context(), id, account.ID)
		switch {
		case err == nil:
			isGM = m.IsActive && m.IsGM
		case errors.Is(err, store.ErrRecordNotFound):
			// not a member; isGM stays false
		default:
			logger(r).ErrorContext(r.Context(), "games: resolve membership", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update game")
			return
		}
	}
	if !isAdmin && !isGM {
		writeError(w, r, http.StatusForbidden, codeForbidden, "you do not have permission to update this game")
		return
	}

	archived := api.GameStatus(g.Status) == api.Archived

	// isActive is the admin-only visibility flag, and frozen while archived.
	if req.IsActive != nil {
		if !isAdmin {
			writeError(w, r, http.StatusForbidden, codeForbidden, "changing visibility requires admin")
			return
		}
		if archived {
			writeError(w, r, http.StatusConflict, codeConflict, "an archived game is frozen")
			return
		}
		g.IsActive = *req.IsActive
	}

	// name / description are metadata: an admin or active GM (already gated above),
	// but rejected once archived.
	if req.Name != nil || req.Description != nil {
		if archived {
			writeError(w, r, http.StatusConflict, codeConflict, "an archived game is frozen")
			return
		}
		if req.Name != nil {
			g.Name = strings.TrimSpace(*req.Name)
		}
		if req.Description != nil {
			g.Description = *req.Description
		}
	}

	// status advances the lifecycle, forward-only with two admin-only exceptions.
	if req.Status != nil {
		newStatus := *req.Status
		cur := gameStatusRank[api.GameStatus(g.Status)]
		next := gameStatusRank[newStatus]
		switch {
		case next > cur:
			// forward move: allowed for an admin or an active GM (gated above).
			g.Status = string(newStatus)
		case next == cur:
			// no-op; leave status unchanged.
		default:
			// backward move: only the un-pause and out-of-archived exceptions are
			// legal, and both require an admin.
			unpause := api.GameStatus(g.Status) == api.Paused && newStatus == api.Active
			if archived || unpause {
				if !isAdmin {
					writeError(w, r, http.StatusForbidden, codeForbidden, "this status change requires admin")
					return
				}
				g.Status = string(newStatus)
			} else {
				writeError(w, r, http.StatusConflict, codeConflict, "cannot move game status backward")
				return
			}
		}
	}

	if err := s.db.UpdateGame(r.Context(), g); err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, codeConflict, "could not update game")
		case errors.Is(err, store.ErrRecordNotFound):
			writeError(w, r, http.StatusNotFound, codeNotFound, "game not found")
		default:
			logger(r).ErrorContext(r.Context(), "games: update", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update game")
		}
		return
	}
	writeJSON(w, r, http.StatusOK, api.GameResponse{Game: toGameDTO(g)})
}

// handleListMyGames serves GET /api/me/games (openapi.yaml: listMyGames). It
// returns the games the caller holds an active seat in, each projected with the
// caller's playerId and GM flag. It is a per-account read scoped to the caller; an
// account with no seats gets an empty list, not an error.
func (s *Server) handleListMyGames(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	games, err := s.db.ListMyGames(r.Context(), account.ID)
	if err != nil {
		logger(r).ErrorContext(r.Context(), "games: list mine", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list your games")
		return
	}
	out := make([]api.MyGame, 0, len(games))
	for _, g := range games {
		out = append(out, toMyGameDTO(g))
	}
	writeJSON(w, r, http.StatusOK, api.ListMyGamesResponse{Games: out})
}
