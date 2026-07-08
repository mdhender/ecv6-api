// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import "net/http"

// requireAuth is a placeholder for the authenticated route group. Real session
// resolution (opaque bearer tokens, ADR-0002) arrives with the auth work; until
// then it denies every request, so any route mounted on the authenticated or
// admin group is fail-closed. No such routes exist in the skeleton.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication is not yet available")
	})
}

// requireAdmin is a placeholder for the admin route group. It runs after
// requireAuth and will check the resolved account's admin role once auth exists.
// It denies until then.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusUnauthorized, codeUnauthorized, "authentication is not yet available")
	})
}
