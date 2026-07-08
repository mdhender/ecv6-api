// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/mdhender/ecv6-api/internal/api"
)

// Stable, machine-readable error codes. These are the wire contract's `code`
// values (doc/api/conventions.md); messages are for humans and may change, codes
// may not. New codes are appended as endpoints land.
const (
	codeInternal     = "internal_error"
	codeNotFound     = "not_found"
	codeUnauthorized = "unauthorized"
	codeForbidden    = "forbidden"
	codeBadRequest   = "bad_request"
)

// writeJSON renders v as an indented JSON body with the given status code and the
// application/json content type. An encoding failure is logged and left to the
// (already-sent) status; the header is written before Encode so a partial body
// cannot change the status.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		logger(r).ErrorContext(r.Context(), "response: encode body", "err", err)
	}
}

// writeError renders the standard error envelope (doc/api/conventions.md):
//
//	{ "error": { "code": ..., "message": ..., "requestId": ... } }
//
// The request id is taken from the context so a client can quote it and an
// operator can find the matching log line. code is a stable machine value;
// message is human-facing.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	var body api.Error
	body.Error.Code = code
	body.Error.Message = message
	if id := requestID(r.Context()); id != "" {
		body.Error.RequestId = &id
	}
	writeJSON(w, r, status, body)
}

// maxRequestBody caps a decoded JSON request body (1 MiB) so a malicious or
// broken client cannot exhaust memory. No application-domain request is remotely
// this large.
const maxRequestBody = 1 << 20

// decodeJSON reads the request body into v as JSON. On success it returns true;
// on a malformed or oversized body it writes the standard 400 envelope and
// returns false, so callers can `if !decodeJSON(...) { return }`.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "request body is not valid JSON")
		return false
	}
	return true
}

// logger returns the request-scoped logger if one was installed by the logging
// middleware, otherwise slog's default. It never returns nil.
func logger(r *http.Request) *slog.Logger {
	if l, ok := r.Context().Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
