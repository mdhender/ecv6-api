// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package secret hashes and verifies account secrets. It lives outside the HTTP
// server so that any binary needing to seed or check a secret — the server and
// ecdb alike — can depend on it without pulling in the web layer.
//
// Secrets are hashed with bcrypt (golang.org/x/crypto/bcrypt). The encoded hash
// is self-describing — it carries the algorithm, cost, and salt — so Verify needs
// nothing but the stored string. bcrypt hashes at most the first 72 bytes of the
// input and rejects longer secrets outright (ErrPasswordTooLong).
//
// The bcrypt cost is a caller-supplied parameter, not a package constant, so each
// caller can trade speed for strength: production picks DefaultCost, tests drop to
// MinCost to stay fast. Verify needs no cost — bcrypt reads it back from the hash.
package secret

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Cost bounds, re-exported from bcrypt so callers can set and clamp a cost
// without importing golang.org/x/crypto/bcrypt themselves.
const (
	// MinCost is the cheapest (weakest) bcrypt cost — for tests, never production.
	MinCost = bcrypt.MinCost
	// DefaultCost is bcrypt's recommended cost, the right choice for production.
	DefaultCost = bcrypt.DefaultCost
	// MaxCost is the most expensive (strongest, slowest) bcrypt cost.
	MaxCost = bcrypt.MaxCost
)

// Hash returns a bcrypt hash of the plaintext secret at the given cost, in the
// self-describing encoded form to store in accounts.hashed_secret. Each call uses
// a fresh random salt, so the same secret hashes differently every time. bcrypt
// silently raises a cost below MinCost to DefaultCost; a cost above MaxCost, or a
// secret longer than 72 bytes, returns an error.
func Hash(plaintext string, cost int) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plaintext), cost)
	if err != nil {
		return "", fmt.Errorf("hash secret: %w", err)
	}
	return string(b), nil
}

// Verify reports whether plaintext matches the encoded bcrypt hash produced by
// Hash. A malformed or non-matching hash returns false (never an error): a bad
// stored value must not authenticate, and it must not leak how it is bad.
func Verify(encoded, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(plaintext)) == nil
}
