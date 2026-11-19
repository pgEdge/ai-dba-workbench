/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package investigate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgEdge/pgedge-go-llm-lib/llm/proxy"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/wbclient"
)

func TestParseTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Target
		str     string
		wantErr bool
	}{
		{"", Target{}, "default", false},
		{"default", Target{}, "default", false},
		{"anthropic", Target{Provider: "anthropic"}, "anthropic", false},
		{"openai:gpt-5", Target{Provider: "openai", Model: "gpt-5"}, "openai:gpt-5", false},
		{"ollama:qwen3:32b", Target{Provider: "ollama", Model: "qwen3:32b"}, "ollama:qwen3:32b", false},
		{"bogus:x", Target{}, "", true},
	}
	for _, tt := range tests {
		got, err := ParseTarget(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseTarget(%q) error = %v", tt.in, err)
			continue
		}
		if tt.wantErr {
			continue
		}
		if got != tt.want || got.String() != tt.str {
			t.Errorf("ParseTarget(%q) = %+v (%s)", tt.in, got, got.String())
		}
	}
}

func TestToolNames(t *testing.T) {
	t.Parallel()
	tr := Transcript{ToolCalls: []ToolCall{{Name: "a"}, {Name: "b"}, {Name: "a"}}}
	if got := strings.Join(tr.ToolNames(), ","); got != "a,b" {
		t.Errorf("ToolNames = %s", got)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if truncate("abc", 5) != "abc" {
		t.Error("short string changed")
	}
	if got := truncate("abcdef", 3); got != "abc...[truncated]" {
		t.Errorf("truncate = %q", got)
	}
	// "é" is two bytes; cutting inside it must back off to the rune start.
	if got := truncate("aé", 2); got != "a...[truncated]" {
		t.Errorf("truncate multibyte = %q", got)
	}
}

// fakeServer scripts the LLM proxy and MCP endpoints.
type fakeServer struct {
	mu        sync.Mutex
	tools     []string
	replies   []proxy.ChatResponse
	chatReqs  []proxy.ChatRequest
	toolCalls []toolCallRequest
	toolReply func(name string) (int, toolCallResponse)
	chatFail  bool
	listFail  bool
}

func (f *fakeServer) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(toolListPath, func(w http.ResponseWriter, _ *http.Request) {
		if f.listFail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var out toolListResponse
		for _, n := range f.tools {
			out.Tools = append(out.Tools, struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			}{Name: n, Description: n + " tool", InputSchema: json.RawMessage(`{"type":"object"}`)})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc(llmChatPath, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var req proxy.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode chat request: %v", err)
		}
		f.chatReqs = append(f.chatReqs, req)
		if f.chatFail || len(f.replies) == 0 {
			http.Error(w, "provider down", http.StatusBadGateway)
			return
		}
		reply := f.replies[0]
		if len(f.replies) > 1 {
			f.replies = f.replies[1:]
		}
		_ = json.NewEncoder(w).Encode(reply)
	})
	mux.HandleFunc(toolCallPath, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var req toolCallRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.toolCalls = append(f.toolCalls, req)
		status, body := http.StatusOK, toolCallResponse{}
		if f.toolReply != nil {
			status, body = f.toolReply(req.Name)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	return mux
}

func toolUse(id, name string) llm.ContentBlock {
	return llm.ContentBlock{Type: llm.BlockToolUse, ToolUse: &llm.ToolUse{ID: id, Name: name, Input: json.RawMessage(`{"connection_id":1}`)}}
}

func text(s string) llm.ContentBlock { return llm.ContentBlock{Type: llm.BlockText, Text: s} }

func textResult(s string, isErr bool) toolCallResponse {
	r := toolCallResponse{IsError: isErr}
	if s != "" {
		r.Content = append(r.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: s})
	}
	return r
}

func runLoop(t *testing.T, f *fakeServer, maxIter int) Transcript {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	clock := time.Unix(0, 0)
	loop := &ChatLoop{
		Client:       wbclient.New(srv.URL, "tok", 5*time.Second),
		SystemPrompt: "be Ellie",
		Now: func() time.Time {
			clock = clock.Add(time.Second)
			return clock
		},
	}
	tr, err := loop.Investigate(context.Background(), Request{
		Prompt: "investigate", Target: Target{Provider: "anthropic", Model: "m"}, MaxIterations: maxIter,
	})
	if err != nil {
		t.Fatalf("Investigate: %v", err)
	}
	return tr
}

func TestChatLoopCompletes(t *testing.T) {
	t.Parallel()
	f := &fakeServer{
		tools: []string{"query_database"},
		replies: []proxy.ChatResponse{
			{Content: []llm.ContentBlock{text("looking"), toolUse("t1", "query_database")}, Usage: llm.TokenUsage{TotalTokens: 10}},
			{Content: []llm.ContentBlock{text("Root cause"), text("is locks")}, Usage: llm.TokenUsage{TotalTokens: 5}},
		},
		toolReply: func(string) (int, toolCallResponse) { return http.StatusOK, textResult("rows", false) },
	}
	tr := runLoop(t, f, 5)
	if tr.Outcome != OutcomeCompleted || tr.FinalText != "Root cause\nis locks" {
		t.Fatalf("transcript = %+v", tr)
	}
	if tr.Iterations != 2 || tr.Usage.TotalTokens != 15 || tr.Duration != time.Second {
		t.Errorf("iterations=%d tokens=%d duration=%v", tr.Iterations, tr.Usage.TotalTokens, tr.Duration)
	}
	if len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Result != "rows" {
		t.Errorf("tool calls = %+v", tr.ToolCalls)
	}
	// The second request carries the assistant turn and the tool result,
	// plus the system prompt and target on every request.
	second := f.chatReqs[1]
	if len(second.Messages) != 3 || second.SystemPrompt != "be Ellie" || second.Provider != "anthropic" || second.Model != "m" {
		t.Fatalf("second request = %+v", second)
	}
	result := second.Messages[2].Content[0]
	if result.Type != llm.BlockToolResult || result.ToolUseID != "t1" || result.Text != "rows" {
		t.Errorf("tool result block = %+v", result)
	}
	if len(second.Tools) != 1 || string(second.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Errorf("tools = %+v", second.Tools)
	}
	if string(f.toolCalls[0].Arguments) != `{"connection_id":1}` {
		t.Errorf("tool arguments = %s", f.toolCalls[0].Arguments)
	}
}

func TestChatLoopEndings(t *testing.T) {
	t.Parallel()
	loopForever := []proxy.ChatResponse{{Content: []llm.ContentBlock{toolUse("t", "query_database")}}}
	tests := []struct {
		name      string
		f         *fakeServer
		maxIter   int
		outcome   Outcome
		finalHas  string
		errHas    string
		toolCalls int
	}{
		{
			name:    "iteration limit",
			f:       &fakeServer{tools: []string{"query_database"}, replies: loopForever},
			maxIter: 2, outcome: OutcomeIterationLimit, finalHas: "allowed number of steps", toolCalls: 2,
		},
		{
			name:    "unknown tool",
			f:       &fakeServer{tools: []string{"other"}, replies: loopForever},
			maxIter: 5, outcome: OutcomeUnknownTool, finalHas: "not available",
		},
		{
			name:    "no tools",
			f:       &fakeServer{},
			maxIter: 5, outcome: OutcomeNoTools, finalHas: "permission",
		},
		{
			name:    "list fails",
			f:       &fakeServer{listFail: true},
			maxIter: 5, outcome: OutcomeError, errHas: "list tools",
		},
		{
			name:    "chat fails",
			f:       &fakeServer{tools: []string{"x"}, chatFail: true},
			maxIter: 5, outcome: OutcomeError, errHas: "LLM request failed",
		},
		{
			name: "repeated tool error",
			f: &fakeServer{tools: []string{"query_database"}, replies: loopForever,
				toolReply: func(string) (int, toolCallResponse) { return http.StatusOK, textResult("Access denied", true) }},
			maxIter: 10, outcome: OutcomeToolFailures, finalHas: "Access denied", toolCalls: 3,
		},
		{
			name: "repeated HTTP failure",
			f: &fakeServer{tools: []string{"query_database"}, replies: loopForever,
				toolReply: func(string) (int, toolCallResponse) { return http.StatusInternalServerError, toolCallResponse{} }},
			maxIter: 10, outcome: OutcomeToolFailures, finalHas: "Tool call failed with status 500", toolCalls: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := runLoop(t, tt.f, tt.maxIter)
			if tr.Outcome != tt.outcome {
				t.Fatalf("outcome = %s (%+v)", tr.Outcome, tr)
			}
			if !strings.Contains(tr.FinalText, tt.finalHas) || !strings.Contains(tr.Error, tt.errHas) {
				t.Errorf("final = %q error = %q", tr.FinalText, tr.Error)
			}
			if len(tr.ToolCalls) != tt.toolCalls {
				t.Errorf("tool calls = %d, want %d", len(tr.ToolCalls), tt.toolCalls)
			}
		})
	}
}

func TestCallToolTexts(t *testing.T) {
	t.Parallel()
	replies := map[string]toolCallResponse{
		"empty_ok":  textResult("", false),
		"empty_err": textResult("", true),
	}
	f := &fakeServer{toolReply: func(name string) (int, toolCallResponse) {
		if name == "forbidden" {
			return http.StatusForbidden, toolCallResponse{}
		}
		return http.StatusOK, replies[name]
	}}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	loop := &ChatLoop{Client: wbclient.New(srv.URL, "", time.Second)}
	ctx := context.Background()

	if got, isErr := loop.callTool(ctx, &llm.ToolUse{Name: "empty_ok"}); got != "No data returned" || isErr {
		t.Errorf("empty_ok = %q %v", got, isErr)
	}
	if got, isErr := loop.callTool(ctx, &llm.ToolUse{Name: "empty_err"}); got != "Tool execution failed" || !isErr {
		t.Errorf("empty_err = %q %v", got, isErr)
	}
	if got, _ := loop.callTool(ctx, &llm.ToolUse{Name: "forbidden"}); got != "Tool execution error: Tool call failed with status 403" {
		t.Errorf("forbidden = %q", got)
	}
	srv.Close()
	if got, isErr := loop.callTool(ctx, &llm.ToolUse{Name: "x"}); !strings.HasPrefix(got, "Tool execution error: POST") || !isErr {
		t.Errorf("transport failure = %q", got)
	}
}

func TestChatLoopCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var loop *ChatLoop
	f := &fakeServer{
		tools:   []string{"query_database"},
		replies: []proxy.ChatResponse{{Content: []llm.ContentBlock{toolUse("t", "query_database")}}},
		toolReply: func(string) (int, toolCallResponse) {
			cancel()
			return http.StatusOK, textResult("rows", false)
		},
	}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	loop = &ChatLoop{Client: wbclient.New(srv.URL, "", time.Second)}
	tr, err := loop.Investigate(ctx, Request{Prompt: "p", MaxIterations: 5})
	if err == nil || tr.Outcome != OutcomeError {
		t.Fatalf("expected cancellation, got %v %+v", err, tr)
	}

	// A context canceled before the loop starts its next iteration.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	f2 := &fakeServer{tools: []string{"x"}}
	srv2 := httptest.NewServer(f2.handler(t))
	defer srv2.Close()
	loop2 := &ChatLoop{Client: wbclient.New(srv2.URL, "", time.Second)}
	tr2 := loop2.run(ctx2, Request{MaxIterations: 1})
	if tr2.Outcome != OutcomeError {
		t.Errorf("outcome = %s", tr2.Outcome)
	}
}

func TestChatLoopCanceledAfterCompleting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeServer{
		tools:   []string{"query_database"},
		replies: []proxy.ChatResponse{{Content: []llm.ContentBlock{text("Root cause")}}},
	}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	calls := 0
	loop := &ChatLoop{
		Client: wbclient.New(srv.URL, "", 5*time.Second),
		// The second call, after the loop has finished, cancels ctx.
		Now: func() time.Time {
			calls++
			if calls == 2 {
				cancel()
			}
			return time.Unix(int64(calls), 0)
		},
	}
	tr, err := loop.Investigate(ctx, Request{Prompt: "p", MaxIterations: 5})
	if err != nil || tr.Outcome != OutcomeCompleted || tr.FinalText != "Root cause" {
		t.Fatalf("Investigate = %+v, %v", tr, err)
	}
}

func TestLoadSystemPromptFromClient(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "..", "..", ClientPromptFile)
	prompt, err := LoadSystemPrompt(path)
	if err != nil {
		t.Fatalf("LoadSystemPrompt: %v", err)
	}
	if !strings.HasPrefix(prompt, "You are Ellie") || !strings.Contains(prompt, "```sql") {
		t.Errorf("unexpected prompt start or missing unescaped fence: %.80q", prompt)
	}
	if strings.Contains(prompt, "\\`") {
		t.Error("prompt still contains escaped backticks")
	}
}

func TestLoadSystemPromptVariants(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if got, err := LoadSystemPrompt(write("p.txt", "  plain prompt \n")); err != nil || got != "plain prompt" {
		t.Errorf("plain = %q %v", got, err)
	}
	if _, err := LoadSystemPrompt(write("empty.md", " \n")); err == nil {
		t.Error("expected empty prompt error")
	}
	if _, err := LoadSystemPrompt(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("expected read error")
	}
	got, err := LoadSystemPrompt(write("ok.ts", "x\n"+clientPromptDecl+"a\\`b\\\\c\\$d $e\\n\\t`;"))
	if err != nil || got != "a`b\\c$d $e\n\t" {
		t.Errorf("escapes = %q %v", got, err)
	}

	bad := map[string]string{
		"none.ts":   "const X = `a`;",
		"interp.ts": clientPromptDecl + "a ${b}`",
		"escape.ts": clientPromptDecl + "a \\u0041`",
		"open.ts":   clientPromptDecl + "never closed",
		"trail.ts":  clientPromptDecl + "ends \\",
	}
	for name, body := range bad {
		if _, err := LoadSystemPrompt(write(name, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
