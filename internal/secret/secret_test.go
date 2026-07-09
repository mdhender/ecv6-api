// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package secret

import "testing"

func TestHashVerify(t *testing.T) {
	hashed, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hashed2, _ := Hash("correct horse battery staple"); hashed2 == hashed {
		t.Errorf("two hashes of the same secret are identical; salt not applied")
	}
	if !Verify(hashed, "correct horse battery staple") {
		t.Errorf("Verify rejected the correct secret")
	}
	if Verify(hashed, "wrong secret") {
		t.Errorf("Verify accepted a wrong secret")
	}
	if Verify("garbage", "anything") {
		t.Errorf("Verify accepted a malformed hash")
	}
	if Verify("", "anything") {
		t.Errorf("Verify accepted an empty hash")
	}
}
