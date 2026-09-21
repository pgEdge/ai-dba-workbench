/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token endpoints: listing, routing and store failures
//
// Every store call behind these handlers can fail, and each failure has
// its own message. The tests below break exactly one table at a time,
// using the same technique as the audit gate tests, so that a handler
// answering 500 for the wrong reason is still caught.
// =============================================================================

// withSuperuserSession marks a request as coming from an interactive
// superuser session, which carries no acting token.
func withSuperuserSession(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(),
		auth.IsSuperuserContextKey, true))
}

// tokenRequest issues a request against the token routes as a superuser
// session.
func tokenRequest(h *RBACHandler, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = withSuperuserSession(req)
	rec := httptest.NewRecorder()
	h.handleTokenSubpath(rec, req)
	return rec
}

// TestListTokensRendersScope verifies that a scoped token is listed
// with its scope, and an unscoped one without, since the web client
// decides from this whether to show a token as restricted.
func TestListTokensRendersScope(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	scoped := mustCreateScopedToken(t, store, "svc-scoped",
		[]string{auth.PermManageUsers})
	if err := store.SetTokenConnectionScope(scoped, []auth.ScopedConnection{
		{ConnectionID: 7, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	unscoped := mustCreateScopedToken(t, store, "svc-unscoped", nil)

	rec := tokenRequest(handler, http.MethodGet, "/api/v1/rbac/tokens/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Tokens []struct {
			ID          int64  `json:"id"`
			TokenPrefix string `json:"token_prefix"`
			Username    string `json:"username"`
			Scope       *struct {
				Scoped      bool `json:"scoped"`
				Connections []struct {
					ConnectionID int    `json:"connection_id"`
					AccessLevel  string `json:"access_level"`
				} `json:"connections"`
				AdminPermissions []string `json:"admin_permissions"`
			} `json:"scope"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to decode the listing: %v", err)
	}
	if len(body.Tokens) != 2 {
		t.Fatalf("Expected two tokens, got %d", len(body.Tokens))
	}

	for _, tok := range body.Tokens {
		switch tok.ID {
		case scoped:
			if tok.Scope == nil || !tok.Scope.Scoped {
				t.Fatalf("Expected the scoped token to report its scope, got %+v",
					tok.Scope)
			}
			if len(tok.Scope.Connections) != 1 ||
				tok.Scope.Connections[0].ConnectionID != 7 ||
				tok.Scope.Connections[0].AccessLevel != string(auth.AccessLevelRead) {
				t.Errorf("Unexpected connection scope: %+v", tok.Scope.Connections)
			}
			if len(tok.Scope.AdminPermissions) != 1 ||
				tok.Scope.AdminPermissions[0] != auth.PermManageUsers {
				t.Errorf("Unexpected admin scope: %+v", tok.Scope.AdminPermissions)
			}
			if tok.Username != "svc-scoped" {
				t.Errorf("Expected the owner's name, got %q", tok.Username)
			}
			if tok.TokenPrefix == "" {
				t.Error("Expected a token prefix to be reported")
			}
		case unscoped:
			if tok.Scope != nil {
				t.Errorf("Expected no scope for an unscoped token, got %+v",
					tok.Scope)
			}
		default:
			t.Errorf("Unexpected token in the listing: %d", tok.ID)
		}
	}
}

// TestListTokensReportsStoreFailure verifies that an unreadable token
// table is reported as a server error rather than an empty listing,
// which a client would render as "no tokens exist".
func TestListTokensReportsStoreFailure(t *testing.T) {
	handler, _, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()

	dropAuthTable(t, dir, "tokens")

	rec := tokenRequest(handler, http.MethodGet, "/api/v1/rbac/tokens/", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Failed to list tokens") {
		t.Errorf("Unexpected body: %s", rec.Body.String())
	}
}

// TestDeleteTokenReportsStoreFailure verifies that a failed delete is
// reported, since answering 204 would tell an administrator the token
// is gone when it is not.
func TestDeleteTokenReportsStoreFailure(t *testing.T) {
	handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-doomed", nil)
	dropAuthTable(t, dir, "tokens")

	rec := tokenRequest(handler, http.MethodDelete,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(tokenID, 10), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Failed to delete token") {
		t.Errorf("Unexpected body: %s", rec.Body.String())
	}
}

// TestTokenSubpathRouting covers the routing answers: an unknown method
// on each route, and a path deeper than the router serves.
func TestTokenSubpathRouting(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-routing", nil)
	base := "/api/v1/rbac/tokens/" + strconv.FormatInt(tokenID, 10)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantAllow  string
	}{
		{name: "unknown method on the token", method: http.MethodPut,
			path: base, wantStatus: http.StatusMethodNotAllowed,
			wantAllow: "DELETE"},
		{name: "unknown method on the scope", method: http.MethodPost,
			path: base + "/scope", wantStatus: http.StatusMethodNotAllowed,
			wantAllow: "GET, PUT, DELETE"},
		{name: "unknown subpath", method: http.MethodGet,
			path: base + "/scope/extra", wantStatus: http.StatusNotFound},
		{name: "invalid token id", method: http.MethodGet,
			path:       "/api/v1/rbac/tokens/not-a-number/scope",
			wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := tokenRequest(handler, tt.method, tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("Expected %d, got %d: %s", tt.wantStatus, rec.Code,
					rec.Body.String())
			}
			if tt.wantAllow != "" &&
				rec.Header().Get("Allow") != tt.wantAllow {
				t.Errorf("Expected Allow: %q, got %q", tt.wantAllow,
					rec.Header().Get("Allow"))
			}
		})
	}
}

// TestGetTokenScopeReportsStoreFailure verifies that an unreadable
// scope is reported rather than rendered as an unscoped token, which
// would misreport a restricted token as unrestricted.
func TestGetTokenScopeReportsStoreFailure(t *testing.T) {
	handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-unreadable",
		[]string{auth.PermManageUsers})
	dropAuthTable(t, dir, "token_connection_scope")

	rec := tokenRequest(handler, http.MethodGet,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(tokenID, 10)+"/scope", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Failed to get token scope") {
		t.Errorf("Unexpected body: %s", rec.Body.String())
	}
}

// TestSetTokenScopeReportsStoreFailures verifies that each of the three
// scope writes reports its own failure, so that an administrator can
// tell which part of the scope did not take.
func TestSetTokenScopeReportsStoreFailures(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		body    string
		message string
	}{
		{
			name:    "connection scope",
			table:   "token_connection_scope",
			body:    `{"connections":[{"connection_id":1,"access_level":"read"}]}`,
			message: "Failed to set connection scope",
		},
		{
			name:    "mcp scope",
			table:   "token_mcp_scope",
			body:    `{"mcp_privileges":["*"]}`,
			message: "Failed to set MCP scope",
		},
		{
			name:    "admin scope",
			table:   "token_admin_scope",
			body:    `{"admin_permissions":["manage_users"]}`,
			message: "Failed to set admin scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
			defer cleanup()

			tokenID := mustCreateScopedToken(t, store, "svc-scope-write", nil)
			dropAuthTable(t, dir, tt.table)

			rec := tokenRequest(handler, http.MethodPut,
				"/api/v1/rbac/tokens/"+strconv.FormatInt(tokenID, 10)+"/scope",
				tt.body)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("Expected 500, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.message) {
				t.Errorf("Expected %q, got %s", tt.message, rec.Body.String())
			}
		})
	}
}

// TestClearTokenScopeReportsStoreFailure verifies that a failed clear
// is reported, since a token whose scope survives a reported success
// would be silently more restricted than the administrator believes.
func TestClearTokenScopeReportsStoreFailure(t *testing.T) {
	handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-clear",
		[]string{auth.PermManageUsers})
	dropAuthTable(t, dir, "token_connection_scope")

	rec := tokenRequest(handler, http.MethodDelete,
		"/api/v1/rbac/tokens/"+strconv.FormatInt(tokenID, 10)+"/scope", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Failed to clear token scope") {
		t.Errorf("Unexpected body: %s", rec.Body.String())
	}
}
