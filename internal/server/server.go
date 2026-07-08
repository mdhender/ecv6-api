// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package server is the application-domain HTTP server: it wires the
// standard-library net/http router (ADR-0011), the cross-cutting middleware
// (request id, request logging, panic recovery), and the standard error
// envelope (doc/api/conventions.md), and serves the routes under the base path
// /api (ADR-0006).
//
// This is the skeleton: only the public System endpoints — GET /healthz and
// GET /version — are served. The authenticated and admin route groups exist so
// later work (auth, accounts, games) plugs into a settled structure. Wire DTOs
// come from the generated internal/api package (spec-first; ADR-0006).
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/mdhender/ecv6-api/internal/store"
)

// BasePath is the unversioned base path every route hangs under (ADR-0006).
const BasePath = "/api"

// Config holds the server's runtime settings.
type Config struct {
	// Addr is the TCP listen address, e.g. ":8080".
	Addr string
	// DevMode enables development-only affordances (e.g. the admin shutdown
	// route, added later). It changes no behavior in the skeleton beyond being
	// reported to the log.
	DevMode bool
}

// Server serves the application API over net/http. Construct it with New and run
// it with Run.
type Server struct {
	cfg     Config
	db      *store.DB
	log     *slog.Logger
	version string
}

// New builds a Server. db is an already-open store (cmd/ec opens it; the server
// never creates one). version is the application version string reported by
// GET /version. logger may be nil, in which case slog's default is used.
func New(cfg Config, db *store.DB, logger *slog.Logger, version string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, db: db, log: logger, version: version}
}

// Handler builds the routed http.Handler: an http.ServeMux whose routes are
// grouped into public, authenticated, and admin, all wrapped in the base
// middleware. Every route is registered under BasePath.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	public := &group{mux: mux}
	// authenticated and admin add credential checks on top of the base chain:
	// requireAuth resolves the bearer token to an account (ADR-0002) and requireAdmin
	// gates on the application role. Any route registered on these groups is
	// authenticated (and, for admin, role-checked) by construction.
	authed := &group{mux: mux, extra: []Middleware{s.requireAuth}}
	admin := &group{mux: mux, extra: []Middleware{s.requireAuth, s.requireAdmin}}

	// Public System endpoints (openapi.yaml: getHealth, getVersion).
	public.handle(http.MethodGet, "/healthz", s.handleHealth)
	public.handle(http.MethodGet, "/version", s.handleVersion)

	// Auth endpoints (openapi.yaml: login, logout). Login is public — it exchanges
	// credentials for a token; logout is authenticated — it revokes the caller's
	// current session (or all of them).
	public.handle(http.MethodPost, "/auth/login", s.handleLogin)
	authed.handle(http.MethodPost, "/auth/logout", s.handleLogout)

	// Self-service account endpoints (openapi.yaml: getMe, updateMe, updateMyEmail,
	// updateMySecret). All authenticated; each returns the caller's own account.
	authed.handle(http.MethodGet, "/me", s.handleGetMe)
	authed.handle(http.MethodPatch, "/me", s.handleUpdateMe)
	authed.handle(http.MethodPost, "/me/email", s.handleUpdateMyEmail)
	authed.handle(http.MethodPost, "/me/secret", s.handleUpdateMySecret)

	// Admin account management (openapi.yaml: listAccounts, createAccount,
	// getAccount, updateAccount). Gated by requireAdmin on the admin group.
	admin.handle(http.MethodGet, "/accounts", s.handleListAccounts)
	admin.handle(http.MethodPost, "/accounts", s.handleCreateAccount)
	admin.handle(http.MethodGet, "/accounts/{accountId}", s.handleGetAccount)
	admin.handle(http.MethodPatch, "/accounts/{accountId}", s.handleUpdateAccount)

	// A catch-all so an unknown path returns the JSON error envelope rather than
	// net/http's plain-text 404.
	mux.HandleFunc("/", s.handleNotFound)

	// Base middleware, outermost first: assign a request id, log the request, and
	// recover panics. Recovery sits inside logging so a recovered panic is still
	// logged with its final (500) status. Applied once around the whole mux so it
	// covers every route, including the catch-all 404.
	return chain(mux, withRequestID, withLogging(s.log), withRecovery)
}

// group registers routes that share the same group-specific middleware, all
// under BasePath. The base middleware (request id, logging, recovery) is applied
// once around the whole mux in Handler, so a group carries only the extra
// middleware layered on top — none for public, credential checks for the others.
type group struct {
	mux   *http.ServeMux
	extra []Middleware
}

// handle registers h for "METHOD BasePath+pattern", wrapping it in the group's
// extra middleware.
func (g *group) handle(method, pattern string, h http.HandlerFunc) {
	g.mux.Handle(method+" "+BasePath+pattern, chain(h, g.extra...))
}

// handleNotFound renders the standard 404 envelope for any unrouted path.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, codeNotFound, "resource not found")
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts it
// down gracefully (draining in-flight requests) via http.Server.Shutdown. It
// returns nil on a clean shutdown, or the first error from serving or shutting
// down.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		s.log.InfoContext(ctx, "server listening", "addr", s.cfg.Addr, "basePath", BasePath, "dev", s.cfg.DevMode)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.log.InfoContext(ctx, "server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	}
}
