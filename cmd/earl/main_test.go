// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdhender/ecv6-api/internal/secret"
	"github.com/mdhender/ecv6-api/internal/server"
	"github.com/mdhender/ecv6-api/internal/store"
)

const (
	testAdminEmail  = "penny@example.com"
	testAdminSecret = "happy.cat.happy.nap"
)

// newTestAPI stands up a real API server backed by a throwaway in-memory store,
// seeds an active admin account, and returns its base URL. This exercises earl
// against the actual server, not a mock.
func newTestAPI(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	db, err := store.OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hashed, err := secret.Hash(testAdminSecret, secret.MinCost)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}
	if _, err := db.CreateAccount(ctx, store.Account{
		Email:        testAdminEmail,
		DisplayName:  "Penny",
		HashedSecret: hashed,
		IsAdmin:      true,
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	srv, err := server.New(server.Config{SecretCost: secret.MinCost}, db, nil, "test")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts.URL + server.BasePath
}

// newTestEarl builds an earl pointed at baseURL as the given email, capturing
// stdout/stderr in the returned buffers, with a temp token file.
func newTestEarl(t *testing.T, baseURL, email string) (*earl, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("EARL_TOKENS", filepath.Join(t.TempDir(), "tokens.json"))
	var out, errOut bytes.Buffer
	e := &earl{
		baseURL: strings.TrimRight(baseURL, "/"),
		email:   email,
		log:     slog.New(slog.DiscardHandler),
		http:    http.DefaultClient,
		out:     &out,
		errOut:  &errOut,
	}
	return e, &out, &errOut
}

func TestEarlPublicRequest(t *testing.T) {
	base := newTestAPI(t)
	e, out, _ := newTestEarl(t, base, "")

	// healthz is public: it succeeds with no saved token.
	if err := e.request(context.Background(), http.MethodGet, "/healthz", nil, false); err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	if !strings.Contains(out.String(), "version") {
		t.Errorf("healthz body = %q, want it to mention version", out.String())
	}
}

func TestEarlLoginWhoamiLogout(t *testing.T) {
	base := newTestAPI(t)
	ctx := context.Background()
	e, _, _ := newTestEarl(t, base, testAdminEmail)

	// login saves a token.
	if err := e.login(ctx, testAdminSecret); err != nil {
		t.Fatalf("login: %v", err)
	}
	store, err := e.loadTokens()
	if err != nil {
		t.Fatalf("loadTokens: %v", err)
	}
	if _, tok := store.resolve(e.baseURL, testAdminEmail); tok == "" {
		t.Fatalf("no token saved after login")
	}

	// whoami (GET /me) uses the saved token and returns penny's account.
	if err := e.request(ctx, http.MethodGet, "/me", nil, false); err != nil {
		t.Fatalf("get /me: %v", err)
	}
	if !strings.Contains(e.out.(*bytes.Buffer).String(), testAdminEmail) {
		t.Errorf("/me body = %q, want it to contain %q", e.out, testAdminEmail)
	}

	// logout revokes and forgets the token.
	if err := e.logout(ctx, false); err != nil {
		t.Fatalf("logout: %v", err)
	}
	store, _ = e.loadTokens()
	if _, tok := store.resolve(e.baseURL, testAdminEmail); tok != "" {
		t.Errorf("token still present after logout")
	}
}

func TestEarlAdminCreateAccount(t *testing.T) {
	base := newTestAPI(t)
	ctx := context.Background()
	e, out, _ := newTestEarl(t, base, testAdminEmail)
	if err := e.login(ctx, testAdminSecret); err != nil {
		t.Fatalf("login: %v", err)
	}

	body := []byte(`{"email":"tester@example.com","secret":"hunter2hunter2","isActive":true,"isAdmin":false}`)
	if err := e.request(ctx, http.MethodPost, "/accounts", body, false); err != nil {
		t.Fatalf("post /accounts: %v", err)
	}
	if !strings.Contains(out.String(), "tester@example.com") {
		t.Errorf("create account response = %q, want it to contain the new email", out.String())
	}
}

func TestEarlImpersonate(t *testing.T) {
	base := newTestAPI(t)
	ctx := context.Background()
	e, out, _ := newTestEarl(t, base, testAdminEmail)
	if err := e.login(ctx, testAdminSecret); err != nil {
		t.Fatalf("login: %v", err)
	}

	// Create an active, non-admin account to impersonate.
	body := []byte(`{"email":"tester@example.com","secret":"hunter2hunter2","isActive":true,"isAdmin":false}`)
	if err := e.request(ctx, http.MethodPost, "/accounts", body, false); err != nil {
		t.Fatalf("post /accounts: %v", err)
	}
	var created struct {
		Account struct {
			ID int64 `json:"id"`
		} `json:"account"`
	}
	if err := json.Unmarshal(out.Bytes(), &created); err != nil {
		t.Fatalf("parse create response %q: %v", out.String(), err)
	}
	if created.Account.ID == 0 {
		t.Fatalf("no account id in create response %q", out.String())
	}

	// Impersonate that account; the token is saved under its email.
	if err := e.impersonate(ctx, created.Account.ID); err != nil {
		t.Fatalf("impersonate: %v", err)
	}
	store, err := e.loadTokens()
	if err != nil {
		t.Fatalf("loadTokens: %v", err)
	}
	if _, tok := store.resolve(e.baseURL, "tester@example.com"); tok == "" {
		t.Fatalf("no impersonation token saved for tester@example.com")
	}

	// Acting as the impersonated user (same token file, selected by --email),
	// /me returns the tester's account.
	var subOut bytes.Buffer
	sub := &earl{baseURL: e.baseURL, email: "tester@example.com", log: e.log, http: e.http, out: &subOut, errOut: &bytes.Buffer{}}
	if err := sub.request(ctx, http.MethodGet, "/me", nil, false); err != nil {
		t.Fatalf("get /me as impersonated user: %v", err)
	}
	if !strings.Contains(subOut.String(), "tester@example.com") {
		t.Errorf("/me as tester = %q, want it to contain tester@example.com", subOut.String())
	}
}

func TestEarlUnauthorizedIsError(t *testing.T) {
	base := newTestAPI(t)
	// An account with no saved token: the request goes out unauthenticated and
	// the protected route returns 401, which earl surfaces as an error.
	e, _, _ := newTestEarl(t, base, "nobody@example.com")
	err := e.request(context.Background(), http.MethodGet, "/me", nil, false)
	if err == nil {
		t.Fatal("expected an error for unauthenticated /me")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want it to mention 401", err)
	}
}
