// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStoreResolve(t *testing.T) {
	base := "http://localhost:8080/api"
	store := tokenStore{}
	store.put(base, "Penny@Example.com", identity{Token: "pt", TokenType: "Bearer"})

	// Email is matched case-insensitively (stored lowercased).
	if who, tok := store.resolve(base, "penny@example.com"); tok != "pt" || who != "penny@example.com" {
		t.Errorf("resolve(explicit) = (%q,%q), want (penny@example.com, pt)", who, tok)
	}
	// A single identity is the default when no email is given.
	if _, tok := store.resolve(base, ""); tok != "pt" {
		t.Errorf("resolve(single default) token = %q, want pt", tok)
	}
	// Unknown server yields nothing, not a panic.
	if _, tok := store.resolve("http://other/api", ""); tok != "" {
		t.Errorf("resolve(unknown server) = %q, want empty", tok)
	}

	// With two identities and no email, the choice is ambiguous -> no token.
	store.put(base, "tester@example.com", identity{Token: "tt"})
	if _, tok := store.resolve(base, ""); tok != "" {
		t.Errorf("resolve(ambiguous) = %q, want empty", tok)
	}
	if _, tok := store.resolve(base, "tester@example.com"); tok != "tt" {
		t.Errorf("resolve(explicit second) = %q, want tt", tok)
	}
}

func TestTokensPath(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)

	// EARL_TOKENS is a full-path override that wins over everything else.
	t.Setenv("EARL_TOKENS", "/explicit/tokens.json")
	t.Setenv("EARL_ENV", "claude")
	if got, err := tokensPath(); err != nil || got != "/explicit/tokens.json" {
		t.Fatalf("tokensPath(override) = (%q,%v), want (/explicit/tokens.json, nil)", got, err)
	}
	t.Setenv("EARL_TOKENS", "")

	// The env segment follows EARL_ENV, isolating each environment's tokens.
	claude := filepath.Join(cfg, "earl", "claude", "tokens.json")
	t.Setenv("EARL_ENV", "claude")
	if got, err := tokensPath(); err != nil || got != claude {
		t.Fatalf("tokensPath(claude) = (%q,%v), want (%q, nil)", got, err, claude)
	}

	dev := filepath.Join(cfg, "earl", "development", "tokens.json")
	t.Setenv("EARL_ENV", "development")
	if got, err := tokensPath(); err != nil || got != dev {
		t.Fatalf("tokensPath(development) = (%q,%v), want (%q, nil)", got, err, dev)
	}
	if claude == dev {
		t.Fatalf("claude and development share a token path %q", claude)
	}

	// Unset EARL_ENV resolves to development.
	t.Setenv("EARL_ENV", "")
	if got, err := tokensPath(); err != nil || got != dev {
		t.Fatalf("tokensPath(unset) = (%q,%v), want (%q, nil)", got, err, dev)
	}
}

func TestTokenStoreDrop(t *testing.T) {
	base := "http://localhost:8080/api"
	store := tokenStore{}
	store.put(base, "a@x.com", identity{Token: "1"})
	store.put(base, "b@x.com", identity{Token: "2"})

	if !store.drop(base, "A@X.com") {
		t.Errorf("drop existing returned false")
	}
	if store.drop(base, "a@x.com") {
		t.Errorf("drop already-removed returned true")
	}
	if _, tok := store.resolve(base, "b@x.com"); tok != "2" {
		t.Errorf("sibling identity lost after drop")
	}
	// Dropping the last identity removes the server entry entirely.
	store.drop(base, "b@x.com")
	if _, ok := store[base]; ok {
		t.Errorf("empty server entry not pruned")
	}
}

func TestTokenStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "tokens.json")
	base := "http://localhost:8080/api"

	store := tokenStore{}
	exp := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	store.put(base, "penny@example.com", identity{Token: "tok", TokenType: "Bearer", ExpiresAt: exp})
	if err := saveTokens(path, store); err != nil {
		t.Fatalf("saveTokens: %v", err)
	}

	// The file must not be world-readable — it holds bearer tokens.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}

	got, err := loadTokens(path)
	if err != nil {
		t.Fatalf("loadTokens: %v", err)
	}
	id, ok := got[base]["penny@example.com"]
	if !ok {
		t.Fatalf("identity not round-tripped")
	}
	if id.Token != "tok" || !id.ExpiresAt.Equal(exp) {
		t.Errorf("round-tripped identity = %+v, want token=tok exp=%v", id, exp)
	}

	// A missing file loads as an empty store, not an error.
	empty, err := loadTokens(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatalf("loadTokens(missing): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("missing file yielded %d entries, want 0", len(empty))
	}
}
