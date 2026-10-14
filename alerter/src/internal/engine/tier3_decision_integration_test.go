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
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/alerter/internal/llm"
)

// TestProcessTier2And3RecordsDecisionSource runs candidates through
// processTier2And3 with only a decision provider, and checks that the
// Tier 3 source and model reach anomaly_candidates (GitHub issue #626).
func TestProcessTier2And3RecordsDecisionSource(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()
	defer func() {
		if _, err := pool.Exec(context.Background(), tierSkipAlertsTeardown); err != nil {
			t.Logf("alert table teardown failed: %v", err)
		}
	}()

	ctx := context.Background()
	cfg := engine.getConfig()
	cfg.Anomaly.Tier2.Enabled = false
	cfg.Anomaly.Tier3.Enabled = true
	engine.reasoningProvider = nil

	tests := []struct {
		name          string
		decider       *fakeDecider
		wantPass      bool
		wantDecision  string
		wantError     bool
		wantAlertRows int
	}{
		{
			name:         "suppressed by the decision model",
			decider:      &fakeDecider{verdict: llm.DecisionVerdict{Alert: false, RealIssueProbability: 0.1}},
			wantPass:     false,
			wantDecision: "suppress",
		},
		{
			name:          "decision failure without a reasoning model raises the alert",
			decider:       &fakeDecider{err: errors.New("boom")},
			wantPass:      true,
			wantDecision:  "alert",
			wantError:     true,
			wantAlertRows: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, tierSkipAlertsSchema); err != nil {
				t.Fatalf("failed to create alert tables: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM blackouts; DELETE FROM anomaly_candidates; DELETE FROM connections`); err != nil {
				t.Fatalf("failed to reset state: %v", err)
			}

			var connID int
			if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
				"decision-source").Scan(&connID); err != nil {
				t.Fatalf("failed to insert connection: %v", err)
			}
			candidate := &database.AnomalyCandidate{
				ConnectionID: connID,
				MetricName:   tierSkipMetric,
				MetricValue:  999,
				ZScore:       10,
				DetectedAt:   time.Now(),
				Context:      "{}",
				Tier1Pass:    true,
			}
			if err := ds.CreateAnomalyCandidate(ctx, candidate); err != nil {
				t.Fatalf("CreateAnomalyCandidate: %v", err)
			}

			engine.decisionProvider = tt.decider
			captureStderr(t, func() { engine.processTier2And3(ctx) })

			if got := tt.decider.calls.Load(); got != 1 {
				t.Errorf("decision calls = %d, want 1", got)
			}
			stored, err := ds.GetAnomalyCandidateByID(ctx, candidate.ID)
			if err != nil {
				t.Fatalf("GetAnomalyCandidateByID: %v", err)
			}
			if stored.ProcessedAt == nil {
				t.Error("candidate was not marked processed")
			}
			if stored.Tier3Pass == nil || *stored.Tier3Pass != tt.wantPass {
				t.Errorf("tier3_pass = %v, want %v", stored.Tier3Pass, tt.wantPass)
			}
			if got := strValue(stored.FinalDecision); got != tt.wantDecision {
				t.Errorf("final_decision = %q, want %q", got, tt.wantDecision)
			}
			if got := strValue(stored.Tier3Source); got != tier3SourceDecision {
				t.Errorf("tier3_source = %q, want %q", got, tier3SourceDecision)
			}
			if got := strValue(stored.Tier3Model); got != "dec-provider/dec-model" {
				t.Errorf("tier3_model = %q, want dec-provider/dec-model", got)
			}
			if (stored.Tier3Error != nil) != tt.wantError {
				t.Errorf("tier3_error = %q, want set=%v", strValue(stored.Tier3Error), tt.wantError)
			}

			var alertRows int
			if err := pool.QueryRow(ctx, countTierSkipAlertsSQL).Scan(&alertRows); err != nil {
				t.Fatalf("failed to count alerts: %v", err)
			}
			if alertRows != tt.wantAlertRows {
				t.Errorf("alerts = %d, want %d", alertRows, tt.wantAlertRows)
			}
		})
	}
}
