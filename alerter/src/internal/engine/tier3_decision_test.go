/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package engine

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/alerter/internal/llm"
)

// These tests cover the decision model path of Tier 3 (GitHub issue #626).

// fakeDecider is a decision provider whose verdict or error a test sets.
// It records what it was asked.
type fakeDecider struct {
	verdict llm.DecisionVerdict
	err     error
	pingErr error

	calls        atomic.Int32
	gotInput     string
	gotThreshold float64
	hadDeadline  bool
}

func (f *fakeDecider) ClassifyDecision(ctx context.Context, input string, threshold float64) (llm.DecisionVerdict, error) {
	f.calls.Add(1)
	f.gotInput = input
	f.gotThreshold = threshold
	_, f.hadDeadline = ctx.Deadline()
	if f.err != nil {
		return llm.DecisionVerdict{}, f.err
	}
	return f.verdict, nil
}

func (f *fakeDecider) Ping(context.Context) error { return f.pingErr }
func (f *fakeDecider) ModelName() string          { return "dec-model" }
func (f *fakeDecider) ProviderName() string       { return "dec-provider" }

// recordingReasoner is a reasoning provider that records the prompts it is
// given and answers with response, or fails with err.
type recordingReasoner struct {
	fakeReasoner
	prompts []string
}

func (r *recordingReasoner) Classify(ctx context.Context, prompt string) (string, error) {
	r.prompts = append(r.prompts, prompt)
	return r.fakeReasoner.Classify(ctx, prompt)
}

func TestDecisionSuppressThreshold(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{"configured", 0.7, 0.7},
		{"low but valid", 0.01, 0.01},
		{"zero falls back", 0, config.DefaultDecisionSuppressThreshold},
		{"negative falls back", -0.5, config.DefaultDecisionSuppressThreshold},
		{"one falls back", 1, config.DefaultDecisionSuppressThreshold},
		{"above one falls back", 3, config.DefaultDecisionSuppressThreshold},
		{"NaN falls back", math.NaN(), config.DefaultDecisionSuppressThreshold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Anomaly.Tier3.DecisionSuppressThreshold = tt.in
			if got := decisionSuppressThreshold(cfg); got != tt.want {
				t.Errorf("decisionSuppressThreshold(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecisionSummary(t *testing.T) {
	if got, want := decisionSummary("typesafe", "jev-latest", true, 0.873, 0.5),
		"Decision model typesafe/jev-latest: alert (P(real issue)=0.87, suppress threshold 0.50)"; got != want {
		t.Errorf("alert summary = %q, want %q", got, want)
	}
	if got, want := decisionSummary("openai", "gpt-6-luna", false, 0.1, 0.75),
		"Decision model openai/gpt-6-luna: suppress (P(real issue)=0.10, suppress threshold 0.75)"; got != want {
		t.Errorf("suppress summary = %q, want %q", got, want)
	}
}

// TestClassificationPromptSplit pins the split the decision model relies
// on: the reasoning prompt is the context followed by the JSON response
// instructions, and the context alone carries no response instructions.
func TestClassificationPromptSplit(t *testing.T) {
	e := &Engine{}
	candidate := &database.AnomalyCandidate{
		ID: 1, ConnectionID: 2, MetricName: "cpu_usage_percent",
		MetricValue: 95, ZScore: 4, DetectedAt: time.Now(), Context: "{}",
	}
	classificationContext := e.buildClassificationContext(candidate, nil, nil, nil, nil)
	prompt := e.buildClassificationPrompt(candidate, nil, nil, nil, nil)

	if prompt != classificationContext+classificationResponseInstructions {
		t.Error("reasoning prompt is not the context followed by the response instructions")
	}
	if strings.Contains(classificationContext, "JSON") || strings.Contains(classificationContext, `"decision"`) {
		t.Errorf("classification context carries response instructions:\n%s", classificationContext)
	}
	if !strings.HasSuffix(classificationContext, "Past feedback suggests context, not a blanket rule.\n") {
		t.Errorf("classification context does not end with the feedback guidance:\n%s", classificationContext)
	}
	if !strings.HasSuffix(prompt, "- \"reasoning\": a brief explanation\n") {
		t.Errorf("reasoning prompt does not end with the response instructions:\n%s", prompt)
	}
}

func strValue(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestRunTier3(t *testing.T) {
	const ctxText = "the classification context\n"
	boom := errors.New("boom")
	decisionFailed := "decision model dec-provider/dec-model failed: boom"

	tests := []struct {
		name      string
		decider   *fakeDecider
		reasoner  *recordingReasoner
		threshold float64

		wantPass        bool
		wantResult      string
		wantResultHas   string
		wantSource      string
		wantModel       string
		wantError       string
		wantReasonCalls int
	}{
		{
			name:       "decision alert",
			decider:    &fakeDecider{verdict: llm.DecisionVerdict{Alert: true, RealIssueProbability: 0.9}},
			reasoner:   &recordingReasoner{},
			threshold:  0.7,
			wantPass:   true,
			wantResult: "Decision model dec-provider/dec-model: alert (P(real issue)=0.90, suppress threshold 0.70)",
			wantSource: tier3SourceDecision,
			wantModel:  "dec-provider/dec-model",
		},
		{
			name:       "decision suppress without a reasoning provider",
			decider:    &fakeDecider{verdict: llm.DecisionVerdict{Alert: false, RealIssueProbability: 0.2}},
			threshold:  0.5,
			wantPass:   false,
			wantResult: "Decision model dec-provider/dec-model: suppress (P(real issue)=0.20, suppress threshold 0.50)",
			wantSource: tier3SourceDecision,
			wantModel:  "dec-provider/dec-model",
		},
		{
			name:     "decision failure falls back to the reasoning model",
			decider:  &fakeDecider{err: boom},
			reasoner: &recordingReasoner{fakeReasoner: fakeReasoner{response: `{"decision": "suppress", "confidence": 0.8, "reasoning": "noise"}`}},
			wantPass: false, wantResultHas: `"suppress"`,
			wantSource:      tier3SourceReasoning,
			wantModel:       "reason-provider/reason-model",
			wantError:       decisionFailed + "; fell back to the reasoning model",
			wantReasonCalls: 1,
		},
		{
			name:     "decision and reasoning both fail",
			decider:  &fakeDecider{err: boom},
			reasoner: &recordingReasoner{fakeReasoner: fakeReasoner{err: errors.New("overloaded")}},
			wantPass: true, wantResult: "LLM classification failed, defaulting to alert",
			wantSource:      tier3SourceReasoning,
			wantModel:       "reason-provider/reason-model",
			wantError:       decisionFailed + "; the reasoning model then failed too: overloaded",
			wantReasonCalls: 1,
		},
		{
			name:       "decision failure without a reasoning provider fails safe",
			decider:    &fakeDecider{err: llm.ErrDecisionRefused},
			wantPass:   true,
			wantResult: "Decision model classification failed, defaulting to alert",
			wantSource: tier3SourceDecision,
			wantModel:  "dec-provider/dec-model",
			wantError:  "decision model dec-provider/dec-model failed: " + llm.ErrDecisionRefused.Error(),
		},
		{
			name:     "reasoning only is unchanged",
			reasoner: &recordingReasoner{fakeReasoner: fakeReasoner{response: `{"decision": "alert", "confidence": 0.9, "reasoning": "real"}`}},
			wantPass: true, wantResultHas: `"alert"`,
			wantSource:      tier3SourceReasoning,
			wantModel:       "reason-provider/reason-model",
			wantReasonCalls: 1,
		},
		{
			name:     "reasoning only with an unknown verdict alerts",
			reasoner: &recordingReasoner{fakeReasoner: fakeReasoner{response: "perhaps"}},
			wantPass: true, wantResult: "perhaps",
			wantSource:      tier3SourceReasoning,
			wantModel:       "reason-provider/reason-model",
			wantReasonCalls: 1,
		},
		{
			name:     "reasoning only failure keeps its error text",
			reasoner: &recordingReasoner{fakeReasoner: fakeReasoner{err: errors.New("overloaded")}},
			wantPass: true, wantResult: "LLM classification failed, defaulting to alert",
			wantSource:      tier3SourceReasoning,
			wantModel:       "reason-provider/reason-model",
			wantError:       "overloaded",
			wantReasonCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Anomaly.Tier3.DecisionSuppressThreshold = tt.threshold
			if tt.threshold == 0 {
				cfg.Anomaly.Tier3.DecisionSuppressThreshold = config.DefaultDecisionSuppressThreshold
			}
			e := &Engine{config: cfg}
			if tt.decider != nil {
				e.decisionProvider = tt.decider
			}
			if tt.reasoner != nil {
				e.reasoningProvider = tt.reasoner
			}
			candidate := &database.AnomalyCandidate{ID: 42}

			output := captureStderr(t, func() { e.runTier3(context.Background(), candidate, ctxText) })

			if candidate.Tier3Pass == nil || *candidate.Tier3Pass != tt.wantPass {
				t.Errorf("Tier3Pass = %v, want %v", candidate.Tier3Pass, tt.wantPass)
			}
			result := strValue(candidate.Tier3Result)
			if tt.wantResult != "" && result != tt.wantResult {
				t.Errorf("Tier3Result = %q, want %q", result, tt.wantResult)
			}
			if tt.wantResultHas != "" && !strings.Contains(result, tt.wantResultHas) {
				t.Errorf("Tier3Result = %q, want it to contain %q", result, tt.wantResultHas)
			}
			if got := strValue(candidate.Tier3Source); got != tt.wantSource {
				t.Errorf("Tier3Source = %q, want %q", got, tt.wantSource)
			}
			if got := strValue(candidate.Tier3Model); got != tt.wantModel {
				t.Errorf("Tier3Model = %q, want %q", got, tt.wantModel)
			}
			if tt.wantError == "" {
				if candidate.Tier3Error != nil {
					t.Errorf("Tier3Error = %q, want nil", *candidate.Tier3Error)
				}
			} else if got := strValue(candidate.Tier3Error); got != tt.wantError {
				t.Errorf("Tier3Error = %q, want %q", got, tt.wantError)
			}

			if tt.decider != nil {
				if tt.decider.calls.Load() != 1 {
					t.Errorf("decision calls = %d, want 1", tt.decider.calls.Load())
				}
				if tt.decider.gotInput != ctxText {
					t.Errorf("decision input = %q, want the context without response instructions", tt.decider.gotInput)
				}
				if tt.decider.gotThreshold != cfg.Anomaly.Tier3.DecisionSuppressThreshold {
					t.Errorf("threshold = %v, want %v", tt.decider.gotThreshold, cfg.Anomaly.Tier3.DecisionSuppressThreshold)
				}
				if !tt.decider.hadDeadline {
					t.Error("decision call had no Tier 3 timeout")
				}
			}
			if tt.reasoner != nil {
				if len(tt.reasoner.prompts) != tt.wantReasonCalls {
					t.Fatalf("reasoning calls = %d, want %d", len(tt.reasoner.prompts), tt.wantReasonCalls)
				}
				for _, p := range tt.reasoner.prompts {
					if p != ctxText+classificationResponseInstructions {
						t.Errorf("reasoning prompt = %q, want the context plus response instructions", p)
					}
				}
			}
			if tt.decider != nil && tt.decider.err != nil {
				if !strings.Contains(output, "Tier 3 decision model classification failed for candidate 42") {
					t.Errorf("decision failure not logged:\n%s", output)
				}
				fellBack := strings.Contains(output, "Falling back to the reasoning model for candidate 42")
				if fellBack != (tt.reasoner != nil) {
					t.Errorf("fallback logged = %v, want %v:\n%s", fellBack, tt.reasoner != nil, output)
				}
			}
		})
	}
}

// TestRunTier3DecisionTimeout checks that a decision model that does not
// answer within the Tier 3 timeout is a failure like any other.
func TestRunTier3DecisionTimeout(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Anomaly.Tier3.TimeoutSeconds = 1
	e := &Engine{config: cfg, decisionProvider: blockingDecider{&fakeDecider{}}}
	candidate := &database.AnomalyCandidate{ID: 7}

	captureStderr(t, func() { e.runTier3(context.Background(), candidate, "x") })

	if candidate.Tier3Pass == nil || !*candidate.Tier3Pass {
		t.Errorf("Tier3Pass = %v, want the fail-safe alert", candidate.Tier3Pass)
	}
	if got := strValue(candidate.Tier3Error); !strings.Contains(got, context.DeadlineExceeded.Error()) {
		t.Errorf("Tier3Error = %q, want a deadline error", got)
	}
}

// blockingDecider answers only when its context ends.
type blockingDecider struct{ *fakeDecider }

func (blockingDecider) ClassifyDecision(ctx context.Context, _ string, _ float64) (llm.DecisionVerdict, error) {
	<-ctx.Done()
	return llm.DecisionVerdict{}, ctx.Err()
}

func TestTier3Timeout(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Anomaly.Tier3.TimeoutSeconds = 12
	if got := tier3Timeout(cfg); got != 12*time.Second {
		t.Errorf("tier3Timeout = %v, want 12s", got)
	}
	cfg.Anomaly.Tier3.TimeoutSeconds = 0
	if got := tier3Timeout(cfg); got != DefaultTier3Timeout {
		t.Errorf("tier3Timeout = %v, want the default %v", got, DefaultTier3Timeout)
	}
}

func TestTier3Available(t *testing.T) {
	tests := []struct {
		name                string
		tier3               bool
		reasoning, decision bool
		wantTier3, wantAny  bool
	}{
		{name: "tier 3 disabled", reasoning: true, decision: true},
		{name: "no providers", tier3: true},
		{name: "reasoning only", tier3: true, reasoning: true, wantTier3: true, wantAny: true},
		{name: "decision only", tier3: true, decision: true, wantTier3: true, wantAny: true},
		{name: "both", tier3: true, reasoning: true, decision: true, wantTier3: true, wantAny: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Anomaly.Tier2.Enabled = false
			cfg.Anomaly.Tier3.Enabled = tt.tier3
			e := &Engine{config: cfg}
			if tt.reasoning {
				e.reasoningProvider = &fakeReasoner{}
			}
			if tt.decision {
				e.decisionProvider = &fakeDecider{}
			}
			if got := e.tier3Available(cfg); got != tt.wantTier3 {
				t.Errorf("tier3Available = %v, want %v", got, tt.wantTier3)
			}
			if got := e.anomalyProcessingAvailable(cfg); got != tt.wantAny {
				t.Errorf("anomalyProcessingAvailable = %v, want %v", got, tt.wantAny)
			}
		})
	}
}

// TestReloadConfigKeepsDecisionOnlyAnomalyDetection checks that a reload
// does not auto-disable anomaly detection when the decision model is the
// only provider.
func TestReloadConfigKeepsDecisionOnlyAnomalyDetection(t *testing.T) {
	engine := NewEngine(nil, nil, false)
	engine.decisionProvider = &fakeDecider{}

	cfg := config.NewConfig()
	cfg.Anomaly.Tier2.Enabled = false
	cfg.Anomaly.Tier3.Enabled = true

	output := captureStderr(t, func() { engine.ReloadConfig(cfg) })

	if !engine.getConfig().Anomaly.Enabled {
		t.Errorf("anomaly detection disabled with a decision provider:\n%s", output)
	}
}

func TestInitLLMProvidersDecision(t *testing.T) {
	// decisionOnly configures Tier 3 with no embedding or reasoning
	// provider, so only the decision provider can be built. The base URL
	// points at a discard port; building a client makes no request.
	decisionOnly := func(provider string) *config.Config {
		cfg := config.NewConfig()
		cfg.Anomaly.Enabled = true
		cfg.Anomaly.Tier2.Enabled = false
		cfg.Anomaly.Tier3.Enabled = true
		cfg.LLM.EmbeddingProvider = ""
		cfg.LLM.ReasoningProvider = ""
		cfg.LLM.DecisionProvider = provider
		cfg.LLM.OpenAI.BaseURL = "http://127.0.0.1:9/v1"
		cfg.LLM.OpenAI.DecisionModel = "local-decider"
		return cfg
	}

	t.Run("decision only keeps anomaly detection on", func(t *testing.T) {
		var e *Engine
		output := captureStderr(t, func() { e = NewEngine(decisionOnly("openai"), nil, false) })
		if e.decisionProvider == nil {
			t.Fatalf("no decision provider built:\n%s", output)
		}
		if got := e.decisionProvider.ProviderName() + "/" + e.decisionProvider.ModelName(); got != "openai/local-decider" {
			t.Errorf("decision provider = %s", got)
		}
		if !e.getConfig().Anomaly.Enabled {
			t.Errorf("anomaly detection auto-disabled:\n%s", output)
		}
		for _, want := range []string{"No reasoning provider configured", "Initialized decision provider: openai/local-decider"} {
			if !strings.Contains(output, want) {
				t.Errorf("log lacks %q:\n%s", want, output)
			}
		}
		if strings.Contains(output, "Tier 3 will be disabled") {
			t.Errorf("Tier 3 reported disabled with a decision provider:\n%s", output)
		}
	})

	t.Run("a decision provider that cannot be built is skipped", func(t *testing.T) {
		var e *Engine
		// TypeSafe always needs a key file, so this one fails to build.
		output := captureStderr(t, func() { e = NewEngine(decisionOnly("typesafe"), nil, false) })
		if e.decisionProvider != nil {
			t.Error("decision provider built without a key")
		}
		for _, want := range []string{
			"WARNING: Failed to initialize decision provider",
			"Tier 3 will not use a decision model",
			"No reasoning or decision provider available, Tier 3 will be disabled",
			"Anomaly detection auto-disabled",
		} {
			if !strings.Contains(output, want) {
				t.Errorf("log lacks %q:\n%s", want, output)
			}
		}
		if e.getConfig().Anomaly.Enabled {
			t.Error("anomaly detection left on with no provider")
		}
	})

	t.Run("an unbuildable reasoning provider and a missing embedding provider are reported", func(t *testing.T) {
		cfg := decisionOnly("")
		cfg.Anomaly.Tier2.Enabled = true
		cfg.LLM.ReasoningProvider = "bogus"
		var e *Engine
		output := captureStderr(t, func() { e = NewEngine(cfg, nil, false) })
		for _, want := range []string{
			"No embedding provider configured, Tier 2 will be disabled",
			"WARNING: Failed to initialize reasoning provider",
		} {
			if !strings.Contains(output, want) {
				t.Errorf("log lacks %q:\n%s", want, output)
			}
		}
		if e.embeddingProvider != nil || e.reasoningProvider != nil || e.decisionProvider != nil {
			t.Error("a provider was built")
		}
	})

	t.Run("an unbuildable embedding provider is reported", func(t *testing.T) {
		cfg := decisionOnly("")
		cfg.Anomaly.Tier2.Enabled = true
		cfg.LLM.EmbeddingProvider = "bogus"
		output := captureStderr(t, func() { NewEngine(cfg, nil, false) })
		if !strings.Contains(output, "WARNING: Failed to initialize embedding provider") {
			t.Errorf("embedding failure not logged:\n%s", output)
		}
	})

	t.Run("tier 3 disabled builds no decision provider", func(t *testing.T) {
		cfg := decisionOnly("openai")
		cfg.Anomaly.Tier3.Enabled = false
		var e *Engine
		captureStderr(t, func() { e = NewEngine(cfg, nil, false) })
		if e.decisionProvider != nil {
			t.Error("decision provider built with Tier 3 disabled")
		}
	})
}

func TestHealthTrackingDecision(t *testing.T) {
	h := newTrackerHarness(1)
	inner := &fakeDecider{err: errors.New("401 access denied")}
	dec := &healthTrackingDecision{inner: inner, provider: "typesafe", tracker: h.tracker}
	key := providerHealthKey(providerTierDecision, "typesafe")

	if dec.ModelName() != "dec-model" || dec.ProviderName() != "dec-provider" {
		t.Errorf("names = %s/%s", dec.ProviderName(), dec.ModelName())
	}

	inner.pingErr = errors.New("unreachable")
	if err := dec.Ping(context.Background()); err == nil {
		t.Error("Ping error swallowed")
	}
	if h.store.openFor(key) != nil {
		t.Error("an unrecorded Ping raised an alert")
	}

	if _, err := dec.ClassifyDecision(context.Background(), "x", 0.5); err == nil {
		t.Error("ClassifyDecision error swallowed")
	}
	alert := h.store.openFor(key)
	if alert == nil {
		t.Fatalf("no alert for %s", key)
	}
	if !strings.Contains(alert.Description, "tier 3 decision calls") {
		t.Errorf("alert description = %q, want it to name the tier", alert.Description)
	}

	inner.err = nil
	inner.verdict = llm.DecisionVerdict{Alert: true, RealIssueProbability: 0.8}
	v, err := dec.ClassifyDecision(context.Background(), "x", 0.5)
	if err != nil || v != inner.verdict {
		t.Errorf("ClassifyDecision = %+v, %v", v, err)
	}
	if h.store.openFor(key) != nil {
		t.Error("decision alert not cleared by success")
	}
	// The decision tier is independent of the reasoning tiers.
	if got := providerTierDecision.sharedTiers(); len(got) != 1 || got[0] != providerTierDecision {
		t.Errorf("sharedTiers = %v", got)
	}

	inner.pingErr = errors.New("unreachable")
	if err := dec.healthCheck(context.Background()); err == nil {
		t.Error("healthCheck error swallowed")
	}
	if h.store.openFor(key) == nil {
		t.Error("failed health check raised no alert")
	}
}

// decisionHealthEngine adds a wrapped decision provider to the engine
// that providerHealthEngine builds.
func decisionHealthEngine(pingErr error) (*Engine, *trackerHarness) {
	e, h := providerHealthEngine(nil, nil)
	e.decisionProvider = &healthTrackingDecision{inner: &fakeDecider{pingErr: pingErr},
		provider: "typesafe", tracker: h.tracker}
	return e, h
}

func TestCheckProviderHealthDecision(t *testing.T) {
	key := providerHealthKey(providerTierDecision, "typesafe")

	t.Run("failure raises at once", func(t *testing.T) {
		e, h := decisionHealthEngine(errors.New("401 access denied"))
		e.checkProviderHealth(context.Background())
		if h.store.openFor(key) == nil {
			t.Error("no decision alert")
		}
		if !h.loggedContaining("Provider health alert raised") {
			t.Error("raised alert not logged")
		}
	})
	t.Run("healthy check clears a previous alert", func(t *testing.T) {
		e, h := decisionHealthEngine(nil)
		old := h.store.put(key)
		e.checkProviderHealth(context.Background())
		if old.Status != "cleared" {
			t.Error("decision alert from a previous run not cleared")
		}
	})
	t.Run("tier 3 disabled checks nothing and drops the key", func(t *testing.T) {
		e, h := decisionHealthEngine(errors.New("down"))
		e.config.Anomaly.Tier3.Enabled = false
		old := h.store.put(key)
		e.checkProviderHealth(context.Background())
		if old.Status != "cleared" {
			t.Error("decision alert kept with Tier 3 disabled")
		}
		if h.store.openFor(key) != nil {
			t.Error("decision provider checked with Tier 3 disabled")
		}
	})
	t.Run("active keys include the decision tier", func(t *testing.T) {
		e, _ := decisionHealthEngine(nil)
		if !e.activeProviderHealthKeys()[key] {
			t.Errorf("active = %v, want %s", e.activeProviderHealthKeys(), key)
		}
		e.config.Anomaly.Tier3.Enabled = false
		if e.activeProviderHealthKeys()[key] {
			t.Error("decision key active with Tier 3 disabled")
		}
	})
}

func TestInitProviderHealthWrapsDecision(t *testing.T) {
	cfg := config.NewConfig()
	cfg.LLM.DecisionProvider = " TypeSafe "
	e := &Engine{config: cfg, datastore: database.NewTestDatastore(nil),
		decisionProvider: &fakeDecider{}}
	e.initProviderHealth()
	dec, ok := e.decisionProvider.(*healthTrackingDecision)
	if !ok || dec.provider != "typesafe" {
		t.Fatalf("decision provider = %#v", e.decisionProvider)
	}
}

func TestProviderTierDecisionText(t *testing.T) {
	if providerTierDecision.label() != "Tier 3 decision" {
		t.Errorf("label = %q", providerTierDecision.label())
	}
	if !strings.Contains(providerTierDecision.consequence(), "reasoning model") {
		t.Errorf("consequence = %q", providerTierDecision.consequence())
	}
}

func TestConfiguredSecretsIncludesTypeSafe(t *testing.T) {
	cfg := config.NewConfig()
	cfg.LLM.TypeSafe.BaseURL = "https://user:hunter2@typesafe.example.com/v1?token=abc"
	secrets := configuredSecrets(cfg)
	found := false
	for _, s := range secrets {
		if strings.Contains(s, "hunter2") || strings.Contains(s, "abc") {
			found = true
		}
	}
	if !found {
		t.Errorf("TypeSafe base URL credentials not among the secrets: %q", secrets)
	}
}
