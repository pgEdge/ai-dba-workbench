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
	withTimeout := func(seconds int) *config.Config {
		cfg := config.NewConfig()
		cfg.Anomaly.Tier3.TimeoutSeconds = seconds
		return cfg
	}

	tests := []struct {
		name string
		cfg  *config.Config
		want time.Duration
	}{
		{"default timeout", withTimeout(30), 150 * time.Minute},
		{"long timeout", withTimeout(120), 10 * time.Hour},
		{"short timeout keeps the one-hour floor", withTimeout(5), time.Hour},
		{"floor boundary", withTimeout(12), time.Hour},
		{"zero timeout uses default", withTimeout(0), 150 * time.Minute},
		{"negative timeout uses default", withTimeout(-1), 150 * time.Minute},
		{"nil config uses default", nil, 150 * time.Minute},
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
// never reaches Tier 3, whilst a fresh one and one merely queued behind a
// backlog for half an hour are both processed as usual.
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
	backlogged := newCandidate(time.Now().Add(-30 * time.Minute))

	output := captureStderr(t, func() { engine.processTier2And3(ctx) })

	if !strings.Contains(output, "Expired 1 anomaly candidates") {
		t.Errorf("log output missing the expiry count:\n%s", output)
	}
	if got := reasoner.calls.Load(); got != 2 {
		t.Errorf("Tier 3 calls = %d, want 2 (the fresh and backlogged candidates)", got)
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

	for name, c := range map[string]*database.AnomalyCandidate{"fresh": fresh, "backlogged": backlogged} {
		got, err = ds.GetAnomalyCandidateByID(ctx, c.ID)
		if err != nil {
			t.Fatalf("GetAnomalyCandidateByID(%s): %v", name, err)
		}
		if got.ProcessedAt == nil || got.FinalDecision == nil || *got.FinalDecision != "suppress" {
			t.Errorf("%s candidate processed_at = %v, final_decision = %v; want processed and suppressed",
				name, got.ProcessedAt, got.FinalDecision)
		}
	}
}

// TestProcessTier2And3StopsWhenReloadDisablesTiers checks that a reload
// which disables Tier 2 and Tier 3 part-way through a pass stops it: the
// rest of the batch would otherwise reach determineFinalDecision with no
// tier result and raise a raw Tier 1 alert. The unprocessed candidates
// stay queued for a later re-enable or for expiry. With nothing stale, the
// pass must not log an expiry either.
func TestProcessTier2And3StopsWhenReloadDisablesTiers(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	reasoner := &countingReasoningProvider{}
	reasoner.onClassify = func() {
		if reasoner.calls.Load() != 1 {
			return
		}
		cfg := config.NewConfig()
		cfg.Anomaly.Tier2.Enabled = false
		cfg.Anomaly.Tier3.Enabled = false
		engine.ReloadConfig(cfg)
	}
	engine.reasoningProvider = suppressingCounter{reasoner}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"reload-mid-pass").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	const total = 5
	ids := make([]int64, 0, total)
	for i := 0; i < total; i++ {
		c := &database.AnomalyCandidate{
			ConnectionID: connID,
			MetricName:   "pg_settings.max_connections",
			MetricValue:  float64(900 + i),
			ZScore:       10,
			DetectedAt:   time.Now().Add(time.Duration(i-total) * time.Second),
			Context:      "{}",
			Tier1Pass:    true,
		}
		if err := ds.CreateAnomalyCandidate(ctx, c); err != nil {
			t.Fatalf("CreateAnomalyCandidate: %v", err)
		}
		ids = append(ids, c.ID)
	}

	output := captureStderr(t, func() { engine.processTier2And3(ctx) })

	if strings.Contains(output, "Expired") {
		t.Errorf("expiry logged with nothing stale:\n%s", output)
	}
	if got := reasoner.calls.Load(); got != 1 {
		t.Errorf("Tier 3 calls = %d, want 1 (the pass should stop after the reload)", got)
	}

	processed := 0
	for _, id := range ids {
		got, err := ds.GetAnomalyCandidateByID(ctx, id)
		if err != nil {
			t.Fatalf("GetAnomalyCandidateByID(%d): %v", id, err)
		}
		if got.ProcessedAt == nil {
			continue
		}
		processed++
		if got.FinalDecision == nil {
			t.Errorf("candidate %d final_decision = NULL, want suppress", id)
		} else if *got.FinalDecision != "suppress" {
			t.Errorf("candidate %d final_decision = %q, want suppress", id, *got.FinalDecision)
		}
	}
	if processed != 1 {
		t.Errorf("processed candidates = %d, want 1; the rest should stay queued", processed)
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
