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
	"math"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestBuildClassificationPrompt tests the buildClassificationPrompt method
// with various combinations of cluster peers, cluster alerts, and ack
// history.
func TestBuildClassificationPrompt(t *testing.T) {
	engine := &Engine{}

	now := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)

	candidate := &database.AnomalyCandidate{
		ID:           1,
		ConnectionID: 5,
		MetricName:   "cpu_usage_percent",
		MetricValue:  92.5,
		ZScore:       3.2,
		DetectedAt:   now,
		Context:      `{"baseline_mean": 40.0, "baseline_stddev": 15.0, "period_type": "hourly"}`,
		Tier1Pass:    true,
	}

	similarAnomalies := []*database.SimilarAnomaly{
		{
			MetricName:    "cpu_usage_percent",
			Similarity:    0.87,
			FinalDecision: strPtr("alert"),
		},
	}

	t.Run("with cluster peers and alerts", func(t *testing.T) {
		peers := []*database.ClusterPeerInfo{
			{ConnectionID: 6, ConnectionName: "primary-node", NodeRole: "binary_primary"},
			{ConnectionID: 7, ConnectionName: "standby-node", NodeRole: "binary_standby"},
		}
		clusterAlerts := []*database.Alert{
			{
				ID:           300,
				Title:        "WAL lag increasing",
				Severity:     "warning",
				ConnectionID: 6,
				TriggeredAt:  now.Add(-3 * time.Minute),
			},
		}

		prompt := engine.buildClassificationPrompt(
			candidate, similarAnomalies, nil, peers, clusterAlerts,
		)

		checks := []struct {
			label    string
			fragment string
		}{
			{"cluster heading", "Cluster Context"},
			{"peer count", "2 other node(s)"},
			{"peer name primary", "primary-node"},
			{"peer role", "binary_primary"},
			{"peer name standby", "standby-node"},
			{"cluster alert title", "WAL lag increasing"},
			{"cluster alert server", "primary-node"},
		}

		for _, c := range checks {
			if !strings.Contains(prompt, c.fragment) {
				t.Errorf("prompt missing %s (expected fragment %q)", c.label, c.fragment)
			}
		}
	})

	t.Run("without cluster peers", func(t *testing.T) {
		prompt := engine.buildClassificationPrompt(
			candidate, similarAnomalies, nil, nil, nil,
		)

		if strings.Contains(prompt, "Cluster Context") {
			t.Error("prompt should not contain cluster context when no peers")
		}
	})

	t.Run("with ack history", func(t *testing.T) {
		ackHistory := []*database.AcknowledgedAnomalyAlert{
			{
				ID:             10,
				AckMessage:     strPtr("Known maintenance window"),
				FalsePositive:  true,
				AcknowledgedBy: strPtr("ops_user"),
				AcknowledgedAt: timePtr(now.Add(-24 * time.Hour)),
				MetricValue:    float64Ptr(88.0),
				ZScore:         float64Ptr(2.8),
				Severity:       "warning",
			},
		}

		prompt := engine.buildClassificationPrompt(
			candidate, similarAnomalies, ackHistory, nil, nil,
		)

		checks := []struct {
			label    string
			fragment string
		}{
			{"feedback heading", "Past User Feedback"},
			{"ack message", "Known maintenance window"},
			{"false positive marker", "FALSE POSITIVE"},
			{"ack user", "ops_user"},
		}

		for _, c := range checks {
			if !strings.Contains(prompt, c.fragment) {
				t.Errorf("prompt missing %s (expected fragment %q)", c.label, c.fragment)
			}
		}
	})
}

// TestIsBaselineWarm verifies the warmup gate helper requires
// both a minimum sample count and a minimum wall-clock span,
// fails closed on a zero EarliestSampleAt unless the span check
// is disabled, and falls back to the daily thresholds for
// unknown period_types.
func TestIsBaselineWarm(t *testing.T) {
	cfg := config.WarmupConfig{
		All: config.PerPeriodWarmupConfig{
			MinSamples: 100, MinSpanHours: 24,
		},
		Hourly: config.PerPeriodWarmupConfig{
			MinSamples: 5, MinSpanHours: 120,
		},
		Daily: config.PerPeriodWarmupConfig{
			MinSamples: 3, MinSpanHours: 336,
		},
	}
	now := time.Now().UTC()

	cases := []struct {
		name        string
		periodType  string
		samples     int64
		earliest    time.Time
		cfgOverride *config.WarmupConfig
		want        bool
	}{
		{"all: both pass", "all", 200,
			now.Add(-48 * time.Hour), nil, true},
		{"all: under-samples", "all", 50,
			now.Add(-48 * time.Hour), nil, false},
		{"all: under-span", "all", 200,
			now.Add(-12 * time.Hour), nil, false},
		{"all: NULL earliest", "all", 200,
			time.Time{}, nil, false},
		{"hourly: both pass", "hourly", 10,
			now.Add(-200 * time.Hour), nil, true},
		{"daily: both pass", "daily", 5,
			now.Add(-400 * time.Hour), nil, true},
		{"gate disabled by zero config", "all", 0,
			time.Time{},
			&config.WarmupConfig{
				All: config.PerPeriodWarmupConfig{
					MinSamples: 0, MinSpanHours: 0,
				},
			},
			true},
		{"unknown period_type falls back to daily",
			"weekly", 5, now.Add(-400 * time.Hour),
			nil, true},
		{"unknown period_type under daily span",
			"weekly", 5, now.Add(-100 * time.Hour),
			nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useCfg := cfg
			if tc.cfgOverride != nil {
				useCfg = *tc.cfgOverride
			}
			b := database.MetricBaseline{
				PeriodType:       tc.periodType,
				SampleCount:      tc.samples,
				EarliestSampleAt: tc.earliest,
			}
			got := isBaselineWarm(b, useCfg, now)
			if got != tc.want {
				t.Errorf("name=%s: got %v, want %v",
					tc.name, got, tc.want)
			}
		})
	}
}

// TestEffectiveStdDev verifies the hybrid variance floor helper
// returns max(raw_stddev, max(|mean| * RelativePct, AbsoluteFloor))
// across the relevant branches of the formula.
func TestEffectiveStdDev(t *testing.T) {
	cases := []struct {
		name     string
		mean     float64
		stddev   float64
		relPct   float64
		absFloor float64
		want     float64
	}{
		{"raw stddev dominates", 100, 10, 0.05, 0.001, 10},
		{"relative floor kicks in", 100, 0.01, 0.05, 0.001, 5},
		{"absolute floor kicks in", 0, 0.0001, 0.05, 0.001, 0.001},
		{"mean zero stddev zero", 0, 0, 0.05, 0.001, 0.001},
		{"negative mean uses abs", -200, 0.01, 0.05, 0.001, 10},
		{"both floors zero", 100, 0.0001, 0, 0, 0.0001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := database.MetricBaseline{Mean: tc.mean, StdDev: tc.stddev}
			cfg := config.VarianceFloorConfig{
				RelativePct:   tc.relPct,
				AbsoluteFloor: tc.absFloor,
			}
			got := effectiveStdDev(b, cfg)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("name=%s: got %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
