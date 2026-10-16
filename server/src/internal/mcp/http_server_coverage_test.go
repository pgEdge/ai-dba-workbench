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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// failingResponseWriter accepts headers but fails every body write.
type failingResponseWriter struct {
	header http.Header
	code   int
}

func newFailingResponseWriter() *failingResponseWriter {
	return &failingResponseWriter{header: http.Header{}}
}

func (w *failingResponseWriter) Header() http.Header { return w.header }

func (w *failingResponseWriter) WriteHeader(code int) { w.code = code }

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

// erroringBody fails its read, its close, or both.
type erroringBody struct {
	io.Reader
	readErr  error
	closeErr error
}

func (b *erroringBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.Reader.Read(p)
}

func (b *erroringBody) Close() error { return b.closeErr }

// postMCP sends body to handleHTTPRequest and decodes the response.
func postMCP(t *testing.T, s *Server, body string) JSONRPCResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp/v1", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleHTTPRequest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode the response %q: %v", w.Body.String(), err)
	}
	return resp
}

// TestRunHTTPRunsTheShutdownHook checks that the OnShutdown hook runs
// once Shutdown starts draining the server RunHTTP is serving.
func TestRunHTTPRunsTheShutdownHook(t *testing.T) {
	server := NewServer(&mockToolProvider{})
	hookRan := make(chan struct{})
	cfg := &HTTPConfig{
		Addr:       "127.0.0.1:0",
		OnShutdown: func() { close(hookRan) },
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- server.RunHTTP(cfg)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		server.httpMu.Lock()
		ready := server.httpServer != nil
		server.httpMu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not install httpServer within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}

	select {
	case <-hookRan:
	case <-time.After(2 * time.Second):
		t.Fatal("the OnShutdown hook did not run")
	}
	if err := <-runErr; err != nil {
		t.Errorf("RunHTTP returned %v after Shutdown", err)
	}
}

// TestLoadTLSConfigAppendsTheChain checks that each PEM block in the
// chain file is appended to the served certificate, and that a missing
// chain file is reported.
func TestLoadTLSConfigAppendsTheChain(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeSelfSignedCert(t, dir)
	server := NewServer(&mockToolProvider{})

	tlsConfig, err := server.loadTLSConfig(&HTTPConfig{
		CertFile: certFile, KeyFile: keyFile, ChainFile: certFile,
	})
	if err != nil {
		t.Fatalf("loadTLSConfig returned %v", err)
	}
	if n := len(tlsConfig.Certificates[0].Certificate); n != 2 {
		t.Errorf("certificate has %d blocks, want 2 with the chain appended", n)
	}

	_, err = server.loadTLSConfig(&HTTPConfig{
		CertFile: certFile, KeyFile: keyFile,
		ChainFile: filepath.Join(dir, "absent-chain.pem"),
	})
	if err == nil || !strings.Contains(err.Error(), "failed to read certificate chain") {
		t.Errorf("loadTLSConfig returned %v, want a chain read error", err)
	}
}

// TestHandleHTTPRequestPassesTheRequestContext checks that a tool sees
// the caller's IP address and a request ID, whether the IP comes from
// the configured extractor or from a remote address without a port.
func TestHandleHTTPRequestPassesTheRequestContext(t *testing.T) {
	var gotIP, gotRequestID string
	server := NewServer(&mockToolProvider{
		executeFunc: func(ctx context.Context, _ string, _ map[string]any) (ToolResponse, error) {
			gotIP, _ = ctx.Value(auth.IPAddressContextKey).(string)
			gotRequestID = GetRequestIDFromContext(ctx)
			return NewToolSuccess("ok")
		},
	})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query"}}`

	t.Run("remote address without a port", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/v1", strings.NewReader(body))
		req.RemoteAddr = "192.0.2.1"
		server.handleHTTPRequest(httptest.NewRecorder(), req)
		if gotIP != "192.0.2.1" {
			t.Errorf("IP = %q, want 192.0.2.1", gotIP)
		}
		if gotRequestID == "" {
			t.Error("the tool saw no request ID")
		}
	})

	t.Run("configured extractor", func(t *testing.T) {
		server.ipExtractor = auth.NewIPExtractor(nil)
		defer func() { server.ipExtractor = nil }()
		req := httptest.NewRequest(http.MethodPost, "/mcp/v1", strings.NewReader(body))
		req.RemoteAddr = "192.0.2.2:5555"
		server.handleHTTPRequest(httptest.NewRecorder(), req)
		if gotIP != "192.0.2.2" {
			t.Errorf("IP = %q, want 192.0.2.2", gotIP)
		}
	})

	if id := GetRequestIDFromContext(context.Background()); id != "" {
		t.Errorf("GetRequestIDFromContext on an empty context = %q, want empty", id)
	}
}

// TestHandleHTTPRequestBodyErrors covers a body over the size limit,
// one that fails to read, and one that fails to close.
func TestHandleHTTPRequestBodyErrors(t *testing.T) {
	server := NewServer(&mockToolProvider{})

	t.Run("too large", func(t *testing.T) {
		handler := MaxBytesMiddleware(16)(http.HandlerFunc(server.handleHTTPRequest))
		req := httptest.NewRequest(http.MethodPost, "/mcp/v1",
			strings.NewReader(strings.Repeat("x", 64)))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", w.Code)
		}
	})

	t.Run("read fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/v1", nil)
		req.Body = &erroringBody{readErr: errors.New("connection reset")}
		w := httptest.NewRecorder()
		server.handleHTTPRequest(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("close fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/v1", nil)
		req.Body = &erroringBody{
			Reader:   strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`),
			closeErr: errors.New("close failed"),
		}
		w := httptest.NewRecorder()
		server.handleHTTPRequest(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 despite the close error", w.Code)
		}
	})

	t.Run("no body passes the limit through", func(t *testing.T) {
		called := false
		handler := MaxBytesMiddleware(16)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		}))
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if !called {
			t.Error("the wrapped handler was not called")
		}
	})
}

// TestHandleHTTPRequestDebugAndTracing runs a successful and a failing
// request with debug logging and tracing enabled, and checks that the
// trace file records both. Tracing is process-wide and cannot be
// turned off again, so this enables it for the rest of the package's
// tests, which do not depend on it. The trace file is set up once in
// TestMain, so a repeated run appends to it; only the lines this run
// adds are counted.
func TestHandleHTTPRequestDebugAndTracing(t *testing.T) {
	if err := tracing.Initialize(traceFile); err != nil {
		t.Fatalf("failed to initialize tracing: %v", err)
	}
	if !tracing.IsEnabled() {
		t.Skip("tracing was already initialized without a file in this process")
	}

	before, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("failed to read the trace file: %v", err)
	}

	server := NewServer(&mockToolProvider{})
	server.debug = true

	if resp := postMCP(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query"}}`); resp.Error != nil {
		t.Errorf("tools/call failed: %+v", resp.Error)
	}
	if resp := postMCP(t, server, `{"jsonrpc":"2.0","id":2,"method":"no/such/method"}`); resp.Error == nil {
		t.Error("an unknown method succeeded")
	}

	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("failed to read the trace file: %v", err)
	}
	added := data[len(before):]
	if got := strings.Count(string(added), "\n"); got < 4 {
		t.Errorf("this run added %d trace entries, want a request and a response for each call:\n%s", got, added)
	}
}

// TestHTTPWriteFailuresAreLogged checks that a failed body write in the
// request, error and health handlers is logged rather than panicking.
func TestHTTPWriteFailuresAreLogged(t *testing.T) {
	server := NewServer(&mockToolProvider{})

	w := newFailingResponseWriter()
	req := httptest.NewRequest(http.MethodPost, "/mcp/v1",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	server.handleHTTPRequest(w, req)
	if w.code != http.StatusOK {
		t.Errorf("request handler status = %d, want 200", w.code)
	}

	w = newFailingResponseWriter()
	sendHTTPError(w, 1, -32603, "Internal error", nil)
	if w.code != http.StatusOK {
		t.Errorf("sendHTTPError status = %d, want 200", w.code)
	}

	w = newFailingResponseWriter()
	server.handleHealthCheck(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.code != http.StatusOK {
		t.Errorf("health check status = %d, want 200", w.code)
	}
}

// TestHTTPHandlersRejectBadParams covers each HTTP handler's params
// failures and the resource and prompt handlers without a provider.
func TestHTTPHandlersRejectBadParams(t *testing.T) {
	full := newStdioTestServer()
	bare := NewServer(&mockToolProvider{})
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(JSONRPCRequest) JSONRPCResponse
		params  any
		code    int
	}{
		{"tools/call unmarshallable", func(r JSONRPCRequest) JSONRPCResponse { return full.handleToolCallHTTP(ctx, r) }, make(chan int), -32602},
		{"tools/call wrong shape", func(r JSONRPCRequest) JSONRPCResponse { return full.handleToolCallHTTP(ctx, r) }, "text", -32602},
		{"resources/read unmarshallable", func(r JSONRPCRequest) JSONRPCResponse { return full.handleResourceReadHTTP(ctx, r) }, make(chan int), -32602},
		{"resources/read wrong shape", func(r JSONRPCRequest) JSONRPCResponse { return full.handleResourceReadHTTP(ctx, r) }, "text", -32602},
		{"resources/read without a provider", func(r JSONRPCRequest) JSONRPCResponse { return bare.handleResourceReadHTTP(ctx, r) }, nil, -32603},
		{"prompts/get unmarshallable", full.handlePromptGetHTTP, make(chan int), -32602},
		{"prompts/get wrong shape", full.handlePromptGetHTTP, "text", -32602},
		{"prompts/get without a provider", bare.handlePromptGetHTTP, nil, -32601},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.handler(JSONRPCRequest{ID: 1, Params: tt.params})
			if resp.Error == nil || resp.Error.Code != tt.code {
				t.Errorf("error = %+v, want code %d", resp.Error, tt.code)
			}
		})
	}
}
