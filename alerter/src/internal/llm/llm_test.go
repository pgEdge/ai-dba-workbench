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

func TestResizeEmbedding_Same(t *testing.T) {
	in := []float32{1, 2, 3}
	got := resizeEmbedding(in, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Should return the same slice (same pointer) because dims match.
	if &got[0] != &in[0] {
		t.Error("resizeEmbedding with equal dim should return the input slice")
	}
}

func TestResizeEmbedding_Truncate(t *testing.T) {
	in := []float32{1, 2, 3, 4, 5}
	got := resizeEmbedding(in, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("got = %v, want [1 2 3]", got)
	}
}

func TestResizeEmbedding_Pad(t *testing.T) {
	in := []float32{1, 2}
	got := resizeEmbedding(in, 5)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	want := []float32{1, 2, 0, 0, 0}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("got[%d] = %v, want %v", i, got[i], v)
		}
	}
}

func TestNormalizeEmbedding_UnitLength(t *testing.T) {
	in := []float32{3, 4}
	got := normalizeEmbedding(in)
	var sumSq float64
	for _, v := range got {
		sumSq += float64(v) * float64(v)
	}
	if math.Abs(math.Sqrt(sumSq)-1.0) > 1e-6 {
		t.Errorf("magnitude = %v, want 1.0", math.Sqrt(sumSq))
	}
}

func TestNormalizeEmbedding_ZeroVector(t *testing.T) {
	in := []float32{0, 0, 0}
	got := normalizeEmbedding(in)
	// Per implementation, zero vector is returned unchanged.
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for _, v := range got {
		if v != 0 {
			t.Errorf("got = %v, want all zeros", got)
		}
	}
}

func TestEmbeddingAdapter_GenerateEmbedding_RightDimension(t *testing.T) {
	// Construct a 1536-dim vector that's already correct size.
	vec := make([]float64, EmbeddingDimension)
	for i := range vec {
		vec[i] = 0.5
	}
	fake := &fakeEmbedding{vec: vec, model: "m", provider: "p", dim: EmbeddingDimension}
	a := &embeddingAdapter{provider: fake}

	got, err := a.GenerateEmbedding(context.Background(), "hello")
	if err != nil {
		t.Fatalf("GenerateEmbedding: %v", err)
	}
	if len(got) != EmbeddingDimension {
		t.Fatalf("len = %d, want %d", len(got), EmbeddingDimension)
	}
	if fake.lastInput != "hello" {
		t.Errorf("lastInput = %q, want hello", fake.lastInput)
	}
	// Should be normalized.
	var sumSq float64
	for _, v := range got {
		sumSq += float64(v) * float64(v)
	}
	if math.Abs(math.Sqrt(sumSq)-1.0) > 1e-5 {
		t.Errorf("not normalized: magnitude = %v", math.Sqrt(sumSq))
	}
}

func TestEmbeddingAdapter_GenerateEmbedding_ResizesUp(t *testing.T) {
	fake := &fakeEmbedding{vec: []float64{1, 2, 3}, model: "m", dim: 3}
	a := &embeddingAdapter{provider: fake}

	got, err := a.GenerateEmbedding(context.Background(), "x")
	if err != nil {
		t.Fatalf("GenerateEmbedding: %v", err)
	}
	if len(got) != EmbeddingDimension {
		t.Errorf("len = %d, want %d", len(got), EmbeddingDimension)
	}
}

func TestEmbeddingAdapter_GenerateEmbedding_ResizesDown(t *testing.T) {
	big := make([]float64, EmbeddingDimension+10)
	for i := range big {
		big[i] = float64(i)
	}
	fake := &fakeEmbedding{vec: big, dim: len(big)}
	a := &embeddingAdapter{provider: fake}

	got, err := a.GenerateEmbedding(context.Background(), "x")
	if err != nil {
		t.Fatalf("GenerateEmbedding: %v", err)
	}
	if len(got) != EmbeddingDimension {
		t.Errorf("len = %d, want %d", len(got), EmbeddingDimension)
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
