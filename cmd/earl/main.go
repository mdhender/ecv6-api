// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command earl is a thin command-line client for the EC API server. It mirrors
// the REST surface directly — the verb and path you would send become the
// command line:
//
//	earl get /healthz
//	earl get /me
//	earl post /accounts -d '{"email":"t@x.com","secret":"hunter2hunter2"}'
//	earl patch /games/1 -d @game.json
//
// so it covers the whole API without per-endpoint code and stays correct as
// endpoints are added. Three commands are not plain HTTP: login exchanges
// credentials for a bearer token and saves it, logout revokes and forgets it,
// and whoami is sugar for `get /me`.
//
// Tokens are saved in ~/.config/earl/<env>/tokens.json, keyed by server base URL
// and account email, so earl can hold several identities (e.g. an admin and an
// impersonated user) at once; --email / EARL_EMAIL selects between them. The
// <env> segment is EARL_ENV (default "development"), so runs against different
// environments — e.g. claude and development — never share a token file;
// EARL_TOKENS overrides the path entirely.
//
// Configuration follows the same ff + EARL_ env-prefix convention as ec and
// ecdb: --base-url (EARL_BASE_URL), --email (EARL_EMAIL), --secret (EARL_SECRET),
// and EARL_ENV selecting both the .env files loaded at startup and the token
// file's environment segment.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mdhender/ecv6-api/internal/cli"
	"github.com/peterbourgon/ff/v4"
)

// defaultBaseURL is the API root earl talks to when EARL_BASE_URL / --base-url is
// unset: a local server on the default listen address, under the /api base path.
const defaultBaseURL = "http://localhost:8080/api"

func main() {
	if err := cli.LoadEnv("EARL"); err != nil {
		fmt.Fprintf(os.Stderr, "earl: %v\n", err)
		os.Exit(1)
	}
	cmd, logging := newRootCommand()
	// ff (like the standard flag package) stops parsing flags at the first
	// positional argument, so `earl post /accounts -d '{...}'` would treat -d as a
	// stray argument. Hoist each subcommand's flags ahead of its path before ff
	// sees them, so flags may follow the path on the command line.
	os.Exit(cli.Run(context.Background(), cmd, "EARL", reorderArgs(os.Args[1:]), logging))
}

// valueFlags is the set of earl flags that consume the following argument as
// their value (as opposed to boolean flags like --no-auth). reorderArgs needs it
// so a flag's value is never mistaken for the positional path. Keep it in sync
// with the flags defined in newRootCommand; reorder_test guards against drift.
var valueFlags = map[string]bool{
	"-d":              true,
	"--data":          true,
	"--email":         true,
	"--secret":        true,
	"--base-url":      true,
	"--logging-level": true,
}

// reorderArgs rewrites a command line so each subcommand's flags precede its
// positional path, letting `earl post /accounts -d '{...}'` parse under ff.
//
// It is section-aware: the leading root flags and the subcommand name keep their
// place (ff routes on the subcommand name, which must stay the first positional),
// and only the tokens after the subcommand name are hoisted — flags (with the
// values they consume) first, positionals last. A literal "--" ends flag
// processing, matching ff.
func reorderArgs(args []string) []string {
	out := make([]string, 0, len(args))

	// Root section: copy root flags (and any values they take) through, up to and
	// including the subcommand name (the first positional).
	i := 0
	for i < len(args) {
		tok := args[i]
		if tok == "--" {
			return append(out, args[i:]...)
		}
		if isFlag(tok) {
			out = append(out, tok)
			i++
			if takesValue(tok) && i < len(args) {
				out = append(out, args[i])
				i++
			}
			continue
		}
		// Subcommand name.
		out = append(out, tok)
		i++
		break
	}

	// Subcommand section: hoist flags ahead of positionals.
	var flags, positionals []string
	for i < len(args) {
		tok := args[i]
		if tok == "--" {
			positionals = append(positionals, args[i:]...)
			break
		}
		if isFlag(tok) {
			flags = append(flags, tok)
			i++
			if takesValue(tok) && i < len(args) {
				flags = append(flags, args[i])
				i++
			}
			continue
		}
		positionals = append(positionals, tok)
		i++
	}
	out = append(out, flags...)
	return append(out, positionals...)
}

// isFlag reports whether tok is a flag token ("-x" or "--long"), as opposed to a
// positional. A bare "-" is a positional; "--" is handled by the caller.
func isFlag(tok string) bool {
	return len(tok) >= 2 && tok[0] == '-' && tok != "--"
}

// takesValue reports whether a flag token consumes the next argument as its
// value. A flag written with "=" (e.g. --data={...}) carries its own value and
// consumes nothing more.
func takesValue(tok string) bool {
	if strings.Contains(tok, "=") {
		return false
	}
	return valueFlags[tok]
}

// newRootCommand builds the earl command tree and shared logging, mirroring the
// ec/ecdb bootstrap. Root flags (base URL, email) are defined once and inherited
// by every subcommand through SetParent.
func newRootCommand() (*ff.Command, *cli.Logging) {
	rootFlags := ff.NewFlagSet("earl")
	logging := cli.NewLogging(rootFlags)
	log := logging.Logger
	baseURL := rootFlags.StringLong("base-url", defaultBaseURL, "API base URL; or set EARL_BASE_URL")
	email := rootFlags.StringLong("email", "", "account email selecting the saved token; or set EARL_EMAIL")

	rootCmd := &ff.Command{
		Name:      "earl",
		Usage:     "earl [FLAGS] COMMAND ...",
		ShortHelp: "command-line client for the Epimethean Challenge API",
		Flags:     rootFlags,
	}

	// newEarl builds the client from the resolved flags. Deferring construction to
	// each Exec keeps the flag pointers authoritative after parsing.
	newEarl := func() *earl {
		return &earl{
			baseURL: strings.TrimRight(*baseURL, "/"),
			email:   *email,
			log:     log,
			http:    &http.Client{Timeout: 30 * time.Second},
			out:     os.Stdout,
			errOut:  os.Stderr,
		}
	}

	// verbCmd builds a get/delete-style command: a positional PATH, no body.
	verbCmd := func(name, method string) *ff.Command {
		fs := ff.NewFlagSet(name).SetParent(rootFlags)
		noAuth := fs.BoolLong("no-auth", "do not attach a saved token")
		return &ff.Command{
			Name:      name,
			Usage:     "earl " + name + " [FLAGS] PATH",
			ShortHelp: method + " the given API path",
			Flags:     fs,
			Exec: func(ctx context.Context, args []string) error {
				path, err := pathArg(name, args)
				if err != nil {
					return err
				}
				return newEarl().request(ctx, method, path, nil, *noAuth)
			},
		}
	}

	// bodyCmd builds a post/patch-style command: a positional PATH plus an
	// optional -d body (inline JSON, @file, or @- for stdin).
	bodyCmd := func(name, method string) *ff.Command {
		fs := ff.NewFlagSet(name).SetParent(rootFlags)
		data := fs.String('d', "data", "", "request body: inline JSON, @file, or @- for stdin")
		noAuth := fs.BoolLong("no-auth", "do not attach a saved token")
		return &ff.Command{
			Name:      name,
			Usage:     "earl " + name + " [FLAGS] PATH",
			ShortHelp: method + " the given API path",
			Flags:     fs,
			Exec: func(ctx context.Context, args []string) error {
				path, err := pathArg(name, args)
				if err != nil {
					return err
				}
				body, err := readBody(*data)
				if err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
				return newEarl().request(ctx, method, path, body, *noAuth)
			},
		}
	}

	loginFlags := ff.NewFlagSet("login").SetParent(rootFlags)
	loginSecret := loginFlags.StringLong("secret", "", "account secret; or set EARL_SECRET")
	loginCmd := &ff.Command{
		Name:      "login",
		Usage:     "earl login [--email EMAIL] [--secret SECRET]",
		ShortHelp: "exchange credentials for a bearer token and save it",
		Flags:     loginFlags,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("login takes no positional arguments")
			}
			return newEarl().login(ctx, *loginSecret)
		},
	}

	logoutFlags := ff.NewFlagSet("logout").SetParent(rootFlags)
	logoutAll := logoutFlags.BoolLong("all", "revoke every session for the account, not just this token")
	logoutCmd := &ff.Command{
		Name:      "logout",
		Usage:     "earl logout [--all] [--email EMAIL]",
		ShortHelp: "revoke the saved token (or all sessions) and forget it",
		Flags:     logoutFlags,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("logout takes no positional arguments")
			}
			return newEarl().logout(ctx, *logoutAll)
		},
	}

	impersonateFlags := ff.NewFlagSet("impersonate").SetParent(rootFlags)
	impersonateCmd := &ff.Command{
		Name:      "impersonate",
		Usage:     "earl impersonate ACCOUNT_ID",
		ShortHelp: "mint and save a bearer token for another account (admin)",
		Flags:     impersonateFlags,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("impersonate requires exactly one ACCOUNT_ID argument")
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id < 1 {
				return fmt.Errorf("impersonate: ACCOUNT_ID must be a positive integer, got %q", args[0])
			}
			return newEarl().impersonate(ctx, id)
		},
	}

	whoamiFlags := ff.NewFlagSet("whoami").SetParent(rootFlags)
	whoamiCmd := &ff.Command{
		Name:      "whoami",
		Usage:     "earl whoami",
		ShortHelp: "show the current account (GET /me)",
		Flags:     whoamiFlags,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("whoami takes no positional arguments")
			}
			return newEarl().request(ctx, http.MethodGet, "/me", nil, false)
		},
	}

	rootCmd.Subcommands = append(rootCmd.Subcommands,
		verbCmd("get", http.MethodGet),
		bodyCmd("post", http.MethodPost),
		bodyCmd("patch", http.MethodPatch),
		bodyCmd("delete", http.MethodDelete),
		loginCmd, logoutCmd, impersonateCmd, whoamiCmd,
	)
	return rootCmd, logging
}

// pathArg returns the single PATH positional for a verb command, or an error
// naming the command.
func pathArg(cmd string, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s requires exactly one PATH argument", cmd)
	}
	return args[0], nil
}
