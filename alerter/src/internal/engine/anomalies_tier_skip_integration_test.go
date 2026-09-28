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
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// countingEmbeddingProvider records how many embeddings Tier 2 asked for.
type countingEmbeddingProvider struct {
	calls atomic.Int32
}

func (p *countingEmbeddingProvider) GenerateEmbedding(context.Context, string) ([]float32, error) {
	p.calls.Add(1)
	return []float32{1, 0, 0}, nil
}

func (p *countingEmbeddingProvider) ModelName() string { return "counting-model" }

// countingReasoningProvider records how many classifications Tier 3 asked
// for and always answers "alert". onClassify, when set, runs inside the
// call so a test can change state while the LLM is "thinking".
type countingReasoningProvider struct {
	calls      atomic.Int32
	onClassify func()
}

func (p *countingReasoningProvider) Classify(context.Context, string) (string, error) {
	p.calls.Add(1)
	if p.onClassify != nil {
		p.onClassify()
	}
	return `{"decision": "alert", "confidence": 0.9, "reasoning": "test"}`, nil
}

func (p *countingReasoningProvider) ModelName() string { return "counting-reasoner" }

// tierSkipAlertsSchema adds the alert tables that the pre-tier checks and
// createAnomalyAlert read and write to the detection test schema.
const tierSkipAlertsSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL,
    rule_id BIGINT,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    probe_name TEXT,
    object_name TEXT,
    metric_name TEXT,
    metric_value REAL,
    threshold_value REAL,
    operator TEXT,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    correlation_id TEXT,
    status TEXT NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_at TIMESTAMPTZ,
    last_updated TIMESTAMPTZ,
    last_reevaluated_at TIMESTAMPTZ,
    reevaluation_count INTEGER NOT NULL DEFAULT 0,
    anomaly_score REAL,
    anomaly_details JSONB,
    ai_analysis TEXT,
    ai_analysis_metric_value REAL
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    acknowledged_by TEXT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    acknowledge_type TEXT NOT NULL DEFAULT 'acknowledge',
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);
`

const tierSkipAlertsTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
`

const tierSkipMetric = "pg_settings.max_connections"

// Seed statements for the prior state each case needs.
const (
	insertTierSkipAlertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, metric_name, severity, title,
             description, status, cleared_at, reevaluation_count)
        VALUES ('anomaly', $1, $2, 'warning', $2, 'seeded', $3, $4, $5)
        RETURNING id
    `

	insertTierSkipAckSQL = `
        INSERT INTO alert_acknowledgments
            (alert_id, acknowledged_by, acknowledge_type, false_positive)
        VALUES ($1, 'tester', 'false_positive', TRUE)
    `

	// breakActiveAlertLookupSQL makes GetActiveAnomalyAlert fail to
	// scan the seeded row, which is the only way the false-positive
	// check can be reached: an acknowledged alert is also an open
	// alert, so the duplicate check otherwise claims it first.
	breakActiveAlertLookupSQL = `
        ALTER TABLE alerts ALTER COLUMN rule_id TYPE TEXT;
        UPDATE alerts SET rule_id = 'not-a-number';
    `

	countTierSkipAlertsSQL = `SELECT COUNT(*) FROM alerts`
)

// seedTierSkipAlert inserts an anomaly alert for the test metric.
func seedTierSkipAlert(t *testing.T, pool *pgxpool.Pool, connID int, status string, clearedAt *time.Time, reevalCount int) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), insertTierSkipAlertSQL,
		connID, tierSkipMetric, status, clearedAt, reevalCount).Scan(&id); err != nil {
		t.Fatalf("failed to seed alert: %v", err)
	}
	return id
}

// TestProcessTier2And3SkipsPaidTiers covers issue #568: the checks that
// decide whether an alert may be raised, and that do not depend on the
// tier results, run before Tier 2 and Tier 3, so neither the embedding
// nor the LLM provider is called when the result would be discarded. The
// candidate is still marked processed, with decision "alert" and, when an
// open alert exists, linked to it.
func TestProcessTier2And3SkipsPaidTiers(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()
	defer func() {
		if _, err := pool.Exec(context.Background(), tierSkipAlertsTeardown); err != nil {
			t.Logf("alert table teardown failed: %v", err)
		}
	}()

	ctx := context.Background()
	cfg := engine.getConfig()
	cfg.Anomaly.Tier2.Enabled = true
	cfg.Anomaly.Tier3.Enabled = true

	recent := time.Now().Add(-time.Hour)

	tests := []struct {
		name string
		// seed prepares prior state and returns the alert the
		// candidate should be linked to, or 0 for none.
		seed      func(t *testing.T, connID int) int64
		wantTiers bool
		// wantAlertRows is the number of alerts rows afterwards.
		wantAlertRows int
	}{
		{
			name:          "no prior state runs both tiers and raises an alert",
			seed:          func(*testing.T, int) int64 { return 0 },
			wantTiers:     true,
			wantAlertRows: 1,
		},
		{
			name: "open active alert skips both tiers",
			seed: func(t *testing.T, connID int) int64 {
				return seedTierSkipAlert(t, pool, connID, "active", nil, 0)
			},
			wantAlertRows: 1,
		},
		{
			name: "open acknowledged alert skips both tiers",
			seed: func(t *testing.T, connID int) int64 {
				return seedTierSkipAlert(t, pool, connID, "acknowledged", nil, 0)
			},
			wantAlertRows: 1,
		},
		{
			name: "active blackout skips both tiers",
			seed: func(t *testing.T, connID int) int64 {
				if _, err := pool.Exec(ctx, insertAnomalyBlackoutSQL, connID,
					time.Now().Add(-time.Hour), time.Now().Add(time.Hour)); err != nil {
					t.Fatalf("failed to seed blackout: %v", err)
				}
				return 0
			},
			wantAlertRows: 0,
		},
		{
			name: "re-evaluation suppression skips both tiers",
			seed: func(t *testing.T, connID int) int64 {
				seedTierSkipAlert(t, pool, connID, "cleared", &recent, 1)
				return 0
			},
			wantAlertRows: 1,
		},
		{
			name: "cleared alert without re-evaluation does not suppress",
			seed: func(t *testing.T, connID int) int64 {
				seedTierSkipAlert(t, pool, connID, "cleared", &recent, 0)
				return 0
			},
			wantTiers:     true,
			wantAlertRows: 2,
		},
		{
			name: "false positive suppression skips both tiers",
			seed: func(t *testing.T, connID int) int64 {
				id := seedTierSkipAlert(t, pool, connID, "acknowledged", nil, 0)
				if _, err := pool.Exec(ctx, insertTierSkipAckSQL, id); err != nil {
					t.Fatalf("failed to seed acknowledgment: %v", err)
				}
				if _, err := pool.Exec(ctx, breakActiveAlertLookupSQL); err != nil {
					t.Fatalf("failed to break the active alert lookup: %v", err)
				}
				return 0
			},
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
				"tier-skip").Scan(&connID); err != nil {
				t.Fatalf("failed to insert connection: %v", err)
			}
			wantAlertID := tt.seed(t, connID)

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

			embedder := &countingEmbeddingProvider{}
			reasoner := &countingReasoningProvider{}
			engine.embeddingProvider = embedder
			engine.reasoningProvider = reasoner

			captureStderr(t, func() { engine.processTier2And3(ctx) })

			wantCalls := int32(0)
			if tt.wantTiers {
				wantCalls = 1
			}
			if got := embedder.calls.Load(); got != wantCalls {
				t.Errorf("Tier 2 embedding calls = %d, want %d", got, wantCalls)
			}
			if got := reasoner.calls.Load(); got != wantCalls {
				t.Errorf("Tier 3 LLM calls = %d, want %d", got, wantCalls)
			}

			stored, err := ds.GetAnomalyCandidateByID(ctx, candidate.ID)
			if err != nil {
				t.Fatalf("GetAnomalyCandidateByID: %v", err)
			}
			if stored.ProcessedAt == nil {
				t.Error("candidate was not marked processed")
			}
			if stored.FinalDecision == nil || *stored.FinalDecision != "alert" {
				t.Errorf("final_decision = %v, want alert", stored.FinalDecision)
			}
			if !tt.wantTiers {
				if stored.Tier2Pass != nil || stored.Tier3Pass != nil ||
					stored.Tier2Score != nil || stored.Tier3Result != nil {
					t.Errorf("skipped candidate has tier results: tier2_pass=%v tier2_score=%v tier3_pass=%v tier3_result=%v",
						stored.Tier2Pass, stored.Tier2Score, stored.Tier3Pass, stored.Tier3Result)
				}
			} else if stored.Tier3Pass == nil || !*stored.Tier3Pass {
				t.Errorf("tier3_pass = %v, want true", stored.Tier3Pass)
			}

			switch {
			case wantAlertID != 0:
				if stored.AlertID == nil || *stored.AlertID != wantAlertID {
					t.Errorf("alert_id = %v, want the open alert %d", stored.AlertID, wantAlertID)
				}
			case tt.wantTiers:
				if stored.AlertID == nil {
					t.Error("alert_id is NULL, want the newly raised alert")
				}
			default:
				if stored.AlertID != nil {
					t.Errorf("alert_id = %d, want NULL", *stored.AlertID)
				}
			}

			var alertRows int
			if err := pool.QueryRow(ctx, countTierSkipAlertsSQL).Scan(&alertRows); err != nil {
				t.Fatalf("failed to count alerts: %v", err)
			}
			if alertRows != tt.wantAlertRows {
				t.Errorf("alerts rows = %d, want %d", alertRows, tt.wantAlertRows)
			}
		})
	}

	// A failing lookup must not hide a real anomaly: with the alert
	// tables missing every alert check errors, and the candidate still
	// goes through both tiers.
	t.Run("lookup errors do not skip the tiers", func(t *testing.T) {
		if _, err := pool.Exec(ctx, tierSkipAlertsTeardown); err != nil {
			t.Fatalf("failed to drop alert tables: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM blackouts; DELETE FROM anomaly_candidates; DELETE FROM connections`); err != nil {
			t.Fatalf("failed to reset state: %v", err)
		}
		var connID int
		if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
			"tier-skip-errors").Scan(&connID); err != nil {
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

		embedder := &countingEmbeddingProvider{}
		reasoner := &countingReasoningProvider{}
		engine.embeddingProvider = embedder
		engine.reasoningProvider = reasoner

		captureStderr(t, func() { engine.processTier2And3(ctx) })

		if got := embedder.calls.Load(); got != 1 {
			t.Errorf("Tier 2 embedding calls = %d, want 1", got)
		}
		if got := reasoner.calls.Load(); got != 1 {
			t.Errorf("Tier 3 LLM calls = %d, want 1", got)
		}
		stored, err := ds.GetAnomalyCandidateByID(ctx, candidate.ID)
		if err != nil {
			t.Fatalf("GetAnomalyCandidateByID: %v", err)
		}
		if stored.ProcessedAt == nil {
			t.Error("candidate was not marked processed")
		}
	})

	// Ending the context mid-batch stops the loop before the next
	// candidate, and the write-back failure for the current one is
	// logged rather than fatal.
	t.Run("cancellation during Tier 3 stops the batch", func(t *testing.T) {
		if _, err := pool.Exec(ctx, tierSkipAlertsSchema); err != nil {
			t.Fatalf("failed to create alert tables: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM blackouts; DELETE FROM anomaly_candidates; DELETE FROM connections`); err != nil {
			t.Fatalf("failed to reset state: %v", err)
		}
		var connID int
		if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
			"tier-skip-cancel").Scan(&connID); err != nil {
			t.Fatalf("failed to insert connection: %v", err)
		}
		for i := 0; i < 2; i++ {
			c := &database.AnomalyCandidate{
				ConnectionID: connID,
				MetricName:   tierSkipMetric,
				MetricValue:  999,
				ZScore:       10,
				DetectedAt:   time.Now(),
				Context:      "{}",
				Tier1Pass:    true,
			}
			if err := ds.CreateAnomalyCandidate(ctx, c); err != nil {
				t.Fatalf("CreateAnomalyCandidate: %v", err)
			}
		}

		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		reasoner := &countingReasoningProvider{onClassify: cancel}
		engine.embeddingProvider = nil
		engine.reasoningProvider = reasoner

		output := captureStderr(t, func() { engine.processTier2And3(runCtx) })

		if got := reasoner.calls.Load(); got != 1 {
			t.Errorf("Tier 3 LLM calls = %d, want 1", got)
		}
		if !strings.Contains(output, "ERROR: Failed to update anomaly candidate") {
			t.Errorf("expected the write-back failure in the log, got:\n%s", output)
		}
	})

	// A blackout that starts whilst Tier 3 is running must still stop
	// the alert: createAnomalyAlert repeats the checks after the tiers.
	t.Run("blackout starting during Tier 3 still prevents the alert", func(t *testing.T) {
		if _, err := pool.Exec(ctx, tierSkipAlertsSchema); err != nil {
			t.Fatalf("failed to create alert tables: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM blackouts; DELETE FROM anomaly_candidates; DELETE FROM connections`); err != nil {
			t.Fatalf("failed to reset state: %v", err)
		}
		var connID int
		if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
			"tier-skip-late-blackout").Scan(&connID); err != nil {
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

		reasoner := &countingReasoningProvider{onClassify: func() {
			if _, err := pool.Exec(ctx, insertAnomalyBlackoutSQL, connID,
				time.Now().Add(-time.Minute), time.Now().Add(time.Hour)); err != nil {
				t.Errorf("failed to seed blackout: %v", err)
			}
		}}
		engine.embeddingProvider = &countingEmbeddingProvider{}
		engine.reasoningProvider = reasoner

		captureStderr(t, func() { engine.processTier2And3(ctx) })

		if got := reasoner.calls.Load(); got != 1 {
			t.Errorf("Tier 3 LLM calls = %d, want 1", got)
		}
		var alertRows int
		if err := pool.QueryRow(ctx, countTierSkipAlertsSQL).Scan(&alertRows); err != nil {
			t.Fatalf("failed to count alerts: %v", err)
		}
		if alertRows != 0 {
			t.Errorf("alerts rows = %d, want 0", alertRows)
		}
	})

	t.Run("candidate fetch failure is logged", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS anomaly_candidates CASCADE`); err != nil {
			t.Fatalf("failed to drop anomaly_candidates: %v", err)
		}
		output := captureStderr(t, func() { engine.processTier2And3(ctx) })
		if !strings.Contains(output, "ERROR: Failed to get anomaly candidates") {
			t.Errorf("expected the fetch failure in the log, got:\n%s", output)
		}
	})
}
