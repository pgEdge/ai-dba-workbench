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
	"testing"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

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
	r := &libReasoning{client: fc}
	out, err := r.Classify(context.Background(), "analyse this anomaly")
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
		fc.gotReq.Messages[0].Content[0].Text != "analyse this anomaly" {
		t.Fatalf("user message content = %+v", fc.gotReq.Messages[0].Content)
	}
	if fc.gotReq.MaxTokens == nil || *fc.gotReq.MaxTokens != 500 {
		t.Fatalf("MaxTokens = %v, want 500", fc.gotReq.MaxTokens)
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

// TestNewLibReasoningDefaultModel verifies that an empty model falls back
// to the per-provider default and that the resulting client reports it.
func TestNewLibReasoningDefaultModel(t *testing.T) {
	r, err := newLibReasoning("openai", "test-key", "", "")
	if err != nil {
		t.Fatalf("newLibReasoning: %v", err)
	}
	if r.ModelName() != defaultReasoningModels["openai"] {
		t.Fatalf("ModelName = %q, want default %q", r.ModelName(), defaultReasoningModels["openai"])
	}
}

// TestNewLibReasoningExplicitModel verifies an explicit model is preserved.
func TestNewLibReasoningExplicitModel(t *testing.T) {
	r, err := newLibReasoning("openai", "test-key", "gpt-4o", "")
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
	if _, err := newLibReasoning("does-not-exist", "k", "m", ""); err == nil {
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
