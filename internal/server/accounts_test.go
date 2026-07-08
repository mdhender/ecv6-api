// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/mdhender/ecv6-api/internal/api"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// tokenFor logs the given account in and returns its bearer token.
func tokenFor(t *testing.T, s *Server, email, secret string) string {
	t.Helper()
	rec := doLogin(t, s, email, secret)
	if rec.Code != http.StatusOK {
		t.Fatalf("login for %s: status = %d, want 200; body=%s", email, rec.Code, rec.Body.String())
	}
	var sess api.AuthSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode AuthSession: %v", err)
	}
	return sess.Token
}

// do issues a request with an optional bearer token and JSON body, returning the
// recorder.
func do(t *testing.T, s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func ptr[T any](v T) *T { return &v }

func TestCreateAccountAdminOnly(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	body := api.CreateAccountRequest{
		Email:    openapi_types.Email("newbie@example.com"),
		IsActive: ptr(true),
	}

	// Non-admin is forbidden.
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")
	if rec := do(t, s, http.MethodPost, "/api/accounts", userTok, body); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin create: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// Unauthenticated is 401.
	if rec := do(t, s, http.MethodPost, "/api/accounts", "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon create: status = %d, want 401", rec.Code)
	}

	// Admin succeeds; no secret given, so one is generated and returned once.
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	rec := do(t, s, http.MethodPost, "/api/accounts", adminTok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got api.CreateAccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.GeneratedSecret == nil || *got.GeneratedSecret == "" {
		t.Fatalf("expected a generated secret, got none")
	}
	if got.Account.Id == 0 {
		t.Errorf("created account missing id")
	}
	if string(got.Account.Email) != "newbie@example.com" {
		t.Errorf("email = %q, want newbie@example.com", got.Account.Email)
	}
	if len(got.Account.Roles) != 1 || got.Account.Roles[0] != "user" {
		t.Errorf("roles = %v, want [user]", got.Account.Roles)
	}
	if !got.Account.IsActive {
		t.Errorf("isActive = false, want true")
	}

	// The generated secret actually works for login.
	if rec := doLogin(t, s, "newbie@example.com", *got.GeneratedSecret); rec.Code != http.StatusOK {
		t.Errorf("login with generated secret: status = %d, want 200", rec.Code)
	}
}

func TestCreateAccountConflict(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	body := api.CreateAccountRequest{Email: openapi_types.Email("dup@example.com"), Secret: ptr("password123")}
	if rec := do(t, s, http.MethodPost, "/api/accounts", adminTok, body); rec.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// Same email (differing only in case) conflicts.
	body.Email = openapi_types.Email("DUP@example.com")
	rec := do(t, s, http.MethodPost, "/api/accounts", adminTok, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup create: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var got api.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Error.Code != codeConflict {
		t.Errorf("code = %q, want %q", got.Error.Code, codeConflict)
	}
}

func TestCreateAccountValidation(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Missing email (raw body: the typed Email marshaler rejects an empty value).
	if rec := do(t, s, http.MethodPost, "/api/accounts", adminTok, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty email: status = %d, want 400", rec.Code)
	}
	// Too-short supplied secret.
	body := api.CreateAccountRequest{Email: openapi_types.Email("x@example.com"), Secret: ptr("short")}
	if rec := do(t, s, http.MethodPost, "/api/accounts", adminTok, body); rec.Code != http.StatusBadRequest {
		t.Errorf("short secret: status = %d, want 400", rec.Code)
	}
}

func TestListAccounts(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "user@example.com", "user-pass-1", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)

	// Non-admin forbidden.
	userTok := tokenFor(t, s, "user@example.com", "user-pass-1")
	if rec := do(t, s, http.MethodGet, "/api/accounts", userTok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin list: status = %d, want 403", rec.Code)
	}

	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")
	rec := do(t, s, http.MethodGet, "/api/accounts", adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.ListAccountsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Accounts) != 2 {
		t.Errorf("len(accounts) = %d, want 2", len(got.Accounts))
	}
}

func TestGetAccount(t *testing.T) {
	s := newTestServer(t)
	id := seedAccount(t, s, "target@example.com", "target-pass", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	rec := do(t, s, http.MethodGet, "/api/accounts/"+itoa(id), adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.AccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Account.Id != id {
		t.Errorf("id = %d, want %d", got.Account.Id, id)
	}

	// Unknown id is 404.
	if rec := do(t, s, http.MethodGet, "/api/accounts/999999", adminTok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", rec.Code)
	}
	// Non-numeric id is 400.
	if rec := do(t, s, http.MethodGet, "/api/accounts/abc", adminTok, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestUpdateAccount(t *testing.T) {
	s := newTestServer(t)
	id := seedAccount(t, s, "target@example.com", "target-pass", false, false)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Empty patch (no fields) is 400.
	if rec := do(t, s, http.MethodPatch, "/api/accounts/"+itoa(id), adminTok, api.UpdateAccountRequest{}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty patch: status = %d, want 400", rec.Code)
	}

	// Activate + promote + rename + reset secret in one call.
	body := api.UpdateAccountRequest{
		DisplayName: ptr("Renamed"),
		IsActive:    ptr(true),
		IsAdmin:     ptr(true),
		Secret:      ptr("brand-new-secret"),
	}
	rec := do(t, s, http.MethodPatch, "/api/accounts/"+itoa(id), adminTok, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.AccountResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Account.DisplayName == nil || *got.Account.DisplayName != "Renamed" {
		t.Errorf("displayName not updated: %+v", got.Account.DisplayName)
	}
	if len(got.Account.Roles) != 1 || got.Account.Roles[0] != "admin" {
		t.Errorf("roles = %v, want [admin]", got.Account.Roles)
	}
	if !got.Account.IsActive {
		t.Errorf("isActive = false, want true")
	}
	// The new secret works (and the account is now active).
	if rec := doLogin(t, s, "target@example.com", "brand-new-secret"); rec.Code != http.StatusOK {
		t.Errorf("login with reset secret: status = %d, want 200", rec.Code)
	}

	// Unknown id is 404.
	if rec := do(t, s, http.MethodPatch, "/api/accounts/999999", adminTok, api.UpdateAccountRequest{IsActive: ptr(true)}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id patch: status = %d, want 404", rec.Code)
	}
}

func TestUpdateAccountEmailConflict(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "one@example.com", "pw-one-one", false, true)
	id2 := seedAccount(t, s, "two@example.com", "pw-two-two", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass-1", true, true)
	adminTok := tokenFor(t, s, "admin@example.com", "admin-pass-1")

	// Rename account two to account one's email -> 409.
	body := api.UpdateAccountRequest{Email: (*openapi_types.Email)(ptr("one@example.com"))}
	rec := do(t, s, http.MethodPatch, "/api/accounts/"+itoa(id2), adminTok, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("email conflict: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

// itoa renders an int64 as a decimal string for path building.
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
