/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package wbclient is the small HTTP client the evaluation harness uses
// to talk to a running Workbench server's REST API with a bearer token.
package wbclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxErrorBody caps how much of an error response is kept in an
// HTTPError, so a large HTML error page does not flood the report.
const maxErrorBody = 4096

// Client calls the Workbench REST API. BaseURL is the server's root URL,
// such as http://127.0.0.1:8080, and Token is a service token sent as a
// bearer credential on every request.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a client for baseURL that authenticates with token. The
// timeout bounds each request; LLM calls can take minutes, so callers
// pick it to suit.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

// HTTPError is returned when the server answers with a status outside
// the 2xx range.
type HTTPError struct {
	Method string
	Path   string
	Status int
	Body   string
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status,
		strings.TrimSpace(e.Body))
}

// Do sends a request with in encoded as the JSON body (when in is not
// nil) and decodes a JSON response into out (when out is not nil).
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode %s %s request: %w", method, path, err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("build %s %s request: %w", method, path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		body := string(data)
		if err != nil {
			body += fmt.Sprintf(" (reading the body failed: %v)", err)
		}
		return &HTTPError{Method: method, Path: path, Status: resp.StatusCode, Body: body}
	}
	if out == nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("read %s %s response: %w", method, path, err)
		}
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return nil
}
