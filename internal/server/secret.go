// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import "github.com/mdhender/ecv6-api/internal/secret"

// hashSecret and VerifySecret adapt the shared internal/secret package to the
// server's local vocabulary. The hashing implementation lives there (not here) so
// ecdb can seed and verify secrets without importing the HTTP layer.

// hashSecret derives a bcrypt hash of a plaintext secret to store in
// accounts.hashed_secret, at the server's configured cost (Config.SecretCost).
// It is a method, not a free function, so the cost travels with the server: tests
// drop to secret.MinCost while production stays at secret.DefaultCost.
func (s *Server) hashSecret(plaintext string) (string, error) {
	return secret.Hash(plaintext, s.secretCost)
}

// VerifySecret reports whether plaintext matches an encoded bcrypt hash produced
// by hashSecret. The cost is carried in the hash, so verification needs no server
// setting and stays a free function.
func VerifySecret(encoded, plaintext string) bool {
	return secret.Verify(encoded, plaintext)
}
