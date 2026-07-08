// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command ecdb runs administrative commands directly against the database,
// assuming it is the only process touching it. Database creation is ecdb's job.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	ecv6 "github.com/mdhender/ecv6-api"
	"github.com/mdhender/ecv6-api/internal/dotenv"
	"github.com/mdhender/ecv6-api/internal/store"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Load .env files before parsing flags so ff reads ECDB_* variables sourced
	// from them. ECDB_ENV selects which files load (see dotenv) and is read
	// straight from the environment — not a flag — because it must be known
	// before any flag is parsed. It defaults to development.
	env := os.Getenv("ECDB_ENV")
	if env == "" {
		env = "development"
	}
	if err := dotenv.Load(env); err != nil {
		slog.Error("load environment", "env", env, "err", err)
		os.Exit(1)
	}

	rootFlags := ff.NewFlagSet("ecdb")
	rootCmd := &ff.Command{
		Name:      "ecdb",
		Usage:     "ecdb [FLAGS] SUBCOMMAND ...",
		ShortHelp: "administer the Epimethean Challenge database",
		Flags:     rootFlags,
	}

	createFlags := ff.NewFlagSet("create").SetParent(rootFlags)
	overwrite := createFlags.BoolLong("overwrite", "remove an existing database file before creating")
	createCmd := &ff.Command{
		Name:      "create",
		Usage:     "ecdb create [--overwrite] PATH",
		ShortHelp: "create a new database in folder PATH and apply migrations",
		Flags:     createFlags,
		Exec: func(ctx context.Context, args []string) error {
			cmdCreate(ctx, args, *overwrite)
			return nil
		},
	}

	versionFlags := ff.NewFlagSet("version").SetParent(rootFlags)
	versionCmd := &ff.Command{
		Name:      "version",
		Usage:     "ecdb version",
		ShortHelp: "print the ecdb application version",
		Flags:     versionFlags,
		Exec: func(_ context.Context, args []string) error {
			if len(args) != 0 {
				fail("version takes no arguments; use `ecdb migration version PATH` for the database's migration version")
			}
			fmt.Println(ecv6.Version().Core())
			return nil
		},
	}

	migrationFlags := ff.NewFlagSet("migration").SetParent(rootFlags)

	migrationVersionFlags := ff.NewFlagSet("version").SetParent(migrationFlags)
	migrationVersionCmd := &ff.Command{
		Name:      "version",
		Usage:     "ecdb migration version PATH",
		ShortHelp: "print the migration (schema) version of the database in folder PATH",
		Flags:     migrationVersionFlags,
		Exec: func(ctx context.Context, args []string) error {
			cmdMigrationVersion(ctx, args)
			return nil
		},
	}

	migrationVerifyFlags := ff.NewFlagSet("verify").SetParent(migrationFlags)
	migrationVerifyCmd := &ff.Command{
		Name:      "verify",
		Usage:     "ecdb migration verify PATH",
		ShortHelp: "exit 0 if the database in folder PATH is current, else exit 1",
		Flags:     migrationVerifyFlags,
		Exec: func(ctx context.Context, args []string) error {
			cmdVerify(ctx, args)
			return nil
		},
	}

	migrationUpFlags := ff.NewFlagSet("up").SetParent(migrationFlags)
	migrationUpCmd := &ff.Command{
		Name:      "up",
		Usage:     "ecdb migration up PATH",
		ShortHelp: "apply any missing migrations to the database in folder PATH",
		Flags:     migrationUpFlags,
		Exec: func(ctx context.Context, args []string) error {
			cmdMigrationUp(ctx, args)
			return nil
		},
	}

	migrationCmd := &ff.Command{
		Name:        "migration",
		Usage:       "ecdb migration SUBCOMMAND ...",
		ShortHelp:   "apply, inspect, and verify database migrations",
		Flags:       migrationFlags,
		Subcommands: []*ff.Command{migrationUpCmd, migrationVersionCmd, migrationVerifyCmd},
	}

	rootCmd.Subcommands = append(rootCmd.Subcommands, createCmd, versionCmd, migrationCmd)

	err := rootCmd.ParseAndRun(context.Background(), os.Args[1:], ff.WithEnvVarPrefix("ECDB"))
	switch {
	case errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Command(rootCmd))
	case err != nil:
		slog.Error("ecdb", "err", err)
		os.Exit(1)
	}
}

// cmdCreate creates a fresh ec.db in the folder given by args[0]. The folder
// must already exist. On any failure it logs and exits non-zero.
func cmdCreate(ctx context.Context, args []string, overwrite bool) {
	if len(args) != 1 {
		fail("create requires exactly one PATH argument")
	}
	folder := args[0]
	if sb, err := os.Stat(folder); err != nil {
		fail("create: cannot access PATH", "path", folder, "err", err)
	} else if !sb.IsDir() {
		fail("create: PATH is not a folder", "path", folder)
	}

	dbPath := filepath.Join(folder, store.DBName)
	if sb, err := os.Stat(dbPath); err == nil {
		if !sb.Mode().IsRegular() {
			fail("create: existing database path is not a regular file", "path", dbPath)
		}
		if !overwrite {
			fail("create: database already exists (pass --overwrite to replace it)", "path", dbPath)
		}
		if err := removeDB(dbPath); err != nil {
			fail("create: cannot remove existing database", "path", dbPath, "err", err)
		}
		slog.Info("create: removed existing database", "path", dbPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		fail("create: cannot access database path", "path", dbPath, "err", err)
	}

	if err := store.Create(ctx, dbPath); err != nil {
		fail("create: cannot build database", "path", dbPath, "err", err)
	}
	slog.Info("created database", "path", dbPath, "version", store.ExpectedVersion())
}

// cmdMigrationUp applies any missing migrations to the database in folder
// args[0].
func cmdMigrationUp(ctx context.Context, args []string) {
	if len(args) != 1 {
		fail("migration up requires exactly one PATH argument")
	}
	dbPath := filepath.Join(args[0], store.DBName)
	if err := store.MigrateUp(ctx, dbPath); err != nil {
		fail("migration up: cannot apply migrations", "path", dbPath, "err", err)
	}
	slog.Info("migrations applied", "path", dbPath, "version", store.ExpectedVersion())
}

// cmdMigrationVersion logs the migration (schema) version of the database in
// folder args[0].
func cmdMigrationVersion(ctx context.Context, args []string) {
	if len(args) != 1 {
		fail("migration version requires exactly one PATH argument")
	}
	dbPath := filepath.Join(args[0], store.DBName)
	v, err := store.Version(ctx, dbPath)
	if err != nil {
		fail("migration version: cannot read database version", "path", dbPath, "err", err)
	}
	slog.Info("migration version", "path", dbPath, "version", v, "expected", store.ExpectedVersion())
}

// cmdVerify exits 0 only if the database in folder args[0] exists and is current;
// otherwise it logs the reason and exits 1.
func cmdVerify(ctx context.Context, args []string) {
	if len(args) != 1 {
		fail("verify requires exactly one PATH argument")
	}
	dbPath := filepath.Join(args[0], store.DBName)
	if err := store.Verify(ctx, dbPath); err != nil {
		fail("verify failed", "path", dbPath, "err", err)
	}
	slog.Info("verify ok", "path", dbPath, "version", store.ExpectedVersion())
}

// removeDB deletes the database file and any SQLite sidecar files, so a stale
// -wal/-shm/-journal cannot corrupt the replacement. Missing files are ignored.
func removeDB(dbPath string) error {
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm", dbPath + "-journal"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// fail logs a hard-failure message via slog and exits with status 1.
func fail(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
