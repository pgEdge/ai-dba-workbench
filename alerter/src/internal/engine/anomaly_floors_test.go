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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// disableMetricFloors overrides a metric's built-in floors (issue #617)
// with zeros, for tests that exercise the global variance floor alone.
func disableMetricFloors(cfg *config.Config, metric string) {
	setMetricFloor(cfg, metric, 0, 0)
}

// setMetricFloor overrides both of a metric's floors.
func setMetricFloor(cfg *config.Config, metric string, minValue, minStdDev float64) {
	if cfg.Anomaly.Tier1.MetricFloors == nil {
		cfg.Anomaly.Tier1.MetricFloors = make(map[string]config.MetricFloorConfig)
	}
	cfg.Anomaly.Tier1.MetricFloors[metric] = config.MetricFloorConfig{
		MinValue:  &minValue,
		MinStdDev: &minStdDev,
	}
}

func TestCappedAnomalySeverity(t *testing.T) {
	cases := []struct {
		name    string
		z       float64
		floored bool
		want    string
	}{
		{"info unfloored", 4, false, "info"},
		{"info floored", 4, true, "info"},
		{"warning unfloored", 7, false, "warning"},
		{"warning floored", -7, true, "warning"},
		{"critical unfloored", 13, false, "critical"},
		{"critical floored capped", 13, true, "warning"},
		{"clamped z floored capped", 100, true, "warning"},
		{"negative clamped z floored capped", -100, true, "warning"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cappedAnomalySeverity(tc.z, 3, tc.floored); got != tc.want {
				t.Errorf("cappedAnomalySeverity(%v, 3, %v) = %s, want %s",
					tc.z, tc.floored, got, tc.want)
			}
			score := anomalyScore{zScore: tc.z, stddevFloored: tc.floored, scored: true}
			if got := score.severity(3); got != tc.want {
				t.Errorf("severity() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestAnomalyScoreInBand(t *testing.T) {
	cases := []struct {
		name  string
		score anomalyScore
		want  bool
	}{
		{"within band", anomalyScore{zScore: 2.9, scored: true}, true},
		{"at upper edge", anomalyScore{zScore: 3, scored: true}, true},
		{"at lower edge", anomalyScore{zScore: -3, scored: true}, true},
		{"above band", anomalyScore{zScore: 3.1, scored: true}, false},
		{"below band", anomalyScore{zScore: -3.1, scored: true}, false},
		{"below value floor", anomalyScore{scored: true, belowFloor: true}, true},
		{"below value floor with a large z", anomalyScore{zScore: 50, scored: true, belowFloor: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.score.inBand(3); got != tc.want {
				t.Errorf("inBand(3) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCandidateStdDevFloored(t *testing.T) {
	cases := []struct {
		name    string
		context string
		want    bool
	}{
		{"floored", `{"baseline_mean": 0.00, "stddev_floored": true}`, true},
		{"not floored", `{"baseline_mean": 0.00, "stddev_floored": false}`, false},
		{"written before issue 617", `{"baseline_mean": 40.0, "baseline_stddev": 15.0}`, false},
		{"empty object", `{}`, false},
		{"unreadable", `not json`, false},
		{"empty string", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := candidateStdDevFloored(tc.context); got != tc.want {
				t.Errorf("candidateStdDevFloored(%q) = %v, want %v", tc.context, got, tc.want)
			}
		})
	}
}

func TestBuildClassificationPromptIncludesDivisor(t *testing.T) {
	e := &Engine{}
	candidate := &database.AnomalyCandidate{
		MetricName:   "pg_stat_database.temp_files_delta",
		MetricValue:  200,
		ZScore:       20,
		ConnectionID: 1,
		DetectedAt:   time.Now(),
		Context:      `{"baseline_mean": 0.00, "baseline_stddev": 0.00, "effective_stddev": 10.0000, "stddev_floored": true, "period_type": "all"}`,
	}
	prompt := e.buildClassificationPrompt(candidate, nil, nil, nil, nil)
	if !strings.Contains(prompt, "- Z-score divisor: 10.0000 (raised to a minimum floor: true)") {
		t.Errorf("prompt missing the divisor line:\n%s", prompt)
	}

	candidate.Context = `{"baseline_mean": 40.0, "baseline_stddev": 15.0, "period_type": "hourly"}`
	prompt = e.buildClassificationPrompt(candidate, nil, nil, nil, nil)
	if strings.Contains(prompt, "Z-score divisor") {
		t.Errorf("prompt has a divisor line for a context without one:\n%s", prompt)
	}
}

func TestUnknownMetricFloorNames(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Anomaly.Enabled = false
	if got := unknownMetricFloorNames(cfg); len(got) != 0 {
		t.Errorf("unknownMetricFloorNames(defaults) = %v, want none", got)
	}

	setMetricFloor(cfg, tierSkipMetric, 1, 1)
	setMetricFloor(cfg, "pg_stat_activty.count", 1, 1)
	setMetricFloor(cfg, "probe_staleness_ratio", 1, 1)
	want := []string{"pg_stat_activty.count", "probe_staleness_ratio"}
	if got := unknownMetricFloorNames(cfg); !reflect.DeepEqual(got, want) {
		t.Errorf("unknownMetricFloorNames() = %v, want %v", got, want)
	}

	// captureStderr holds its lock until the test ends, so each capture
	// runs in its own subtest.
	e := NewEngine(config.NewConfig(), nil, false)
	cases := []struct {
		name string
		run  func()
		want bool
	}{
		{"startup", func() { NewEngine(cfg, nil, false) }, true},
		{"reload", func() { e.ReloadConfig(cfg) }, true},
		{"reload with valid floors", func() { e.ReloadConfig(config.NewConfig()) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStderr(t, tc.run)
			got := strings.Contains(out, "metric_floors names metrics that anomaly detection does not score") &&
				strings.Contains(out, "pg_stat_activty.count, probe_staleness_ratio")
			if got != tc.want {
				t.Errorf("log %q: warning present = %v, want %v", out, got, tc.want)
			}
		})
	}
}
