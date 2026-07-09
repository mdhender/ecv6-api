// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// earl is a configured client: where to talk, which identity to use, and where
// to write results. out/errOut are injectable so tests can capture output.
type earl struct {
	baseURL string
	email   string
	log     *slog.Logger
	http    *http.Client
	out     io.Writer
	errOut  io.Writer
}

// request performs one API call: METHOD baseURL+path, with the given body, and
// (unless noAuth) the saved bearer token for the active identity. A 2xx writes
// the body to out; any other status is returned as an error carrying the status
// and the response envelope.
func (e *earl) request(ctx context.Context, method, path string, body []byte, noAuth bool) error {
	token := ""
	if !noAuth {
		store, err := e.loadTokens()
		if err != nil {
			return err
		}
		var who string
		who, token = store.resolve(e.baseURL, e.email)
		if token != "" {
			e.log.Debug("attaching token", "email", who)
		}
	}

	status, respBody, err := e.do(ctx, method, path, body, token)
	if err != nil {
		return err
	}
	return e.emit(method, path, status, respBody)
}

// do sends one HTTP request and returns the status and response body. It sets
// the JSON Accept header, a Content-Type when there is a body, and a bearer
// Authorization header when token is non-empty. It is the shared transport
// beneath request, login, and logout.
func (e *earl) do(ctx context.Context, method, path string, body []byte, token string) (int, []byte, error) {
	url := joinURL(e.baseURL, path)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	e.log.Debug("request", "method", method, "url", url, "authenticated", token != "", "bodyBytes", len(body))
	resp, err := e.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read response from %s %s: %w", method, url, err)
	}
	e.log.Debug("response", "status", resp.StatusCode, "bodyBytes", len(respBody))
	return resp.StatusCode, respBody, nil
}

// emit renders a completed response. On 2xx it writes the body to out and
// returns nil; otherwise it returns an error whose message carries the status
// line and the (pretty-printed) error envelope, so the caller's non-zero exit
// and the server's explanation land together on stderr.
func (e *earl) emit(method, path string, status int, body []byte) error {
	if status/100 == 2 {
		e.writeBody(body)
		return nil
	}
	msg := fmt.Sprintf("%s %s -> %d %s", method, path, status, http.StatusText(status))
	if len(body) > 0 {
		msg += "\n" + formatJSON(body, true)
	}
	return fmt.Errorf("%s", msg)
}

// writeBody writes a successful response body to out: pretty-printed when out is
// a terminal (and the body is JSON), raw otherwise so a pipe into jq stays
// machine-readable. An empty body (e.g. 204) writes nothing.
func (e *earl) writeBody(body []byte) {
	if len(body) == 0 {
		return
	}
	out := formatJSON(body, isTerminal(e.out))
	fmt.Fprintln(e.out, strings.TrimRight(out, "\n"))
}

// loadTokens resolves the token file path and reads it.
func (e *earl) loadTokens() (tokenStore, error) {
	path, err := tokensPath()
	if err != nil {
		return nil, err
	}
	return loadTokens(path)
}

// saveTokens resolves the token file path and writes store to it.
func (e *earl) saveTokens(store tokenStore) error {
	path, err := tokensPath()
	if err != nil {
		return err
	}
	return saveTokens(path, store)
}

// readBody turns a -d value into a request body: "" means no body; "@-" reads
// stdin; "@name" reads the file name; anything else is used literally (inline
// JSON).
func readBody(d string) ([]byte, error) {
	switch {
	case d == "":
		return nil, nil
	case d == "@-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read body from stdin: %w", err)
		}
		return data, nil
	case strings.HasPrefix(d, "@"):
		name := d[1:]
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read body file: %w", err)
		}
		return data, nil
	default:
		return []byte(d), nil
	}
}

// joinURL joins a base URL and an API path with exactly one slash between them.
// The path is taken relative to the base (which already includes /api), so
// `get /healthz` against http://host/api hits http://host/api/healthz.
func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

// formatJSON returns body indented when pretty is true and the bytes are valid
// JSON; otherwise it returns the bytes unchanged. Non-JSON (or compact-mode)
// output is passed through verbatim.
func formatJSON(body []byte, pretty bool) string {
	if !pretty || !json.Valid(body) {
		return string(body)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		return string(body)
	}
	return buf.String()
}

// isTerminal reports whether w is a character device (a TTY), used to decide
// pretty vs. raw output. A non-*os.File writer (as in tests) is never a TTY.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
