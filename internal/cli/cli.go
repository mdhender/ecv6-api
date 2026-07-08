// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package cli holds the startup plumbing shared by the ecdb and ec commands:
// selecting the runtime environment, loading .env files, and mapping an ff
// command's result to a process exit code. Keeping it here stops the two
// binaries' main functions from drifting apart.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mdhender/ecv6-api/internal/dotenv"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

// LoadEnv selects the runtime environment from <prefix>_ENV (defaulting to
// "development") and loads the matching .env files. It must run before flags are
// parsed, because those files supply the <prefix>_* variables that ff reads, and
// <prefix>_ENV itself is read straight from the environment rather than as a flag
// for the same reason.
func LoadEnv(prefix string) error {
	env := os.Getenv(prefix + "_ENV")
	if env == "" {
		env = "development"
	}
	if err := dotenv.Load(env); err != nil {
		return fmt.Errorf("load %q environment: %w", env, err)
	}
	return nil
}

// Run parses and executes cmd with the given env-var prefix, printing any
// diagnostics to stderr, and returns the process exit code the caller should
// pass to os.Exit:
//
//   - success                   -> 0
//   - ff.ErrHelp (help asked)   -> print help, 0
//   - ff.ErrNoExec (no command) -> print help, 2 (a usage error)
//   - any other error           -> print "<name>: <err>", 1
//
// Parsing and running are split so that logging, if configured, is applied in
// between — after the flags (and their env-var fallbacks) are known, but before
// the selected command runs. Pass a nil logging to skip that step. Returning the
// code rather than calling os.Exit keeps this testable.
func Run(ctx context.Context, cmd *ff.Command, prefix string, args []string, logging *Logging) int {
	if err := cmd.Parse(args, ff.WithEnvVarPrefix(prefix)); err != nil {
		return exitCode(cmd, err)
	}
	if logging != nil {
		if err := logging.Apply(); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmd.Name, err)
			return 1
		}
	}
	if err := cmd.Run(ctx); err != nil {
		return exitCode(cmd, err)
	}
	return 0
}

// exitCode renders err to stderr in the house style and returns the matching
// process exit code. It is shared by Run's parse and run phases so both map
// errors the same way.
func exitCode(cmd *ff.Command, err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ff.ErrHelp):
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Command(cmd))
		return 0
	case errors.Is(err, ff.ErrNoExec):
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Command(cmd))
		return 2
	default:
		fmt.Fprintf(os.Stderr, "%s: %v\n", cmd.Name, err)
		return 1
	}
}
