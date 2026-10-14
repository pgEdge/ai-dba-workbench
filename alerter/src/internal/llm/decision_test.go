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
	"path/filepath"
	"strings"
	"testing"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// fakeDecisionClient is a test double for pgllm.Client that overrides only
// Decide and Ping, the methods libDecision uses.
type fakeDecisionClient struct {
	pgllm.Client
	gotReq  pgllm.DecisionRequest
	resp    *pgllm.DecisionResponse
	err     error
	pingErr error
	pinged  bool
}

func (f *fakeDecisionClient) Decide(_ context.Context, req pgllm.DecisionRequest) (*pgllm.DecisionResponse, error) {
	f.gotReq = req
	return f.resp, f.err
}

func (f *fakeDecisionClient) Ping(context.Context) error {
	f.pinged = true
	return f.pingErr
}

// yesNo builds a response answering the decision question with p.
func yesNo(p float64) *pgllm.DecisionResponse {
	return &pgllm.DecisionResponse{Answers: []pgllm.DecisionAnswer{
		{Name: decisionQuestionName, Kind: pgllm.DecisionYesNo, Probability: p},
	}}
}

func TestLibDecisionClassifyRequestShape(t *testing.T) {
	fc := &fakeDecisionClient{resp: yesNo(0.9)}
	d := &libDecision{client: fc, provider: "openai", model: "decide-model"}
	v, err := d.ClassifyDecision(context.Background(), "the anomaly context", 0.5)
	if err != nil {
		t.Fatalf("ClassifyDecision: %v", err)
	}
	if !v.Alert || v.RealIssueProbability != 0.9 {
		t.Errorf("verdict = %+v, want alert at 0.9", v)
	}
	req := fc.gotReq
	if req.Model != "decide-model" {
		t.Errorf("Model = %q, want decide-model (always set)", req.Model)
	}
	if req.Input != "the anomaly context" {
		t.Errorf("Input = %q", req.Input)
	}
	if len(req.Questions) != 1 {
		t.Fatalf("Questions = %+v, want one", req.Questions)
	}
	q := req.Questions[0]
	if q.Name != decisionQuestionName || q.Kind != pgllm.DecisionYesNo ||
		q.Instructions != decisionQuestion || len(q.Options) != 0 {
		t.Errorf("question = %+v", q)
	}
	if err := req.Validate(); err != nil {
		t.Errorf("request fails the library's validation: %v", err)
	}
}

func TestLibDecisionClassifyThreshold(t *testing.T) {
	tests := []struct {
		name      string
		p         float64
		threshold float64
		wantAlert bool
	}{
		{"clear real issue", 0.95, 0.5, true},
		{"clear false positive", 0.05, 0.5, false},
		{"exactly even suppresses at 0.5", 0.5, 0.5, false},
		{"just above even alerts", 0.51, 0.5, true},
		{"higher threshold keeps a likely false positive", 0.3, 0.8, true},
		{"higher threshold still suppresses a near-certain one", 0.1, 0.8, false},
		{"lower threshold suppresses a likely real issue", 0.6, 0.3, false},
		{"zero probability", 0, 0.5, false},
		{"certain", 1, 0.5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &libDecision{client: &fakeDecisionClient{resp: yesNo(tt.p)}, model: "m"}
			v, err := d.ClassifyDecision(context.Background(), "x", tt.threshold)
			if err != nil {
				t.Fatalf("ClassifyDecision: %v", err)
			}
			if v.Alert != tt.wantAlert {
				t.Errorf("Alert = %v, want %v (p=%v threshold=%v)", v.Alert, tt.wantAlert, tt.p, tt.threshold)
			}
			if v.RealIssueProbability != tt.p {
				t.Errorf("RealIssueProbability = %v, want %v", v.RealIssueProbability, tt.p)
			}
		})
	}
}

func TestLibDecisionClassifyErrors(t *testing.T) {
	transport := errors.New("connection refused")
	tests := []struct {
		name    string
		client  *fakeDecisionClient
		wantErr error
		wantMsg string
	}{
		{"transport error", &fakeDecisionClient{err: transport}, transport, ""},
		{"nil response", &fakeDecisionClient{}, ErrInvalidResponse, "no answer"},
		{"missing answer", &fakeDecisionClient{resp: &pgllm.DecisionResponse{
			Answers: []pgllm.DecisionAnswer{{Name: "other", Probability: 0.9}},
		}}, ErrInvalidResponse, "no answer"},
		{"refused", &fakeDecisionClient{resp: &pgllm.DecisionResponse{
			Answers: []pgllm.DecisionAnswer{{Name: decisionQuestionName, Refused: true}},
		}}, ErrDecisionRefused, ""},
		{"negative", &fakeDecisionClient{resp: yesNo(-0.01)}, ErrInvalidResponse, "outside"},
		{"above one", &fakeDecisionClient{resp: yesNo(1.01)}, ErrInvalidResponse, "outside"},
		{"NaN", &fakeDecisionClient{resp: yesNo(math.NaN())}, ErrInvalidResponse, "outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &libDecision{client: tt.client, model: "m"}
			v, err := d.ClassifyDecision(context.Background(), "x", 0.5)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err = %q, want it to mention %q", err, tt.wantMsg)
			}
			if v != (DecisionVerdict{}) {
				t.Errorf("verdict = %+v alongside an error, want zero", v)
			}
		})
	}
}

func TestLibDecisionPingAndNames(t *testing.T) {
	pingErr := errors.New("401 access denied")
	fc := &fakeDecisionClient{pingErr: pingErr}
	d := &libDecision{client: fc, provider: "typesafe", model: "jev-test"}
	if err := d.Ping(context.Background()); !errors.Is(err, pingErr) || !fc.pinged {
		t.Errorf("Ping = %v (pinged %v), want the client's error", err, fc.pinged)
	}
	if d.ModelName() != "jev-test" || d.ProviderName() != "typesafe" {
		t.Errorf("names = %s/%s", d.ProviderName(), d.ModelName())
	}
}

func TestDecisionSuppresses(t *testing.T) {
	if !DecisionSuppresses(0.5, 0.5) {
		t.Error("P(real)=0.5 at threshold 0.5 should suppress")
	}
	if DecisionSuppresses(0.75, 0.5) {
		t.Error("P(real)=0.75 at threshold 0.5 should alert")
	}
}

// writeKey writes an API key file and returns its path.
func writeKey(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("test-key-0123456789\n"), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

func TestNewDecisionProvider(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*config.Config)
		wantNil      bool
		wantErr      string
		wantProvider string
		wantModel    string
	}{
		{name: "nil config is off", setup: nil, wantNil: true},
		{name: "unset is off", setup: func(*config.Config) {}, wantNil: true},
		{name: "none is off", setup: func(c *config.Config) { c.LLM.DecisionProvider = "none" }, wantNil: true},
		{name: "disabled is off", setup: func(c *config.Config) { c.LLM.DecisionProvider = "disabled" }, wantNil: true},
		{name: "unknown", setup: func(c *config.Config) { c.LLM.DecisionProvider = "ollama" },
			wantErr: "unknown decision provider"},
		{name: "typesafe without key", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "typesafe"
			// A base URL alone is not enough: TypeSafe always needs a key.
			c.LLM.TypeSafe.BaseURL = "http://127.0.0.1:9/v1"
		}, wantErr: ErrAPIKeyMissing.Error()},
		{name: "typesafe default model", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "typesafe"
			c.LLM.TypeSafe.APIKeyFile = writeKey(t, "ts.key")
		}, wantProvider: "typesafe", wantModel: "jev-latest"},
		{name: "typesafe empty model falls back", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "typesafe"
			c.LLM.TypeSafe.APIKeyFile = writeKey(t, "ts.key")
			c.LLM.TypeSafe.DecisionModel = ""
		}, wantProvider: "typesafe", wantModel: "jev-latest"},
		{name: "typesafe custom model and base URL", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "typesafe"
			c.LLM.TypeSafe.APIKeyFile = writeKey(t, "ts.key")
			c.LLM.TypeSafe.DecisionModel = "jev-test"
			c.LLM.TypeSafe.BaseURL = "http://127.0.0.1:9/v1"
		}, wantProvider: "typesafe", wantModel: "jev-test"},
		{name: "typesafe bad base URL", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "typesafe"
			c.LLM.TypeSafe.APIKeyFile = writeKey(t, "ts.key")
			c.LLM.TypeSafe.BaseURL = "ftp://example.com"
		}, wantErr: "create typesafe decision client"},
		{name: "openai without key or base URL", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "openai"
		}, wantErr: ErrAPIKeyMissing.Error()},
		{name: "openai keyless base URL", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "openai"
			c.LLM.OpenAI.BaseURL = "http://127.0.0.1:9/v1"
			c.LLM.OpenAI.DecisionModel = "local-decider"
		}, wantProvider: "openai", wantModel: "local-decider"},
		{name: "openai with key", setup: func(c *config.Config) {
			c.LLM.DecisionProvider = "openai"
			c.LLM.OpenAI.APIKeyFile = writeKey(t, "openai.key")
		}, wantProvider: "openai", wantModel: "gpt-6-luna"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *config.Config
			if tt.setup != nil {
				cfg = config.NewConfig()
				tt.setup(cfg)
				if err := cfg.LoadAPIKeys(); err != nil {
					t.Fatalf("LoadAPIKeys: %v", err)
				}
			}
			p, err := NewDecisionProvider(cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if tt.wantNil {
				if p != nil {
					t.Fatalf("provider = %v, want nil", p)
				}
				return
			}
			if p == nil {
				t.Fatal("provider nil")
			}
			if p.ProviderName() != tt.wantProvider || p.ModelName() != tt.wantModel {
				t.Errorf("provider = %s/%s, want %s/%s",
					p.ProviderName(), p.ModelName(), tt.wantProvider, tt.wantModel)
			}
		})
	}
}
