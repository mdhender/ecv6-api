// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v4"
)

// Logging bundles the shared slog.Logger with the LevelVar that gates it and the
// --logging-level flag that sets it. Build it once with NewLogging while
// assembling a command's root flag set, inject Logger into any component that
// needs to log, and let Run resolve the flag into the level after parsing.
//
// The logger exists before flags are parsed (its level defaults to INFO, slog's
// own default), so it can be handed to the command tree up front; Run adjusts the
// LevelVar afterwards. This is why the level lives behind a slog.LevelVar rather
// than being baked into the handler.
//
// See ADR-0009: logging is a channel distinct from stdout (results) and stderr
// (error reports); it is written for the developer or agent and filtered by level.
type Logging struct {
	// Logger is the configured logger. Pass it to components that log; do not
	// have them reach for the package-level slog functions.
	Logger *slog.Logger

	level *slog.LevelVar
	name  *string
}

// NewLogging registers the shared --logging-level flag on fs and returns a
// Logging whose Logger writes to stderr. The level is INFO until Apply resolves
// the parsed flag. With ff's env-var binding the flag is also settable as
// <PREFIX>_LOGGING_LEVEL (e.g. ECDB_LOGGING_LEVEL); an explicit flag wins over
// the environment.
func NewLogging(fs *ff.FlagSet) *Logging {
	level := new(slog.LevelVar) // zero value is LevelInfo
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	name := fs.StringLong("logging-level", "INFO", "minimum log level: DEBUG, INFO, WARN, or ERROR")
	return &Logging{Logger: logger, level: level, name: name}
}

// Apply resolves the parsed --logging-level into the LevelVar and installs the
// logger as slog's process default. Injecting Logger is the intended path; the
// default is a backstop so a call site that was missed still logs through the
// correctly-filtered handler rather than the unfiltered stock default.
//
// It returns an error for an unrecognized level name. ERROR is a floor: no
// accepted name disables it.
func (l *Logging) Apply() error {
	level, err := parseLevel(*l.name)
	if err != nil {
		return err
	}
	l.level.Set(level)
	slog.SetDefault(l.Logger)
	return nil
}

// parseLevel maps a level name (case-insensitive) to a slog.Level. ERROR is the
// highest accepted level, so there is no name that turns ERROR off; the clamp is
// a defensive floor should the accepted set ever grow.
func parseLevel(name string) (slog.Level, error) {
	var level slog.Level
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		return 0, fmt.Errorf("unknown logging level %q (want DEBUG, INFO, WARN, or ERROR)", name)
	}
	if level > slog.LevelError {
		level = slog.LevelError // floor: ERROR can never be turned off
	}
	return level, nil
}
