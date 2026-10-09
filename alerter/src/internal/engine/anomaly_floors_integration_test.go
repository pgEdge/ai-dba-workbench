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
	"math"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

const selectFloorCandidateSQL = `
        SELECT id, metric_value, z_score, context
        FROM anomaly_candidates
        ORDER BY id DESC
        LIMIT 1
    `

// setFlatBaseline replaces the test baseline with one whose mean and
// standard deviation are both 0, as a metric that has never moved has.
func (env *recoveryEnv) setFlatBaseline(t *testing.T) {
	t.Helper()
	now := time.Now().UTC()
	if err := env.ds.UpsertMetricBaseline(context.Background(), &database.MetricBaseline{
		ConnectionID:     env.connID,
		MetricName:       tierSkipMetric,
		PeriodType:       "all",
		SampleCount:      200,
		LastCalculated:   now,
		EarliestSampleAt: now.Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertMetricBaseline failed: %v", err)
	}
}

// TestFlatBaselineMinStdDev covers the reported case of issue #617: a
// metric with a baseline mean and standard deviation of 0 raised a
// critical alert for a trivial value because the z-score hit its cap.
// With a per-metric MinStdDev, a small value raises nothing, whilst a
// large jump still raises a candidate whose alert is capped at warning.
func TestFlatBaselineMinStdDev(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()
	setMetricFloor(env.engine.getConfig(), tierSkipMetric, 0, 10)
	env.setFlatBaseline(t)

	// 12 / 10 = 1.2, inside the default sensitivity of 3.
	env.passes(t, val(12))
	if n := env.candidates(t); n != 0 {
		t.Fatalf("candidates after a small value = %d, want 0", n)
	}

	// 200 / 10 = 20: a genuine jump is still reported.
	env.passes(t, val(200))
	if n := env.candidates(t); n != 1 {
		t.Fatalf("candidates after a large jump = %d, want 1", n)
	}

	candidate := &database.AnomalyCandidate{ConnectionID: env.connID, MetricName: tierSkipMetric}
	if err := env.pool.QueryRow(ctx, selectFloorCandidateSQL).Scan(
		&candidate.ID, &candidate.MetricValue, &candidate.ZScore, &candidate.Context); err != nil {
		t.Fatalf("failed to read candidate: %v", err)
	}
	if math.Abs(candidate.ZScore-20) > 1e-9 {
		t.Errorf("z-score = %v, want 20", candidate.ZScore)
	}
	if !candidateStdDevFloored(candidate.Context) {
		t.Errorf("context %s does not record a floored divisor", candidate.Context)
	}

	// z = 20 is past the critical multiple of the sensitivity, but the
	// divisor came from the floor, so the alert is a warning.
	env.engine.createAnomalyAlert(ctx, candidate, 3)
	if candidate.AlertID == nil {
		t.Fatal("expected an alert to be created")
	}
	if got := env.alert(t, *candidate.AlertID); got.severity != "warning" {
		t.Errorf("alert severity = %s, want warning", got.severity)
	}
}

// TestUnflooredCandidateCanBeCritical checks that the severity cap applies
// only to floored divisors: a large z-score against a baseline's own
// spread still raises a critical alert.
func TestUnflooredCandidateCanBeCritical(t *testing.T) {
	env := newRecoveryEnv(t)
	ctx := context.Background()

	// recoveryMean + 13 against a standard deviation of 1 gives z = 13.
	env.passes(t, val(criticalValue))
	candidate := &database.AnomalyCandidate{ConnectionID: env.connID, MetricName: tierSkipMetric}
	if err := env.pool.QueryRow(ctx, selectFloorCandidateSQL).Scan(
		&candidate.ID, &candidate.MetricValue, &candidate.ZScore, &candidate.Context); err != nil {
		t.Fatalf("failed to read candidate: %v", err)
	}
	if candidateStdDevFloored(candidate.Context) {
		t.Errorf("context %s records a floored divisor, want unfloored", candidate.Context)
	}
	env.engine.createAnomalyAlert(ctx, candidate, 3)
	if candidate.AlertID == nil {
		t.Fatal("expected an alert to be created")
	}
	if got := env.alert(t, *candidate.AlertID); got.severity != "critical" {
		t.Errorf("alert severity = %s, want critical", got.severity)
	}
}

// TestFlooredRealSpreadCanBeCritical checks that the severity cap is
// confined to flat baselines: a baseline with a spread of its own keeps
// the critical severity even when the relative variance floor or the
// metric's MinStdDev supplies the divisor.
func TestFlooredRealSpreadCanBeCritical(t *testing.T) {
	cases := []struct {
		name      string
		mean      float64
		stddev    float64
		minStdDev float64
		value     float64
		wantZ     float64
	}{
		// 5% of 70 is 3.5, above the spread of 2: (120 - 70) / 3.5.
		{"relative floor", 70, 2, 0, 120, 50 / 3.5},
		// MinStdDev 5 is above the spread of 3: (80 - 20) / 5.
		{"metric MinStdDev", 20, 3, 5, 80, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newRecoveryEnv(t)
			ctx := context.Background()
			setMetricFloor(env.engine.getConfig(), tierSkipMetric, 0, tc.minStdDev)
			now := time.Now().UTC()
			if err := env.ds.UpsertMetricBaseline(ctx, &database.MetricBaseline{
				ConnectionID:     env.connID,
				MetricName:       tierSkipMetric,
				PeriodType:       "all",
				Mean:             tc.mean,
				StdDev:           tc.stddev,
				SampleCount:      200,
				LastCalculated:   now,
				EarliestSampleAt: now.Add(-48 * time.Hour),
			}); err != nil {
				t.Fatalf("UpsertMetricBaseline failed: %v", err)
			}

			env.passes(t, val(tc.value))
			candidate := &database.AnomalyCandidate{ConnectionID: env.connID, MetricName: tierSkipMetric}
			if err := env.pool.QueryRow(ctx, selectFloorCandidateSQL).Scan(
				&candidate.ID, &candidate.MetricValue, &candidate.ZScore, &candidate.Context); err != nil {
				t.Fatalf("failed to read candidate: %v", err)
			}
			if math.Abs(candidate.ZScore-tc.wantZ) > 1e-6 { // z_score is stored as a real
				t.Errorf("z-score = %v, want %v", candidate.ZScore, tc.wantZ)
			}
			if candidateStdDevFloored(candidate.Context) {
				t.Errorf("context %s records a flat baseline", candidate.Context)
			}
			env.engine.createAnomalyAlert(ctx, candidate, 3)
			if candidate.AlertID == nil {
				t.Fatal("expected an alert to be created")
			}
			if got := env.alert(t, *candidate.AlertID); got.severity != "critical" {
				t.Errorf("alert severity = %s, want critical", got.severity)
			}
		})
	}
}

// TestValueFloorSuppressesAndClears covers the MinValue floor: a value
// under it raises no candidate however far it is from the baseline, and
// counts as in band for clearing an open anomaly alert, consistently with
// the recovery added for issue #611.
func TestValueFloorSuppressesAndClears(t *testing.T) {
	t.Run("no candidate under the floor", func(t *testing.T) {
		env := newRecoveryEnv(t)
		setMetricFloor(env.engine.getConfig(), tierSkipMetric, 50, 0)

		// z = 30 against the baseline, but under the floor of 50.
		env.passes(t, val(recoveryMean+30))
		if n := env.candidates(t); n != 0 {
			t.Fatalf("candidates = %d, want 0", n)
		}

		// At the floor the value is scored as usual.
		env.passes(t, val(50))
		if n := env.candidates(t); n != 1 {
			t.Fatalf("candidates at the floor = %d, want 1", n)
		}
	})

	t.Run("under the floor clears an open alert", func(t *testing.T) {
		env := newRecoveryEnv(t)
		cfg := env.engine.getConfig()
		setMetricFloor(cfg, tierSkipMetric, 50, 0)
		cfg.Anomaly.Tier1.ClearCount = 2
		id := env.seedAlert(t, nil, "warning", "active")

		env.passes(t, val(recoveryMean+30))
		wantStatus(t, env.alert(t, id), "active")
		env.passes(t, val(recoveryMean+30))
		wantStatus(t, env.alert(t, id), "cleared")
	})

	t.Run("under the floor clears with a cold baseline", func(t *testing.T) {
		env := newRecoveryEnv(t)
		cfg := env.engine.getConfig()
		setMetricFloor(cfg, tierSkipMetric, 50, 0)
		cfg.Anomaly.Tier1.ClearCount = 1
		env.setBaseline(t, 1)
		id := env.seedAlert(t, nil, "warning", "active")

		env.passes(t, val(recoveryMean+30))
		wantStatus(t, env.alert(t, id), "cleared")
	})

	t.Run("above the floor out of band holds the alert", func(t *testing.T) {
		env := newRecoveryEnv(t)
		cfg := env.engine.getConfig()
		setMetricFloor(cfg, tierSkipMetric, 15, 0)
		cfg.Anomaly.Tier1.ClearCount = 1
		id := env.seedAlert(t, nil, "warning", "active")

		env.passes(t, val(criticalValue))
		wantStatus(t, env.alert(t, id), "active")
	})
}

// TestRecoveryRefreshCapsFlooredSeverity checks that refreshing an open
// alert applies the same severity cap as raising it: a floored critical
// z-score escalates an info alert only to warning.
func TestRecoveryRefreshCapsFlooredSeverity(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		floored bool
		want    string
	}{
		{"floored divisor capped at warning", true, "warning"},
		{"unfloored divisor reaches critical", false, "critical"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newRecoveryEnv(t)
			id := env.seedAlert(t, nil, "info", "active")
			pass := env.engine.loadAnomalyRecovery(ctx)
			value := &database.MetricValue{ConnectionID: env.connID, Value: criticalValue}
			score := anomalyScore{zScore: 100, scored: true, stddevFloored: tc.floored}
			cfg := env.engine.getConfig()
			env.engine.recoverAnomalyAlerts(ctx, pass, tierSkipMetric, value, score, cfg, 3)
			if got := env.alert(t, id); got.severity != tc.want {
				t.Errorf("severity = %s, want %s", got.severity, tc.want)
			}
		})
	}
}

// TestDefaultLoadAverageFloor checks a built-in default end to end: a
// fifteen-minute load average under 1 is never anomalous, even against a
// baseline it is many standard deviations from. The baseline sits above
// the floor so that the fall to 0.9 scores out of band with the default
// MinStdDev alone, and only the default MinValue of 1 keeps it quiet.
func TestDefaultLoadAverageFloor(t *testing.T) {
	env := newRecoveryEnv(t)
	now := time.Now().UTC()
	if err := env.ds.UpsertMetricBaseline(context.Background(), &database.MetricBaseline{
		ConnectionID:     env.connID,
		MetricName:       tierSkipMetric,
		PeriodType:       "all",
		Mean:             3,
		StdDev:           0.1,
		SampleCount:      200,
		LastCalculated:   now,
		EarliestSampleAt: now.Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("UpsertMetricBaseline failed: %v", err)
	}

	// (0.9 - 3) / 0.5 = -4.2, out of band but under the floor.
	env.passes(t, val(0.9))
	if n := env.candidates(t); n != 0 {
		t.Fatalf("candidates under the default floor = %d, want 0", n)
	}

	// 6 is above the floor; (6 - 3) / 0.5 = 6 with the default
	// MinStdDev, so it is still reported.
	env.passes(t, val(6))
	if n := env.candidates(t); n != 1 {
		t.Fatalf("candidates above the default floor = %d, want 1", n)
	}
}
