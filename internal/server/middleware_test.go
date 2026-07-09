// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import "testing"

// TestFallbackRequestID checks that the crypto/rand-failure fallback yields
// distinct, non-empty ids that are valid correlation ids, even when called back
// to back (the atomic counter guarantees uniqueness).
func TestFallbackRequestID(t *testing.T) {
	a := fallbackRequestID()
	b := fallbackRequestID()

	if a == "" || b == "" {
		t.Fatalf("fallbackRequestID returned empty: a=%q b=%q", a, b)
	}
	if a == b {
		t.Fatalf("fallbackRequestID returned duplicate ids: %q", a)
	}
	if !validRequestID(a) {
		t.Errorf("fallbackRequestID %q is not a valid correlation id", a)
	}
	if !validRequestID(b) {
		t.Errorf("fallbackRequestID %q is not a valid correlation id", b)
	}
}
