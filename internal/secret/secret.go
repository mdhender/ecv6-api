// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package secret hashes and verifies account secrets. It lives outside the HTTP
// server so that any binary needing to seed or check a secret — the server and
// ecdb alike — can depend on it without pulling in the web layer.
//
// Secrets are hashed with bcrypt (golang.org/x/crypto/bcrypt). The encoded hash
// is self-describing — it carries the algorithm, cost, and salt — so Verify needs
// nothing but the stored string. bcrypt hashes at most the first 72 bytes of the
// input and rejects longer secrets outright (ErrPasswordTooLong).
package secret

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// cost is the bcrypt work factor. We use bcrypt.MinCost deliberately: EC is alpha
// and account secrets are low-value, so the cheapest hash keeps account creation
// and login fast. Raise this before the data becomes worth protecting.
const cost = bcrypt.MinCost

// Hash returns a bcrypt hash of the plaintext secret, in the self-describing
// encoded form to store in accounts.hashed_secret. Each call uses a fresh random
// salt, so the same secret hashes differently every time. A secret longer than 72
// bytes returns an error (bcrypt.ErrPasswordTooLong).
func Hash(plaintext string) (string, error) {
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
