/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package embedding

import (
	"context"
	"errors"
	"testing"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
)

type fakeClient struct {
	llm.Client
	embed    []float64
	embedErr error
	model    string
	provider string
}

func (f *fakeClient) Embed(_ context.Context, _ string) ([]float64, error) {
	return f.embed, f.embedErr
}
func (f *fakeClient) Model() string    { return f.model }
func (f *fakeClient) Provider() string { return f.provider }

func TestLibProviderDelegates(t *testing.T) {
	fc := &fakeClient{embed: []float64{0.1, 0.2, 0.3}, model: "m1", provider: "openai"}
	p := &libProvider{client: fc}
	v, err := p.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 3 || v[0] != 0.1 {
		t.Fatalf("Embed = %v, want [0.1 0.2 0.3]", v)
	}
	if p.ModelName() != "m1" {
		t.Fatalf("ModelName = %q, want m1", p.ModelName())
	}
	if p.ProviderName() != "openai" {
		t.Fatalf("ProviderName = %q, want openai", p.ProviderName())
	}
}

func TestLibProviderEmbedError(t *testing.T) {
	fc := &fakeClient{embedErr: errors.New("boom")}
	p := &libProvider{client: fc}
	if _, err := p.Embed(context.Background(), "x"); err == nil {
		t.Fatal("expected error from Embed")
	}
}

func TestNewLibProviderClientError(t *testing.T) {
	// An unregistered provider name makes llm.NewClient fail, exercising
	// the error-wrapping branch in newLibProvider.
	if _, err := newLibProvider("definitely-not-registered", "k", "m", ""); err == nil {
		t.Fatal("expected error from newLibProvider for unregistered provider")
	}
}

func TestNewProviderDispatch(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
		wantPrv string
	}{
		{"openai ok", Config{Provider: "openai", OpenAIAPIKey: "k"}, false, "openai"},
		{
			"openai accepts a local model server's model name",
			Config{
				Provider:      "openai",
				OpenAIBaseURL: "http://127.0.0.1:8080/v1",
				Model:         "nomic-embed-text-v1.5",
			},
			false, "openai",
		},
		{"voyage any model accepted", Config{Provider: "voyage", VoyageAPIKey: "k", Model: "voyage-4-future"}, false, "voyage"},
		{"gemini any model accepted", Config{Provider: "gemini", GeminiAPIKey: "k", Model: "text-embedding-004"}, false, "gemini"},
		{"openai needs key or url", Config{Provider: "openai"}, true, ""},
		{"voyage ok", Config{Provider: "voyage", VoyageAPIKey: "k"}, false, "voyage"},
		{"voyage needs key", Config{Provider: "voyage"}, true, ""},
		{"gemini ok", Config{Provider: "gemini", GeminiAPIKey: "k"}, false, "gemini"},
		{"gemini needs key", Config{Provider: "gemini"}, true, ""},
		{"ollama ok", Config{Provider: "ollama"}, false, "ollama"},
		{"ollama any custom model accepted", Config{Provider: "ollama", Model: "any-custom-model"}, false, "ollama"},
		{"unknown", Config{Provider: "nope"}, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewProvider(c.cfg)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for %+v", c.cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.ProviderName() != c.wantPrv {
				t.Fatalf("ProviderName = %q, want %q", p.ProviderName(), c.wantPrv)
			}
		})
	}
}

// TestNewProviderModelSelection checks that a caller-supplied model name is
// passed through unchanged, whatever it is, and that an empty model still
// falls back to the provider default.
func TestNewProviderModelSelection(t *testing.T) {
	cases := []struct {
		name      string
		cfg       Config
		wantModel string
	}{
		{
			"custom openai-compatible model accepted",
			Config{
				Provider:      "openai",
				OpenAIBaseURL: "http://127.0.0.1:8080/v1",
				Model:         "nomic-embed-text-v1.5",
			},
			"nomic-embed-text-v1.5",
		},
		{
			"openai default applied when model empty",
			Config{Provider: "openai", OpenAIAPIKey: "k"},
			defaultModels["openai"],
		},
		{
			"voyage default applied when model empty",
			Config{Provider: "voyage", VoyageAPIKey: "k"},
			defaultModels["voyage"],
		},
		{
			"gemini default applied when model empty",
			Config{Provider: "gemini", GeminiAPIKey: "k"},
			defaultModels["gemini"],
		},
		{
			"ollama default applied when model empty",
			Config{Provider: "ollama"},
			defaultModels["ollama"],
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := NewProvider(c.cfg)
			if err != nil {
				t.Fatalf("NewProvider(%+v): %v", c.cfg, err)
			}
			if p.ModelName() != c.wantModel {
				t.Fatalf("ModelName = %q, want %q", p.ModelName(), c.wantModel)
			}
		})
	}
}
