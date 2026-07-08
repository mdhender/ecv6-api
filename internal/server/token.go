// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// A session has two independent opaque strings, both drawn from crypto/rand:
//
//   - the token, a bearer credential the client presents; only its SHA-256 hash
//     is stored (accounts never hold a reversible token), and it is shown to the
//     client exactly once, at login (ADR-0002);
//   - the id, the session's public handle used in /me/sessions URLs, which is not
//     a credential and can be listed freely.
//
// The token is high-entropy (256 bits), so a fast hash (SHA-256) is the correct
// choice for it — unlike an account secret, there is nothing to brute-force, and
// resolving a bearer credential on every request must be cheap. This is why the
// token uses SHA-256 while the secret uses PBKDF2.
const (
	// tokenBytes is the token's entropy in bytes (256 bits).
	tokenBytes = 32
	// sessionIDBytes is the public session id's entropy in bytes (128 bits).
	sessionIDBytes = 16
)

// newToken mints a fresh opaque session token — the raw bearer credential shown
// to the client once — as a URL-safe base64 string.
func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("new token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA-256 of a raw token — the form stored in
// sessions.hashed_token and looked up on each authenticated request. Hashing is
// deterministic, so the same token always maps to the same stored value.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newSessionID mints a fresh opaque public session id (not a credential).
func newSessionID() (string, error) {
	b := make([]byte, sessionIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("new session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
