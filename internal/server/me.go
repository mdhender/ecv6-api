// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// handleGetMe serves GET /api/me (openapi.yaml: getMe). It returns the caller's
// account — an application-domain projection only, with no in-game data (ADR-0004).
// requireAuth has already resolved and freshly read the account.
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	writeJSON(w, r, http.StatusOK, api.AccountResponse{Account: toAccountDTO(account)})
}

// handleUpdateMe serves PATCH /api/me (openapi.yaml: updateMe). It is the
// self-service profile update, limited to the non-sensitive displayName; email
// and secret changes have their own confirmation-guarded routes.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	var req api.UpdateMeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	account.DisplayName = req.DisplayName
	if err := s.db.UpdateAccount(r.Context(), account); err != nil {
		logger(r).ErrorContext(r.Context(), "me: update profile", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update profile")
		return
	}
	writeJSON(w, r, http.StatusOK, api.AccountResponse{Account: toAccountDTO(account)})
}

// handleUpdateMyEmail serves POST /api/me/email (openapi.yaml: updateMyEmail).
// Because email is the login identity, the caller must supply the current secret,
// verified before the change. The new email is lowercased and must be unique
// (409). Existing sessions are not revoked — the secret is unchanged.
func (s *Server) handleUpdateMyEmail(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	var req api.ChangeEmailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !VerifySecret(account.HashedSecret, req.CurrentSecret) {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "current secret is incorrect")
		return
	}
	email := strings.ToLower(strings.TrimSpace(string(req.NewEmail)))
	if email == "" {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "newEmail is required")
		return
	}
	account.Email = email
	if err := s.db.UpdateAccount(r.Context(), account); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, r, http.StatusConflict, codeConflict, "an account with that email already exists")
			return
		}
		logger(r).ErrorContext(r.Context(), "me: update email", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update email")
		return
	}
	writeJSON(w, r, http.StatusOK, api.AccountResponse{Account: toAccountDTO(account)})
}

// handleUpdateMySecret serves POST /api/me/secret (openapi.yaml: updateMySecret).
// The caller supplies the current secret, verified before the new one is applied;
// on success the account's other sessions are revoked so a stolen session cannot
// outlive the secret it was created under. requireAuth already re-read the account
// this request and rejected an inactive one, so the account here is fresh and
// active. Success is 204 No Content.
func (s *Server) handleUpdateMySecret(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	var req api.ChangeSecretRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !VerifySecret(account.HashedSecret, req.CurrentSecret) {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "current secret is incorrect")
		return
	}
	if len(req.NewSecret) < minSecretLen {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "newSecret must be at least 8 characters")
		return
	}
	hashed, err := s.hashSecret(req.NewSecret)
	if err != nil {
		logger(r).ErrorContext(r.Context(), "me: hash secret", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update secret")
		return
	}
	account.HashedSecret = hashed
	if err := s.db.UpdateAccount(r.Context(), account); err != nil {
		logger(r).ErrorContext(r.Context(), "me: update secret", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update secret")
		return
	}
	// Revoke the caller's other sessions, sparing the one behind this request.
	if _, err := s.db.RevokeAccountSessionsExcept(r.Context(), account.ID, session.ID, time.Now()); err != nil {
		logger(r).ErrorContext(r.Context(), "me: revoke other sessions", "err", err)
		// The secret is already changed; report success rather than fail the change.
	}
	w.WriteHeader(http.StatusNoContent)
}
