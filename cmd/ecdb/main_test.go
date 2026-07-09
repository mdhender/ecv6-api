// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdhender/ecv6-api/internal/cli"
	"github.com/mdhender/ecv6-api/internal/secret"
	"github.com/mdhender/ecv6-api/internal/store"
)

// newTestDB creates a fresh, migrated database in a temp folder and returns the
// folder path (the argument admin create expects).
func newTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := store.Create(context.Background(), filepath.Join(dir, store.DBName)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	return dir
}

func TestAdminCreate(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := newTestDB(t)

	if err := cmdAdminCreate(ctx, log, []string{dir}, "s3cret", "admin@ecv6.example.com", "admin"); err != nil {
		t.Fatalf("cmdAdminCreate: %v", err)
	}

	db, err := store.OpenPersistent(ctx, log, dir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	a, err := db.GetAccountByEmail(ctx, "admin@ecv6.example.com")
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if !a.IsAdmin {
		t.Errorf("account is not admin")
	}
	if !a.IsActive {
		t.Errorf("account is not active")
	}
	if a.DisplayName != "admin" {
		t.Errorf("display name = %q, want %q", a.DisplayName, "admin")
	}
	if !secret.Verify(a.HashedSecret, "s3cret") {
		t.Errorf("stored hash does not verify against the given secret")
	}
	if secret.Verify(a.HashedSecret, "wrong") {
		t.Errorf("stored hash verifies against the wrong secret")
	}
}

// TestAdminCreateDefaults confirms the built-in defaults land when only the
// required secret is supplied (as they would from the flag defaults).
func TestAdminCreateDefaults(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := newTestDB(t)

	if err := cmdAdminCreate(ctx, log, []string{dir}, "s3cret", "admin@ecv6.example.com", "admin"); err != nil {
		t.Fatalf("cmdAdminCreate: %v", err)
	}
	db, err := store.OpenPersistent(ctx, log, dir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if _, err := db.GetAccountByEmail(ctx, "admin@ecv6.example.com"); err != nil {
		t.Errorf("default admin email not found: %v", err)
	}
}

// TestAdminCreateLowercasesEmail confirms a mixed-case email is stored lowercased.
func TestAdminCreateLowercasesEmail(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := newTestDB(t)

	if err := cmdAdminCreate(ctx, log, []string{dir}, "s3cret", "Admin@EC.Example.COM", "Boss"); err != nil {
		t.Fatalf("cmdAdminCreate: %v", err)
	}
	db, err := store.OpenPersistent(ctx, log, dir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	a, err := db.GetAccountByEmail(ctx, "admin@ec.example.com")
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if a.Email != "admin@ec.example.com" {
		t.Errorf("stored email = %q, want lowercased %q", a.Email, "admin@ec.example.com")
	}
}

// TestAdminCreateSecretRequired confirms an empty secret (neither --secret nor
// ECDB_SECRET set) is rejected before any database work.
func TestAdminCreateSecretRequired(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := newTestDB(t)

	err := cmdAdminCreate(ctx, log, []string{dir}, "", "admin@ecv6.example.com", "admin")
	if err == nil {
		t.Fatal("cmdAdminCreate accepted an empty secret")
	}
}

// TestAdminCreateEnvSecret confirms the secret resolves from ECDB_SECRET when
// --secret is omitted, exercising ff's env-var binding through the full parse.
func TestAdminCreateEnvSecret(t *testing.T) {
	ctx := context.Background()
	dir := newTestDB(t)
	t.Setenv("ECDB_SECRET", "envpass")

	cmd, _ := newRootCommand()
	if code := cli.Run(ctx, cmd, "ECDB", []string{"admin", "create", dir}, nil); code != 0 {
		t.Fatalf("cli.Run exit code = %d, want 0", code)
	}

	db, err := store.OpenPersistent(ctx, slog.New(slog.DiscardHandler), dir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	a, err := db.GetAccountByEmail(ctx, "admin@ecv6.example.com")
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if !secret.Verify(a.HashedSecret, "envpass") {
		t.Errorf("account secret did not come from ECDB_SECRET")
	}
}

// TestAdminCreateDuplicateEmail confirms a second account with the same email is
// rejected as a conflict.
func TestAdminCreateDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := newTestDB(t)

	if err := cmdAdminCreate(ctx, log, []string{dir}, "s3cret", "admin@ecv6.example.com", "admin"); err != nil {
		t.Fatalf("first cmdAdminCreate: %v", err)
	}
	err := cmdAdminCreate(ctx, log, []string{dir}, "another", "admin@ecv6.example.com", "admin")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate email error = %v, want ErrConflict", err)
	}
}

// TestAdminCreateMissingDatabase confirms admin create refuses to run against a
// folder with no database rather than creating one.
func TestAdminCreateMissingDatabase(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := t.TempDir() // no ec.db created

	err := cmdAdminCreate(ctx, log, []string{dir}, "s3cret", "admin@ecv6.example.com", "admin")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing database error = %v, want ErrNotFound", err)
	}
}

// TestAdminCreateArgCount confirms the single-PATH-argument contract.
func TestAdminCreateArgCount(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	if err := cmdAdminCreate(ctx, log, nil, "s3cret", "admin@ecv6.example.com", "admin"); err == nil {
		t.Error("cmdAdminCreate accepted zero PATH arguments")
	}
	if err := cmdAdminCreate(ctx, log, []string{"a", "b"}, "s3cret", "admin@ecv6.example.com", "admin"); err == nil {
		t.Error("cmdAdminCreate accepted two PATH arguments")
	}
}

func TestBackupName(t *testing.T) {
	ts := time.Date(2026, 7, 8, 19, 3, 45, 0, time.UTC)
	tests := []struct {
		name         string
		version      int
		versionStamp bool
		want         string
	}{
		{"plain", 1, false, "ec.db.20260708T190345Z"},
		{"stamped", 1, true, "ec.db.20260708T190345Z-1"},
		{"stamped multi-digit", 12, true, "ec.db.20260708T190345Z-12"},
		{"version ignored when not stamping", 12, false, "ec.db.20260708T190345Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backupName(ts, tt.version, tt.versionStamp); got != tt.want {
				t.Errorf("backupName(%v, %d, %v) = %q, want %q", ts, tt.version, tt.versionStamp, got, tt.want)
			}
		})
	}
}

// TestBackupNameUsesUTC confirms a non-UTC input is normalized to UTC in the name.
func TestBackupNameUsesUTC(t *testing.T) {
	// 14:03:45 in a UTC-5 zone is 19:03:45 UTC.
	zone := time.FixedZone("UTC-5", -5*60*60)
	ts := time.Date(2026, 7, 8, 14, 3, 45, 0, zone)
	if got, want := backupName(ts, 1, false), "ec.db.20260708T190345Z"; got != want {
		t.Errorf("backupName = %q, want %q (UTC-normalized)", got, want)
	}
}
