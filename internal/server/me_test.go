// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mdhender/ecv6-api/internal/api"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func TestGetMe(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "me@example.com", "my-secret-1", false, true)
	tok := tokenFor(t, s, "me@example.com", "my-secret-1")

	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodGet, "/api/me", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon /me: status = %d, want 401", rec.Code)
	}

	rec := do(t, s, http.MethodGet, "/api/me", tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.AccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(got.Account.Email) != "me@example.com" {
		t.Errorf("email = %q, want me@example.com", got.Account.Email)
	}
	if len(got.Account.Roles) != 1 || got.Account.Roles[0] != "user" {
		t.Errorf("roles = %v, want [user]", got.Account.Roles)
	}
}

func TestUpdateMe(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "me@example.com", "my-secret-1", false, true)
	tok := tokenFor(t, s, "me@example.com", "my-secret-1")

	rec := do(t, s, http.MethodPatch, "/api/me", tok, api.UpdateMeRequest{DisplayName: "New Name"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch /me: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.AccountResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Account.DisplayName == nil || *got.Account.DisplayName != "New Name" {
		t.Errorf("displayName = %v, want New Name", got.Account.DisplayName)
	}

	// Persisted: a fresh GET shows the new name.
	rec = do(t, s, http.MethodGet, "/api/me", tok, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Account.DisplayName == nil || *got.Account.DisplayName != "New Name" {
		t.Errorf("persisted displayName = %v, want New Name", got.Account.DisplayName)
	}
}

func TestUpdateMyEmail(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "me@example.com", "my-secret-1", false, true)
	seedAccount(t, s, "taken@example.com", "other-secret", false, true)
	tok := tokenFor(t, s, "me@example.com", "my-secret-1")

	// Wrong current secret is 401.
	body := api.ChangeEmailRequest{CurrentSecret: "wrong", NewEmail: openapi_types.Email("fresh@example.com")}
	if rec := do(t, s, http.MethodPost, "/api/me/email", tok, body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: status = %d, want 401", rec.Code)
	}

	// Conflict with an existing email is 409.
	body = api.ChangeEmailRequest{CurrentSecret: "my-secret-1", NewEmail: openapi_types.Email("taken@example.com")}
	if rec := do(t, s, http.MethodPost, "/api/me/email", tok, body); rec.Code != http.StatusConflict {
		t.Fatalf("email conflict: status = %d, want 409", rec.Code)
	}

	// Successful change.
	body = api.ChangeEmailRequest{CurrentSecret: "my-secret-1", NewEmail: openapi_types.Email("fresh@example.com")}
	rec := do(t, s, http.MethodPost, "/api/me/email", tok, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("change email: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.AccountResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if string(got.Account.Email) != "fresh@example.com" {
		t.Errorf("email = %q, want fresh@example.com", got.Account.Email)
	}
	// Login now works under the new email (session was not revoked).
	if rec := doLogin(t, s, "fresh@example.com", "my-secret-1"); rec.Code != http.StatusOK {
		t.Errorf("login under new email: status = %d, want 200", rec.Code)
	}
}

func TestUpdateMySecret(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "me@example.com", "old-secret-1", false, true)

	// Two sessions for the same account.
	keepTok := tokenFor(t, s, "me@example.com", "old-secret-1")
	otherTok := tokenFor(t, s, "me@example.com", "old-secret-1")

	// Wrong current secret is 401.
	if rec := do(t, s, http.MethodPost, "/api/me/secret", keepTok,
		api.ChangeSecretRequest{CurrentSecret: "nope", NewSecret: "new-secret-9"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current secret: status = %d, want 401", rec.Code)
	}
	// Too-short new secret is 400.
	if rec := do(t, s, http.MethodPost, "/api/me/secret", keepTok,
		api.ChangeSecretRequest{CurrentSecret: "old-secret-1", NewSecret: "short"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("short new secret: status = %d, want 400", rec.Code)
	}

	// Successful change is 204.
	rec := do(t, s, http.MethodPost, "/api/me/secret", keepTok,
		api.ChangeSecretRequest{CurrentSecret: "old-secret-1", NewSecret: "new-secret-9"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change secret: status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	// The old secret no longer logs in; the new one does.
	if rec := doLogin(t, s, "me@example.com", "old-secret-1"); rec.Code != http.StatusUnauthorized {
		t.Errorf("login with old secret: status = %d, want 401", rec.Code)
	}
	if rec := doLogin(t, s, "me@example.com", "new-secret-9"); rec.Code != http.StatusOK {
		t.Errorf("login with new secret: status = %d, want 200", rec.Code)
	}

	// The session that made the change is spared; the other is revoked.
	if code := doLogout(t, s, keepTok, false); code != http.StatusNoContent {
		t.Errorf("current session revoked unexpectedly: status = %d, want 204", code)
	}
	if code := doLogout(t, s, otherTok, false); code != http.StatusUnauthorized {
		t.Errorf("other session not revoked: status = %d, want 401", code)
	}
}
