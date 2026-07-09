// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// sessionTTL is how long a login session stays valid before it expires. Opaque
// server-side sessions are resolved on every request and revoked immediately on
// logout or account deactivation (ADR-0002), so the TTL is only a backstop for
// abandoned sessions rather than a security-critical window; a generous 30 days
// suits the CLI/script/bot clients that submit turns over a game's lifetime. The
// token format and lifetime are explicitly not a frozen surface (ADR-0002) and
// may change freely.
const sessionTTL = 30 * 24 * time.Hour

// decoySecretHash is a valid encoded bcrypt hash used to equalize login timing
// when no account matches: verifying a presented secret against it does the same
// work as a real check, so a caller cannot distinguish "no such account" from
// "wrong secret" by response time. Computed once at startup.
var decoySecretHash, _ = HashSecret("decoy-secret-for-constant-time-login")

// handleHealth serves GET /api/healthz (openapi.yaml: getHealth). It is a
// liveness probe: it reports the running application version and does not touch
// the database, so it stays green even if the store is degraded.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, api.HealthResponse{
		Status:  "ok",
		Version: s.version,
	})
}

// handleVersion serves GET /api/version (openapi.yaml: getVersion). It reports
// the application version and the open database's schema version (SQLite
// user_version). A failure to read the schema version is a 500 in the standard
// envelope.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	schema, err := s.db.SchemaVersion(r.Context())
	if err != nil {
		logger(r).ErrorContext(r.Context(), "version: read schema version", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not read database version")
		return
	}
	writeJSON(w, r, http.StatusOK, api.VersionResponse{
		Application: s.version,
		Database: api.DatabaseVersion{
			SchemaVersion: int32(schema),
		},
	})
}

// handleLogin serves POST /api/auth/login (openapi.yaml: login). It verifies an
// email + secret against the stored hash and, on success, mints an opaque
// server-side session, returning the raw token once (ADR-0002). Every failure
// mode — malformed body, unknown email, wrong secret, inactive account — returns
// the same 401 so the response never reveals which accounts exist.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req api.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := string(req.Email)
	if email == "" || req.Secret == "" {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "email and secret are required")
		return
	}

	account, err := s.db.GetAccountByEmail(r.Context(), email)
	if err != nil {
		if !errors.Is(err, store.ErrRecordNotFound) {
			logger(r).ErrorContext(r.Context(), "login: lookup account", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not process login")
			return
		}
		// Unknown email: do equivalent work against a decoy so timing cannot be
		// used to enumerate accounts, then deny.
		_ = VerifySecret(decoySecretHash, req.Secret)
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "invalid email or secret")
		return
	}
	if !VerifySecret(account.HashedSecret, req.Secret) || !account.IsActive {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "invalid email or secret")
		return
	}

	token, err := newToken()
	if err != nil {
		logger(r).ErrorContext(r.Context(), "login: mint token", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not process login")
		return
	}
	id, err := newSessionID()
	if err != nil {
		logger(r).ErrorContext(r.Context(), "login: mint session id", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not process login")
		return
	}
	now := time.Now()
	expiresAt := now.Add(sessionTTL)
	if err := s.db.CreateSession(r.Context(), store.Session{
		ID:          id,
		AccountID:   account.ID,
		HashedToken: hashToken(token),
		IssuedAt:    now,
		ExpiresAt:   expiresAt,
	}); err != nil {
		logger(r).ErrorContext(r.Context(), "login: create session", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not process login")
		return
	}

	writeJSON(w, r, http.StatusOK, api.AuthSession{
		Token:     token,
		TokenType: api.AuthSessionTokenTypeBearer,
		ExpiresAt: expiresAt.UTC(),
	})
}

// handleLogout serves POST /api/auth/logout (openapi.yaml: logout). It runs on
// the authenticated group, so requireAuth has already resolved the caller. It
// revokes the session behind the presented token, or — with allSessions: true —
// every active session for the account. Revocation is immediate (ADR-0002).
// Success is 204 No Content.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// The body is optional; default to revoking just the current session.
	var req api.LogoutRequest
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		// requireAuth guarantees this; treat a missing context as unauthorized.
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	now := time.Now()
	var err error
	if req.AllSessions != nil && *req.AllSessions {
		_, err = s.db.RevokeAccountSessions(r.Context(), session.AccountID, now)
	} else {
		err = s.db.RevokeSession(r.Context(), session.ID, now)
	}
	if err != nil && !errors.Is(err, store.ErrRecordNotFound) {
		logger(r).ErrorContext(r.Context(), "logout: revoke session", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not process logout")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
