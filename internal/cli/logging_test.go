// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package cli

import (
	"context"
	"log/slog"
	"testing"

	"github.com/peterbourgon/ff/v4"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		want    slog.Level
		wantErr bool
	}{
		{"DEBUG", slog.LevelDebug, false},
		{"debug", slog.LevelDebug, false},
		{"Info", slog.LevelInfo, false},
		{"  warn  ", slog.LevelWarn, false},
		{"WARNING", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"", 0, true},
		{"trace", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLevel(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseLevel(%q) = %v, want error", tt.name, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLevel(%q) unexpected error: %v", tt.name, err)
			}
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// newLoggingCmd mirrors newTestCmd but wires the shared logging flag onto the
// root flag set, returning the Logging so a test can inspect the resolved level.
func newLoggingCmd() (*ff.Command, *Logging) {
	rootFlags := ff.NewFlagSet("t")
	logging := NewLogging(rootFlags)
	root := &ff.Command{Name: "t", Flags: rootFlags}
	root.Subcommands = []*ff.Command{
		{
			Name:  "ok",
			Flags: ff.NewFlagSet("ok").SetParent(rootFlags),
			Exec:  func(context.Context, []string) error { return nil },
		},
	}
	return root, logging
}

// assertEnabled checks whether the logger logs at the given level.
func assertEnabled(t *testing.T, l *Logging, level slog.Level, want bool) {
	t.Helper()
	if got := l.Logger.Enabled(context.Background(), level); got != want {
		t.Errorf("Enabled(%v) = %v, want %v", level, got, want)
	}
}

func TestLoggingDefaultIsInfo(t *testing.T) {
	cmd, logging := newLoggingCmd()
	if code := Run(context.Background(), cmd, "T", []string{"ok"}, logging); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	assertEnabled(t, logging, slog.LevelInfo, true)
	assertEnabled(t, logging, slog.LevelDebug, false)
}

func TestLoggingLevelFromFlag(t *testing.T) {
	cmd, logging := newLoggingCmd()
	if code := Run(context.Background(), cmd, "T", []string{"--logging-level", "debug", "ok"}, logging); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	assertEnabled(t, logging, slog.LevelDebug, true)
}

func TestLoggingLevelFromEnv(t *testing.T) {
	t.Setenv("T_LOGGING_LEVEL", "WARN")
	cmd, logging := newLoggingCmd()
	if code := Run(context.Background(), cmd, "T", []string{"ok"}, logging); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	assertEnabled(t, logging, slog.LevelWarn, true)
	assertEnabled(t, logging, slog.LevelInfo, false)
}

func TestLoggingFlagBeatsEnv(t *testing.T) {
	t.Setenv("T_LOGGING_LEVEL", "WARN")
	cmd, logging := newLoggingCmd()
	if code := Run(context.Background(), cmd, "T", []string{"--logging-level", "debug", "ok"}, logging); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	assertEnabled(t, logging, slog.LevelDebug, true)
}

// TestLoggingErrorFloor confirms ERROR is always enabled and lower levels can be
// silenced — there is no accepted level that disables ERROR.
func TestLoggingErrorFloor(t *testing.T) {
	cmd, logging := newLoggingCmd()
	if code := Run(context.Background(), cmd, "T", []string{"--logging-level", "error", "ok"}, logging); code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	assertEnabled(t, logging, slog.LevelError, true)
	assertEnabled(t, logging, slog.LevelWarn, false)
}

func TestLoggingUnknownLevelIsUsageError(t *testing.T) {
	cmd, logging := newLoggingCmd()
	if code := Run(context.Background(), cmd, "T", []string{"--logging-level", "bogus", "ok"}, logging); code != 1 {
		t.Fatalf("Run = %d, want 1", code)
	}
}
