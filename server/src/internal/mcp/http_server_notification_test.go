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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleHTTPRequest_NotificationsAndResponsesGet202 checks the
// Streamable HTTP rule that an accepted notification or client response
// gets 202 Accepted with no body, while a request still gets its reply.
func TestHandleHTTPRequest_NotificationsAndResponsesGet202(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   bool
	}{
		{
			name:       "initialized notification",
			body:       `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "cancellation notification",
			body:       `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"reason":"user"}}`, //nolint:misspell // MCP method name
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "unknown notification",
			body:       `{"jsonrpc":"2.0","method":"notifications/no_such_thing"}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "notification with a null id",
			body:       `{"jsonrpc":"2.0","id":null,"method":"notifications/initialized"}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "client result response",
			body:       `{"jsonrpc":"2.0","id":3,"result":{}}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "client error response",
			body:       `{"jsonrpc":"2.0","id":4,"error":{"code":-32601,"message":"Method not found"}}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "initialize request",
			body:       `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
			wantStatus: http.StatusOK,
			wantBody:   true,
		},
		{
			name:       "tools/list request",
			body:       `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			wantStatus: http.StatusOK,
			wantBody:   true,
		},
		{
			name:       "unknown method request",
			body:       `{"jsonrpc":"2.0","id":5,"method":"no/such/method"}`,
			wantStatus: http.StatusOK,
			wantBody:   true,
		},
		{
			name:       "id without method, result or error",
			body:       `{"jsonrpc":"2.0","id":6}`,
			wantStatus: http.StatusOK,
			wantBody:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			req := httptest.NewRequest(http.MethodPost, "/mcp/v1",
				strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			server.handleHTTPRequest(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %q",
					w.Code, tt.wantStatus, w.Body.String())
			}
			if gotBody := w.Body.Len() > 0; gotBody != tt.wantBody {
				t.Errorf("body present = %v, want %v; body %q",
					gotBody, tt.wantBody, w.Body.String())
			}
		})
	}
}

// TestHandleHTTPRequest_NotificationStillDispatched checks that a
// notification is still handled for its side effects, as on stdio, even
// though the client gets no reply.
func TestHandleHTTPRequest_NotificationStillDispatched(t *testing.T) {
	called := false
	tools := &mockToolProvider{
		executeFunc: func(_ context.Context, _ string, _ map[string]any) (ToolResponse, error) {
			called = true
			return NewToolSuccess("ok")
		},
	}
	server := NewServer(tools)
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", strings.NewReader(
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"query"}}`))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	if w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Fatalf("got %d with body %q, want 202 and no body",
			w.Code, w.Body.String())
	}
	if !called {
		t.Error("notification was not dispatched")
	}
}
