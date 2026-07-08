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
	"time"

	ecv6 "github.com/mdhender/ecv6-api"
	"github.com/mdhender/ecv6-api/internal/cli"
	"github.com/mdhender/ecv6-api/internal/store"
	"github.com/peterbourgon/ff/v4"
)

func main() {
	if err := cli.LoadEnv("ECDB"); err != nil {
		fmt.Fprintf(os.Stderr, "ecdb: %v\n", err)
		os.Exit(1)
	}
	cmd, logging := newRootCommand()
	os.Exit(cli.Run(context.Background(), cmd, "ECDB", os.Args[1:], logging))
}

// newRootCommand builds the ecdb command tree, returning it alongside the shared
// logging setup so main can apply the resolved log level after parsing. main
// stays a thin bootstrap so the tree can grow here without turning main into a
// wall of setup.
func newRootCommand() (*ff.Command, *cli.Logging) {
	rootFlags := ff.NewFlagSet("ecdb")
	logging := cli.NewLogging(rootFlags)
	log := logging.Logger
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
			return cmdCreate(ctx, log, args, *overwrite)
		},
	}

	backupFlags := ff.NewFlagSet("backup").SetParent(rootFlags)
	outputPath := backupFlags.StringLong("output-path", "", "folder to write the backup into (default: the database's folder)")
	backupCmd := &ff.Command{
		Name:      "backup",
		Usage:     "ecdb backup [FLAGS] PATH",
		ShortHelp: "back up the database in folder PATH to a timestamped copy",
		Flags:     backupFlags,
		Exec: func(ctx context.Context, args []string) error {
			return cmdBackup(ctx, log, args, *outputPath)
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
				return errors.New("version takes no arguments; use `ecdb migration version PATH` for the database's migration version")
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
			return cmdMigrationVersion(ctx, log, args)
		},
	}

	migrationVerifyFlags := ff.NewFlagSet("verify").SetParent(migrationFlags)
	migrationVerifyCmd := &ff.Command{
		Name:      "verify",
		Usage:     "ecdb migration verify PATH",
		ShortHelp: "exit 0 if the database in folder PATH is current, else exit 1",
		Flags:     migrationVerifyFlags,
		Exec: func(ctx context.Context, args []string) error {
			return cmdVerify(ctx, log, args)
		},
	}

	migrationUpFlags := ff.NewFlagSet("up").SetParent(migrationFlags)
	migrationUpCmd := &ff.Command{
		Name:      "up",
		Usage:     "ecdb migration up PATH",
		ShortHelp: "apply any missing migrations to the database in folder PATH",
		Flags:     migrationUpFlags,
		Exec: func(ctx context.Context, args []string) error {
			return cmdMigrationUp(ctx, log, args)
		},
	}

	migrationCmd := &ff.Command{
		Name:        "migration",
		Usage:       "ecdb migration SUBCOMMAND ...",
		ShortHelp:   "apply, inspect, and verify database migrations",
		Flags:       migrationFlags,
		Subcommands: []*ff.Command{migrationUpCmd, migrationVersionCmd, migrationVerifyCmd},
	}

	rootCmd.Subcommands = append(rootCmd.Subcommands, createCmd, backupCmd, versionCmd, migrationCmd)
	return rootCmd, logging
}

// requirePath returns the single PATH argument (the data folder that holds
// ec.db) for a subcommand, or an error naming the subcommand if the wrong number
// of arguments was given.
func requirePath(cmd string, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s requires exactly one PATH argument", cmd)
	}
	return args[0], nil
}

// cmdCreate creates a fresh ec.db in the folder given by args[0]. The folder must
// already exist.
func cmdCreate(ctx context.Context, log *slog.Logger, args []string, overwrite bool) error {
	folder, err := requirePath("create", args)
	if err != nil {
		return err
	}
	log.Debug("create: starting", "folder", folder, "overwrite", overwrite)
	if sb, err := os.Stat(folder); err != nil {
		return fmt.Errorf("create: cannot access PATH %s: %w", folder, err)
	} else if !sb.IsDir() {
		return fmt.Errorf("create: PATH is not a folder: %s", folder)
	}

	dbPath := filepath.Join(folder, store.DBName)
	if sb, err := os.Stat(dbPath); err == nil {
		// Refuse to touch a non-regular file even with --overwrite: we will not rm
		// a directory or device that happens to sit at ec.db (ADR-0008).
		if !sb.Mode().IsRegular() {
			return fmt.Errorf("create: existing database path is not a regular file: %s", dbPath)
		}
		if !overwrite {
			return fmt.Errorf("create: database already exists (pass --overwrite to replace it): %s", dbPath)
		}
		if err := removeDB(dbPath); err != nil {
			return fmt.Errorf("create: cannot remove existing database %s: %w", dbPath, err)
		}
		fmt.Fprintf(os.Stderr, "removed existing database %s\n", dbPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("create: cannot access database path %s: %w", dbPath, err)
	}

	// The stat checks above are best-effort (ecdb assumes it is the only process
	// touching the folder); store.Create's own existence check is the authority
	// (ADR-0008).
	if err := store.Create(ctx, dbPath); err != nil {
		return fmt.Errorf("create: cannot build database %s: %w", dbPath, err)
	}
	fmt.Fprintf(os.Stderr, "created database %s (version %d)\n", dbPath, store.ExpectedVersion())
	return nil
}

// cmdBackup writes a consistent copy of the database in folder args[0] to a
// timestamped file. The backup file is always named ec.db.<timestamp-utc>; the
// caller chooses only the destination folder (--output-path, defaulting to the
// database's own folder), never the file name. See ADR-0010.
func cmdBackup(ctx context.Context, log *slog.Logger, args []string, outputPath string) error {
	folder, err := requirePath("backup", args)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(folder, store.DBName)

	// Verify before backing up: refuse to snapshot a database that is missing, not
	// an EC database, or not current. This is deliberate (ADR-0010 / issue #1) —
	// the version must match exactly, so we never capture a stale or foreign file.
	if err := store.Verify(ctx, dbPath); err != nil {
		return fmt.Errorf("backup: cannot verify %s: %w", dbPath, err)
	}

	// Default the destination folder to the database's own folder.
	if outputPath == "" {
		outputPath = folder
	}
	if sb, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("backup: cannot access --output-path %s: %w", outputPath, err)
	} else if !sb.IsDir() {
		return fmt.Errorf("backup: --output-path is not a folder: %s", outputPath)
	}

	destPath := filepath.Join(outputPath, backupName(time.Now()))
	log.Debug("backup: starting", "src", dbPath, "dest", destPath)

	// Best-effort friendly pre-check; store.Backup's VACUUM INTO is the authority
	// and also refuses to overwrite an existing file (ADR-0008).
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("backup: destination already exists: %s", destPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: cannot access destination %s: %w", destPath, err)
	}

	if err := store.Backup(ctx, dbPath, destPath); err != nil {
		return fmt.Errorf("backup: cannot copy %s: %w", dbPath, err)
	}
	// The backup path is the command's result: print it to stdout so scripts can
	// capture it (ADR-0009).
	fmt.Println(destPath)
	return nil
}

// backupName returns the fixed backup file name for the given time: ec.db.
// followed by a filesystem-safe, lexicographically sortable UTC stamp
// (ISO 8601 basic, e.g. ec.db.20260708T183245Z). The trailing Z is appended
// literally rather than via a layout token to avoid timezone-formatting quirks.
func backupName(t time.Time) string {
	return store.DBName + "." + t.UTC().Format("20060102T150405") + "Z"
}

// cmdMigrationUp applies any missing migrations to the database in folder
// args[0].
func cmdMigrationUp(ctx context.Context, log *slog.Logger, args []string) error {
	folder, err := requirePath("migration up", args)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(folder, store.DBName)
	log.Debug("migration up: starting", "path", dbPath)
	if err := store.MigrateUp(ctx, dbPath); err != nil {
		return fmt.Errorf("migration up: cannot apply migrations to %s: %w", dbPath, err)
	}
	fmt.Fprintf(os.Stderr, "migrations applied to %s (version %d)\n", dbPath, store.ExpectedVersion())
	return nil
}

// cmdMigrationVersion prints the migration (schema) version of the database in
// folder args[0] to stdout as a plain integer, so it can be captured in scripts.
func cmdMigrationVersion(ctx context.Context, log *slog.Logger, args []string) error {
	folder, err := requirePath("migration version", args)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(folder, store.DBName)
	log.Debug("migration version: starting", "path", dbPath)
	v, err := store.Version(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("migration version: cannot read database version of %s: %w", dbPath, err)
	}
	fmt.Println(v)
	return nil
}

// cmdVerify returns nil only if the database in folder args[0] exists and is
// current; otherwise it returns an error (which the caller maps to exit 1).
func cmdVerify(ctx context.Context, log *slog.Logger, args []string) error {
	folder, err := requirePath("verify", args)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(folder, store.DBName)
	log.Debug("verify: starting", "path", dbPath)
	if err := store.Verify(ctx, dbPath); err != nil {
		return fmt.Errorf("verify failed for %s: %w", dbPath, err)
	}
	fmt.Fprintf(os.Stderr, "verified %s (version %d)\n", dbPath, store.ExpectedVersion())
	return nil
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
