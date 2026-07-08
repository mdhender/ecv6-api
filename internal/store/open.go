// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

// DB is a live handle to an EC data store: a pool of SQLite connections with all
// migrations applied. Obtain a connection with Get, return it with Put, and
// release the whole pool with Close.
//
// A DB is created only by OpenPersistent (an existing on-disk store) or
// OpenTemporary (a throwaway in-memory store). Creating a new *persistent* store
// is deliberately not offered here — that is ecdb's job alone.
type DB struct {
	pool *sqlitemigration.Pool
}

// OpenPersistent opens the existing store in folder dir (the folder that holds
// ec.db, given without the filename), applies any missing migrations, and
// returns it.
//
// It never creates a store: if ec.db is missing it returns ErrNotFound. This
// enforces the rule that only ecdb creates databases — ec opens and migrates,
// but never creates a persistent file.
//
// Connections use WAL journaling and enforce foreign keys. If the store's
// version does not equal ExpectedVersion after migration, OpenPersistent returns
// ErrVersionMismatch; the most likely cause is running an old binary against a
// store whose schema is newer than this build knows about.
func OpenPersistent(ctx context.Context, logger *slog.Logger, dir string) (*DB, error) {
	dbPath := filepath.Join(dir, DBName)
	if sb, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", dbPath, ErrNotFound)
		}
		return nil, fmt.Errorf("%s: %w", dbPath, err)
	} else if !sb.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", dbPath, ErrNotFound)
	}
	// Deliberately no OpenCreate: if the file vanished between the stat above and
	// the open below, fail rather than silently create a new persistent store.
	return openPool(ctx, logger, dbPath, sqlite.OpenReadWrite|sqlite.OpenWAL)
}

// sharedMemoryURI names the single process-wide in-memory database. Every
// OpenTemporary(shared=true) reaches this same database.
const sharedMemoryURI = "file::memory:?mode=memory&cache=shared"

// OpenTemporary creates an in-memory store, applies all migrations, and returns
// it. It is intended for tests.
//
// The shared flag selects between two kinds of in-memory database, a distinction
// that is a property of the URI we build, NOT something ZombieZen does for us.
// Verified against zombiezen.com/go/sqlite v1.4.2:
//
//   - A pool opens several connections to the same URI, and they must all reach
//     the SAME in-memory database. Bare ":memory:" is rejected outright by
//     sqlitex.NewPool, and a private ":memory:" would give each pooled connection
//     its own empty database. So the URI must always use "cache=shared".
//   - Under "cache=shared", the in-memory database is keyed by the NAME in the
//     URI. Two pools opened with the same name SHARE one database; two pools with
//     different names are fully ISOLATED. ZombieZen does not uniquify for you.
//
// shared == false (the usual choice for tests): each call mints a UNIQUE random
// name — "file:<random>?mode=memory&cache=shared" — so every store is isolated
// and nothing is shared. Run tests with t.Parallel() and each gets its own
// database.
//
// shared == true: every call uses the one unnamed process-wide database
// ("file::memory:?mode=memory&cache=shared"). All such stores see each other's
// data, and the database lives until the last connection to it is closed. Use
// this only when separate handles must observe the same data; it is not safe for
// parallel tests.
//
// Either way, closing the returned DB releases its connections; for an isolated
// store that discards the database.
func OpenTemporary(ctx context.Context, logger *slog.Logger, shared bool) (*DB, error) {
	uri := sharedMemoryURI
	if !shared {
		name, err := uniqueName()
		if err != nil {
			return nil, fmt.Errorf("temporary store name: %w", err)
		}
		uri = "file:" + name + "?mode=memory&cache=shared"
	}
	return openPool(ctx, logger, uri, sqlite.OpenReadWrite|sqlite.OpenCreate|sqlite.OpenURI)
}

// openPool opens a migrating connection pool at uri, waits for migrations to
// finish, and confirms the resulting version equals ExpectedVersion. On any
// failure it closes the pool and returns the error.
func openPool(ctx context.Context, logger *slog.Logger, uri string, flags sqlite.OpenFlags) (*DB, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	pool := sqlitemigration.NewPool(uri, schema(), sqlitemigration.Options{
		Flags:          flags,
		PrepareConn:    enableForeignKeys,
		OnStartMigrate: func() { logger.InfoContext(ctx, "store: applying migrations") },
		OnReady:        func() { logger.DebugContext(ctx, "store: ready") },
		OnError:        func(err error) { logger.ErrorContext(ctx, "store: pool error", "err", err) },
	})

	// Get blocks until the initial migration finishes (or fails), surfacing any
	// migration error.
	conn, err := pool.Get(ctx)
	if err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("open store: %w", err)
	}
	got, err := pragmaInt(conn, "user_version")
	pool.Put(conn)
	if err != nil {
		_ = pool.Close()
		return nil, err
	}
	if want := ExpectedVersion(); got != want {
		_ = pool.Close()
		return nil, fmt.Errorf("%w: store is version %d, this binary expects %d", ErrVersionMismatch, got, want)
	}
	logger.InfoContext(ctx, "store opened", "version", got)
	return &DB{pool: pool}, nil
}

// Get obtains a connection from the pool, blocking until one is free or ctx is
// done. Return it with Put.
func (db *DB) Get(ctx context.Context) (*sqlite.Conn, error) {
	return db.pool.Get(ctx)
}

// Put returns a connection obtained from Get back to the pool.
func (db *DB) Put(conn *sqlite.Conn) {
	db.pool.Put(conn)
}

// SchemaVersion returns the open store's schema version — SQLite's user_version
// pragma, equal to the number of migrations applied. Because a DB is only
// returned once its version has been confirmed to equal ExpectedVersion, this
// normally matches ExpectedVersion; it is read live so callers report the
// database's own value rather than the build's expectation.
func (db *DB) SchemaVersion(ctx context.Context) (int, error) {
	conn, err := db.pool.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("schema version: %w", err)
	}
	defer db.pool.Put(conn)
	return pragmaInt(conn, "user_version")
}

// Close releases every connection in the pool. For a temporary store this
// discards the in-memory database.
func (db *DB) Close() error {
	return db.pool.Close()
}

// enableForeignKeys is the per-connection setup hook: foreign key enforcement is
// off by default in SQLite and is a per-connection setting, so every pooled
// connection must turn it on.
func enableForeignKeys(conn *sqlite.Conn) error {
	return sqlitex.ExecuteTransient(conn, "PRAGMA foreign_keys = ON;", nil)
}

// uniqueName returns a random identifier used to isolate an in-memory store. See
// OpenTemporary for why uniqueness matters.
func uniqueName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "ec-mem-" + hex.EncodeToString(b[:]), nil
}
