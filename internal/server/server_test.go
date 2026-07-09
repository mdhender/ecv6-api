// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/secret"
	"github.com/mdhender/ecv6-api/internal/store"
)

// discardLogger returns a logger that drops everything, for tests that exercise
// logging middleware without cluttering output.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// newTestServer builds a Server backed by a throwaway in-memory store. The store
// is closed when the test finishes.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// MinCost keeps bcrypt hashing fast across the suite; production defaults to
	// secret.DefaultCost via New.
	return New(Config{Addr: ":0", SecretCost: secret.MinCost}, db, nil, "9.9.9-test")
}

// TestNewDefaultsSecretCost confirms a zero SecretCost resolves to the secure
// production default, and that the decoy hash is computed at that same cost so
// unknown-account login timing matches a real check.
func TestNewDefaultsSecretCost(t *testing.T) {
	s := New(Config{Addr: ":0"}, nil, nil, "test")
	if s.secretCost != secret.DefaultCost {
		t.Errorf("secretCost = %d, want DefaultCost %d", s.secretCost, secret.DefaultCost)
	}
	if !secret.Verify(s.decoySecretHash, loginDecoySecret) {
		t.Errorf("decoy hash was not computed for the resolved cost")
	}
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if id := rec.Header().Get(requestIDHeader); id == "" {
		t.Errorf("missing %s response header", requestIDHeader)
	}
	var got api.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status field = %q, want ok", got.Status)
	}
	if got.Version != "9.9.9-test" {
		t.Errorf("version field = %q, want 9.9.9-test", got.Version)
	}
}

func TestVersion(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got api.VersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Application != "9.9.9-test" {
		t.Errorf("application = %q, want 9.9.9-test", got.Application)
	}
	if want := int32(store.ExpectedVersion()); got.Database.SchemaVersion != want {
		t.Errorf("schemaVersion = %d, want %d", got.Database.SchemaVersion, want)
	}
}

func TestNotFoundEnvelope(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var got api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error.Code != codeNotFound {
		t.Errorf("code = %q, want %q", got.Error.Code, codeNotFound)
	}
	// The error envelope carries the correlation id, matching the response header.
	if got.Error.RequestId == nil || *got.Error.RequestId == "" {
		t.Fatalf("envelope missing requestId")
	}
	if hdr := rec.Header().Get(requestIDHeader); hdr != *got.Error.RequestId {
		t.Errorf("envelope requestId %q != header %q", *got.Error.RequestId, hdr)
	}
}

// TestWrongMethod confirms a defined path with an undefined method falls through
// to the JSON 404 rather than net/http's plain-text 405.
func TestWrongMethod(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/healthz", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInboundRequestIDPreserved(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	req.Header.Set(requestIDHeader, "client-supplied-id")
	srv.Handler().ServeHTTP(rec, req)

	if got := rec.Header().Get(requestIDHeader); got != "client-supplied-id" {
		t.Errorf("request id = %q, want client-supplied-id", got)
	}
}

// TestRecovery confirms a panicking handler yields a 500 error envelope rather
// than crashing the server.
func TestRecovery(t *testing.T) {
	panicHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	h := chain(panicHandler, withRequestID, withLogging(discardLogger()), withRecovery)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/boom", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var got api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error.Code != codeInternal {
		t.Errorf("code = %q, want %q", got.Error.Code, codeInternal)
	}
}
