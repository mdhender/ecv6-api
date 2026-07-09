// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
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
	s, err := New(Config{Addr: ":0", SecretCost: secret.MinCost}, db, nil, "9.9.9-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestNewDefaultsSecretCost confirms a zero SecretCost resolves to the secure
// production default, and that the decoy hash is computed at that same cost so
// unknown-account login timing matches a real check.
func TestNewDefaultsSecretCost(t *testing.T) {
	s, err := New(Config{Addr: ":0"}, nil, nil, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.secretCost != secret.DefaultCost {
		t.Errorf("secretCost = %d, want DefaultCost %d", s.secretCost, secret.DefaultCost)
	}
	if !secret.Verify(s.decoySecretHash, loginDecoySecret) {
		t.Errorf("decoy hash was not computed for the resolved cost")
	}
}

// TestNewRejectsUnhashableCost confirms New surfaces the bcrypt error rather than
// building a Server with an empty decoy hash when the secret cost is above
// secret.MaxCost — the failure that would otherwise silently reopen the
// login-timing side channel.
func TestNewRejectsUnhashableCost(t *testing.T) {
	s, err := New(Config{Addr: ":0", SecretCost: secret.MaxCost + 1}, nil, nil, "test")
	if err == nil {
		t.Fatal("New with cost above MaxCost returned nil error, want a hash failure")
	}
	if s != nil {
		t.Errorf("New returned a non-nil Server on error: %+v", s)
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

// getVersion issues GET /api/version against srv and returns the recorder and the
// decoded body.
func getVersion(t *testing.T, srv *Server) (*httptest.ResponseRecorder, api.VersionResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	srv.Handler().ServeHTTP(rec, req)
	var got api.VersionResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return rec, got
}

// TestVersionReadsSchemaAtMostOnce confirms /version reads the schema version from
// the database exactly once and serves the cached value on later requests, so an
// anonymous caller cannot make each request consume a connection-pool slot (issue
// #45). The reader is counted via the readSchemaVersion seam.
func TestVersionReadsSchemaAtMostOnce(t *testing.T) {
	srv := newTestServer(t)
	var reads int
	real := srv.readSchemaVersion
	srv.readSchemaVersion = func(ctx context.Context) (int, error) {
		reads++
		return real(ctx)
	}

	var want int32
	for i := 0; i < 3; i++ {
		rec, got := getVersion(t, srv)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200; body=%s", i, rec.Code, rec.Body.String())
		}
		if i == 0 {
			want = got.Database.SchemaVersion
		} else if got.Database.SchemaVersion != want {
			t.Errorf("call %d: schemaVersion = %d, want %d", i, got.Database.SchemaVersion, want)
		}
	}
	if reads != 1 {
		t.Errorf("schema version read %d times, want exactly 1", reads)
	}
}

// TestVersionDoesNotCacheFailure confirms a failed schema read is not cached: the
// first (failing) call returns 500 and a later call retries the read and succeeds,
// so a transient database error does not poison /version for the process lifetime.
func TestVersionDoesNotCacheFailure(t *testing.T) {
	srv := newTestServer(t)
	real := srv.readSchemaVersion
	fail := true
	srv.readSchemaVersion = func(ctx context.Context) (int, error) {
		if fail {
			return 0, errors.New("transient db failure")
		}
		return real(ctx)
	}

	if rec, _ := getVersion(t, srv); rec.Code != http.StatusInternalServerError {
		t.Fatalf("first call status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}

	fail = false
	rec, got := getVersion(t, srv)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if want := int32(store.ExpectedVersion()); got.Database.SchemaVersion != want {
		t.Errorf("schemaVersion = %d, want %d", got.Database.SchemaVersion, want)
	}
}

// TestVersionRejectsOutOfRangeSchema confirms /version returns 500 rather than a
// wrapped negative SchemaVersion when the schema version cannot fit the int32 wire
// field. The value is injected via the readSchemaVersion seam (issue #46).
func TestVersionRejectsOutOfRangeSchema(t *testing.T) {
	srv := newTestServer(t)
	srv.readSchemaVersion = func(ctx context.Context) (int, error) {
		return math.MaxInt32 + 1, nil
	}

	rec, _ := getVersion(t, srv)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
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

// TestInboundRequestIDRejected confirms an unacceptable inbound X-Request-Id —
// over-length or carrying a disallowed character (whitespace, CR/LF, angle
// brackets, control bytes) — is ignored and replaced by a freshly minted id, so a
// client cannot bloat log lines or inject bytes into the reflected header.
func TestInboundRequestIDRejected(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"over length", strings.Repeat("a", maxRequestIDLen+1)},
		{"newline", "abc\ndef"},
		{"carriage return", "abc\rdef"},
		{"space", "abc def"},
		{"angle bracket", "<script>"},
		{"control byte", "abc\x00def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
			req.Header.Set(requestIDHeader, tt.id)
			srv.Handler().ServeHTTP(rec, req)

			got := rec.Header().Get(requestIDHeader)
			if got == tt.id {
				t.Fatalf("request id = %q, want a freshly minted id", got)
			}
			if !validRequestID(got) {
				t.Errorf("minted request id = %q is not a valid id", got)
			}
		})
	}
}

// TestRequestIDMintedWhenAbsent confirms a request with no inbound X-Request-Id
// gets a freshly minted, valid id echoed back in the response header.
func TestRequestIDMintedWhenAbsent(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	srv.Handler().ServeHTTP(rec, req)

	got := rec.Header().Get(requestIDHeader)
	if got == "" {
		t.Fatalf("missing %s response header", requestIDHeader)
	}
	if !validRequestID(got) {
		t.Errorf("minted request id = %q is not a valid id", got)
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

// TestRecoveryPassesThroughErrAbortHandler confirms withRecovery re-panics the
// http.ErrAbortHandler sentinel — net/http uses it to abort a connection
// silently — rather than converting it into a logged 500 envelope.
func TestRecoveryPassesThroughErrAbortHandler(t *testing.T) {
	abortHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})
	h := chain(abortHandler, withRequestID, withLogging(discardLogger()), withRecovery)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/abort", nil)

	defer func() {
		v := recover()
		if v == nil {
			t.Fatalf("ErrAbortHandler was swallowed; want it to propagate")
		}
		if v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
		}
		// The sentinel must not be turned into a 500 envelope.
		if rec.Code == http.StatusInternalServerError {
			t.Errorf("status = 500, want the connection aborted without an envelope")
		}
	}()
	h.ServeHTTP(rec, req)
	t.Fatal("ServeHTTP returned without propagating the panic")
}
