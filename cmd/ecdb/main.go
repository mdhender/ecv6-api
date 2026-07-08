// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command ecdb runs administrative commands directly against the database,
// assuming it is the only process touching it. Database creation is ecdb's job.
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
	// Load .env files before parsing flags so ff reads ECDB_* variables sourced
	// from them. ECDB_ENV selects which files load (see dotenv) and is read
	// straight from the environment — not a flag — because it must be known
	// before any flag is parsed. It defaults to development.
	env := os.Getenv("ECDB_ENV")
	if env == "" {
		env = "development"
	}
	if err := dotenv.Load(env); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: load %q environment: %v\n", env, err)
		os.Exit(1)
	}

	rootFlags := ff.NewFlagSet("ecdb")
	rootCmd := &ff.Command{
		Name:      "ecdb",
		Usage:     "ecdb [FLAGS] SUBCOMMAND ...",
		ShortHelp: "administer the Epimethean Challenge database",
		Flags:     rootFlags,
	}

	versionFlags := ff.NewFlagSet("version").SetParent(rootFlags)
	versionCmd := &ff.Command{
		Name:      "version",
		Usage:     "ecdb version",
		ShortHelp: "print the ecdb version",
		Flags:     versionFlags,
		Exec: func(_ context.Context, _ []string) error {
			fmt.Println(ecv6.Version().Core())
			return nil
		},
	}
	rootCmd.Subcommands = append(rootCmd.Subcommands, versionCmd)

	err := rootCmd.ParseAndRun(context.Background(), os.Args[1:], ff.WithEnvVarPrefix("ECDB"))
	switch {
	case errors.Is(err, ff.ErrHelp), errors.Is(err, ff.ErrNoExec):
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Command(rootCmd))
	case err != nil:
		fmt.Fprintf(os.Stderr, "ecdb: %v\n", err)
		os.Exit(1)
	}
}
