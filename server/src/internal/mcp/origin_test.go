/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCPEndpointValidatesOrigin drives the full handler chain RunHTTP
// serves and checks the Streamable HTTP rule that a present, invalid
// Origin gets 403 while a request without one (a CLI client) is served.
func TestMCPEndpointValidatesOrigin(t *testing.T) {
	const toolsList = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

	tests := []struct {
		name       string
		corsOrigin string
		method     string
		path       string
		origin     string // empty leaves the header out
		wantStatus int
	}{
		{"absent, no CORS origin", "", http.MethodPost, "/mcp/v1", "", http.StatusOK},
		{"absent, CORS origin set", "https://dba.example.com", http.MethodPost, "/mcp/v1", "", http.StatusOK},
		{"allowed", "https://dba.example.com", http.MethodPost, "/mcp/v1", "https://dba.example.com", http.StatusOK},
		{"allowed, different case", "https://dba.example.com", http.MethodPost, "/mcp/v1", "HTTPS://DBA.Example.COM", http.StatusOK},
		{"allowed, configured with trailing slash", "https://dba.example.com/", http.MethodPost, "/mcp/v1", "https://dba.example.com", http.StatusOK},
		{"mismatched host", "https://dba.example.com", http.MethodPost, "/mcp/v1", "https://evil.example.com", http.StatusForbidden},
		{"mismatched scheme", "https://dba.example.com", http.MethodPost, "/mcp/v1", "http://dba.example.com", http.StatusForbidden},
		{"mismatched port", "https://dba.example.com", http.MethodPost, "/mcp/v1", "https://dba.example.com:8443", http.StatusForbidden},
		{"present with no CORS origin", "", http.MethodPost, "/mcp/v1", "http://localhost:3000", http.StatusForbidden},
		{"null origin", "https://dba.example.com", http.MethodPost, "/mcp/v1", "null", http.StatusForbidden},
		{"wildcard CORS origin allows any", "*", http.MethodPost, "/mcp/v1", "https://anywhere.example.com", http.StatusOK},
		{"mismatched on GET is still 403", "https://dba.example.com", http.MethodGet, "/mcp/v1", "https://evil.example.com", http.StatusForbidden},
		{"other paths are not checked", "", http.MethodGet, "/health", "https://evil.example.com", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			handler, err := server.buildHTTPHandler(&HTTPConfig{CORSOrigin: tt.corsOrigin})
			if err != nil {
				t.Fatalf("buildHTTPHandler returned %v", err)
			}

			var body *strings.Reader
			if tt.method == http.MethodPost {
				body = strings.NewReader(toolsList)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %q",
					w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantStatus != http.StatusForbidden {
				return
			}
			var resp JSONRPCResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode %q: %v", w.Body.String(), err)
			}
			if resp.Error == nil || resp.ID != nil {
				t.Errorf("got %+v, want a JSON-RPC error with no id", resp)
			}
		})
	}
}

func TestNormalizeOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   string
	}{
		{"https://dba.example.com", "https://dba.example.com"},
		{" HTTP://Localhost:8080/ ", "http://localhost:8080"},
		{"null", ""},
		{"file://", ""},
		{"chrome-extension://abcdef", ""},
		{"https://%zz", ""},
	}
	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			if got := normalizeOrigin(tt.origin); got != tt.want {
				t.Errorf("normalizeOrigin(%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}

func TestOriginAllowedRefusesRepeatedHeader(t *testing.T) {
	h := http.Header{"Origin": {"https://dba.example.com", "https://dba.example.com"}}
	if originAllowed(h, "https://dba.example.com", "https://dba.example.com") {
		t.Error("a repeated Origin header was allowed")
	}
}

// TestBuildHTTPHandlerReportsSetupErrors covers the two ways building
// the handler chain fails.
func TestBuildHTTPHandlerReportsSetupErrors(t *testing.T) {
	setupErr := errors.New("handler setup failed")
	tests := []struct {
		name   string
		config *HTTPConfig
		want   string
	}{
		{
			name: "handler setup fails",
			config: &HTTPConfig{SetupHandlers: func(*http.ServeMux) error {
				return setupErr
			}},
			want: "failed to setup custom handlers",
		},
		{
			name:   "invalid CORS origin",
			config: &HTTPConfig{CORSOrigin: "not a url"},
			want:   "invalid CORS origin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewServer(&mockToolProvider{}).buildHTTPHandler(tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
