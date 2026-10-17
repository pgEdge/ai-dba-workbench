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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// stdioResponse is a decoded JSON-RPC response line from the stdio
// server. Result is kept raw so each test decodes what it expects.
type stdioResponse struct {
	ID     any             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

// withStdio points os.Stdin at input and os.Stdout at a pipe whilst fn
// runs, and returns what fn wrote to stdout. The stdio server reads
// and writes the process's own streams, so the tests that use this
// must not run in parallel.
func withStdio(t *testing.T, input string, fn func()) string {
	t.Helper()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create the stdin pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create the stdout pipe: %v", err)
	}

	go func() {
		_, _ = io.WriteString(inW, input)
		_ = inW.Close()
	}()
	output := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, outR)
		output <- buf.String()
	}()

	savedIn, savedOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	defer func() {
		os.Stdin, os.Stdout = savedIn, savedOut
		_ = inR.Close()
	}()

	fn()

	_ = outW.Close()
	return <-output
}

// decodeStdio parses the server's output into one response per line.
func decodeStdio(t *testing.T, output string) []stdioResponse {
	t.Helper()

	var responses []stdioResponse
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var resp stdioResponse
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response line %q: %v", scanner.Text(), err)
		}
		responses = append(responses, resp)
	}
	return responses
}

// newStdioTestServer returns a server with every provider set, each
// of which fails for a name or URI of "fail".
func newStdioTestServer() *Server {
	s := NewServer(&mockToolProvider{
		tools: []Tool{{Name: "query"}},
		executeFunc: func(_ context.Context, name string, _ map[string]any) (ToolResponse, error) {
			if name == "fail" {
				return ToolResponse{}, errors.New("tool failed")
			}
			return NewToolSuccess("ran " + name)
		},
	})
	s.SetResourceProvider(&mockResourceProvider{
		resources: []Resource{{URI: "pg://status"}},
		readFunc: func(_ context.Context, uri string) (ResourceContent, error) {
			if uri == "fail" {
				return ResourceContent{}, errors.New("read failed")
			}
			return NewResourceSuccess(uri, "text/plain", "content")
		},
	})
	s.SetPromptProvider(&mockPromptProvider{
		prompts: []Prompt{{Name: "explain"}},
		executeFunc: func(name string, _ map[string]string) (PromptResult, error) {
			if name == "fail" {
				return PromptResult{}, errors.New("prompt failed")
			}
			return PromptResult{Description: "ran " + name}, nil
		},
	})
	return s
}

// TestStdioRunAnswersEveryMethod drives Run with one request per
// method, plus a blank line, a parse error, a notification and an
// unknown method, and checks the response to each in order.
func TestStdioRunAnswersEveryMethod(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		``,
		`not json`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"query"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fail"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"pg://status"}}`,
		`{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"fail"}}`,
		`{"jsonrpc":"2.0","id":8,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":9,"method":"prompts/get","params":{"name":"explain"}}`,
		`{"jsonrpc":"2.0","id":10,"method":"prompts/get","params":{"name":"fail"}}`,
		`{"jsonrpc":"2.0","id":11,"method":"no/such/method"}`,
		`{"jsonrpc":"2.0","method":"no/such/notification"}`,
	}, "\n") + "\n"

	s := newStdioTestServer()
	var runErr error
	output := withStdio(t, input, func() { runErr = s.Run() })
	if runErr != nil {
		t.Fatalf("Run returned %v", runErr)
	}

	responses := decodeStdio(t, output)
	// The blank line and both notifications get no response.
	want := []struct {
		errCode int    // 0 for a result
		inBody  string // substring of the result
	}{
		{0, `"protocolVersion":"2024-11-05"`},
		{-32700, ""},
		{0, `"query"`},
		{0, `ran query`},
		{-32603, ""},
		{0, `pg://status`},
		{0, `content`},
		{-32603, ""},
		{0, `"explain"`},
		{0, `ran explain`},
		{-32603, ""},
		{-32601, ""},
	}
	if len(responses) != len(want) {
		t.Fatalf("got %d responses, want %d:\n%s", len(responses), len(want), output)
	}
	for i, w := range want {
		resp := responses[i]
		if w.errCode != 0 {
			if resp.Error == nil || resp.Error.Code != w.errCode {
				t.Errorf("response %d: error = %+v, want code %d", i, resp.Error, w.errCode)
			}
			continue
		}
		if resp.Error != nil {
			t.Errorf("response %d: unexpected error %+v", i, resp.Error)
			continue
		}
		if !strings.Contains(string(resp.Result), w.inBody) {
			t.Errorf("response %d: result %s does not contain %q", i, resp.Result, w.inBody)
		}
	}
}

// TestStdioRunReportsAnOverlongLine checks that a line beyond the
// scanner's limit ends Run with an error.
func TestStdioRunReportsAnOverlongLine(t *testing.T) {
	s := newStdioTestServer()
	var runErr error
	withStdio(t, strings.Repeat("x", ScannerMaxBufferSize+1)+"\n", func() {
		runErr = s.Run()
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "scanner error") {
		t.Errorf("Run returned %v, want a scanner error", runErr)
	}
}

// TestStdioInitializeDefaultsAndCapabilities checks the protocol
// version fallback and that only the configured providers are
// advertised.
func TestStdioInitializeDefaultsAndCapabilities(t *testing.T) {
	s := NewServer(&mockToolProvider{})
	output := withStdio(t, "", func() {
		s.handleInitialize(JSONRPCRequest{ID: 1, Method: "initialize"})
	})

	responses := decodeStdio(t, output)
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("unexpected responses: %s", output)
	}
	var result InitializeResult
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatalf("failed to decode the result: %v", err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
	if _, ok := result.Capabilities["resources"]; ok {
		t.Error("resources advertised without a resource provider")
	}
	if _, ok := result.Capabilities["prompts"]; ok {
		t.Error("prompts advertised without a prompt provider")
	}
}

// TestStdioHandlersRejectBadParams covers each handler's two params
// failures, a value that cannot be marshaled and one that does not
// decode into the expected shape, and the resource and prompt handlers
// without a provider.
func TestStdioHandlersRejectBadParams(t *testing.T) {
	full := newStdioTestServer()
	bare := NewServer(&mockToolProvider{})

	tests := []struct {
		name    string
		handler func(JSONRPCRequest)
		params  any
		code    int
	}{
		{"initialize unmarshallable", full.handleInitialize, make(chan int), -32602},
		{"initialize wrong shape", full.handleInitialize, "text", -32602},
		{"tools/call unmarshallable", full.handleToolCall, make(chan int), -32602},
		{"tools/call wrong shape", full.handleToolCall, "text", -32602},
		{"resources/read unmarshallable", full.handleResourceRead, make(chan int), -32602},
		{"resources/read wrong shape", full.handleResourceRead, "text", -32602},
		{"prompts/get unmarshallable", full.handlePromptsGet, make(chan int), -32602},
		{"prompts/get wrong shape", full.handlePromptsGet, "text", -32602},
		{"resources/list without a provider", bare.handleResourcesList, nil, -32601},
		{"resources/read without a provider", bare.handleResourceRead, nil, -32601},
		{"prompts/list without a provider", bare.handlePromptsList, nil, -32601},
		{"prompts/get without a provider", bare.handlePromptsGet, nil, -32601},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := withStdio(t, "", func() {
				tt.handler(JSONRPCRequest{ID: 1, Params: tt.params})
			})
			responses := decodeStdio(t, output)
			if len(responses) != 1 || responses[0].Error == nil ||
				responses[0].Error.Code != tt.code {
				t.Errorf("got %s, want one error with code %d", output, tt.code)
			}
		})
	}
}

// TestStdioSendFailsQuietlyOnAnUnmarshallableValue checks that a
// response or error whose payload cannot be encoded writes nothing to
// stdout rather than a broken line.
func TestStdioSendFailsQuietlyOnAnUnmarshallableValue(t *testing.T) {
	output := withStdio(t, "", func() {
		sendResponse(1, make(chan int))
		sendError(1, -32603, "Internal error", make(chan int))
	})
	if output != "" {
		t.Errorf("expected no output, got %q", output)
	}
}
