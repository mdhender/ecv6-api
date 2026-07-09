// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// seedAccount inserts an account with the given plaintext secret and returns its
// id, hashing the secret exactly as the login path expects.
func seedAccount(t *testing.T, s *Server, email, secret string, admin, active bool) int64 {
	t.Helper()
	hashed, err := HashSecret(secret)
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}
	id, err := s.db.CreateAccount(context.Background(), store.Account{
		Email:        email,
		DisplayName:  "Test",
		HashedSecret: hashed,
		IsAdmin:      admin,
		IsActive:     active,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return id
}

// doLogin posts credentials and returns the recorder.
func doLogin(t *testing.T, s *Server, email, secret string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(api.LoginRequest{Email: openapi_types.Email(email), Secret: secret})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// doLogout posts to the logout route with the given bearer token (omitted when
// empty) and returns the status code. allSessions sets the request body flag.
func doLogout(t *testing.T, s *Server, token string, allSessions bool) int {
	t.Helper()
	var body []byte
	if allSessions {
		body, _ = json.Marshal(api.LogoutRequest{AllSessions: &allSessions})
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(rec, req)
	return rec.Code
}

// nowish returns the current time, for future-time assertions.
func nowish() time.Time { return time.Now() }

func TestTokenHashStable(t *testing.T) {
	tok, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	if tok == "" {
		t.Fatal("newToken returned empty token")
	}
	if hashToken(tok) != hashToken(tok) {
		t.Errorf("hashToken is not stable for the same token")
	}
	tok2, _ := newToken()
	if hashToken(tok) == hashToken(tok2) {
		t.Errorf("distinct tokens hashed to the same value")
	}
}

func TestLoginLogoutFlow(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "alice@example.com", "s3cret-secret", false, true)

	// Login succeeds and returns a bearer token.
	rec := doLogin(t, s, "alice@example.com", "s3cret-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var sess api.AuthSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode AuthSession: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("login returned an empty token")
	}
	if sess.TokenType != api.AuthSessionTokenTypeBearer {
		t.Errorf("tokenType = %q, want Bearer", sess.TokenType)
	}
	if !sess.ExpiresAt.After(nowish()) {
		t.Errorf("expiresAt %v is not in the future", sess.ExpiresAt)
	}

	// The token authenticates a protected route (logout is on the authed group).
	if code := doLogout(t, s, sess.Token, false); code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", code)
	}

	// After logout the token is rejected.
	if code := doLogout(t, s, sess.Token, false); code != http.StatusUnauthorized {
		t.Fatalf("post-logout logout status = %d, want 401", code)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "bob@example.com", "right-secret", false, true)
	seedAccount(t, s, "gone@example.com", "right-secret", false, false) // inactive

	cases := []struct {
		name, email, secret string
	}{
		{"wrong secret", "bob@example.com", "wrong-secret"},
		{"unknown email", "nobody@example.com", "right-secret"},
		{"inactive account", "gone@example.com", "right-secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doLogin(t, s, tc.email, tc.secret)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
			}
			var got api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Error.Code != codeUnauthorized {
				t.Errorf("code = %q, want %q", got.Error.Code, codeUnauthorized)
			}
		})
	}
}

func TestLoginBadBody(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte("not json")))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRequireAuthRejects(t *testing.T) {
	s := newTestServer(t)

	// No Authorization header.
	if code := doLogout(t, s, "", false); code != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want 401", code)
	}
	// A syntactically valid but unknown token.
	if code := doLogout(t, s, "not-a-real-token", false); code != http.StatusUnauthorized {
		t.Errorf("bogus token status = %d, want 401", code)
	}
}

func TestRequireAuthRejectsDeactivatedAccount(t *testing.T) {
	s := newTestServer(t)
	id := seedAccount(t, s, "carol@example.com", "pw-pw-pw", false, true)

	rec := doLogin(t, s, "carol@example.com", "pw-pw-pw")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", rec.Code)
	}
	var sess api.AuthSession
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)

	// Deactivate the account; the still-unexpired session must stop working.
	acct, err := s.db.GetAccount(context.Background(), id)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	acct.IsActive = false
	if err := s.db.UpdateAccount(context.Background(), acct); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if code := doLogout(t, s, sess.Token, false); code != http.StatusUnauthorized {
		t.Errorf("deactivated-account status = %d, want 401", code)
	}
}

func TestLogoutAllSessions(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "dave@example.com", "many-sessions", false, true)

	var tokens []string
	for range 3 {
		rec := doLogin(t, s, "dave@example.com", "many-sessions")
		if rec.Code != http.StatusOK {
			t.Fatalf("login status = %d, want 200", rec.Code)
		}
		var sess api.AuthSession
		_ = json.Unmarshal(rec.Body.Bytes(), &sess)
		tokens = append(tokens, sess.Token)
	}

	// Logout with allSessions using one token revokes all of them.
	if code := doLogout(t, s, tokens[0], true); code != http.StatusNoContent {
		t.Fatalf("logout-all status = %d, want 204", code)
	}
	for i, tok := range tokens {
		if code := doLogout(t, s, tok, false); code != http.StatusUnauthorized {
			t.Errorf("token %d still valid after logout-all: status %d, want 401", i, code)
		}
	}
}

func TestRequireAdmin(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "user@example.com", "user-pass", false, true)
	seedAccount(t, s, "admin@example.com", "admin-pass", true, true)

	// A protected admin handler for the test.
	handler := chain(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		withRequestID, s.requireAuth, s.requireAdmin,
	)

	call := func(email, secret string) int {
		rec := doLogin(t, s, email, secret)
		var sess api.AuthSession
		_ = json.Unmarshal(rec.Body.Bytes(), &sess)
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/thing", nil)
		req.Header.Set("Authorization", "Bearer "+sess.Token)
		handler.ServeHTTP(r, req)
		return r.Code
	}

	if code := call("admin@example.com", "admin-pass"); code != http.StatusOK {
		t.Errorf("admin call status = %d, want 200", code)
	}
	if code := call("user@example.com", "user-pass"); code != http.StatusForbidden {
		t.Errorf("non-admin call status = %d, want 403", code)
	}
}
