/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package llm

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// thinkingBlock is the content-block type a reasoning model emits for its
// chain of thought. The library has no named constant for it, so the tests
// use the wire value directly to reproduce the issue #399 response shape.
const thinkingBlock pgllm.ContentBlockType = "thinking"

// fakeChatClient is a test double for pgllm.Client. It embeds the
// interface so it satisfies every method, but only Chat and Model are
// overridden; calling any other method would panic on the nil embedded
// interface, which is fine because libReasoning only uses Chat/Model.
type fakeChatClient struct {
	pgllm.Client
	gotReq pgllm.ChatRequest
	resp   *pgllm.ChatResponse
	err    error
	model  string
}

func (f *fakeChatClient) Chat(_ context.Context, req pgllm.ChatRequest) (*pgllm.ChatResponse, error) {
	f.gotReq = req
	return f.resp, f.err
}

func (f *fakeChatClient) Model() string { return f.model }

func TestLibReasoningClassify(t *testing.T) {
	fc := &fakeChatClient{
		model: "claude-3-5-haiku-20241022",
		resp: &pgllm.ChatResponse{Content: []pgllm.ContentBlock{
			{Type: pgllm.BlockText, Text: `{"decision":"alert",`},
			// A non-text block must be ignored by the concatenation.
			{Type: pgllm.BlockToolUse},
			{Type: pgllm.BlockText, Text: `"confidence":0.9}`},
		}},
	}
	r := &libReasoning{client: fc, maxTokens: 4096}
	out, err := r.Classify(context.Background(), "analyze this anomaly")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != `{"decision":"alert","confidence":0.9}` {
		t.Fatalf("text = %q", out)
	}
	if fc.gotReq.SystemPrompt != classificationSystemPrompt {
		t.Fatalf("system prompt not set to classificationSystemPrompt")
	}
	if len(fc.gotReq.Messages) != 1 || fc.gotReq.Messages[0].Role != pgllm.RoleUser {
		t.Fatalf("expected one user message, got %+v", fc.gotReq.Messages)
	}
	if len(fc.gotReq.Messages[0].Content) != 1 ||
		fc.gotReq.Messages[0].Content[0].Type != pgllm.BlockText ||
		fc.gotReq.Messages[0].Content[0].Text != "analyze this anomaly" {
		t.Fatalf("user message content = %+v", fc.gotReq.Messages[0].Content)
	}
	if fc.gotReq.MaxTokens == nil || *fc.gotReq.MaxTokens != 4096 {
		t.Fatalf("MaxTokens = %v, want 4096", fc.gotReq.MaxTokens)
	}
	if fc.gotReq.Temperature == nil || *fc.gotReq.Temperature != 0.1 {
		t.Fatalf("Temperature = %v, want 0.1", fc.gotReq.Temperature)
	}
	if r.ModelName() != "claude-3-5-haiku-20241022" {
		t.Fatalf("ModelName = %q", r.ModelName())
	}
}

func TestLibReasoningClassifyError(t *testing.T) {
	r := &libReasoning{client: &fakeChatClient{err: errors.New("boom")}}
	if _, err := r.Classify(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}

// TestLibReasoningClassifyNoTextContent verifies that a response carrying
// no usable text is reported as ErrNoTextContent rather than an empty
// string. A reasoning model whose thinking exhausts the output budget
// produces exactly these shapes (issue #399), and the engine must see a
// failure instead of parsing an empty verdict.
func TestLibReasoningClassifyNoTextContent(t *testing.T) {
	tests := []struct {
		name string
		resp *pgllm.ChatResponse
	}{
		{
			name: "no content blocks",
			resp: &pgllm.ChatResponse{Content: []pgllm.ContentBlock{}},
		},
		{
			name: "nil response",
			resp: nil,
		},
		{
			name: "reasoning block only",
			resp: &pgllm.ChatResponse{Content: []pgllm.ContentBlock{
				{Type: thinkingBlock, Text: "weighing the evidence at length"},
			}},
		},
		{
			name: "whitespace-only text",
			resp: &pgllm.ChatResponse{Content: []pgllm.ContentBlock{
				{Type: pgllm.BlockText, Text: " \n\t "},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &fakeChatClient{model: "gpt-4o-mini", resp: tt.resp}
			r := &libReasoning{client: fc, maxTokens: DefaultReasoningMaxTokens}
			out, err := r.Classify(context.Background(), "prompt")
			if !errors.Is(err, ErrNoTextContent) {
				t.Fatalf("expected ErrNoTextContent, got %v", err)
			}
			if out != "" {
				t.Fatalf("out = %q, want empty string", out)
			}
		})
	}
}

// TestErrNoTextContentMessage verifies the sentinel names the actionable
// cause, since the engine records it in the candidate tier-3 error field.
func TestErrNoTextContentMessage(t *testing.T) {
	msg := ErrNoTextContent.Error()
	for _, want := range []string{"no text content", "reasoning tokens", "llm.max_tokens"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrNoTextContent message %q does not mention %q", msg, want)
		}
	}
}

// TestNewLibReasoningMaxTokensResolution verifies that a positive
// configured budget reaches the chat request unchanged and that an unset,
// zero, or negative budget falls back to DefaultReasoningMaxTokens.
func TestNewLibReasoningMaxTokensResolution(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
		want      int
	}{
		{"configured value used", 8192, 8192},
		{"unset falls back", 0, DefaultReasoningMaxTokens},
		{"negative falls back", -1, DefaultReasoningMaxTokens},
		{"small configured value still honoured", 32, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := newLibReasoning("openai", "test-key", "gpt-4o", "", tt.maxTokens)
			if err != nil {
				t.Fatalf("newLibReasoning: %v", err)
			}
			lr, ok := p.(*libReasoning)
			if !ok {
				t.Fatalf("expected *libReasoning, got %T", p)
			}
			if lr.maxTokens != tt.want {
				t.Fatalf("maxTokens = %d, want %d", lr.maxTokens, tt.want)
			}

			// The resolved budget must reach the request itself.
			fc := &fakeChatClient{resp: &pgllm.ChatResponse{Content: []pgllm.ContentBlock{
				{Type: pgllm.BlockText, Text: `{"decision":"alert"}`},
			}}}
			lr.client = fc
			if _, err := lr.Classify(context.Background(), "prompt"); err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if fc.gotReq.MaxTokens == nil || *fc.gotReq.MaxTokens != tt.want {
				t.Fatalf("request MaxTokens = %v, want %d", fc.gotReq.MaxTokens, tt.want)
			}
		})
	}
}

// TestDefaultReasoningMaxTokensIsGenerousEnough guards the issue #399
// regression: the fallback must leave a reasoning model room for both its
// thinking block and the verdict, so it must exceed the old 500-token cap.
func TestDefaultReasoningMaxTokensIsGenerousEnough(t *testing.T) {
	const oldCap = 500
	if DefaultReasoningMaxTokens <= oldCap {
		t.Fatalf("DefaultReasoningMaxTokens = %d, want > %d",
			DefaultReasoningMaxTokens, oldCap)
	}
	if DefaultReasoningMaxTokens != config.DefaultLLMMaxTokens {
		t.Errorf("DefaultReasoningMaxTokens = %d, want it to match config.DefaultLLMMaxTokens = %d",
			DefaultReasoningMaxTokens, config.DefaultLLMMaxTokens)
	}
}

// TestNewReasoningProviderPassesConfiguredMaxTokens verifies the config
// value is plumbed all the way to the provider, and that an unset value
// resolves to the package default.
func TestNewReasoningProviderPassesConfiguredMaxTokens(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
		want      int
	}{
		{"configured value used", 1024, 1024},
		{"unset falls back", 0, DefaultReasoningMaxTokens},
		{"negative falls back", -2, DefaultReasoningMaxTokens},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.LLM.ReasoningProvider = "ollama"
			cfg.LLM.MaxTokens = tt.maxTokens
			p, err := NewReasoningProvider(cfg)
			if err != nil {
				t.Fatalf("NewReasoningProvider: %v", err)
			}
			lr, ok := p.(*libReasoning)
			if !ok {
				t.Fatalf("expected *libReasoning, got %T", p)
			}
			if lr.maxTokens != tt.want {
				t.Fatalf("maxTokens = %d, want %d", lr.maxTokens, tt.want)
			}
		})
	}
}

// TestNewLibReasoningOllamaExplicitBaseURL verifies that an explicit BaseURL is
// accepted and that the default model (llama3.2) is applied when none is given.
// No network is attempted: the library constructs the client without dialing.
func TestNewLibReasoningOllamaExplicitBaseURL(t *testing.T) {
	r, err := newLibReasoning("ollama", "", "", "http://localhost:11434", 0)
	if err != nil {
		t.Fatalf("newLibReasoning: %v", err)
	}
	if r.ModelName() != "llama3.2" {
		t.Fatalf("ModelName = %q, want llama3.2", r.ModelName())
	}
}

// TestNewLibReasoningDefaultModel verifies that an empty model falls back
// to the per-provider default and that the resulting client reports it.
func TestNewLibReasoningDefaultModel(t *testing.T) {
	r, err := newLibReasoning("openai", "test-key", "", "", 4096)
	if err != nil {
		t.Fatalf("newLibReasoning: %v", err)
	}
	if r.ModelName() != defaultReasoningModels["openai"] {
		t.Fatalf("ModelName = %q, want default %q", r.ModelName(), defaultReasoningModels["openai"])
	}
}

// TestNewLibReasoningExplicitModel verifies an explicit model is preserved.
func TestNewLibReasoningExplicitModel(t *testing.T) {
	r, err := newLibReasoning("openai", "test-key", "gpt-4o", "", 4096)
	if err != nil {
		t.Fatalf("newLibReasoning: %v", err)
	}
	if r.ModelName() != "gpt-4o" {
		t.Fatalf("ModelName = %q, want gpt-4o", r.ModelName())
	}
}

// TestNewLibReasoningUnknownProvider verifies NewClient errors propagate
// through newLibReasoning as a wrapped error.
func TestNewLibReasoningUnknownProvider(t *testing.T) {
	if _, err := newLibReasoning("does-not-exist", "k", "m", "", 4096); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// writeKeyFile creates a temporary API-key file and returns its path.
func writeKeyFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/api.key"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

// TestNewReasoningProviderWithKeys covers the success paths for the
// key-required providers (openai, anthropic, gemini), which the existing
// missing-key tests in llm_test.go do not reach. Keys are injected via the
// APIKeyFile mechanism and LoadAPIKeys, matching production loading.
func TestNewReasoningProviderWithKeys(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() *config.Config
		wantModel string
	}{
		{
			name: "openai with key and explicit model",
			setup: func() *config.Config {
				c := config.NewConfig()
				c.LLM.ReasoningProvider = "openai"
				c.LLM.OpenAI.APIKeyFile = writeKeyFile(t, "sk-test-12345678\n")
				c.LLM.OpenAI.ReasoningModel = "gpt-4o"
				return c
			},
			wantModel: "gpt-4o",
		},
		{
			name: "anthropic with key uses default model",
			setup: func() *config.Config {
				c := config.NewConfig()
				c.LLM.ReasoningProvider = "anthropic"
				c.LLM.Anthropic.APIKeyFile = writeKeyFile(t, "sk-ant-12345678\n")
				c.LLM.Anthropic.ReasoningModel = ""
				return c
			},
			wantModel: defaultReasoningModels["anthropic"],
		},
		{
			name: "gemini with key and explicit model",
			setup: func() *config.Config {
				c := config.NewConfig()
				c.LLM.ReasoningProvider = "gemini"
				c.LLM.Gemini.APIKeyFile = writeKeyFile(t, "AIza-test-12345678\n")
				c.LLM.Gemini.ReasoningModel = "gemini-2.5-pro"
				return c
			},
			wantModel: "gemini-2.5-pro",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setup()
			if err := cfg.LoadAPIKeys(); err != nil {
				t.Fatalf("LoadAPIKeys: %v", err)
			}
			p, err := NewReasoningProvider(cfg)
			if err != nil {
				t.Fatalf("NewReasoningProvider: %v", err)
			}
			if p == nil {
				t.Fatal("expected provider, got nil")
			}
			if p.ModelName() != tt.wantModel {
				t.Fatalf("ModelName = %q, want %q", p.ModelName(), tt.wantModel)
			}
		})
	}
}
