// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdhender/ecv6-api/internal/api"
)

// postLogin posts a raw body to the login route and returns the recorder, so a
// test can exercise the JSON decoder with payloads json.Marshal would never
// produce (unknown fields, trailing data).
func postLogin(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte(body)))
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// assertBadRequest fails unless rec is a 400 carrying the bad_request code.
func assertBadRequest(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
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

// TestDecodeJSONRejectsUnknownField is the regression for issue #44: a body with
// a field the request struct does not define (e.g. a misspelling) must be a 400
// bad_request, not a silently ignored no-op.
func TestDecodeJSONRejectsUnknownField(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "ivan@example.com", "unknown-field-secret", false, true)
	rec := postLogin(t, s, `{"email":"ivan@example.com","secret":"unknown-field-secret","bogus":true}`)
	assertBadRequest(t, rec)
}

// TestDecodeJSONRejectsTrailingData is the second half of issue #44: bytes after
// the JSON value (`{...}{junk}`) must be rejected, not accepted.
func TestDecodeJSONRejectsTrailingData(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "judy@example.com", "trailing-secret", false, true)
	rec := postLogin(t, s, `{"email":"judy@example.com","secret":"trailing-secret"}{junk}`)
	assertBadRequest(t, rec)
}

// TestDecodeJSONAcceptsExactFields confirms the strict decoder still accepts a
// well-formed body carrying exactly the fields the request struct defines.
func TestDecodeJSONAcceptsExactFields(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "mallory@example.com", "exact-fields-secret", false, true)
	rec := postLogin(t, s, `{"email":"mallory@example.com","secret":"exact-fields-secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestDecodeOptionalJSONAcceptsEmptyBody guards the decodeOptionalJSON path: the
// strict decoder must still treat an empty logout body as success (204), not
// trip the new trailing-data check.
func TestDecodeOptionalJSONAcceptsEmptyBody(t *testing.T) {
	s := newTestServer(t)
	seedAccount(t, s, "niaj@example.com", "empty-body-secret", false, true)
	tok := loginToken(t, s, "niaj@example.com", "empty-body-secret")
	if code := postLogout(t, s, tok, nil); code != http.StatusNoContent {
		t.Fatalf("logout (empty body) status = %d, want 204", code)
	}
}
