// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command ec starts and stops the Epimethean Challenge server. It runs
// migrations automatically whenever it opens the database, but must never
// create a new persistent database — if the persistent file is missing it
// fails rather than creating one.
package main

import (
	"context"
	"fmt"
	"os"

	ecv6 "github.com/mdhender/ecv6-api"
	"github.com/mdhender/ecv6-api/internal/cli"
	"github.com/peterbourgon/ff/v4"
)

func main() {
	if err := cli.LoadEnv("EC"); err != nil {
		fmt.Fprintf(os.Stderr, "ec: %v\n", err)
		os.Exit(1)
	}
	cmd, logging := newRootCommand()
	os.Exit(cli.Run(context.Background(), cmd, "EC", os.Args[1:], logging))
}

// newRootCommand builds the ec command tree, returning it alongside the shared
// logging setup so main can apply the resolved log level after parsing.
func newRootCommand() (*ff.Command, *cli.Logging) {
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
	serveCmd := &ff.Command{
		Name:      "serve",
		Usage:     "ec serve [FLAGS]",
		ShortHelp: "open the existing database and run the API server",
		Flags:     serveFlags,
		Exec: func(ctx context.Context, _ []string) error {
			return cmdServe(ctx, log, *dataDir, *listen, *dev)
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
