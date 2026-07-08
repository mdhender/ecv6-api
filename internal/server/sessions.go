// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// toSessionDTO projects a store.Session onto the wire Session schema. Raw tokens
// and the token hash are never included (ADR-0002). current marks the session
// behind the request and is only meaningful on self listings; it is left nil
// (omitted) for admin listings.
func toSessionDTO(s store.Session, current bool) api.Session {
	dto := api.Session{
		Id:        s.ID,
		IssuedAt:  s.IssuedAt,
		ExpiresAt: s.ExpiresAt,
	}
	if current {
		c := true
		dto.Current = &c
	}
	return dto
}

// handleListMySessions serves GET /api/me/sessions (openapi.yaml: listMySessions).
// It returns the caller's active sessions (neither revoked nor expired), newest
// first, with the session behind the current request marked current: true.
func (s *Server) handleListMySessions(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	current, _ := sessionFromContext(r.Context())

	sessions, err := s.db.ListActiveSessionsByAccount(r.Context(), account.ID, time.Now())
	if err != nil {
		logger(r).ErrorContext(r.Context(), "sessions: list mine", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list sessions")
		return
	}
	out := make([]api.Session, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, toSessionDTO(sess, sess.ID == current.ID))
	}
	writeJSON(w, r, http.StatusOK, api.ListSessionsResponse{Sessions: out})
}

// handleRevokeMySession serves DELETE /api/me/sessions/{sessionId} (openapi.yaml:
// revokeMySession) — the "log out this device" counterpart to logout. Only the
// caller's own sessions are revocable: an unknown session, or one owned by another
// account, yields 404 rather than revealing its existence. Revoking the current
// session is allowed (it is just a logout). Idempotent while the record persists.
func (s *Server) handleRevokeMySession(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	id := r.PathValue("sessionId")
	if id == "" {
		writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
		return
	}

	sess, err := s.db.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "sessions: get mine", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not revoke session")
		return
	}
	// A session owned by another account is invisible to the caller: 404, not 403,
	// so its existence is not revealed cross-account.
	if sess.AccountID != account.ID {
		writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
		return
	}

	if err := s.db.RevokeSession(r.Context(), id, time.Now()); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "sessions: revoke mine", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not revoke session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListAccountSessions serves GET /api/accounts/{accountId}/sessions
// (openapi.yaml: listAccountSessions). Admin only. It lists the target account's
// active sessions; an unknown account is 404. The current marker is meaningless
// here (the admin is not the subject), so it is omitted.
func (s *Server) handleListAccountSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseAccountID(w, r)
	if !ok {
		return
	}
	if _, err := s.db.GetAccount(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "account not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "sessions: load account", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list sessions")
		return
	}

	sessions, err := s.db.ListActiveSessionsByAccount(r.Context(), id, time.Now())
	if err != nil {
		logger(r).ErrorContext(r.Context(), "sessions: list for account", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list sessions")
		return
	}
	out := make([]api.Session, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, toSessionDTO(sess, false))
	}
	writeJSON(w, r, http.StatusOK, api.ListSessionsResponse{Sessions: out})
}

// handleRevokeAccountSessions serves DELETE /api/accounts/{accountId}/sessions
// (openapi.yaml: revokeAccountSessions) — the admin "force logout everywhere" for
// a compromised or deactivated account. Admin only; an unknown account is 404.
// Immediate and idempotent (revoking with nothing active is a no-op 204).
func (s *Server) handleRevokeAccountSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseAccountID(w, r)
	if !ok {
		return
	}
	if _, err := s.db.GetAccount(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "account not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "sessions: load account", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not revoke sessions")
		return
	}

	if _, err := s.db.RevokeAccountSessions(r.Context(), id, time.Now()); err != nil {
		logger(r).ErrorContext(r.Context(), "sessions: revoke account", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not revoke sessions")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRevokeAccountSession serves DELETE
// /api/accounts/{accountId}/sessions/{sessionId} (openapi.yaml:
// revokeAccountSession). Admin only. It revokes a single session of the target
// account; a session that does not belong to the account (or does not exist) is
// 404. Immediate and idempotent while the record persists.
func (s *Server) handleRevokeAccountSession(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseAccountID(w, r)
	if !ok {
		return
	}
	sid := r.PathValue("sessionId")
	if sid == "" {
		writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
		return
	}

	sess, err := s.db.GetSession(r.Context(), sid)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "sessions: get for account", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not revoke session")
		return
	}
	if sess.AccountID != id {
		writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
		return
	}

	if err := s.db.RevokeSession(r.Context(), sid, time.Now()); err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "session not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "sessions: revoke for account", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not revoke session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePurgeSessions serves POST /api/admin/sessions/purge (openapi.yaml:
// purgeSessions). Admin only. It hard-deletes session records that have already
// expired — the only physical delete in the store — and returns the number
// removed. Expired sessions no longer authenticate, so this affects neither
// active sessions nor revocation.
func (s *Server) handlePurgeSessions(w http.ResponseWriter, r *http.Request) {
	purged, err := s.db.PurgeExpiredSessions(r.Context(), time.Now())
	if err != nil {
		logger(r).ErrorContext(r.Context(), "sessions: purge expired", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not purge sessions")
		return
	}
	writeJSON(w, r, http.StatusOK, api.PurgeSessionsResponse{Purged: purged})
}
