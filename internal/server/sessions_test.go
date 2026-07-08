// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
)

// listMySessions issues GET /api/me/sessions with the given token and decodes the
// response.
func listMySessions(t *testing.T, s *Server, token string) (int, api.ListSessionsResponse) {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/me/sessions", token, nil)
	var out api.ListSessionsResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode ListSessionsResponse: %v", err)
		}
	}
	return rec.Code, out
}

func TestListMySessions(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "me@example.com", "my-secret-1", false, true)

	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodGet, "/api/me/sessions", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon list: status = %d, want 401", rec.Code)
	}

	// Two sessions for the same account; list from the first.
	tok1 := tokenFor(t, s, "me@example.com", "my-secret-1")
	tok2 := tokenFor(t, s, "me@example.com", "my-secret-1")
	_ = tok2

	code, got := listMySessions(t, s, tok1)
	if code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", code)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(got.Sessions))
	}

	// Exactly one is marked current, and it is the caller's own session.
	currentCount := 0
	for _, sess := range got.Sessions {
		if sess.Current != nil && *sess.Current {
			currentCount++
		}
		if sess.IssuedAt.IsZero() || sess.ExpiresAt.IsZero() {
			t.Errorf("session %q missing issuedAt/expiresAt", sess.Id)
		}
	}
	if currentCount != 1 {
		t.Errorf("current-marked sessions = %d, want 1", currentCount)
	}
}

func TestRevokeMySession(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "me@example.com", "my-secret-1", false, true)
	seedAccount(t, s, "other@example.com", "other-secret", false, true)

	keepTok := tokenFor(t, s, "me@example.com", "my-secret-1")
	dropTok := tokenFor(t, s, "me@example.com", "my-secret-1")
	otherTok := tokenFor(t, s, "other@example.com", "other-secret")

	// Discover the id of the session to drop by listing from it.
	_, mine := listMySessions(t, s, dropTok)
	var dropID string
	for _, sess := range mine.Sessions {
		if sess.Current != nil && *sess.Current {
			dropID = sess.Id
		}
	}
	if dropID == "" {
		t.Fatal("could not find current session id to drop")
	}

	// An unknown session id is 404.
	if rec := do(t, s, http.MethodDelete, "/api/me/sessions/nope", keepTok, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: status = %d, want 404", rec.Code)
	}

	// A session belonging to another account is 404 (not revealed cross-account).
	_, others := listMySessions(t, s, otherTok)
	if len(others.Sessions) != 1 {
		t.Fatalf("other account sessions = %d, want 1", len(others.Sessions))
	}
	otherID := others.Sessions[0].Id
	if rec := do(t, s, http.MethodDelete, "/api/me/sessions/"+otherID, keepTok, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-account revoke: status = %d, want 404", rec.Code)
	}
	// The other account's session is still usable — it was not revoked.
	if rec := do(t, s, http.MethodGet, "/api/me", otherTok, nil); rec.Code != http.StatusOK {
		t.Errorf("other session collaterally revoked: status = %d, want 200", rec.Code)
	}

	// Revoke my own (non-current) session: 204, and it can no longer authenticate.
	if rec := do(t, s, http.MethodDelete, "/api/me/sessions/"+dropID, keepTok, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke own: status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, http.MethodGet, "/api/me", dropTok, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("dropped session still valid: status = %d, want 401", rec.Code)
	}
	// Idempotent while the record persists: a second revoke is still 204.
	if rec := do(t, s, http.MethodDelete, "/api/me/sessions/"+dropID, keepTok, nil); rec.Code != http.StatusNoContent {
		t.Errorf("idempotent revoke: status = %d, want 204", rec.Code)
	}
	// The keep session remains valid.
	if rec := do(t, s, http.MethodGet, "/api/me", keepTok, nil); rec.Code != http.StatusOK {
		t.Errorf("keep session revoked unexpectedly: status = %d, want 200", rec.Code)
	}
}

func TestRevokeMyCurrentSession(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "me@example.com", "my-secret-1", false, true)
	tok := tokenFor(t, s, "me@example.com", "my-secret-1")

	_, mine := listMySessions(t, s, tok)
	if len(mine.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(mine.Sessions))
	}
	curID := mine.Sessions[0].Id

	// Revoking the current session is allowed (like logout).
	if rec := do(t, s, http.MethodDelete, "/api/me/sessions/"+curID, tok, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke current: status = %d, want 204", rec.Code)
	}
	if rec := do(t, s, http.MethodGet, "/api/me", tok, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("current session still valid after revoke: status = %d, want 401", rec.Code)
	}
}

func TestListAccountSessionsAdmin(t *testing.T) {
	s := newTestServer(t)
	subjectID := seedAccount(t, s, "subject@example.com", "subject-pass-1", false, true)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	// Give the subject two sessions.
	_ = tokenFor(t, s, "subject@example.com", "subject-pass-1")
	_ = tokenFor(t, s, "subject@example.com", "subject-pass-1")

	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")

	path := "/api/accounts/" + itoa(subjectID) + "/sessions"

	// Non-admin is forbidden.
	if rec := do(t, s, http.MethodGet, path, userTok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin list: status = %d, want 403", rec.Code)
	}

	// Admin sees the subject's two sessions, with no current marker.
	rec := do(t, s, http.MethodGet, path, adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.ListSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(got.Sessions))
	}
	for _, sess := range got.Sessions {
		if sess.Current != nil {
			t.Errorf("admin listing marked current on %q", sess.Id)
		}
	}

	// An unknown account is 404.
	if rec := do(t, s, http.MethodGet, "/api/accounts/99999/sessions", adminTok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown account list: status = %d, want 404", rec.Code)
	}
}

func TestRevokeAccountSessionsAdmin(t *testing.T) {
	s := newTestServer(t)
	subjectID := seedAccount(t, s, "subject@example.com", "subject-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	subTok1 := tokenFor(t, s, "subject@example.com", "subject-pass-1")
	subTok2 := tokenFor(t, s, "subject@example.com", "subject-pass-1")
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	path := "/api/accounts/" + itoa(subjectID) + "/sessions"

	// Revoke all of the subject's sessions.
	if rec := do(t, s, http.MethodDelete, path, adminTok, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke all: status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	// Both subject sessions are now dead.
	if rec := do(t, s, http.MethodGet, "/api/me", subTok1, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("subject session 1 still valid: status = %d, want 401", rec.Code)
	}
	if rec := do(t, s, http.MethodGet, "/api/me", subTok2, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("subject session 2 still valid: status = %d, want 401", rec.Code)
	}
	// Idempotent: a second revoke-all is still 204.
	if rec := do(t, s, http.MethodDelete, path, adminTok, nil); rec.Code != http.StatusNoContent {
		t.Errorf("idempotent revoke all: status = %d, want 204", rec.Code)
	}

	// An unknown account is 404.
	if rec := do(t, s, http.MethodDelete, "/api/accounts/99999/sessions", adminTok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown account revoke all: status = %d, want 404", rec.Code)
	}
}

func TestRevokeAccountSessionAdmin(t *testing.T) {
	s := newTestServer(t)
	subjectID := seedAccount(t, s, "subject@example.com", "subject-pass-1", false, true)
	otherID := seedAccount(t, s, "other@example.com", "other-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	subTok := tokenFor(t, s, "subject@example.com", "subject-pass-1")
	otherTok := tokenFor(t, s, "other@example.com", "other-pass-1")
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Find the subject's session id.
	_, subSessions := listMySessions(t, s, subTok)
	if len(subSessions.Sessions) != 1 {
		t.Fatalf("subject sessions = %d, want 1", len(subSessions.Sessions))
	}
	subSessID := subSessions.Sessions[0].Id

	// A session that belongs to a different account than {accountId} is 404.
	crossPath := "/api/accounts/" + itoa(otherID) + "/sessions/" + subSessID
	if rec := do(t, s, http.MethodDelete, crossPath, adminTok, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("mismatched account/session: status = %d, want 404", rec.Code)
	}
	// The subject's session is still valid (the mismatched revoke was a no-op).
	if rec := do(t, s, http.MethodGet, "/api/me", subTok, nil); rec.Code != http.StatusOK {
		t.Errorf("subject session collaterally revoked: status = %d, want 200", rec.Code)
	}

	// Revoke the correct account+session pair: 204, then the session is dead.
	okPath := "/api/accounts/" + itoa(subjectID) + "/sessions/" + subSessID
	if rec := do(t, s, http.MethodDelete, okPath, adminTok, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke one: status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, http.MethodGet, "/api/me", subTok, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked session still valid: status = %d, want 401", rec.Code)
	}

	// An unknown session id is 404.
	if rec := do(t, s, http.MethodDelete, "/api/accounts/"+itoa(subjectID)+"/sessions/nope", adminTok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404", rec.Code)
	}
	_ = otherTok
}

func TestPurgeSessionsAdmin(t *testing.T) {
	s := newTestServer(t)
	subjectID := seedAccount(t, s, "subject@example.com", "subject-pass-1", false, true)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")

	// Non-admin is forbidden.
	if rec := do(t, s, http.MethodPost, "/api/admin/sessions/purge", userTok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin purge: status = %d, want 403", rec.Code)
	}

	// Seed one already-expired session directly in the store.
	past := time.Now().Add(-2 * time.Hour)
	expired := store.Session{
		ID:          "expired-session-1",
		AccountID:   subjectID,
		HashedToken: hashToken("stale-token"),
		IssuedAt:    past.Add(-time.Hour),
		ExpiresAt:   past,
	}
	if err := s.db.CreateSession(context.Background(), expired); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := do(t, s, http.MethodPost, "/api/admin/sessions/purge", adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("purge: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.PurgeSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Purged < 1 {
		t.Errorf("purged = %d, want >= 1", got.Purged)
	}

	// The admin's own (unexpired) session still works.
	if rec := do(t, s, http.MethodGet, "/api/me", adminTok, nil); rec.Code != http.StatusOK {
		t.Errorf("admin session purged unexpectedly: status = %d, want 200", rec.Code)
	}
}
