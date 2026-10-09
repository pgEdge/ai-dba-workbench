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
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestDetectAnomaliesSkipsCandidatesThatCannotAlert covers issue #577:
// detectAnomalies runs anomalyAlertSkipReason before it writes a Tier 1
// candidate, so a condition that persists under an open alert, a blackout
// or a suppression does not add a row to anomaly_candidates on every
// cycle. A lookup error still lets the candidate through.
func TestDetectAnomaliesSkipsCandidatesThatCannotAlert(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()
	defer func() {
		if _, err := pool.Exec(context.Background(), tierSkipAlertsTeardown); err != nil {
			t.Logf("alert table teardown failed: %v", err)
		}
	}()

	ctx := context.Background()

	if _, err := pool.Exec(ctx, insertAnomalyAlertRuleSQL,
		"candidate_skip_rule", tierSkipMetric); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	now := time.Now().UTC()
	recent := now.Add(-time.Hour)

	// setup resets state, creates the alert tables (unless dropAlerts
	// is set), and seeds a connection whose latest value is far outside
	// a warm baseline, so Tier 1 would record a candidate.
	setup := func(t *testing.T, name string, dropAlerts bool) int {
		t.Helper()
		schema := tierSkipAlertsSchema
		if dropAlerts {
			schema = tierSkipAlertsTeardown
		}
		if _, err := pool.Exec(ctx, schema); err != nil {
			t.Fatalf("failed to prepare alert tables: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM blackouts; DELETE FROM anomaly_candidates;
            DELETE FROM metric_baselines; DELETE FROM metrics.pg_sys_load_avg_info;
            DELETE FROM connections`); err != nil {
			t.Fatalf("failed to reset state: %v", err)
		}

		var connID int
		if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL, name).Scan(&connID); err != nil {
			t.Fatalf("failed to insert connection: %v", err)
		}
		if _, err := pool.Exec(ctx, insertAnomalyLoadAvgSQL, connID, "999"); err != nil {
			t.Fatalf("failed to insert load average sample: %v", err)
		}
		if err := ds.UpsertMetricBaseline(ctx, &database.MetricBaseline{
			ConnectionID:     connID,
			MetricName:       tierSkipMetric,
			PeriodType:       "all",
			Mean:             10,
			StdDev:           1,
			Min:              9,
			Max:              11,
			SampleCount:      200,
			LastCalculated:   now,
			EarliestSampleAt: now.Add(-48 * time.Hour),
		}); err != nil {
			t.Fatalf("UpsertMetricBaseline failed: %v", err)
		}
		return connID
	}

	countCandidates := func(t *testing.T, connID int) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, selectAnomalyCountByConnSQL, connID).Scan(&n); err != nil {
			t.Fatalf("failed to count anomaly_candidates: %v", err)
		}
		return n
	}

	tests := []struct {
		name       string
		dropAlerts bool
		seed       func(t *testing.T, connID int)
		want       int
	}{
		{
			name: "no prior state records a candidate",
			seed: func(*testing.T, int) {},
			want: 1,
		},
		{
			name:       "lookup errors still record a candidate",
			dropAlerts: true,
			seed:       func(*testing.T, int) {},
			want:       1,
		},
		{
			name: "open active alert records none",
			seed: func(t *testing.T, connID int) {
				seedTierSkipAlert(t, pool, connID, "active", nil, 0)
			},
		},
		{
			name: "open acknowledged alert records none",
			seed: func(t *testing.T, connID int) {
				seedTierSkipAlert(t, pool, connID, "acknowledged", nil, 0)
			},
		},
		{
			name: "re-evaluation suppression records none",
			seed: func(t *testing.T, connID int) {
				seedTierSkipAlert(t, pool, connID, "cleared", &recent, 1)
			},
		},
		{
			name: "cleared alert without re-evaluation records a candidate",
			seed: func(t *testing.T, connID int) {
				seedTierSkipAlert(t, pool, connID, "cleared", &recent, 0)
			},
			want: 1,
		},
		{
			name: "false positive suppression records none",
			seed: func(t *testing.T, connID int) {
				id := seedTierSkipAlert(t, pool, connID, "acknowledged", nil, 0)
				if _, err := pool.Exec(ctx, insertTierSkipAckSQL, id); err != nil {
					t.Fatalf("failed to seed acknowledgment: %v", err)
				}
				if _, err := pool.Exec(ctx, breakActiveAlertLookupSQL); err != nil {
					t.Fatalf("failed to break the active alert lookup: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connID := setup(t, "candidate-skip", tt.dropAlerts)
			tt.seed(t, connID)

			captureStderr(t, func() { engine.detectAnomalies(ctx) })

			if got := countCandidates(t, connID); got != tt.want {
				t.Errorf("anomaly_candidates rows = %d, want %d", got, tt.want)
			}
		})
	}

	// detectAnomalies drops a connection under a server blackout before
	// scoring, so the helper's own blackout check is exercised by scoring
	// the value directly, as it would be for a blackout that starts
	// between that lookup and the candidate insert.
	t.Run("blackout records none", func(t *testing.T) {
		connID := setup(t, "candidate-skip-blackout", false)
		if _, err := pool.Exec(ctx, insertAnomalyBlackoutSQL, connID,
			now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
			t.Fatalf("failed to seed blackout: %v", err)
		}

		value := &database.MetricValue{ConnectionID: connID, Value: 999}
		captureStderr(t, func() {
			engine.detectAnomalyForValue(ctx, tierSkipMetric, value,
				engine.getConfig(), engine.getConfig().Anomaly.Tier1.DefaultSensitivity, time.Now())
		})

		if got := countCandidates(t, connID); got != 0 {
			t.Errorf("anomaly_candidates rows = %d, want 0", got)
		}
	})
}
