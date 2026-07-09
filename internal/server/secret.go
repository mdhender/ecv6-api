// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import "github.com/mdhender/ecv6-api/internal/secret"

// HashSecret and VerifySecret adapt the shared internal/secret package to the
// server's local vocabulary. The hashing implementation lives there (not here)
// so ecdb can seed and verify secrets without importing the HTTP layer.

// HashSecret derives a salted hash of a plaintext secret to store in
// accounts.hashed_secret. See secret.Hash.
func HashSecret(plaintext string) (string, error) {
	return secret.Hash(plaintext)
}

// VerifySecret reports whether plaintext matches an encoded hash produced by
// HashSecret. See secret.Verify.
func VerifySecret(encoded, plaintext string) bool {
	return secret.Verify(encoded, plaintext)
}
