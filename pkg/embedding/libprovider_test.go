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
		{"openai needs key or url", Config{Provider: "openai"}, true, ""},
		{"voyage ok", Config{Provider: "voyage", VoyageAPIKey: "k"}, false, "voyage"},
		{"voyage needs key", Config{Provider: "voyage"}, true, ""},
		{"gemini ok", Config{Provider: "gemini", GeminiAPIKey: "k"}, false, "gemini"},
		{"gemini needs key", Config{Provider: "gemini"}, true, ""},
		{"ollama ok", Config{Provider: "ollama"}, false, "ollama"},
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
