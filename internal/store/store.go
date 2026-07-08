// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package store manages the SQLite data store: creating a database, applying
// migrations, and reporting the schema version.
//
// The database version is the number of migrations applied, recorded in
// SQLite's user_version pragma by sqlitemigration. ExpectedVersion reports the
// number of migrations this build knows about; a database is current when its
// version equals ExpectedVersion.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mdhender/ecv6-api/internal/cerrs"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

// DBName is the fixed filename of an EC database within its data folder.
const DBName = "ec.db"

// appID tags an EC database via SQLite's application_id pragma, so an EC
// database can be told apart from an arbitrary SQLite file. It is a frozen
// on-disk identifier: never change it once databases exist.
const appID int32 = 0x0EC0DB

const (
	// ErrNotFound is returned when the database file does not exist.
	ErrNotFound = cerrs.Error("database not found")
	// ErrNotEC is returned when the file is a SQLite database whose
	// application_id does not match ours.
	ErrNotEC = cerrs.Error("not an EC database")
	// ErrVersionMismatch is returned when the database version differs from the
	// version this build expects.
	ErrVersionMismatch = cerrs.Error("database version mismatch")
)

// schema is the migration schema applied to a database.
func schema() sqlitemigration.Schema {
	return sqlitemigration.Schema{
		AppID:      appID,
		Migrations: migrations,
	}
}

// ExpectedVersion is the schema version this build expects: the number of
// migrations it knows about.
func ExpectedVersion() int {
	return len(migrations)
}

// Create creates a new database at dbPath and applies all migrations. It fails
// if the file already exists; callers that allow overwriting must remove it
// (and any sidecar files) first.
func Create(ctx context.Context, dbPath string) error {
	if _, err := os.Stat(dbPath); err == nil {
		return fmt.Errorf("%s: %w", dbPath, os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", dbPath, err)
	}
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadWrite, sqlite.OpenCreate)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer conn.Close()
	if err := sqlitemigration.Migrate(ctx, conn, schema()); err != nil {
		return fmt.Errorf("migrate %s: %w", dbPath, err)
	}
	return nil
}

// Version opens the database read-only and returns its schema version. It
// returns ErrNotFound if the file is missing and ErrNotEC if the file is not an
// EC database.
func Version(_ context.Context, dbPath string) (int, error) {
	conn, err := openReadOnly(dbPath)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	return pragmaInt(conn, "user_version")
}

// Verify returns nil only if the database exists, is an EC database, and its
// version matches ExpectedVersion. Otherwise it returns an error describing the
// problem (ErrNotFound, ErrNotEC, or ErrVersionMismatch).
func Verify(ctx context.Context, dbPath string) error {
	got, err := Version(ctx, dbPath)
	if err != nil {
		return err
	}
	if want := ExpectedVersion(); got != want {
		return fmt.Errorf("%w: database is version %d, expected %d", ErrVersionMismatch, got, want)
	}
	return nil
}

// openReadOnly opens dbPath read-only after confirming it exists as a regular
// file and carries our application_id.
func openReadOnly(dbPath string) (*sqlite.Conn, error) {
	if sb, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", dbPath, ErrNotFound)
		}
		return nil, fmt.Errorf("%s: %w", dbPath, err)
	} else if !sb.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", dbPath, ErrNotFound)
	}
	conn, err := sqlite.OpenConn(dbPath, sqlite.OpenReadOnly)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	id, err := pragmaInt(conn, "application_id")
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if int32(id) != appID {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: %w", dbPath, ErrNotEC)
	}
	return conn, nil
}

// pragmaInt reads a single-integer PRAGMA such as user_version or
// application_id. The pragma name is a trusted constant, never user input.
func pragmaInt(conn *sqlite.Conn, pragma string) (int, error) {
	var v int
	err := sqlitex.ExecuteTransient(conn, "PRAGMA "+pragma+";", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			v = stmt.ColumnInt(0)
			return nil
		},
	})
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", pragma, err)
	}
	return v, nil
}
