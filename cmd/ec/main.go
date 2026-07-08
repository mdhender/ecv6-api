// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command ec starts and stops the Epimethean Challenge server. It runs
// migrations automatically whenever it opens the database, but must never
// create a new persistent database — if the persistent file is missing it
// fails rather than creating one.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	ecv6 "github.com/mdhender/ecv6-api"
	"github.com/mdhender/ecv6-api/internal/dotenv"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

func main() {
	// Load .env files before parsing flags so ff reads EC_* variables sourced
	// from them. EC_ENV selects which files load (see dotenv) and is read
	// straight from the environment — not a flag — because it must be known
	// before any flag is parsed. It defaults to development.
	env := os.Getenv("EC_ENV")
	if env == "" {
		env = "development"
	}
	if err := dotenv.Load(env); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: load %q environment: %v\n", env, err)
		os.Exit(1)
	}

	rootFlags := ff.NewFlagSet("ec")
	rootCmd := &ff.Command{
		Name:      "ec",
		Usage:     "ec [FLAGS] SUBCOMMAND ...",
		ShortHelp: "run the Epimethean Challenge server",
		Flags:     rootFlags,
	}

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

	err := rootCmd.ParseAndRun(context.Background(), os.Args[1:], ff.WithEnvVarPrefix("EC"))
	switch {
	case errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Command(rootCmd))
	case err != nil:
		fmt.Fprintf(os.Stderr, "ec: %v\n", err)
		os.Exit(1)
	}
}
