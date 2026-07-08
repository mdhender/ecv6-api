// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdhender/ecv6-api/internal/api"
	"github.com/mdhender/ecv6-api/internal/store"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// minSecretLen is the shortest secret the server accepts on a create or change
// (openapi.yaml: "at least 8 characters"). It applies to any caller-supplied
// secret; a randomly generated one is always longer.
const minSecretLen = 8

// generatedSecretBytes is the entropy (in bytes) of an auto-generated account
// secret, base64url-encoded into the plaintext returned once at creation.
const generatedSecretBytes = 12

// toAccountDTO projects a store.Account onto the wire Account schema. The
// application role is expressed as a single-element roles array — "admin" when
// IsAdmin, otherwise "user" (ADR-0004); it never carries per-game roles.
// displayName is omitted when empty.
func toAccountDTO(a store.Account) api.Account {
	role := "user"
	if a.IsAdmin {
		role = "admin"
	}
	dto := api.Account{
		Email:    openapi_types.Email(a.Email),
		Id:       a.ID,
		IsActive: a.IsActive,
		Roles:    []string{role},
	}
	if a.DisplayName != "" {
		dn := a.DisplayName
		dto.DisplayName = &dn
	}
	return dto
}

// generateSecret mints a random plaintext secret (base64url) for an account
// created without one; the plaintext is returned to the admin once and only its
// hash is stored.
func generateSecret() (string, error) {
	b := make([]byte, generatedSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// parseAccountID reads the {accountId} path value as an int64, writing the
// standard 400 envelope and returning ok=false when it is missing or malformed.
func (s *Server) parseAccountID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("accountId"), 10, 64)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "invalid account id")
		return 0, false
	}
	return id, true
}

// handleListAccounts serves GET /api/accounts (openapi.yaml: listAccounts). Admin
// only (enforced by the group's requireAdmin). Returns every account ordered by id.
func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.db.ListAccounts(r.Context())
	if err != nil {
		logger(r).ErrorContext(r.Context(), "accounts: list", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not list accounts")
		return
	}
	out := make([]api.Account, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, toAccountDTO(a))
	}
	writeJSON(w, r, http.StatusOK, api.ListAccountsResponse{Accounts: out})
}

// handleCreateAccount serves POST /api/accounts (openapi.yaml: createAccount).
// Admin only. The account is inactive and non-admin unless isActive/isAdmin are
// set; email is lowercased and must be unique (a duplicate is 409). When secret
// is omitted a random one is generated and returned once in generatedSecret.
func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req api.CreateAccountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(string(req.Email)))
	if email == "" {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "email is required")
		return
	}

	var (
		secret    string
		generated *string
	)
	if req.Secret != nil {
		if len(*req.Secret) < minSecretLen {
			writeError(w, r, http.StatusBadRequest, codeBadRequest, "secret must be at least 8 characters")
			return
		}
		secret = *req.Secret
	} else {
		gen, err := generateSecret()
		if err != nil {
			logger(r).ErrorContext(r.Context(), "accounts: generate secret", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create account")
			return
		}
		secret = gen
		generated = &gen
	}

	hashed, err := HashSecret(secret)
	if err != nil {
		logger(r).ErrorContext(r.Context(), "accounts: hash secret", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create account")
		return
	}

	a := store.Account{
		Email:        email,
		DisplayName:  derefOr(req.DisplayName, ""),
		HashedSecret: hashed,
		IsAdmin:      derefOr(req.IsAdmin, false),
		IsActive:     derefOr(req.IsActive, false),
	}
	id, err := s.db.CreateAccount(r.Context(), a)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, r, http.StatusConflict, codeConflict, "an account with that email already exists")
			return
		}
		logger(r).ErrorContext(r.Context(), "accounts: create", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not create account")
		return
	}
	a.ID = id

	writeJSON(w, r, http.StatusCreated, api.CreateAccountResponse{
		Account:         toAccountDTO(a),
		GeneratedSecret: generated,
	})
}

// handleGetAccount serves GET /api/accounts/{accountId} (openapi.yaml:
// getAccount). Admin only. An unknown id is 404.
func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseAccountID(w, r)
	if !ok {
		return
	}
	a, err := s.db.GetAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "account not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "accounts: get", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not read account")
		return
	}
	writeJSON(w, r, http.StatusOK, api.AccountResponse{Account: toAccountDTO(a)})
}

// handleUpdateAccount serves PATCH /api/accounts/{accountId} (openapi.yaml:
// updateAccount). Admin only, partial update: a present field is applied, an
// absent field left unchanged, and at least one field is required. email must
// stay unique (409); a new secret must be at least 8 characters. This is the
// admin-only credential-recovery path.
func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := s.parseAccountID(w, r)
	if !ok {
		return
	}
	var req api.UpdateAccountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DisplayName == nil && req.Email == nil && req.IsActive == nil && req.IsAdmin == nil && req.Secret == nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest, "at least one field is required")
		return
	}

	a, err := s.db.GetAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrRecordNotFound) {
			writeError(w, r, http.StatusNotFound, codeNotFound, "account not found")
			return
		}
		logger(r).ErrorContext(r.Context(), "accounts: get for update", "err", err)
		writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update account")
		return
	}

	if req.DisplayName != nil {
		a.DisplayName = *req.DisplayName
	}
	if req.Email != nil {
		email := strings.ToLower(strings.TrimSpace(string(*req.Email)))
		if email == "" {
			writeError(w, r, http.StatusBadRequest, codeBadRequest, "email must not be empty")
			return
		}
		a.Email = email
	}
	if req.IsActive != nil {
		a.IsActive = *req.IsActive
	}
	if req.IsAdmin != nil {
		a.IsAdmin = *req.IsAdmin
	}
	if req.Secret != nil {
		if len(*req.Secret) < minSecretLen {
			writeError(w, r, http.StatusBadRequest, codeBadRequest, "secret must be at least 8 characters")
			return
		}
		hashed, err := HashSecret(*req.Secret)
		if err != nil {
			logger(r).ErrorContext(r.Context(), "accounts: hash secret", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update account")
			return
		}
		a.HashedSecret = hashed
	}

	if err := s.db.UpdateAccount(r.Context(), a); err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, r, http.StatusConflict, codeConflict, "an account with that email already exists")
		case errors.Is(err, store.ErrRecordNotFound):
			writeError(w, r, http.StatusNotFound, codeNotFound, "account not found")
		default:
			logger(r).ErrorContext(r.Context(), "accounts: update", "err", err)
			writeError(w, r, http.StatusInternalServerError, codeInternal, "could not update account")
		}
		return
	}
	writeJSON(w, r, http.StatusOK, api.AccountResponse{Account: toAccountDTO(a)})
}

// derefOr returns *p when p is non-nil, otherwise def.
func derefOr[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}
