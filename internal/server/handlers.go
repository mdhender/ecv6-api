// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"net/http"

	"github.com/mdhender/ecv6-api/internal/api"
)

// handleHealth serves GET /api/healthz (openapi.yaml: getHealth). It is a
// liveness probe: it reports the running application version and does not touch
// the database, so it stays green even if the store is degraded.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, api.HealthResponse{
		Status:  "ok",
		Version: s.version,
	})
}

// handleVersion serves GET /api/version (openapi.yaml: getVersion). It reports
// the application version and the open database's schema version (SQLite
// user_version). A failure to read the schema version is a 500 in the standard
// envelope.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	schema, err := s.db.SchemaVersion(r.Context())
	if err != nil {
		logger(r).ErrorContext(r.Context(), "version: read schema version", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not read database version")
		return
	}
	writeJSON(w, r, http.StatusOK, api.VersionResponse{
		Application: s.version,
		Database: api.DatabaseVersion{
			SchemaVersion: int32(schema),
		},
	})
}
