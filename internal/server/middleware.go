// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// Middleware wraps an http.Handler, returning a handler that runs some logic
// before and/or after the wrapped one. Middlewares compose with chain.
type Middleware func(http.Handler) http.Handler

// chain applies mws to h so the first listed middleware is the outermost wrapper
// (it sees the request first and the response last). chain(h, a, b) yields
// a(b(h)).
func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// requestIDHeader is the request/response header carrying the correlation id.
const requestIDHeader = "X-Request-Id"

type (
	requestIDKey struct{}
	loggerKey    struct{}
)

// requestID returns the correlation id carried in ctx, or "" if none was set.
func requestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// newRequestID returns a short random hex correlation id. It falls back to a
// timestamp-derived value on the (practically impossible) chance crypto/rand
// fails, so a request is never left without an id.
func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "t" + hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000")))
	}
	return hex.EncodeToString(b[:])
}

// withRequestID assigns each request a correlation id — reusing an inbound
// X-Request-Id when the client supplied one, otherwise minting a fresh one — and
// echoes it back in the response header so the client can quote it.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withLogging installs a request-scoped slog.Logger (tagged with the request id)
// on the context and logs one line per request when it completes, recording
// method, path, status, and duration. It must run inside withRequestID so the id
// is available.
func withLogging(base *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqLog := base.With("requestId", requestID(r.Context()))
			ctx := context.WithValue(r.Context(), loggerKey{}, reqLog)
			sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))
			reqLog.InfoContext(ctx, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"bytes", sw.bytes,
				"duration", time.Since(start).String(),
			)
		})
	}
}

// withRecovery converts a panic in a downstream handler into a logged 500 with
// the standard error envelope, so one bad handler cannot take the server down. If
// the response was already partly written the envelope cannot be sent; the panic
// is still logged. http.ErrAbortHandler is passed straight through — net/http
// documents it as a sentinel handlers panic with to abort a connection silently,
// so we neither log it nor try to write an envelope onto the aborted connection.
func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw, _ := w.(*statusRecorder)
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v) // sentinel: let net/http abort the connection
				}
				logger(r).ErrorContext(r.Context(), "handler panic", "panic", v, "path", r.URL.Path)
				if sw != nil && sw.wrote {
					return // headers already sent; can't send an envelope
				}
				writeError(w, r, http.StatusInternalServerError, codeInternal, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps http.ResponseWriter to capture the status code and byte
// count for logging, and to note whether the header has been written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.wrote = true // an implicit 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}
