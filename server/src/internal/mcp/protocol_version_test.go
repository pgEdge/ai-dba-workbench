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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// negotiationCases maps the protocol version a client requests in
// initialize to the version the server must answer with: a supported
// version is echoed, anything else gets the latest supported version.
var negotiationCases = []struct {
	name      string
	requested string
	want      string
}{
	{"2025-11-25 is echoed", "2025-11-25", "2025-11-25"},
	{"2025-06-18 is echoed", "2025-06-18", "2025-06-18"},
	{"2025-03-26 is echoed", "2025-03-26", "2025-03-26"},
	{"2024-11-05 is echoed", "2024-11-05", "2024-11-05"},
	{"empty gets the latest", "", "2025-11-25"},
	{"future 2026-07-28 gets the latest", "2026-07-28", "2025-11-25"},
	{"unknown older date gets the latest", "2024-10-07", "2025-11-25"},
	{"non-date gets the latest", "1.0.0", "2025-11-25"},
}

// initializeBody returns an initialize request asking for version, or
// one without a protocolVersion field when version is empty.
func initializeBody(version string) string {
	if version == "" {
		return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	}
	return fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`,
		version)
}

// decodeInitializeResult decodes a JSON-RPC initialize response body.
func decodeInitializeResult(t *testing.T, body []byte) InitializeResult {
	t.Helper()
	var resp struct {
		Result InitializeResult `json:"result"`
		Error  *RPCError        `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to decode %q: %v", body, err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error %+v", resp.Error)
	}
	return resp.Result
}

func TestInitializeNegotiatesProtocolVersion_HTTP(t *testing.T) {
	for _, tt := range negotiationCases {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			req := httptest.NewRequest(http.MethodPost, "/mcp/v1",
				strings.NewReader(initializeBody(tt.requested)))
			w := httptest.NewRecorder()

			server.handleHTTPRequest(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %q", w.Code, w.Body.String())
			}
			got := decodeInitializeResult(t, w.Body.Bytes()).ProtocolVersion
			if got != tt.want {
				t.Errorf("protocolVersion = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInitializeNegotiatesProtocolVersion_Stdio(t *testing.T) {
	for _, tt := range negotiationCases {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			output := withStdio(t, initializeBody(tt.requested)+"\n", func() {
				if err := server.Run(); err != nil {
					t.Errorf("Run returned %v", err)
				}
			})

			responses := decodeStdio(t, output)
			if len(responses) != 1 || responses[0].Error != nil {
				t.Fatalf("unexpected responses: %s", output)
			}
			var result InitializeResult
			if err := json.Unmarshal(responses[0].Result, &result); err != nil {
				t.Fatalf("failed to decode the result: %v", err)
			}
			if result.ProtocolVersion != tt.want {
				t.Errorf("protocolVersion = %q, want %q", result.ProtocolVersion, tt.want)
			}
		})
	}
}

func TestInitializeHTTPRejectsBadParams(t *testing.T) {
	server := NewServer(&mockToolProvider{})
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":"text"}`))
	w := httptest.NewRecorder()

	server.handleHTTPRequest(w, req)

	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode %q: %v", w.Body.String(), err)
	}
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Errorf("error = %+v, want code -32602", resp.Error)
	}
}

func TestInitializeHTTPRejectsUnmarshallableParams(t *testing.T) {
	resp := NewServer(&mockToolProvider{}).handleInitializeHTTP(
		JSONRPCRequest{ID: 1, Method: "initialize", Params: make(chan int)})
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Errorf("error = %+v, want code -32602", resp.Error)
	}
}

func TestRequestProtocolVersionTruncatesTheEcho(t *testing.T) {
	long := strings.Repeat("9", 200)
	got, ok := requestProtocolVersion(http.Header{"Mcp-Protocol-Version": {long}})
	if ok || len(got) != maxEchoedVersionLen {
		t.Errorf("got %q, %v; want a refused value cut to %d bytes", got, ok, maxEchoedVersionLen)
	}
}

func TestWriteHTTPErrorSurvivesAnUnencodableBody(t *testing.T) {
	w := httptest.NewRecorder()
	writeHTTPError(w, http.StatusBadRequest, nil, -32600, "Invalid Request", make(chan int))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestProtocolVersionHeader covers the Streamable HTTP rule that a
// request after initialize carrying an invalid or unsupported
// MCP-Protocol-Version header gets 400, while an absent header is
// accepted as 2025-03-26 and initialize itself is never refused for it.
func TestProtocolVersionHeader(t *testing.T) {
	const toolsList = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	const notification = `{"jsonrpc":"2.0","method":"notifications/initialized"}`

	tests := []struct {
		name       string
		body       string
		header     []string // nil leaves the header out
		wantStatus int
	}{
		{"absent", toolsList, nil, http.StatusOK},
		{"2025-11-25", toolsList, []string{"2025-11-25"}, http.StatusOK},
		{"2025-06-18", toolsList, []string{"2025-06-18"}, http.StatusOK},
		{"2025-03-26", toolsList, []string{"2025-03-26"}, http.StatusOK},
		{"2024-11-05", toolsList, []string{"2024-11-05"}, http.StatusOK},
		{"future 2026-07-28", toolsList, []string{"2026-07-28"}, http.StatusBadRequest},
		{"garbage", toolsList, []string{"not-a-version"}, http.StatusBadRequest},
		{"present but empty", toolsList, []string{""}, http.StatusBadRequest},
		{"repeated", toolsList, []string{"2025-11-25", "2025-06-18"}, http.StatusBadRequest},
		{"unsupported on a notification", notification, []string{"2026-07-28"}, http.StatusBadRequest},
		{"supported on a notification", notification, []string{"2025-11-25"}, http.StatusAccepted},
		{"unsupported on initialize", initializeBody("2025-11-25"), []string{"2026-07-28"}, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			req := httptest.NewRequest(http.MethodPost, "/mcp/v1",
				strings.NewReader(tt.body))
			for _, v := range tt.header {
				req.Header.Add("MCP-Protocol-Version", v)
			}
			w := httptest.NewRecorder()

			server.handleHTTPRequest(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %q",
					w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantStatus != http.StatusBadRequest {
				return
			}
			// The 400 body must be a plain JSON-RPC error, not the
			// 2026-07-28 UnsupportedProtocolVersionError (-32022): a
			// dual-era client falls back to initialize only when the
			// 400 carries no recognized modern error.
			var resp JSONRPCResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode %q: %v", w.Body.String(), err)
			}
			if resp.Error == nil || resp.Error.Code != -32600 {
				t.Errorf("error = %+v, want code -32600", resp.Error)
			}
		})
	}
}

// TestBatchRequestsRejected checks that a JSON array body gets a
// JSON-RPC Invalid Request error, since 2025-06-18 removed batching.
func TestBatchRequestsRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"one request", `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`},
		{"leading whitespace", " \n\t[{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}]"},
		{"empty array", `[]`},
		{"notifications only", `[{"jsonrpc":"2.0","method":"notifications/initialized"}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			req := httptest.NewRequest(http.MethodPost, "/mcp/v1",
				strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			server.handleHTTPRequest(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %q", w.Code, w.Body.String())
			}
			var resp JSONRPCResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode %q: %v", w.Body.String(), err)
			}
			if resp.Error == nil || resp.Error.Code != -32600 {
				t.Errorf("error = %+v, want code -32600", resp.Error)
			}
			if resp.ID != nil {
				t.Errorf("id = %v, want none", resp.ID)
			}
		})
	}
}

// TestNonPostMethodsGet405 checks that the endpoint offers no SSE
// stream: a GET, like any other non-POST method, gets 405 naming POST.
func TestNonPostMethodsGet405(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			server := NewServer(&mockToolProvider{})
			req := httptest.NewRequest(method, "/mcp/v1", nil)
			req.Header.Set("Accept", "text/event-stream")
			w := httptest.NewRecorder()

			server.handleHTTPRequest(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405", w.Code)
			}
			if got := w.Header().Get("Allow"); got != http.MethodPost {
				t.Errorf("Allow = %q, want POST", got)
			}
			if ct := w.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
				t.Errorf("Content-Type = %q, want no event stream", ct)
			}
		})
	}
}
