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

func TestOpenTemporary(t *testing.T) {
	ctx := context.Background()
	db, err := OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary: %v", err)
	}
	defer db.Close()

	conn, err := db.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer db.Put(conn)

	// All migrations were applied.
	if got := pragma(t, conn, "user_version"); got != ExpectedVersion() {
		t.Errorf("user_version = %d, want %d", got, ExpectedVersion())
	}
	// Foreign keys are enforced on pooled connections.
	if got := pragma(t, conn, "foreign_keys"); got != 1 {
		t.Errorf("foreign_keys = %d, want 1", got)
	}
	// The migrated schema is present.
	err = sqlitex.ExecuteTransient(conn, "INSERT INTO accounts (email, hashed_secret) VALUES ('a@b.c', 'x');", nil)
	if err != nil {
		t.Errorf("insert into accounts: %v", err)
	}
	// Foreign keys actually bite: a membership for a missing game is rejected.
	err = sqlitex.ExecuteTransient(conn, "INSERT INTO game_account_role (game_id, player_id, account_id) VALUES (999, 1, 1);", nil)
	if err == nil {
		t.Error("insert with dangling game_id succeeded; foreign keys not enforced")
	}
}

// TestOpenTemporaryIsolation confirms two temporary stores share nothing, which
// is what makes them safe for parallel tests.
func TestOpenTemporaryIsolation(t *testing.T) {
	ctx := context.Background()
	a, err := OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary a: %v", err)
	}
	defer a.Close()
	b, err := OpenTemporary(ctx, nil, false)
	if err != nil {
		t.Fatalf("OpenTemporary b: %v", err)
	}
	defer b.Close()

	ca, _ := a.Get(ctx)
	defer a.Put(ca)
	if err := sqlitex.ExecuteTransient(ca, "INSERT INTO accounts (email, hashed_secret) VALUES ('only@a.c', 'x');", nil); err != nil {
		t.Fatalf("insert into a: %v", err)
	}

	cb, _ := b.Get(ctx)
	defer b.Put(cb)
	var n int
	err = sqlitex.ExecuteTransient(cb, "SELECT count(*) FROM accounts;", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error { n = stmt.ColumnInt(0); return nil },
	})
	if err != nil {
		t.Fatalf("count in b: %v", err)
	}
	if n != 0 {
		t.Errorf("store b sees %d accounts; stores are not isolated", n)
	}
}

// TestOpenTemporaryShared confirms that shared=true handles observe the same
// process-wide database. This test is intentionally not parallel: the shared
// database is global to the process.
func TestOpenTemporaryShared(t *testing.T) {
	ctx := context.Background()
	a, err := OpenTemporary(ctx, nil, true)
	if err != nil {
		t.Fatalf("OpenTemporary a: %v", err)
	}
	defer a.Close()
	b, err := OpenTemporary(ctx, nil, true)
	if err != nil {
		t.Fatalf("OpenTemporary b: %v", err)
	}
	defer b.Close()

	ca, _ := a.Get(ctx)
	defer a.Put(ca)
	if err := sqlitex.ExecuteTransient(ca, "INSERT INTO accounts (email, hashed_secret) VALUES ('shared@x.c', 'x');", nil); err != nil {
		t.Fatalf("insert into a: %v", err)
	}

	cb, _ := b.Get(ctx)
	defer b.Put(cb)
	var n int
	err = sqlitex.ExecuteTransient(cb, "SELECT count(*) FROM accounts WHERE email = 'shared@x.c';", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error { n = stmt.ColumnInt(0); return nil },
	})
	if err != nil {
		t.Fatalf("count in b: %v", err)
	}
	if n != 1 {
		t.Errorf("store b sees %d matching accounts; shared handles are not sharing", n)
	}
}

// TestOpenTemporaryParallel exercises many isolated stores concurrently.
func TestOpenTemporaryParallel(t *testing.T) {
	for i := range 8 {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db, err := OpenTemporary(ctx, nil, false)
			if err != nil {
				t.Fatalf("OpenTemporary %d: %v", i, err)
			}
			defer db.Close()
			conn, err := db.Get(ctx)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			defer db.Put(conn)
			if got := pragma(t, conn, "user_version"); got != ExpectedVersion() {
				t.Errorf("user_version = %d, want %d", got, ExpectedVersion())
			}
		})
	}
}

func TestOpenPersistent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Missing store: OpenPersistent must not create it.
	if _, err := OpenPersistent(ctx, nil, dir); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OpenPersistent on empty dir: err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(dir, DBName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenPersistent created a file; only ecdb may create: %v", err)
	}

	// After ecdb-style creation, OpenPersistent opens it.
	if err := Create(ctx, filepath.Join(dir, DBName)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	db, err := OpenPersistent(ctx, nil, dir)
	if err != nil {
		t.Fatalf("OpenPersistent: %v", err)
	}
	db.Close()
}

// TestOpenPersistentVersionAhead simulates an old binary against a newer schema:
// the recorded version exceeds ExpectedVersion, so opening is a hard error.
func TestOpenPersistentVersionAhead(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, DBName)
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

	if _, err := OpenPersistent(ctx, nil, dir); !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("OpenPersistent err = %v, want ErrVersionMismatch", err)
	}
}

func pragma(t *testing.T, conn *sqlite.Conn, name string) int {
	t.Helper()
	v, err := pragmaInt(conn, name)
	if err != nil {
		t.Fatalf("read pragma %s: %v", name, err)
	}
	return v
}
