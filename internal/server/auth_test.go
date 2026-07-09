// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	hashed, err := s.hashSecret(secret)
	if err != nil {
		t.Fatalf("hashSecret: %v", err)
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
		// A blank secret is just a wrong secret: it must deny with the same
		// opaque 401 as the cases above, never a distinguishable 400 (issue #43).
		{"empty secret", "bob@example.com", ""},
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

// TestLoginEmptyEmail: an empty (or otherwise malformed) email field cannot be
// read as a valid address, so the request is rejected as a 400 bad_request before
// any credential check. That is a pure input-format error — identical regardless
// of account state — not the account-state-revealing 400 that issue #43 removed.
func TestLoginEmptyEmail(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte(`{"email":"","secret":"right-secret"}`)))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var got api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error.Code != codeBadRequest {
		t.Errorf("code = %q, want %q", got.Error.Code, codeBadRequest)
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

// loginToken seeds an active account (if email is new to the server it must be
// seeded by the caller) and returns a fresh bearer token for it.
func loginToken(t *testing.T, s *Server, email, secret string) string {
	t.Helper()
	rec := doLogin(t, s, email, secret)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var sess api.AuthSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("decode AuthSession: %v", err)
	}
	return sess.Token
}

// postLogout posts to the logout route with the given bearer token and raw
// request body, returning the status code. Passing the body as an io.Reader that
// is not one of httptest's length-sniffed types (*bytes.Reader and friends)
// yields an unknown-length request (ContentLength -1), as an HTTP/1.1 chunked
// body does.
func postLogout(t *testing.T, s *Server, token string, body io.Reader) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", body)
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(rec, req)
	return rec.Code
}

func TestLogoutNoBody(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "erin@example.com", "no-body-secret", false, true)
	tok := loginToken(t, s, "erin@example.com", "no-body-secret")

	// A nil body (no Content-Length, no bytes) revokes just the current session.
	if code := postLogout(t, s, tok, nil); code != http.StatusNoContent {
		t.Fatalf("logout (no body) status = %d, want 204", code)
	}
	// The session is gone: the token no longer authenticates.
	if code := postLogout(t, s, tok, nil); code != http.StatusUnauthorized {
		t.Fatalf("post-logout status = %d, want 401", code)
	}
}

// TestLogoutEmptyChunkedBody is the regression test for issue #42: an
// unknown-length (chunked) request carries ContentLength -1 even when its body
// is empty, so logout must not treat that as a malformed body. io.NopCloser
// hides the *strings.Reader from httptest's length sniffing, forcing -1.
func TestLogoutEmptyChunkedBody(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "frank@example.com", "chunked-secret", false, true)
	tok := loginToken(t, s, "frank@example.com", "chunked-secret")

	body := io.NopCloser(strings.NewReader(""))
	if code := postLogout(t, s, tok, body); code != http.StatusNoContent {
		t.Fatalf("logout (empty chunked body) status = %d, want 204", code)
	}
	if code := postLogout(t, s, tok, nil); code != http.StatusUnauthorized {
		t.Fatalf("post-logout status = %d, want 401", code)
	}
}

func TestLogoutValidBodyRevokesAll(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "grace@example.com", "all-sessions-secret", false, true)

	var tokens []string
	for range 3 {
		tokens = append(tokens, loginToken(t, s, "grace@example.com", "all-sessions-secret"))
	}

	// A valid {"allSessions": true} body still revokes every session.
	allSessions := true
	raw, _ := json.Marshal(api.LogoutRequest{AllSessions: &allSessions})
	if code := postLogout(t, s, tokens[0], bytes.NewReader(raw)); code != http.StatusNoContent {
		t.Fatalf("logout-all status = %d, want 204", code)
	}
	for i, tok := range tokens {
		if code := postLogout(t, s, tok, nil); code != http.StatusUnauthorized {
			t.Errorf("token %d still valid after logout-all: status %d, want 401", i, code)
		}
	}
}

func TestLogoutMalformedBody(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "heidi@example.com", "bad-body-secret", false, true)
	tok := loginToken(t, s, "heidi@example.com", "bad-body-secret")

	// A non-empty but malformed body is still a 400, and the session survives.
	if code := postLogout(t, s, tok, bytes.NewReader([]byte("not json"))); code != http.StatusBadRequest {
		t.Fatalf("logout (malformed body) status = %d, want 400", code)
	}
	if code := postLogout(t, s, tok, nil); code != http.StatusNoContent {
		t.Fatalf("logout after rejected malformed body status = %d, want 204", code)
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
