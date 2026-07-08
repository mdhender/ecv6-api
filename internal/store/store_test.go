// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func TestCreateVersionVerify(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), DBName)

	if err := Create(ctx, dbPath); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := Version(ctx, dbPath)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if want := ExpectedVersion(); got != want {
		t.Errorf("Version = %d, want %d", got, want)
	}
	if err := Verify(ctx, dbPath); err != nil {
		t.Errorf("Verify: %v", err)
	}

	// Creating over an existing database must fail rather than clobber it.
	if err := Create(ctx, dbPath); !errors.Is(err, os.ErrExist) {
		t.Errorf("second Create error = %v, want os.ErrExist", err)
	}
}

func TestVersionNotFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), DBName)
	if _, err := Version(context.Background(), dbPath); !errors.Is(err, ErrNotFound) {
		t.Errorf("Version error = %v, want ErrNotFound", err)
	}
}

func TestVerifyNotEC(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), DBName)

	// A valid SQLite database that lacks our application_id is not an EC database.
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite, sqlite.OpenCreate)
	if err != nil {
		t.Fatalf("OpenConn: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "CREATE TABLE t (x);", nil); err != nil {
		t.Fatalf("create table: %v", err)
	}
	_ = conn.Close()

	if err := Verify(context.Background(), dbPath); !errors.Is(err, ErrNotEC) {
		t.Errorf("Verify error = %v, want ErrNotEC", err)
	}
}

func TestVerifyVersionMismatch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), DBName)
	if err := Create(ctx, dbPath); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Rewrite the recorded version so it no longer matches ExpectedVersion.
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite)
	if err != nil {
		t.Fatalf("OpenConn: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA user_version = 999;", nil); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	_ = conn.Close()

	if err := Verify(ctx, dbPath); !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("Verify error = %v, want ErrVersionMismatch", err)
	}
}

// TestBackup copies a database and confirms the copy is a current EC database.
func TestBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, DBName)
	if err := Create(ctx, dbPath); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dest := filepath.Join(dir, "backup.db")
	if err := Backup(ctx, dbPath, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// The copy must itself verify as a current EC database.
	if err := Verify(ctx, dest); err != nil {
		t.Errorf("Verify(backup): %v", err)
	}
	// The source must be untouched and still current.
	if err := Verify(ctx, dbPath); err != nil {
		t.Errorf("Verify(source) after backup: %v", err)
	}
}

// TestBackupDestExists confirms Backup refuses to overwrite an existing file.
func TestBackupDestExists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, DBName)
	if err := Create(ctx, dbPath); err != nil {
		t.Fatalf("Create: %v", err)
	}
	dest := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(dest, []byte("in the way"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Backup(ctx, dbPath, dest); err == nil {
		t.Errorf("Backup over existing file = nil, want error")
	}
}

// TestBackupSourceMissing confirms a missing source is reported, not created.
func TestBackupSourceMissing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, DBName)
	if err := Backup(context.Background(), dbPath, filepath.Join(dir, "backup.db")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Backup error = %v, want ErrNotFound", err)
	}
}

// TestMigrateUpApplies drives the apply path: an empty EC database at version 0
// is brought current.
func TestMigrateUpApplies(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), DBName)

	// Make an empty EC database — application_id set, no migrations applied.
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite, sqlite.OpenCreate)
	if err != nil {
		t.Fatalf("OpenConn: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, fmt.Sprintf("PRAGMA application_id = %d;", appID), nil); err != nil {
		t.Fatalf("set application_id: %v", err)
	}
	_ = conn.Close()

	if v, err := Version(ctx, dbPath); err != nil || v != 0 {
		t.Fatalf("setup version = %d (err %v), want 0", v, err)
	}
	if err := MigrateUp(ctx, dbPath); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	if v, err := Version(ctx, dbPath); err != nil || v != ExpectedVersion() {
		t.Errorf("after MigrateUp version = %d (err %v), want %d", v, err, ExpectedVersion())
	}
}

// TestMigrateUpIdempotent: running MigrateUp on an already-current database is a
// successful no-op.
func TestMigrateUpIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), DBName)
	if err := Create(ctx, dbPath); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := MigrateUp(ctx, dbPath); err != nil {
		t.Errorf("MigrateUp on current database: %v", err)
	}
}

func TestMigrateUpNotFound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), DBName)
	if err := MigrateUp(context.Background(), dbPath); !errors.Is(err, ErrNotFound) {
		t.Errorf("MigrateUp error = %v, want ErrNotFound", err)
	}
}

func TestMigrateUpVersionAhead(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), DBName)
	if err := Create(ctx, dbPath); err != nil {
		t.Fatalf("Create: %v", err)
	}
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite)
	if err != nil {
		t.Fatalf("OpenConn: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, "PRAGMA user_version = 999;", nil); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	_ = conn.Close()

	if err := MigrateUp(ctx, dbPath); !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("MigrateUp error = %v, want ErrVersionMismatch", err)
	}
}
