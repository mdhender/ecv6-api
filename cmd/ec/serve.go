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
	"github.com/mdhender/ecv6-api/internal/server"
	"github.com/mdhender/ecv6-api/internal/store"
)

// cmdServe opens the existing database in the data folder and runs the HTTP
// server until interrupted (SIGINT/SIGTERM), then shuts down gracefully. It
// never creates a database: a missing store.db is a fatal error, not a prompt to
// create one — that is ecdb's job.
func cmdServe(ctx context.Context, log *slog.Logger, dataDir, listen string, dev bool, secretCost int) error {
	if dataDir == "" {
		return fmt.Errorf("serve: no data folder set (pass --data or set EC_DATA)")
	}

	// Open (never create) the existing store, applying any pending migrations.
	db, err := store.OpenPersistent(ctx, log, dataDir)
	if err != nil {
		return fmt.Errorf("serve: open store in %s: %w", dataDir, err)
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
