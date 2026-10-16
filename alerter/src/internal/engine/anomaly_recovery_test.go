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
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

func TestAnomalySeverity(t *testing.T) {
	tests := []struct {
		z    float64
		want string
	}{
		{0, "info"},
		{3.5, "info"},
		{-5.99, "info"},
		{6, "warning"},
		{-6, "warning"},
		{11.9, "warning"},
		{12, "critical"},
		{-100, "critical"},
	}
	for _, tc := range tests {
		if got := anomalySeverity(tc.z, 3); got != tc.want {
			t.Errorf("anomalySeverity(%v, 3) = %s, want %s", tc.z, got, tc.want)
		}
	}
}

func TestEscalatedSeverity(t *testing.T) {
	tests := []struct {
		current, candidate, want string
	}{
		{"info", "warning", "warning"},
		{"info", "critical", "critical"},
		{"warning", "critical", "critical"},
		{"warning", "info", "warning"},
		{"critical", "warning", "critical"},
		{"critical", "critical", "critical"},
		{"unknown", "info", "info"},
		{"warning", "unknown", "warning"},
	}
	for _, tc := range tests {
		if got := escalatedSeverity(tc.current, tc.candidate); got != tc.want {
			t.Errorf("escalatedSeverity(%s, %s) = %s, want %s",
				tc.current, tc.candidate, got, tc.want)
		}
	}
}

func TestNewAnomalyAlertKey(t *testing.T) {
	empty := ""
	app := "app"
	wide := newAnomalyAlertKey("m", 1, nil)
	if wide.hasDB {
		t.Error("connection-wide key must not carry a database")
	}
	if wide == newAnomalyAlertKey("m", 1, &empty) {
		t.Error("an empty database name must not match a connection-wide series")
	}
	sameApp := "app"
	if newAnomalyAlertKey("m", 1, &app) != newAnomalyAlertKey("m", 1, &sameApp) {
		t.Error("equal series held in different strings must produce equal keys")
	}
	if newAnomalyAlertKey("m", 1, &app) == newAnomalyAlertKey("m", 2, &app) {
		t.Error("different connections must produce different keys")
	}
}

func TestAnomalyRecoveryPassNil(t *testing.T) {
	var pass *anomalyRecoveryPass
	if !pass.empty() {
		t.Error("a nil pass must be empty")
	}
	if pass.covers(newAnomalyAlertKey("m", 1, nil)) {
		t.Error("a nil pass must cover nothing")
	}
	pass.resetConnection(1) // must not panic
}

// TestAnomalyRecoveryPassResetConnection checks a connection-wide reset
// discards the streaks of every series on that connection, and only
// those.
func TestAnomalyRecoveryPassResetConnection(t *testing.T) {
	app := "app"
	pass := &anomalyRecoveryPass{
		alerts: map[anomalyAlertKey][]*database.Alert{
			newAnomalyAlertKey("m", 1, nil):  {{ID: 10}},
			newAnomalyAlertKey("m", 1, &app): {{ID: 11}, {ID: 12}},
			newAnomalyAlertKey("m", 2, nil):  {{ID: 20}},
		},
		next: map[int64]anomalyStreak{
			10: {count: 1}, 11: {count: 2}, 12: {count: 1}, 20: {count: 2},
		},
	}
	pass.resetConnection(1)
	want := map[int64]anomalyStreak{20: {count: 2}}
	if !reflect.DeepEqual(pass.next, want) {
		t.Errorf("streaks after reset = %v, want %v", pass.next, want)
	}
}

// TestAnomalyRecoveryPassResetAlert checks a reset drops the count but
// remembers the newest sample seen, so the same sample is never counted
// twice.
func TestAnomalyRecoveryPassResetAlert(t *testing.T) {
	early := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	late := early.Add(5 * time.Minute)
	tests := []struct {
		name   string
		before map[int64]anomalyStreak
		sample time.Time
		want   map[int64]anomalyStreak
	}{
		{"no streak, no sample", map[int64]anomalyStreak{}, time.Time{}, map[int64]anomalyStreak{}},
		{"no streak, new sample", map[int64]anomalyStreak{}, late,
			map[int64]anomalyStreak{1: {lastSample: late}}},
		{"streak, no sample", map[int64]anomalyStreak{1: {count: 2, lastSample: early}}, time.Time{},
			map[int64]anomalyStreak{1: {lastSample: early}}},
		{"streak, newer sample", map[int64]anomalyStreak{1: {count: 2, lastSample: early}}, late,
			map[int64]anomalyStreak{1: {lastSample: late}}},
		{"streak, older sample", map[int64]anomalyStreak{1: {count: 2, lastSample: late}}, early,
			map[int64]anomalyStreak{1: {lastSample: late}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pass := &anomalyRecoveryPass{next: tc.before}
			pass.resetAlert(1, tc.sample)
			if !reflect.DeepEqual(pass.next, tc.want) {
				t.Errorf("streaks = %v, want %v", pass.next, tc.want)
			}
		})
	}
}

func TestAnomalyMetrics(t *testing.T) {
	const (
		baselined    = "pg_sys_load_avg_info.load_avg_fifteen_minutes"
		notBaselined = "no_such_metric"
	)
	var otherBaselined string
	for _, name := range database.BaselineSupportedMetrics() {
		if name != baselined {
			otherBaselined = name
			break
		}
	}
	if !database.SupportsBaselines(baselined) || otherBaselined == "" {
		t.Fatal("test needs two metrics that support baselines")
	}

	rules := []*database.AlertRule{
		{MetricName: baselined},
		{MetricName: notBaselined},
		{MetricName: baselined},
	}

	if got, want := anomalyMetrics(rules, nil), []anomalyMetric{{baselined, true}}; !reflect.DeepEqual(got, want) {
		t.Errorf("rules only: got %v, want %v", got, want)
	}

	pass := &anomalyRecoveryPass{metrics: []string{baselined, otherBaselined, notBaselined}}
	want := []anomalyMetric{{baselined, true}, {otherBaselined, false}}
	if got := anomalyMetrics(rules, pass); !reflect.DeepEqual(got, want) {
		t.Errorf("rules and alerts: got %v, want %v", got, want)
	}
}

func TestFinishAnomalyRecoveryNilKeepsStreaks(t *testing.T) {
	e := &Engine{}
	e.finishAnomalyRecovery(&anomalyRecoveryPass{next: map[int64]anomalyStreak{7: {count: 2}}})
	e.finishAnomalyRecovery(nil)
	if got := e.anomalyStreak(7); got != 2 {
		t.Errorf("streak = %d, want 2 kept across a nil pass", got)
	}
	e.resetAnomalyStreaks()
	if got := e.anomalyStreak(7); got != 0 {
		t.Errorf("streak = %d, want 0 after a reset", got)
	}
}
