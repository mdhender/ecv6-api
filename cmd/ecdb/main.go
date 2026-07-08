// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command ecdb runs administrative commands directly against the database,
// assuming it is the only process touching it. Database creation is ecdb's job.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	os.Exit(cli.Run(context.Background(), newRootCommand(), "ECDB", os.Args[1:]))
}

// newRootCommand builds the ecdb command tree. main stays a thin bootstrap so the
// tree can grow here without turning main into a wall of setup.
func newRootCommand() *ff.Command {
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
			return cmdCreate(ctx, args, *overwrite)
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
		Exec:      cmdMigrationVersion,
	}

	migrationVerifyFlags := ff.NewFlagSet("verify").SetParent(migrationFlags)
	migrationVerifyCmd := &ff.Command{
		Name:      "verify",
		Usage:     "ecdb migration verify PATH",
		ShortHelp: "exit 0 if the database in folder PATH is current, else exit 1",
		Flags:     migrationVerifyFlags,
		Exec:      cmdVerify,
	}

	migrationUpFlags := ff.NewFlagSet("up").SetParent(migrationFlags)
	migrationUpCmd := &ff.Command{
		Name:      "up",
		Usage:     "ecdb migration up PATH",
		ShortHelp: "apply any missing migrations to the database in folder PATH",
		Flags:     migrationUpFlags,
		Exec:      cmdMigrationUp,
	}

	migrationCmd := &ff.Command{
		Name:        "migration",
		Usage:       "ecdb migration SUBCOMMAND ...",
		ShortHelp:   "apply, inspect, and verify database migrations",
		Flags:       migrationFlags,
		Subcommands: []*ff.Command{migrationUpCmd, migrationVersionCmd, migrationVerifyCmd},
	}

	rootCmd.Subcommands = append(rootCmd.Subcommands, createCmd, versionCmd, migrationCmd)
	return rootCmd
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
func cmdCreate(ctx context.Context, args []string, overwrite bool) error {
	folder, err := requirePath("create", args)
	if err != nil {
		return err
	}
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

// cmdMigrationUp applies any missing migrations to the database in folder
// args[0].
func cmdMigrationUp(ctx context.Context, args []string) error {
	folder, err := requirePath("migration up", args)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(folder, store.DBName)
	if err := store.MigrateUp(ctx, dbPath); err != nil {
		return fmt.Errorf("migration up: cannot apply migrations to %s: %w", dbPath, err)
	}
	fmt.Fprintf(os.Stderr, "migrations applied to %s (version %d)\n", dbPath, store.ExpectedVersion())
	return nil
}

// cmdMigrationVersion prints the migration (schema) version of the database in
// folder args[0] to stdout as a plain integer, so it can be captured in scripts.
func cmdMigrationVersion(ctx context.Context, args []string) error {
	folder, err := requirePath("migration version", args)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(folder, store.DBName)
	v, err := store.Version(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("migration version: cannot read database version of %s: %w", dbPath, err)
	}
	fmt.Println(v)
	return nil
}

// cmdVerify returns nil only if the database in folder args[0] exists and is
// current; otherwise it returns an error (which the caller maps to exit 1).
func cmdVerify(ctx context.Context, args []string) error {
	folder, err := requirePath("verify", args)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(folder, store.DBName)
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
