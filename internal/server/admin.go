// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// impersonationTTL is how long an admin-minted impersonation session stays valid.
// Impersonation is a support/debugging affordance, not a working credential, so
// it is deliberately short-lived (an hour) rather than the 30-day login TTL: it
// should expire on its own soon after the session it was minted for ends.
const impersonationTTL = time.Hour

// handleShutdown serves POST /api/admin/shutdown (openapi.yaml: shutdownServer).
// Admin only, and enabled only in development mode: when the server was not
// started with --dev it responds 404, as if the route did not exist, so the
// operational shutdown is not exposed in production. In dev mode it acknowledges
// with 202 and then triggers the same graceful drain as an interrupt signal —
// the 202 is itself an in-flight request, so it reaches the client before the
// process stops.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DevMode {
		// Not in dev mode: behave as if the route is not registered at all.
		writeError(w, r, http.StatusNotFound, codeNotFound, "resource not found")
		return
	}
	account, _ := accountFromContext(r.Context())
	logger(r).WarnContext(r.Context(), "admin shutdown requested", "adminAccountId", account.ID, "adminEmail", account.Email)

	// Acknowledge before draining. The drain (http.Server.Shutdown) waits for this
	// handler to return, so writing the status here guarantees the client sees the
	// 202 before the connection closes.
	w.WriteHeader(http.StatusAccepted)
	s.triggerShutdown()
}

// handleCreateImpersonation serves POST /api/admin/impersonation (openapi.yaml:
// createImpersonation). Admin only. It mints a short-lived session bearing a
// target account's identity so support and debugging can reproduce what that
// player sees. The session records the calling admin in actor_account_id, so every
// request made with the returned token is auditably attributed to the real admin
// while authorization uses the target's (effective) identity (ADR-0002). The
// target must be an active, non-admin account other than the caller.
func (s *Server) handleCreateImpersonation(w http.ResponseWriter, r *http.Request) {
	admin, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	var req api.ImpersonationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AccountId < 1 {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "accountId is required")
		return
	}

	target, err := s.db.GetAccount(r.Context(), req.AccountId)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "account not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "impersonation: load target", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create impersonation")
		return
	}
	// The target must be eligible: not the caller, not an admin, and active. Each is
	// a conflict with the current state of an existing account rather than a
	// malformed request, so all three are 409.
	switch {
	case target.ID == admin.ID:
		writeError(w, r, http.StatusConflict, codeConflict, "cannot impersonate yourself")
		return
	case target.IsAdmin:
		writeError(w, r, http.StatusConflict, codeConflict, "cannot impersonate an admin account")
		return
	case !target.IsActive:
		writeError(w, r, http.StatusConflict, codeConflict, "cannot impersonate an inactive account")
		return
	}

	token, err := newToken()
	if err != nil {
		logger(r).ErrorContext(r.Context(), "impersonation: mint token", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create impersonation")
		return
	}
	id, err := newSessionID()
	if err != nil {
		logger(r).ErrorContext(r.Context(), "impersonation: mint session id", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create impersonation")
		return
	}
	now := time.Now()
	expiresAt := now.Add(impersonationTTL)
	if err := s.db.CreateSession(r.Context(), store.Session{
		ID:          id,
		AccountID:   target.ID, // the effective (impersonated) identity
		HashedToken: hashToken(token),
		Actor:       admin.ID, // the auditable actor: the admin acting
		IssuedAt:    now,
		ExpiresAt:   expiresAt,
	}); err != nil {
		logger(r).ErrorContext(r.Context(), "impersonation: create session", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create impersonation")
		return
	}

	// Audit trail: record who impersonated whom, with the session id so the minted
	// credential can be tied back to this event and, if needed, revoked.
	logger(r).WarnContext(r.Context(), "impersonation session minted",
		"actorAccountId", admin.ID, "actorEmail", admin.Email,
		"subjectAccountId", target.ID, "subjectEmail", target.Email,
		"sessionId", id, "expiresAt", expiresAt.UTC())

	writeJSON(w, r, http.StatusOK, api.ImpersonationResponse{
		Token:     token,
		TokenType: api.ImpersonationResponseTokenTypeBearer,
		ExpiresAt: expiresAt.UTC(),
		Subject: api.ImpersonationSubject{
			AccountId: target.ID,
			Email:     openapi_types.Email(target.Email),
		},
		Actor: api.ImpersonationActor{AccountId: admin.ID},
	})
}
