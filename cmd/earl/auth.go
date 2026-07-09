// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// loginResponse is the subset of the API's auth session payload earl needs: the
// bearer token and its expiry. Field names match the server's JSON.
type loginResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"tokenType"`
	ExpiresAt string `json:"expiresAt"`
}

// login exchanges the active email (from --email / EARL_EMAIL) and the given
// secret for a bearer token via POST /auth/login, then saves it under that
// server and email. Progress goes to errOut so stdout stays clean for piping.
func (e *earl) login(ctx context.Context, secret string) error {
	email := strings.TrimSpace(e.email)
	if email == "" || secret == "" {
		return fmt.Errorf("login needs an email and secret (--email/--secret or EARL_EMAIL/EARL_SECRET)")
	}

	reqBody, err := json.Marshal(map[string]string{"email": email, "secret": secret})
	if err != nil {
		return fmt.Errorf("encode login request: %w", err)
	}
	status, respBody, err := e.do(ctx, http.MethodPost, "/auth/login", reqBody, "")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return e.emit(http.MethodPost, "/auth/login", status, respBody)
	}

	var lr loginResponse
	if err := json.Unmarshal(respBody, &lr); err != nil {
		return fmt.Errorf("parse login response: %w", err)
	}
	if lr.Token == "" {
		return fmt.Errorf("login response contained no token")
	}

	store, err := e.loadTokens()
	if err != nil {
		return err
	}
	store.put(e.baseURL, email, identity{Token: lr.Token, TokenType: lr.TokenType, ExpiresAt: parseTime(lr.ExpiresAt)})
	if err := e.saveTokens(store); err != nil {
		return err
	}

	if lr.ExpiresAt != "" {
		fmt.Fprintf(e.errOut, "logged in as %s at %s (token expires %s)\n", strings.ToLower(email), e.baseURL, lr.ExpiresAt)
	} else {
		fmt.Fprintf(e.errOut, "logged in as %s at %s\n", strings.ToLower(email), e.baseURL)
	}
	return nil
}

// logout revokes the saved session via POST /auth/logout (all sessions when all
// is true) and removes the token locally. It requires a resolved identity: with
// no saved token, or an ambiguous choice, it reports what to do. A server-side
// revocation failure still drops the local token — a token we can no longer use
// is not worth keeping.
func (e *earl) logout(ctx context.Context, all bool) error {
	store, err := e.loadTokens()
	if err != nil {
		return err
	}
	who, token := store.resolve(e.baseURL, e.email)
	if token == "" {
		if emails := store.emails(e.baseURL); len(emails) > 1 && e.email == "" {
			return fmt.Errorf("multiple saved logins for %s; pass --email (one of: %s)", e.baseURL, strings.Join(emails, ", "))
		}
		return fmt.Errorf("no saved login for %s at %s", emailOrAny(e.email), e.baseURL)
	}

	reqBody, err := json.Marshal(map[string]bool{"allSessions": all})
	if err != nil {
		return fmt.Errorf("encode logout request: %w", err)
	}
	status, respBody, err := e.do(ctx, http.MethodPost, "/auth/logout", reqBody, token)
	if err != nil {
		return err
	}

	// Drop the local token regardless: after logout (or if the server already
	// considers it invalid) it cannot be used again.
	store.drop(e.baseURL, who)
	if saveErr := e.saveTokens(store); saveErr != nil {
		return saveErr
	}

	switch {
	case status == http.StatusNoContent || status/100 == 2:
		fmt.Fprintf(e.errOut, "logged out %s at %s\n", who, e.baseURL)
		return nil
	case status == http.StatusUnauthorized:
		// The token was already invalid server-side; we still cleaned up locally.
		fmt.Fprintf(e.errOut, "token for %s was already invalid; removed locally\n", who)
		return nil
	default:
		return e.emit(http.MethodPost, "/auth/logout", status, respBody)
	}
}

// impersonationResponse is the subset of POST /admin/impersonation's payload
// earl needs: the minted token and the subject it identifies. earl saves the
// token under the subject's email, so it becomes a selectable identity.
type impersonationResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"tokenType"`
	ExpiresAt string `json:"expiresAt"`
	Subject   struct {
		AccountID int64  `json:"accountId"`
		Email     string `json:"email"`
	} `json:"subject"`
}

// impersonate mints a bearer token for another account via POST
// /admin/impersonation (an admin action, so it uses the active identity's token)
// and saves it under the subject's email. Afterwards, --email <subject> selects
// that identity, letting the caller exercise the API as the impersonated user.
func (e *earl) impersonate(ctx context.Context, accountID int64) error {
	store, err := e.loadTokens()
	if err != nil {
		return err
	}
	who, token := store.resolve(e.baseURL, e.email)
	if token == "" {
		if emails := store.emails(e.baseURL); len(emails) > 1 && e.email == "" {
			return fmt.Errorf("multiple saved logins for %s; pass --email to pick the admin (one of: %s)", e.baseURL, strings.Join(emails, ", "))
		}
		return fmt.Errorf("no saved login for %s at %s; run `earl login` as an admin first", emailOrAny(e.email), e.baseURL)
	}

	reqBody, err := json.Marshal(map[string]int64{"accountId": accountID})
	if err != nil {
		return fmt.Errorf("encode impersonation request: %w", err)
	}
	status, respBody, err := e.do(ctx, http.MethodPost, "/admin/impersonation", reqBody, token)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return e.emit(http.MethodPost, "/admin/impersonation", status, respBody)
	}

	var ir impersonationResponse
	if err := json.Unmarshal(respBody, &ir); err != nil {
		return fmt.Errorf("parse impersonation response: %w", err)
	}
	if ir.Token == "" || ir.Subject.Email == "" {
		return fmt.Errorf("impersonation response missing token or subject email")
	}

	store.put(e.baseURL, ir.Subject.Email, identity{Token: ir.Token, TokenType: ir.TokenType, ExpiresAt: parseTime(ir.ExpiresAt)})
	if err := e.saveTokens(store); err != nil {
		return err
	}

	subject := strings.ToLower(ir.Subject.Email)
	fmt.Fprintf(e.errOut, "%s is now impersonating %s (account %d); select it with --email %s\n", who, subject, ir.Subject.AccountID, subject)
	return nil
}

// parseTime parses an RFC 3339 timestamp (the server's expiresAt format),
// returning the zero time when the string is empty or unparseable — the expiry
// is advisory metadata, not worth failing a login over.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// emailOrAny renders an email for an error message, or "any account" when none
// was specified.
func emailOrAny(email string) string {
	if email == "" {
		return "any account"
	}
	return strings.ToLower(email)
}
