// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"slices"
	"testing"

	"github.com/peterbourgon/ff/v4"
)

func TestReorderArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"verb only", []string{"get", "/healthz"}, []string{"get", "/healthz"}},
		{"body flag after path", []string{"post", "/accounts", "-d", "{}"}, []string{"post", "-d", "{}", "/accounts"}},
		{"body flag already first", []string{"post", "-d", "{}", "/accounts"}, []string{"post", "-d", "{}", "/accounts"}},
		{"long data after path", []string{"patch", "/games/1", "--data", "@f.json"}, []string{"patch", "--data", "@f.json", "/games/1"}},
		{"bool flag after path", []string{"get", "/me", "--no-auth"}, []string{"get", "--no-auth", "/me"}},
		{"root flag preserved", []string{"--base-url", "http://x/api", "get", "/me"}, []string{"--base-url", "http://x/api", "get", "/me"}},
		{"root email then verb", []string{"--email", "p@x.com", "get", "/me"}, []string{"--email", "p@x.com", "get", "/me"}},
		{"secret value not treated as positional", []string{"login", "--secret", "s3cret"}, []string{"login", "--secret", "s3cret"}},
		{"equals form consumes nothing", []string{"post", "/x", "--data={}"}, []string{"post", "--data={}", "/x"}},
		{"double dash ends flags", []string{"get", "/me", "--", "--weird"}, []string{"get", "/me", "--", "--weird"}},
		{"empty", nil, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reorderArgs(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("reorderArgs(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestValueFlagsCoverTree guards against drift: every value-taking flag in the
// command tree (identified by a non-empty help placeholder) must be registered
// in valueFlags, or reorderArgs would mistake its value for the positional path.
func TestValueFlagsCoverTree(t *testing.T) {
	cmd, _ := newRootCommand()
	check := func(flags ff.Flags) {
		_ = flags.WalkFlags(func(f ff.Flag) error {
			if f.GetPlaceholder() == "" {
				return nil // boolean flag: consumes no value
			}
			var names []string
			if l, ok := f.GetLongName(); ok {
				names = append(names, "--"+l)
			}
			if s, ok := f.GetShortName(); ok {
				names = append(names, "-"+string(s))
			}
			for _, n := range names {
				if valueFlags[n] {
					return nil
				}
			}
			t.Errorf("value flag %v is not in valueFlags; reorderArgs would mishandle its value", names)
			return nil
		})
	}
	check(cmd.Flags)
	for _, sc := range cmd.Subcommands {
		check(sc.Flags)
	}
}
