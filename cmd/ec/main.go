// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command ec starts and stops the Epimethean Challenge server. It runs
// migrations automatically whenever it opens the database, but must never
// create a new persistent database — if the persistent file is missing it
// fails rather than creating one.
//
// For a throwaway dev or smoke-test server, "ec serve --memory" (env EC_MEMORY)
// serves a fresh, migrated, in-memory database that never touches disk and
// auto-seeds a well-known admin (MemoryAdminEmail / MemoryAdminSecret), logged at
// startup, so it is immediately usable. --memory and --data are mutually
// exclusive, and a persistent database is never auto-seeded.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	ecv6 "github.com/mdhender/ecv6-api"
	"github.com/mdhender/ecv6-api/internal/cli"
	"github.com/mdhender/ecv6-api/internal/secret"
	"github.com/peterbourgon/ff/v4"
)

func main() {
	if err := cli.LoadEnv("EC"); err != nil {
		fmt.Fprintf(os.Stderr, "ec: %v\n", err)
		os.Exit(1)
	}
	args := os.Args[1:]
	cmd, logging := newRootCommand(args)
	os.Exit(cli.Run(context.Background(), cmd, "EC", args, logging))
}

// dataFlagOnCLI reports whether --data (equivalently -data) was passed as a
// command-line flag in args, in either the space-separated (`--data DIR`) or
// attached (`--data=DIR`) form. It lets serve distinguish an explicit --data from
// a data dir sourced only from EC_DATA: ff sets both through the same SetValue
// path during Parse, so the parsed flag alone cannot tell them apart. Scanning
// stops at a bare "--", which ends flag parsing. Over-detection is safe here — the
// worst case is treating an explicit --data as the conflict it already is.
func dataFlagOnCLI(args []string) bool {
	for _, a := range args {
		if a == "--" {
			break
		}
		switch {
		case a == "--data", a == "-data":
			return true
		case strings.HasPrefix(a, "--data="), strings.HasPrefix(a, "-data="):
			return true
		}
	}
	return false
}

// newRootCommand builds the ec command tree, returning it alongside the shared
// logging setup so main can apply the resolved log level after parsing. args is
// the raw command-line slice, scanned for an explicit --data so serve can let
// --memory override an env-sourced EC_DATA while still rejecting an explicit
// --data (see resolveServeStore).
func newRootCommand(args []string) (*ff.Command, *cli.Logging) {
	rootFlags := ff.NewFlagSet("ec")
	logging := cli.NewLogging(rootFlags)
	log := logging.Logger
	rootCmd := &ff.Command{
		Name:      "ec",
		Usage:     "ec [FLAGS] SUBCOMMAND ...",
		ShortHelp: "run the Epimethean Challenge server",
		Flags:     rootFlags,
	}

	serveFlags := ff.NewFlagSet("serve").SetParent(rootFlags)
	dataDir := serveFlags.StringLong("data", "", "folder holding the database (ec.db); or set EC_DATA")
	listen := serveFlags.StringLong("listen", ":8080", "TCP address the server listens on")
	dev := serveFlags.BoolLong("dev", "enable development-only affordances")
	memory := serveFlags.BoolLong("memory", "serve a throwaway in-memory database seeded with a well-known admin (testing only; never touches disk; mutually exclusive with --data); or set EC_MEMORY")
	secretCost := serveFlags.IntLong("secret-cost", secret.DefaultCost, "bcrypt cost (rounds) for hashing account secrets; or set EC_SECRET_COST")
	serveCmd := &ff.Command{
		Name:      "serve",
		Usage:     "ec serve [FLAGS]",
		ShortHelp: "open the existing database and run the API server",
		Flags:     serveFlags,
		Exec: func(ctx context.Context, _ []string) error {
			useMemory, dir, err := resolveServeStore(*memory, *dataDir, dataFlagOnCLI(args))
			if err != nil {
				return err
			}
			return cmdServe(ctx, log, dir, *listen, *dev, useMemory, *secretCost)
		},
	}
	rootCmd.Subcommands = append(rootCmd.Subcommands, serveCmd)

	versionFlags := ff.NewFlagSet("version").SetParent(rootFlags)
	versionCmd := &ff.Command{
		Name:      "version",
		Usage:     "ec version",
		ShortHelp: "print the ec version",
		Flags:     versionFlags,
		Exec: func(_ context.Context, _ []string) error {
			fmt.Println(ecv6.Version().Core())
			return nil
		},
	}
	rootCmd.Subcommands = append(rootCmd.Subcommands, versionCmd)
	return rootCmd, logging
}
