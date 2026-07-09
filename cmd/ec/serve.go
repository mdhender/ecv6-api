// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	ecv6 "github.com/mdhender/ecv6-api"
	"github.com/mdhender/ecv6-api/internal/secret"
	"github.com/mdhender/ecv6-api/internal/server"
	"github.com/mdhender/ecv6-api/internal/store"
)

// Well-known credentials for the admin auto-seeded into a --memory server. An
// in-memory database is empty and process-local, so ecdb cannot seed it from
// another process; --memory instead inserts this one admin at startup so the
// server is immediately usable. These are hard-coded on purpose: in-memory mode
// is testing-only, holds no persistent data, and vanishes when the process exits,
// so a fixed login is not a meaningful security risk. Tests and docs reference
// these constants as the single source of truth.
const (
	// MemoryAdminEmail is the email of the well-known admin seeded in --memory
	// mode. It matches the ecdb admin create default email.
	MemoryAdminEmail = "admin@ecv6.example.com"
	// MemoryAdminSecret is the secret (password) of the well-known admin seeded in
	// --memory mode.
	MemoryAdminSecret = "password"
)

// cmdServe opens the database and runs the HTTP server until interrupted
// (SIGINT/SIGTERM), then shuts down gracefully.
//
// With memory set, it serves a fresh, migrated, in-memory database that never
// touches disk and auto-seeds the well-known admin (MemoryAdminEmail /
// MemoryAdminSecret) so the server is immediately usable; --memory and --data are
// mutually exclusive. Otherwise it opens the existing on-disk database in dataDir:
// it never creates one, so a missing ec.db is a fatal error, not a prompt to
// create it — that is ecdb's job — and an on-disk store is never auto-seeded.
func cmdServe(ctx context.Context, log *slog.Logger, dataDir, listen string, dev, memory bool, secretCost int) error {
	if memory && dataDir != "" {
		return fmt.Errorf("serve: --memory and --data are mutually exclusive (unset one)")
	}

	var db *store.DB
	if memory {
		// A uniquely-named shared-cache in-memory database the whole pool reaches;
		// correct for a single server process. It never touches disk.
		mdb, err := store.OpenTemporary(ctx, log, false)
		if err != nil {
			return fmt.Errorf("serve: open in-memory store: %w", err)
		}
		db = mdb
		if err := seedMemoryAdmin(ctx, log, db, secretCost); err != nil {
			_ = db.Close()
			return fmt.Errorf("serve: %w", err)
		}
	} else {
		if dataDir == "" {
			return fmt.Errorf("serve: no data folder set (pass --data or set EC_DATA, or use --memory)")
		}
		// Open (never create) the existing store, applying any pending migrations.
		pdb, err := store.OpenPersistent(ctx, log, dataDir)
		if err != nil {
			return fmt.Errorf("serve: open store in %s: %w", dataDir, err)
		}
		db = pdb
	}
	defer func() { _ = db.Close() }()

	// Cancel the run context on the first interrupt signal so Run drains and
	// stops; a second signal restores the default behavior (immediate exit).
	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(server.Config{
		Addr:       listen,
		DevMode:    dev,
		SecretCost: secretCost,
	}, db, log, ecv6.Version().String())

	if err := srv.Run(runCtx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// seedMemoryAdmin inserts the well-known admin (MemoryAdminEmail /
// MemoryAdminSecret) into an in-memory store, hashing the secret with secretCost
// exactly as ecdb admin create does, then logs the credentials prominently at
// WARN so it is obvious this is a throwaway test server and testers know the
// login. It is only ever called in --memory mode; a persistent store is never
// auto-seeded.
func seedMemoryAdmin(ctx context.Context, log *slog.Logger, db *store.DB, secretCost int) error {
	hashed, err := secret.Hash(MemoryAdminSecret, secretCost)
	if err != nil {
		return fmt.Errorf("seed admin: hash secret: %w", err)
	}
	if _, err := db.CreateAccount(ctx, store.Account{
		Email:        MemoryAdminEmail, // CreateAccount lowercases before storing.
		DisplayName:  "admin",
		HashedSecret: hashed,
		IsAdmin:      true,
		IsActive:     true,
	}); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	log.Warn("in-memory server: seeded well-known admin",
		"email", MemoryAdminEmail, "secret", MemoryAdminSecret)
	return nil
}
