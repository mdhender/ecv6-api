// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdhender/ecv6-api/internal/store"
)

// authKey types the context value carrying the resolved caller.
type authKey struct{}

// authInfo is the resolved identity for an authenticated request: the effective
// account (the session's subject) and the session behind the presented token.
// For an impersonation session Session.Actor names the admin acting; ordinary
// sessions leave it zero. Handlers read it with accountFromContext /
// sessionFromContext.
type authInfo struct {
	account store.Account
	session store.Session
}

// withAuth stores the resolved caller on ctx for downstream handlers.
func withAuth(ctx context.Context, info authInfo) context.Context {
	return context.WithValue(ctx, authKey{}, info)
}

// accountFromContext returns the authenticated account and true when the request
// passed through requireAuth, or the zero account and false otherwise.
func accountFromContext(ctx context.Context) (store.Account, bool) {
	info, ok := ctx.Value(authKey{}).(authInfo)
	return info.account, ok
}

// sessionFromContext returns the session behind the presented token and true
// when the request passed through requireAuth, or the zero session and false.
func sessionFromContext(ctx context.Context) (store.Session, bool) {
	info, ok := ctx.Value(authKey{}).(authInfo)
	return info.session, ok
}

// bearerToken extracts the raw token from an "Authorization: Bearer <token>"
// header, returning "" when the header is absent or not a well-formed bearer
// credential. The scheme is matched case-insensitively per RFC 7235.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// requireAuth resolves the bearer token to an active session and account and
// stores both on the request context, or denies with the standard 401 envelope.
// It replaces the fail-closed skeleton placeholder with real session resolution
// (ADR-0002): the token is hashed and looked up, the session must be neither
// revoked nor expired, and the account is re-read every request so a deactivated
// account fails immediately. A missing, malformed, unknown, revoked, or expired
// credential — and a deactivated account — all yield the same opaque 401, so a
// caller cannot tell them apart.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication required")
			return
		}
		session, err := s.db.GetActiveSessionByToken(r.Context(), hashToken(raw), time.Now())
		if err != nil {
			if !errors.Is(err, store.ErrRecordNotFound) {
				logger(r).ErrorContext(r.Context(), "auth: resolve session", "err", err)
			}
			writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "invalid or expired session")
			return
		}
		account, err := s.db.GetAccount(r.Context(), session.AccountID)
		if err != nil {
			if !errors.Is(err, store.ErrRecordNotFound) {
				logger(r).ErrorContext(r.Context(), "auth: load account", "err", err)
			}
			writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "invalid or expired session")
			return
		}
		if !account.IsActive {
			writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "invalid or expired session")
			return
		}
		// An impersonation session carries the acting admin in Actor; surface the
		// effective (impersonated) subject on the response so every call made with
		// the token is visibly attributed, and the acting is auditable (ADR-0002).
		if session.Actor != 0 {
			w.Header().Set("Impersonated-Subject", strconv.FormatInt(account.ID, 10))
		}
		ctx := withAuth(r.Context(), authInfo{account: account, session: session})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin authorizes the admin route group. It runs after requireAuth and
// rejects a non-admin caller with the standard 403 envelope; an unauthenticated
// caller has already been stopped by requireAuth. A missing auth context (the
// group misconfigured) is treated as forbidden, fail-closed.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := accountFromContext(r.Context())
		if !ok || !account.IsAdmin {
			writeError(w, r, http.StatusForbidden, codeForbidden, "admin privileges required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
