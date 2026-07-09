// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// identity is one saved bearer token: the credential earl attaches, plus the
// type and expiry the server returned at login. The plaintext token is stored
// (there is no server-side lookup by anything else), so the file is written 0600.
type identity struct {
	Token     string    `json:"token"`
	TokenType string    `json:"tokenType"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// tokenStore maps a server base URL to the identities logged in against it,
// keyed by (lowercased) account email. Keying by email lets earl hold several
// accounts' tokens for one server at once — e.g. an admin and an impersonated
// user — and pick between them with --email / EARL_EMAIL.
type tokenStore map[string]map[string]identity

// tokensPath returns the path to tokens.json: EARL_TOKENS if set (an explicit
// full-path override, also handy for tests), else the env-scoped default
// $XDG_CONFIG_HOME/earl/<env>/tokens.json, else ~/.config/earl/<env>/tokens.json.
// The <env> segment (from earlEnv) segregates state by EARL_ENV so, e.g., a
// claude run and a development run never share a token file. It deliberately does
// not use os.UserConfigDir, which resolves to ~/Library/Application Support on
// macOS.
func tokensPath() (string, error) {
	if p := os.Getenv("EARL_TOKENS"); p != "" {
		return p, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "earl", earlEnv(), "tokens.json"), nil
}

// earlEnv reports the runtime environment that scopes earl's state, read from
// EARL_ENV and defaulting to "development" when unset — mirroring cli.LoadEnv.
// main aborts at startup for any value outside the accepted set (via cli.LoadEnv
// -> dotenv.Load), so callers can treat the result as a valid, safe path segment.
func earlEnv() string {
	if env := os.Getenv("EARL_ENV"); env != "" {
		return env
	}
	return "development"
}

// loadTokens reads the token store from path. A missing file is not an error —
// it yields an empty store, so the first login has somewhere to write.
func loadTokens(path string) (tokenStore, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return tokenStore{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	store := tokenStore{}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return store, nil
}

// saveTokens writes the store to path, creating the directory (0700) if needed
// and the file 0600 so the bearer tokens are not world-readable. It writes to a
// temp file in the same directory and renames, so a crash cannot leave a
// half-written token file.
func saveTokens(path string, store tokenStore) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tokens: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".tokens-*.json")
	if err != nil {
		return fmt.Errorf("create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp token file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp token file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp token file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install token file %s: %w", path, err)
	}
	return nil
}

// put records id for (baseURL, email), replacing any existing token for that
// account on that server. Email is lowercased to match the server's canonical
// form and how resolve looks it up.
func (s tokenStore) put(baseURL, email string, id identity) {
	perServer := s[baseURL]
	if perServer == nil {
		perServer = map[string]identity{}
		s[baseURL] = perServer
	}
	perServer[strings.ToLower(email)] = id
}

// drop removes the token for (baseURL, email), and the server entry if it was
// the last one. It reports whether anything was removed.
func (s tokenStore) drop(baseURL, email string) bool {
	perServer := s[baseURL]
	if perServer == nil {
		return false
	}
	key := strings.ToLower(email)
	if _, ok := perServer[key]; !ok {
		return false
	}
	delete(perServer, key)
	if len(perServer) == 0 {
		delete(s, baseURL)
	}
	return true
}

// emails returns the account emails with a saved token for baseURL, for error
// messages that list the caller's choices.
func (s tokenStore) emails(baseURL string) []string {
	perServer := s[baseURL]
	out := make([]string, 0, len(perServer))
	for e := range perServer {
		out = append(out, e)
	}
	return out
}

// resolve picks the active token for a verb request against baseURL, given the
// requested email (from --email / EARL_EMAIL, possibly empty). It never errors:
// a request with no usable token is sent unauthenticated and the server decides
// (public routes succeed; protected ones return 401). It returns the resolved
// email alongside the token so callers can report which identity was used.
//
// Resolution: an explicit email selects that account's token; with no email and
// exactly one saved identity, that one is used; otherwise (none, or ambiguous)
// no token is attached.
func (s tokenStore) resolve(baseURL, email string) (string, string) {
	perServer := s[baseURL]
	if len(perServer) == 0 {
		return "", ""
	}
	if email != "" {
		key := strings.ToLower(email)
		return key, perServer[key].Token
	}
	if len(perServer) == 1 {
		for e, id := range perServer {
			return e, id.Token
		}
	}
	return "", ""
}
