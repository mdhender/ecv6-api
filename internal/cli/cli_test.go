// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/peterbourgon/ff/v4"
)

// newTestCmd builds a root command with an "ok" subcommand and a "boom"
// subcommand, mirroring the shape ecdb and ec use.
func newTestCmd() *ff.Command {
	rootFlags := ff.NewFlagSet("t")
	root := &ff.Command{Name: "t", Flags: rootFlags}
	root.Subcommands = []*ff.Command{
		{
			Name:  "ok",
			Flags: ff.NewFlagSet("ok").SetParent(rootFlags),
			Exec:  func(context.Context, []string) error { return nil },
		},
		{
			Name:  "boom",
			Flags: ff.NewFlagSet("boom").SetParent(rootFlags),
			Exec:  func(context.Context, []string) error { return errors.New("kaboom") },
		},
	}
	return root
}

func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"ok"}, 0},
		{"help", []string{"-h"}, 0},
		{"no subcommand is a usage error", nil, 2},
		{"exec error", []string{"boom"}, 1},
		{"unknown flag", []string{"--nope"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A fresh command per run: ff commands retain parse state.
			if got := Run(context.Background(), newTestCmd(), "T", tt.args, nil); got != tt.want {
				t.Errorf("Run(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}
