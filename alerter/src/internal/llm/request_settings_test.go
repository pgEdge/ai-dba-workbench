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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgEdge/pgedge-go-llm-lib/llm/provider/anthropic"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/llmsettings"
)

// captureServer records the JSON body of the first request it receives
// and answers it with a 400, so the client returns at once without
// retrying. The tests only care about what was sent.
func captureServer(t *testing.T) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var (
		mu   sync.Mutex
		body map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		mu.Lock()
		if body == nil {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("decode request body: %v", err)
			}
		}
		mu.Unlock()
		http.Error(w, `{"error":{"message":"test"}}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return body
	}
}

// TestNewReasoningProviderSendsRequestSettings checks end to end that a
// provider section's reasoning_effort and extra_body reach the request the
// OpenAI provider sends (issue #726).
func TestNewReasoningProviderSendsRequestSettings(t *testing.T) {
	srv, sent := captureServer(t)
	cfg := config.NewConfig()
	cfg.LLM.ReasoningProvider = "openai"
	cfg.LLM.OpenAI.BaseURL = srv.URL
	cfg.LLM.OpenAI.ReasoningModel = "test-model"
	cfg.LLM.OpenAI.RequestSettings = llmsettings.Settings{
		ReasoningEffort: "low",
		ExtraBody:       map[string]any{"gateway_tag": "alerter"},
	}

	p, err := NewReasoningProvider(cfg)
	if err != nil {
		t.Fatalf("NewReasoningProvider: %v", err)
	}
	if _, err := p.Classify(context.Background(), "prompt"); err == nil {
		t.Fatal("expected the test server's 400 to fail the call")
	}

	body := sent()
	if body == nil {
		t.Fatal("no request reached the test server")
	}
	if body["gateway_tag"] != "alerter" {
		t.Errorf("extra_body field missing from the request: %v", body)
	}
	if body["reasoning_effort"] != "low" {
		t.Errorf("reasoning_effort = %v, want low", body["reasoning_effort"])
	}
}

// TestNewReasoningProviderEachSectionsSettings checks that each provider
// takes its settings from its own section of the configuration.
func TestNewReasoningProviderEachSectionsSettings(t *testing.T) {
	tests := []struct {
		provider string
		set      func(*config.Config, llmsettings.Settings)
		keyFile  func(*config.Config, string)
	}{
		{"anthropic", func(c *config.Config, s llmsettings.Settings) { c.LLM.Anthropic.RequestSettings = s },
			func(c *config.Config, f string) { c.LLM.Anthropic.APIKeyFile = f }},
		{"gemini", func(c *config.Config, s llmsettings.Settings) { c.LLM.Gemini.RequestSettings = s },
			func(c *config.Config, f string) { c.LLM.Gemini.APIKeyFile = f }},
		{"ollama", func(c *config.Config, s llmsettings.Settings) { c.LLM.Ollama.RequestSettings = s }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.LLM.ReasoningProvider = tt.provider
			tt.set(cfg, llmsettings.Settings{PromptCaching: "auto"})
			if tt.keyFile != nil {
				tt.keyFile(cfg, writeKeyFile(t, "test-key-12345678\n"))
				if err := cfg.LoadAPIKeys(); err != nil {
					t.Fatalf("LoadAPIKeys: %v", err)
				}
			}
			p, err := NewReasoningProvider(cfg)
			if err != nil {
				t.Fatalf("NewReasoningProvider: %v", err)
			}
			lr, ok := p.(*libReasoning)
			if !ok {
				t.Fatalf("expected *libReasoning, got %T", p)
			}
			if !lr.cacheSystem {
				t.Errorf("%s: prompt_caching auto did not reach the client", tt.provider)
			}
		})
	}
}

// TestLibReasoningSystemCaching checks that the system prompt is marked
// for Anthropic prompt caching only when prompt_caching is auto.
func TestLibReasoningSystemCaching(t *testing.T) {
	for _, cache := range []bool{false, true} {
		fc := &fakeChatClient{resp: &pgllm.ChatResponse{Content: []pgllm.ContentBlock{
			{Type: pgllm.BlockText, Text: `{"decision":"alert"}`},
		}}}
		r := &libReasoning{client: fc, maxTokens: 4096, cacheSystem: cache}
		if _, err := r.Reevaluate(context.Background(), "prompt"); err != nil {
			t.Fatalf("Reevaluate: %v", err)
		}
		ext := pgllm.FindExtension[anthropic.Extension](fc.gotReq, "anthropic")
		if cache != (ext != nil) {
			t.Fatalf("cacheSystem=%v: extension = %+v", cache, ext)
		}
		if cache && (!ext.CacheSystem || ext.CacheToolsThrough != -1) {
			t.Errorf("extension = %+v, want only the system prompt cached", ext)
		}
	}
}
