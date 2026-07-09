// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mdhender/ecv6-api/internal/secret"
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
	// SecretCost is the bcrypt cost used to hash account secrets. Zero means
	// secret.DefaultCost — the secure production default — so the zero-value Config
	// is safe. Tests set it to secret.MinCost to keep hashing fast.
	SecretCost int
}

// Server serves the application API over net/http. Construct it with New and run
// it with Run.
type Server struct {
	cfg     Config
	db      *store.DB
	log     *slog.Logger
	version string

	// secretCost is the resolved bcrypt cost (Config.SecretCost, defaulted to
	// secret.DefaultCost) used by hashSecret.
	secretCost int
	// decoySecretHash is a valid bcrypt hash at secretCost, verified against on a
	// login for an unknown account so timing matches a real check (see handlers.go).
	decoySecretHash string

	// shutdown is closed once to request the same graceful drain as an interrupt
	// signal; Run selects on it alongside the run context. shutdownOnce guards the
	// close so a duplicate trigger (e.g. two admin requests) is a safe no-op.
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// New builds a Server. db is an already-open store (cmd/ec opens it; the server
// never creates one). version is the application version string reported by
// GET /version. logger may be nil, in which case slog's default is used.
func New(cfg Config, db *store.DB, logger *slog.Logger, version string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	// A zero cost means "unset": use the secure production default. The decoy hash
	// is computed once here at the same cost, so an unknown-account login does the
	// same bcrypt work as a real one (equal timing). An error leaves the decoy
	// empty, which still fails to verify — it only weakens the timing guarantee.
	cost := cfg.SecretCost
	if cost == 0 {
		cost = secret.DefaultCost
	}
	decoy, _ := secret.Hash(loginDecoySecret, cost)
	return &Server{
		cfg:             cfg,
		db:              db,
		log:             logger,
		version:         version,
		secretCost:      cost,
		decoySecretHash: decoy,
		shutdown:        make(chan struct{}),
	}
}

// triggerShutdown requests a graceful shutdown, waking Run to drain in-flight
// requests and stop — the same path an interrupt signal takes. It is safe to call
// more than once; only the first call has any effect.
func (s *Server) triggerShutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdown) })
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

	// The caller's own sessions (openapi.yaml: listMySessions, revokeMySession).
	// Authenticated and scoped to the caller: listing marks the current session,
	// and only the caller's own sessions are revocable (others are 404).
	authed.handle(http.MethodGet, "/me/sessions", s.handleListMySessions)
	authed.handle(http.MethodDelete, "/me/sessions/{sessionId}", s.handleRevokeMySession)

	// The caller's own game memberships (openapi.yaml: listMyGames). Authenticated,
	// scoped to the caller; a game-scoped read that stays off the account projection
	// served by /me (ADR-0004).
	authed.handle(http.MethodGet, "/me/games", s.handleListMyGames)

	// Admin account management (openapi.yaml: listAccounts, createAccount,
	// getAccount, updateAccount). Gated by requireAdmin on the admin group.
	admin.handle(http.MethodGet, "/accounts", s.handleListAccounts)
	admin.handle(http.MethodPost, "/accounts", s.handleCreateAccount)
	admin.handle(http.MethodGet, "/accounts/{accountId}", s.handleGetAccount)
	admin.handle(http.MethodPatch, "/accounts/{accountId}", s.handleUpdateAccount)

	// Admin session management (openapi.yaml: listAccountSessions,
	// revokeAccountSessions, revokeAccountSession). Gated by requireAdmin: list or
	// force-logout a compromised or deactivated account's sessions, wholesale or one
	// at a time.
	admin.handle(http.MethodGet, "/accounts/{accountId}/sessions", s.handleListAccountSessions)
	admin.handle(http.MethodDelete, "/accounts/{accountId}/sessions", s.handleRevokeAccountSessions)
	admin.handle(http.MethodDelete, "/accounts/{accountId}/sessions/{sessionId}", s.handleRevokeAccountSession)

	// Admin session maintenance (openapi.yaml: purgeSessions). Hard-deletes expired
	// session records on demand; admin only.
	admin.handle(http.MethodPost, "/admin/sessions/purge", s.handlePurgeSessions)

	// Admin operational endpoints (openapi.yaml: shutdownServer, createImpersonation).
	// Gated by requireAdmin. Shutdown drains and stops the process (dev mode only);
	// impersonation mints a short-lived session bearing a target account's identity
	// while recording the admin as the auditable actor (ADR-0002).
	admin.handle(http.MethodPost, "/admin/shutdown", s.handleShutdown)
	admin.handle(http.MethodPost, "/admin/impersonation", s.handleCreateImpersonation)

	// Game catalog and lifecycle (openapi.yaml: listGames, createGame, getGame,
	// updateGame). Listing and reading are authenticated (results filtered by
	// visibility); creating is admin-only; updating is admin-or-active-GM, so it
	// lives on the authenticated group with the role check in the handler.
	authed.handle(http.MethodGet, "/games", s.handleListGames)
	admin.handle(http.MethodPost, "/games", s.handleCreateGame)
	authed.handle(http.MethodGet, "/games/{gameId}", s.handleGetGame)
	authed.handle(http.MethodPatch, "/games/{gameId}", s.handleUpdateGame)

	// Game membership — the game_account_role boundary table (openapi.yaml:
	// listGameMembers, addGameMember, updateGameMember). All authenticated; the
	// per-game authorization (admin-or-active-GM, plus self-drop) lives in the
	// handlers, so they hang on the authenticated group. No engine-owned identity
	// crosses this surface (ADR-0003).
	authed.handle(http.MethodGet, "/games/{gameId}/members", s.handleListGameMembers)
	authed.handle(http.MethodPost, "/games/{gameId}/members", s.handleAddGameMember)
	authed.handle(http.MethodPatch, "/games/{gameId}/members/{playerId}", s.handleUpdateGameMember)

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
		// Interrupt signal (SIGINT/SIGTERM): drain and stop.
		return s.drain(ctx, srv)
	case <-s.shutdown:
		// Admin-requested shutdown (POST /admin/shutdown): the same drain path.
		return s.drain(ctx, srv)
	}
}

// drain gracefully stops srv, letting in-flight requests finish (up to a fixed
// deadline) before returning. It backs both the interrupt-signal and the
// admin-requested shutdown paths. The 202 already written by the shutdown handler
// is one such in-flight request, so it reaches the client before the process
// exits.
func (s *Server) drain(ctx context.Context, srv *http.Server) error {
	s.log.InfoContext(ctx, "server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return nil
}
