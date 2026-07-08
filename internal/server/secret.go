// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"crypto/pbkdf2"
)

// Account secrets are hashed with PBKDF2-HMAC-SHA256 (crypto/pbkdf2, standard
// library as of Go 1.24) — a deliberately slow, salted key-derivation function,
// the right tool for a low-entropy human secret. This is stdlib-first (CLAUDE.md)
// and avoids a bcrypt/argon dependency. The stored form is self-describing:
//
//	pbkdf2_sha256$<iterations>$<salt-b64>$<hash-b64>
//
// so the iteration count and salt travel with the hash and can be raised later
// without a schema change (older hashes still verify against their own params).
const (
	// secretScheme tags the encoded hash so VerifySecret can reject anything it
	// does not recognise and a future scheme can coexist during migration.
	secretScheme = "pbkdf2_sha256"
	// secretIterations is the PBKDF2 work factor (OWASP's 2023 guidance for
	// PBKDF2-HMAC-SHA256). Stored per-hash, so raising it only affects new hashes.
	secretIterations = 600_000
	// secretKeyLen is the derived-key length in bytes (256 bits, matching SHA-256).
	secretKeyLen = 32
	// secretSaltLen is the per-secret random salt length in bytes.
	secretSaltLen = 16
)

// HashSecret derives a salted PBKDF2 hash of a plaintext secret, returning the
// self-describing encoded form to store in accounts.hashed_secret. Each call
// draws a fresh random salt, so the same secret hashes differently every time.
func HashSecret(plaintext string) (string, error) {
	salt := make([]byte, secretSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("hash secret: read salt: %w", err)
	}
	dk, err := pbkdf2.Key(sha256.New, plaintext, salt, secretIterations, secretKeyLen)
	if err != nil {
		return "", fmt.Errorf("hash secret: derive key: %w", err)
	}
	return strings.Join([]string{
		secretScheme,
		strconv.Itoa(secretIterations),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(dk),
	}, "$"), nil
}

// VerifySecret reports whether plaintext matches the encoded hash produced by
// HashSecret. It re-derives the key with the salt and iteration count carried in
// the encoded form and compares in constant time. A malformed or unknown-scheme
// hash returns false (never an error): a bad stored value must not authenticate,
// and it must not leak how it is bad.
func VerifySecret(encoded, plaintext string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != secretScheme {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, plaintext, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
