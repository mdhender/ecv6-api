// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	ecv6 "github.com/mdhender/ecv6-api"
	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/secret"
	"github.com/mdhender/ecv6-api/internal/server"
	"github.com/mdhender/ecv6-api/internal/store"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// TestServeMemorySeedsAdminAndLogsIn boots the in-memory serve path — a fresh
// migrated store seeded with the well-known admin — and confirms the well-known
// credentials log in with no other setup, mirroring what "ec serve --memory"
// stands up. It drives the store/server directly rather than blocking on the
// full serve loop.
func TestServeMemorySeedsAdminAndLogsIn(t *testing.T) {
	ctx := context.Background()

	db, err := store.OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// MinCost keeps bcrypt fast; the seed path is otherwise identical to serve.
	if err := seedMemoryAdmin(ctx, discardLogger(), db, secret.MinCost); err != nil {
		t.Fatalf("seedMemoryAdmin: %v", err)
	}

	srv := server.New(server.Config{Addr: ":0", SecretCost: secret.MinCost}, db, nil, ecv6.Version().String())

	body, _ := json.Marshal(api.LoginRequest{
		Email:  openapi_types.Email(MemoryAdminEmail),
		Secret: MemoryAdminSecret,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The seeded account is an active admin.
	a, err := db.GetAccountByEmail(ctx, MemoryAdminEmail)
	if err != nil {
		t.Fatalf("GetAccountByEmail: %v", err)
	}
	if !a.IsAdmin || !a.IsActive {
		t.Errorf("seeded admin: IsAdmin=%v IsActive=%v, want both true", a.IsAdmin, a.IsActive)
	}
}

// TestServeMemoryRejectsDataDir confirms --memory and --data are mutually
// exclusive: cmdServe errors before opening anything when both are set.
func TestServeMemoryRejectsDataDir(t *testing.T) {
	err := cmdServe(context.Background(), discardLogger(), "games/example", ":0", false, true, secret.MinCost)
	if err == nil {
		t.Fatal("cmdServe with both --memory and --data returned nil, want a usage error")
	}
}

// TestSeedMemoryAdminLogsCredentials confirms the seed logs the well-known email
// and secret at WARN so testers can find the login.
func TestSeedMemoryAdminLogsCredentials(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := seedMemoryAdmin(ctx, log, db, secret.MinCost); err != nil {
		t.Fatalf("seedMemoryAdmin: %v", err)
	}

	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("level=WARN")) {
		t.Errorf("seed log not at WARN: %s", out)
	}
	for _, want := range []string{MemoryAdminEmail, MemoryAdminSecret} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("seed log missing %q: %s", want, out)
		}
	}
}

// discardLogger returns a logger that drops everything.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
