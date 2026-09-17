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
	"math"
	"os"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/embedding"
)

// fakeEmbedding implements embedding.Provider for adapter tests.
type fakeEmbedding struct {
	vec       []float64
	err       error
	model     string
	provider  string
	dim       int
	lastInput string
}

func (f *fakeEmbedding) Embed(_ context.Context, text string) ([]float64, error) {
	f.lastInput = text
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

func (f *fakeEmbedding) Dimensions() int   { return f.dim }
func (f *fakeEmbedding) ModelName() string { return f.model }
func (f *fakeEmbedding) ProviderName() string {
	return f.provider
}

// TestEmbeddingAdapter_GenerateEmbedding_KeepsNativeWidth checks that the
// adapter returns the model's own width, neither padding a short vector
// nor truncating a wide one, and normalizes the result to unit length.
// Padding to the halfvec column width, and rejecting a vector that is
// too wide for it, is the datastore's job via embedding.PadTo.
func TestEmbeddingAdapter_GenerateEmbedding_KeepsNativeWidth(t *testing.T) {
	tests := []struct {
		name string
		dim  int
	}{
		{name: "short", dim: 3},
		{name: "openai small", dim: 1536},
		{name: "wider than 1536", dim: 3072},
		{name: "wider than the halfvec column", dim: embedding.MaxDimensions + 96},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := make([]float64, tc.dim)
			for i := range in {
				in[i] = float64(i + 1)
			}
			fake := &fakeEmbedding{vec: in, model: "m", provider: "p", dim: tc.dim}
			a := &embeddingAdapter{provider: fake}

			got, err := a.GenerateEmbedding(context.Background(), "hello")
			if err != nil {
				t.Fatalf("GenerateEmbedding: %v", err)
			}
			if len(got) != tc.dim {
				t.Fatalf("len = %d, want %d", len(got), tc.dim)
			}
			if fake.lastInput != "hello" {
				t.Errorf("lastInput = %q, want hello", fake.lastInput)
			}
			// The last input component must survive: a truncating
			// adapter would drop it.
			if got[tc.dim-1] == 0 {
				t.Errorf("last component is zero; vector was truncated")
			}
			var sumSq float64
			for _, v := range got {
				sumSq += float64(v) * float64(v)
			}
			if math.Abs(math.Sqrt(sumSq)-1.0) > 1e-5 {
				t.Errorf("not normalized: magnitude = %v", math.Sqrt(sumSq))
			}
		})
	}
}

func TestEmbeddingAdapter_GenerateEmbedding_ProviderError(t *testing.T) {
	fake := &fakeEmbedding{err: errors.New("boom")}
	a := &embeddingAdapter{provider: fake}
	_, err := a.GenerateEmbedding(context.Background(), "x")
	if err == nil || err.Error() != "boom" {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestEmbeddingAdapter_ModelName(t *testing.T) {
	fake := &fakeEmbedding{model: "ada-xyz"}
	a := &embeddingAdapter{provider: fake}
	if got := a.ModelName(); got != "ada-xyz" {
		t.Errorf("ModelName = %q, want ada-xyz", got)
	}
}

func TestNewEmbeddingProvider_NilConfig(t *testing.T) {
	p, err := NewEmbeddingProvider(nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if p != nil {
		t.Errorf("provider = %v, want nil", p)
	}
}

func TestNewEmbeddingProvider_Disabled(t *testing.T) {
	for _, s := range []string{"", "none", "disabled"} {
		cfg := &config.Config{}
		cfg.LLM.EmbeddingProvider = s
		p, err := NewEmbeddingProvider(cfg)
		if err != nil {
			t.Errorf("provider=%q: err = %v", s, err)
		}
		if p != nil {
			t.Errorf("provider=%q: got %v, want nil", s, p)
		}
	}
}

func TestNewEmbeddingProvider_Unknown(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.EmbeddingProvider = "nope"
	_, err := NewEmbeddingProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown embedding provider") {
		t.Errorf("err = %v, want unknown embedding provider", err)
	}
}

func TestNewEmbeddingProvider_OpenAIMissingKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.EmbeddingProvider = "openai"
	// No API key and no BaseURL => missing key error.
	_, err := NewEmbeddingProvider(cfg)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Errorf("err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestNewEmbeddingProvider_OpenAIWithBaseURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.EmbeddingProvider = "openai"
	cfg.LLM.OpenAI.BaseURL = "http://localhost:1234"
	cfg.LLM.OpenAI.EmbeddingModel = "text-embedding-3-small"
	p, err := NewEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p == nil {
		t.Fatal("provider = nil")
	}
}

func TestNewEmbeddingProvider_VoyageMissingKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.EmbeddingProvider = "voyage"
	_, err := NewEmbeddingProvider(cfg)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Errorf("err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestNewEmbeddingProvider_VoyageSuccess(t *testing.T) {
	keyFile := t.TempDir() + "/voyage.key"
	if err := os.WriteFile(keyFile, []byte("pa-test-key-12345678\n"), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	cfg := config.NewConfig()
	cfg.LLM.EmbeddingProvider = "voyage"
	cfg.LLM.Voyage.APIKeyFile = keyFile
	if err := cfg.LoadAPIKeys(); err != nil {
		t.Fatalf("LoadAPIKeys: %v", err)
	}

	p, err := NewEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p == nil {
		t.Fatal("provider nil")
	}
	if p.ModelName() != "voyage-3-lite" {
		t.Errorf("model = %q, want voyage-3-lite", p.ModelName())
	}
}

func TestNewEmbeddingProvider_GeminiMissingKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.EmbeddingProvider = "gemini"
	_, err := NewEmbeddingProvider(cfg)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Errorf("err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestNewEmbeddingProvider_GeminiSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := tmpDir + "/gemini.key"
	if err := os.WriteFile(keyFile, []byte("AIza-test-key-12345678\n"), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	cfg := config.NewConfig()
	cfg.LLM.EmbeddingProvider = "gemini"
	cfg.LLM.Gemini.APIKeyFile = keyFile
	if err := cfg.LoadAPIKeys(); err != nil {
		t.Fatalf("LoadAPIKeys: %v", err)
	}

	p, err := NewEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p == nil {
		t.Fatal("provider nil")
	}
	if p.ModelName() != "gemini-embedding-001" {
		t.Errorf("model = %q, want gemini-embedding-001", p.ModelName())
	}
}

func TestNewEmbeddingProvider_GeminiCustomModel(t *testing.T) {
	// Embedding model names are no longer checked at construction time, so a
	// model outside the old allow-list, such as "text-embedding-004", is
	// accepted and passed straight through to the provider. An embedding
	// that is too wide for the knowledge-base vector column is caught later,
	// at store and query time, by embedding.PadTo.
	tmpDir := t.TempDir()
	keyFile := tmpDir + "/gemini.key"
	if err := os.WriteFile(keyFile, []byte("AIza-test-key-12345678\n"), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	cfg := config.NewConfig()
	cfg.LLM.EmbeddingProvider = "gemini"
	cfg.LLM.Gemini.APIKeyFile = keyFile
	cfg.LLM.Gemini.EmbeddingModel = "text-embedding-004"
	if err := cfg.LoadAPIKeys(); err != nil {
		t.Fatalf("LoadAPIKeys: %v", err)
	}

	p, err := NewEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p == nil {
		t.Fatal("provider nil")
	}
	if p.ModelName() != "text-embedding-004" {
		t.Errorf("model = %q, want text-embedding-004", p.ModelName())
	}
}

func TestNewEmbeddingProvider_OllamaDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.EmbeddingProvider = "ollama"
	p, err := NewEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p == nil {
		t.Fatal("provider nil")
	}
}

func TestNewEmbeddingProvider_OllamaExplicit(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.EmbeddingProvider = "ollama"
	cfg.LLM.Ollama.BaseURL = "http://localhost:11434"
	cfg.LLM.Ollama.EmbeddingModel = "nomic-embed-text"
	p, err := NewEmbeddingProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p == nil {
		t.Fatal("provider nil")
	}
}

func TestNewReasoningProvider_NilConfig(t *testing.T) {
	p, err := NewReasoningProvider(nil)
	if err != nil || p != nil {
		t.Errorf("want nil/nil, got %v/%v", p, err)
	}
}

func TestNewReasoningProvider_Disabled(t *testing.T) {
	for _, s := range []string{"", "none", "disabled"} {
		cfg := &config.Config{}
		cfg.LLM.ReasoningProvider = s
		p, err := NewReasoningProvider(cfg)
		if err != nil {
			t.Errorf("reasoning=%q: err = %v", s, err)
		}
		if p != nil {
			t.Errorf("reasoning=%q: got %v, want nil", s, p)
		}
	}
}

func TestNewReasoningProvider_Unknown(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.ReasoningProvider = "fantasy"
	_, err := NewReasoningProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown reasoning provider") {
		t.Errorf("err = %v, want unknown reasoning provider", err)
	}
}

func TestNewReasoningProvider_OpenAIMissingKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.ReasoningProvider = "openai"
	_, err := NewReasoningProvider(cfg)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Errorf("err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestNewReasoningProvider_OpenAIWithBaseURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.ReasoningProvider = "openai"
	cfg.LLM.OpenAI.BaseURL = "http://localhost"
	cfg.LLM.OpenAI.ReasoningModel = "gpt-test"
	p, err := NewReasoningProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p.ModelName() != "gpt-test" {
		t.Errorf("model = %q, want gpt-test", p.ModelName())
	}
}

func TestNewReasoningProvider_AnthropicMissingKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.ReasoningProvider = "anthropic"
	_, err := NewReasoningProvider(cfg)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Errorf("err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestNewReasoningProvider_GeminiMissingKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.ReasoningProvider = "gemini"
	_, err := NewReasoningProvider(cfg)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Errorf("err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestNewReasoningProvider_OllamaDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.ReasoningProvider = "ollama"
	p, err := NewReasoningProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p == nil {
		t.Fatal("nil provider")
	}
	if p.ModelName() != "llama3.2" {
		t.Errorf("default model = %q, want llama3.2", p.ModelName())
	}
}

func TestNewReasoningProvider_OllamaExplicit(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.ReasoningProvider = "ollama"
	cfg.LLM.Ollama.BaseURL = "http://localhost:12345"
	cfg.LLM.Ollama.ReasoningModel = "llama-custom"
	p, err := NewReasoningProvider(cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if p.ModelName() != "llama-custom" {
		t.Errorf("model = %q, want llama-custom", p.ModelName())
	}
}

// Compile-time check that fakeEmbedding satisfies embedding.Provider.
var _ embedding.Provider = (*fakeEmbedding)(nil)
