// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// newTestServerDev builds a Server in development mode backed by a throwaway
// in-memory store, so the dev-only /admin/shutdown route is enabled.
func newTestServerDev(t *testing.T) *Server {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(Config{Addr: ":0", DevMode: true}, db, nil, "9.9.9-test")
}

// closed reports whether the shutdown channel has been closed (i.e. a drain was
// triggered), without blocking.
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestShutdownAdminOnly(t *testing.T) {
	s := newTestServerDev(t)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")

	// Non-admin is forbidden and does not trigger a drain.
	if rec := do(t, s, http.MethodPost, "/api/admin/shutdown", userTok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin shutdown: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodPost, "/api/admin/shutdown", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon shutdown: status = %d, want 401", rec.Code)
	}
	if closed(s.shutdown) {
		t.Fatalf("shutdown triggered by an unauthorized caller")
	}
}

func TestShutdownNotInDevModeIs404(t *testing.T) {
	// A non-dev server hides the route: an admin gets 404, and no drain fires.
	s := newTestServer(t) // DevMode is false
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	rec := do(t, s, http.MethodPost, "/api/admin/shutdown", adminTok, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-dev shutdown: status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if closed(s.shutdown) {
		t.Fatalf("shutdown triggered while not in dev mode")
	}
}

func TestShutdownTriggersDrain(t *testing.T) {
	s := newTestServerDev(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	rec := do(t, s, http.MethodPost, "/api/admin/shutdown", adminTok, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("admin shutdown: status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if !closed(s.shutdown) {
		t.Fatalf("shutdown was acknowledged but no drain was triggered")
	}
	// A second request is a safe no-op (does not panic on a re-close).
	if rec := do(t, s, http.MethodPost, "/api/admin/shutdown", adminTok, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("second shutdown: status = %d, want 202", rec.Code)
	}
}

func TestImpersonationAdminOnly(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	target := seedAccount(t, s, "target@example.com", "target-pass-1", false, true)
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")

	body := api.ImpersonationRequest{AccountId: target}
	// Non-admin is forbidden.
	if rec := do(t, s, http.MethodPost, "/api/admin/impersonation", userTok, body); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin impersonation: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodPost, "/api/admin/impersonation", "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon impersonation: status = %d, want 401", rec.Code)
	}
}

func TestImpersonationValidation(t *testing.T) {
	s := newTestServer(t)
	adminID := seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	seedAccount(t, s, "other-admin@example.com", "admin-pass-2", true, true)
	inactive := seedAccount(t, s, "inactive@example.com", "inactive-pass-1", false, false)
	otherAdmin := int64(0)
	// Find the other admin's id via the store to avoid coupling to seed order.
	if acct, err := s.db.GetAccountByEmail(context.Background(), "other-admin@example.com"); err == nil {
		otherAdmin = acct.ID
	}
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	cases := []struct {
		name   string
		body   api.ImpersonationRequest
		status int
	}{
		{"missing accountId", api.ImpersonationRequest{AccountId: 0}, http.StatusBadRequest},
		{"unknown target", api.ImpersonationRequest{AccountId: 99999}, http.StatusNotFound},
		{"self", api.ImpersonationRequest{AccountId: adminID}, http.StatusConflict},
		{"admin target", api.ImpersonationRequest{AccountId: otherAdmin}, http.StatusConflict},
		{"inactive target", api.ImpersonationRequest{AccountId: inactive}, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, s, http.MethodPost, "/api/admin/impersonation", adminTok, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestImpersonationMintsUsableTargetSession(t *testing.T) {
	s := newTestServer(t)
	adminID := seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	targetID := seedAccount(t, s, "target@example.com", "target-pass-1", false, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	rec := do(t, s, http.MethodPost, "/api/admin/impersonation", adminTok, api.ImpersonationRequest{AccountId: targetID})
	if rec.Code != http.StatusOK {
		t.Fatalf("impersonation: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var out api.ImpersonationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode ImpersonationResponse: %v", err)
	}
	if out.Token == "" {
		t.Fatalf("empty token")
	}
	if out.TokenType != api.ImpersonationResponseTokenTypeBearer {
		t.Errorf("tokenType = %q, want Bearer", out.TokenType)
	}
	if out.Subject.AccountId != targetID {
		t.Errorf("subject.accountId = %d, want %d", out.Subject.AccountId, targetID)
	}
	if string(out.Subject.Email) != "target@example.com" {
		t.Errorf("subject.email = %q, want target@example.com", out.Subject.Email)
	}
	if out.Actor.AccountId != adminID {
		t.Errorf("actor.accountId = %d, want %d", out.Actor.AccountId, adminID)
	}
	if !out.ExpiresAt.After(time.Now()) {
		t.Errorf("expiresAt = %v, want a future time", out.ExpiresAt)
	}

	// The token resolves to the TARGET account (the effective identity), and the
	// response carries the Impersonated-Subject header so the acting is observable.
	meRec := do(t, s, http.MethodGet, "/api/me", out.Token, nil)
	if meRec.Code != http.StatusOK {
		t.Fatalf("GET /me with impersonation token: status = %d, want 200; body=%s", meRec.Code, meRec.Body.String())
	}
	if got := meRec.Header().Get("Impersonated-Subject"); got != strconv.FormatInt(targetID, 10) {
		t.Errorf("Impersonated-Subject = %q, want %d", got, targetID)
	}
	var me api.AccountResponse
	if err := json.Unmarshal(meRec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode AccountResponse: %v", err)
	}
	if me.Account.Id != targetID {
		t.Errorf("/me id = %d, want target %d", me.Account.Id, targetID)
	}
	if string(me.Account.Email) != "target@example.com" {
		t.Errorf("/me email = %q, want target@example.com", me.Account.Email)
	}

	// The persisted session records the admin as the auditable actor.
	sess, err := s.db.GetActiveSessionByToken(context.Background(), hashToken(out.Token), time.Now())
	if err != nil {
		t.Fatalf("GetActiveSessionByToken: %v", err)
	}
	if sess.AccountID != targetID {
		t.Errorf("session.AccountID = %d, want target %d", sess.AccountID, targetID)
	}
	if sess.Actor != adminID {
		t.Errorf("session.Actor = %d, want admin %d (impersonation must be auditable)", sess.Actor, adminID)
	}
}
