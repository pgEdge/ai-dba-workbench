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
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// These tests cover issue #581: anomaly detection is off whenever no tier
// after Tier 1 can process a candidate, a reload applies the startup rule
// that says so, and candidates left unprocessed are expired rather than
// alerted on.

func TestStaleCandidateAge(t *testing.T) {
	withInterval := func(seconds int) *config.Config {
		cfg := config.NewConfig()
		cfg.Anomaly.Tier1.EvaluationIntervalSeconds = seconds
		return cfg
	}

	tests := []struct {
		name string
		cfg  *config.Config
		want time.Duration
	}{
		{"configured interval", withInterval(120), 10 * time.Minute},
		{"zero interval uses default", withInterval(0), 5 * time.Minute},
		{"negative interval uses default", withInterval(-1), 5 * time.Minute},
		{"nil config uses default", nil, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staleCandidateAge(tt.cfg); got != tt.want {
				t.Errorf("staleCandidateAge = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReloadConfigAppliesAnomalyProviderRule(t *testing.T) {
	tests := []struct {
		name         string
		tier2, tier3 bool
		withEmbedder bool
		withReasoner bool
		wantEnabled  bool
	}{
		{name: "both tiers disabled", withEmbedder: true, withReasoner: true},
		{name: "tier 2 without embedding provider", tier2: true, withReasoner: true},
		{name: "tier 3 without reasoning provider", tier3: true, withEmbedder: true},
		{name: "tier 2 with embedding provider", tier2: true, withEmbedder: true, wantEnabled: true},
		{name: "tier 3 with reasoning provider", tier3: true, withReasoner: true, wantEnabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(nil, nil, false)
			if tt.withEmbedder {
				engine.embeddingProvider = &countingEmbeddingProvider{}
			}
			if tt.withReasoner {
				engine.reasoningProvider = suppressingReasoningProvider{}
			}

			cfg := config.NewConfig()
			cfg.Anomaly.Tier2.Enabled = tt.tier2
			cfg.Anomaly.Tier3.Enabled = tt.tier3

			output := captureStderr(t, func() { engine.ReloadConfig(cfg) })

			if got := engine.getConfig().Anomaly.Enabled; got != tt.wantEnabled {
				t.Errorf("Anomaly.Enabled after reload = %v, want %v", got, tt.wantEnabled)
			}
			logged := strings.Contains(output, "Anomaly detection auto-disabled")
			if logged == tt.wantEnabled {
				t.Errorf("auto-disable logged = %v, want %v:\n%s", logged, !tt.wantEnabled, output)
			}
		})
	}

	t.Run("already disabled stays quiet", func(t *testing.T) {
		engine := NewEngine(nil, nil, false)
		cfg := config.NewConfig()
		cfg.Anomaly.Enabled = false

		output := captureStderr(t, func() { engine.ReloadConfig(cfg) })

		if engine.getConfig().Anomaly.Enabled {
			t.Error("Anomaly.Enabled should stay false")
		}
		if strings.Contains(output, "auto-disabled") {
			t.Errorf("unexpected auto-disable log:\n%s", output)
		}
	})
}

// TestProcessTier2And3ExpiresStaleCandidates checks that a candidate older
// than the stale cut-off is stamped processed with no final decision and
// never reaches Tier 3, whilst a fresh one is processed as usual.
func TestProcessTier2And3ExpiresStaleCandidates(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	reasoner := &countingReasoningProvider{}
	engine.reasoningProvider = suppressingCounter{reasoner}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"stale-candidates").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	newCandidate := func(detectedAt time.Time) *database.AnomalyCandidate {
		t.Helper()
		c := &database.AnomalyCandidate{
			ConnectionID: connID,
			MetricName:   "pg_settings.max_connections",
			MetricValue:  999,
			ZScore:       10,
			DetectedAt:   detectedAt,
			Context:      "{}",
			Tier1Pass:    true,
		}
		if err := ds.CreateAnomalyCandidate(ctx, c); err != nil {
			t.Fatalf("CreateAnomalyCandidate: %v", err)
		}
		return c
	}
	cutoff := staleCandidateAge(engine.getConfig())
	stale := newCandidate(time.Now().Add(-cutoff - time.Minute))
	fresh := newCandidate(time.Now())

	output := captureStderr(t, func() { engine.processTier2And3(ctx) })

	if !strings.Contains(output, "Expired 1 anomaly candidates") {
		t.Errorf("log output missing the expiry count:\n%s", output)
	}
	if got := reasoner.calls.Load(); got != 1 {
		t.Errorf("Tier 3 calls = %d, want 1 (the fresh candidate only)", got)
	}

	got, err := ds.GetAnomalyCandidateByID(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetAnomalyCandidateByID(stale): %v", err)
	}
	if got.ProcessedAt == nil || got.FinalDecision != nil || got.Tier3Result != nil {
		t.Errorf("stale candidate processed_at = %v, final_decision = %v, tier3_result = %v; "+
			"want processed with no decision and no Tier 3 result",
			got.ProcessedAt, got.FinalDecision, got.Tier3Result)
	}

	got, err = ds.GetAnomalyCandidateByID(ctx, fresh.ID)
	if err != nil {
		t.Fatalf("GetAnomalyCandidateByID(fresh): %v", err)
	}
	if got.ProcessedAt == nil || got.FinalDecision == nil || *got.FinalDecision != "suppress" {
		t.Errorf("fresh candidate processed_at = %v, final_decision = %v; want processed and suppressed",
			got.ProcessedAt, got.FinalDecision)
	}
}

// TestExpireStaleAnomalyCandidatesLogsFailure checks that a failing expiry
// is logged at default verbosity and does not stop the caller.
func TestExpireStaleAnomalyCandidatesLogsFailure(t *testing.T) {
	engine, _, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE anomaly_candidates CASCADE`); err != nil {
		t.Fatalf("failed to drop anomaly_candidates: %v", err)
	}
	output := captureStderr(t, func() { engine.expireStaleAnomalyCandidates(ctx) })
	if !strings.Contains(output, "ERROR: Failed to expire stale anomaly candidates") {
		t.Errorf("log output missing the expiry error:\n%s", output)
	}
}

// suppressingCounter counts Tier 3 calls through the wrapped provider but
// answers "suppress", so the test needs no alert tables.
type suppressingCounter struct {
	counter *countingReasoningProvider
}

func (s suppressingCounter) Classify(ctx context.Context, prompt string) (string, error) {
	if _, err := s.counter.Classify(ctx, prompt); err != nil {
		return "", err
	}
	return suppressingReasoningProvider{}.Classify(ctx, prompt)
}

func (s suppressingCounter) ModelName() string { return "suppressing-counter" }
