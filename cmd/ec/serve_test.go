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

	srv, err := server.New(server.Config{Addr: ":0", SecretCost: secret.MinCost}, db, nil, ecv6.Version().String())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

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

// TestResolveServeStore covers the five allowed --memory / --data combinations
// resolveServeStore encodes, including the fix for #48: an explicit --memory
// overrides a data dir that came only from the ambient EC_DATA (dataFromCLI
// false), while an explicit command-line --data still conflicts.
func TestResolveServeStore(t *testing.T) {
	for _, tc := range []struct {
		name        string
		memory      bool
		dataDir     string
		dataFromCLI bool
		wantMemory  bool
		wantDir     string
		wantErr     bool
	}{
		{name: "memory with explicit --data conflicts", memory: true, dataDir: "games/example", dataFromCLI: true, wantErr: true},
		{name: "memory overrides env-sourced data dir", memory: true, dataDir: "games/example", dataFromCLI: false, wantMemory: true, wantDir: ""},
		{name: "memory with no data dir", memory: true, dataDir: "", dataFromCLI: false, wantMemory: true, wantDir: ""},
		{name: "persistent with data dir (flag)", memory: false, dataDir: "games/example", dataFromCLI: true, wantMemory: false, wantDir: "games/example"},
		{name: "persistent with data dir (env)", memory: false, dataDir: "games/example", dataFromCLI: false, wantMemory: false, wantDir: "games/example"},
		{name: "no memory and no data dir errors", memory: false, dataDir: "", dataFromCLI: false, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMemory, dir, err := resolveServeStore(tc.memory, tc.dataDir, tc.dataFromCLI)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveServeStore(%v, %q, %v) = (%v, %q, nil), want an error", tc.memory, tc.dataDir, tc.dataFromCLI, useMemory, dir)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveServeStore(%v, %q, %v) unexpected error: %v", tc.memory, tc.dataDir, tc.dataFromCLI, err)
			}
			if useMemory != tc.wantMemory || dir != tc.wantDir {
				t.Fatalf("resolveServeStore(%v, %q, %v) = (%v, %q), want (%v, %q)", tc.memory, tc.dataDir, tc.dataFromCLI, useMemory, dir, tc.wantMemory, tc.wantDir)
			}
		})
	}
}

// TestDataFlagOnCLI checks the command-line --data detector recognizes the
// space-separated and attached forms (long and short) and reports false when
// --data is absent, so an env-sourced EC_DATA is not mistaken for an explicit flag.
func TestDataFlagOnCLI(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "long space-separated", args: []string{"serve", "--data", "games/example"}, want: true},
		{name: "long attached", args: []string{"serve", "--data=games/example"}, want: true},
		{name: "short space-separated", args: []string{"serve", "-data", "games/example"}, want: true},
		{name: "short attached", args: []string{"serve", "-data=games/example"}, want: true},
		{name: "memory only", args: []string{"serve", "--memory"}, want: false},
		{name: "no data flag", args: []string{"serve", "--listen", ":8080"}, want: false},
		{name: "nil args", args: nil, want: false},
		{name: "stops at bare dashdash", args: []string{"serve", "--", "--data", "x"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dataFlagOnCLI(tc.args); got != tc.want {
				t.Fatalf("dataFlagOnCLI(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestServeRejectsOutOfRangeSecretCost confirms cmdServe fails fast on a
// --secret-cost outside the valid bcrypt range [secret.MinCost, secret.MaxCost],
// before opening any store. A cost bcrypt would reject must not reach server.New,
// where it would otherwise blank the login-timing decoy hash.
func TestServeRejectsOutOfRangeSecretCost(t *testing.T) {
	for _, cost := range []int{secret.MinCost - 1, secret.MaxCost + 1} {
		err := cmdServe(context.Background(), discardLogger(), "", ":0", false, true, cost)
		if err == nil {
			t.Errorf("cmdServe with --secret-cost %d returned nil, want an out-of-range error", cost)
		}
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
