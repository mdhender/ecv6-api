// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
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
