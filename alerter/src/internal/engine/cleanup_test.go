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

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestCheckAlertResolvedMissingFields tests checkAlertResolved with missing required fields
func TestCheckAlertResolvedMissingFields(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name  string
		alert *database.Alert
	}{
		{
			name: "nil metric name",
			alert: &database.Alert{
				ID:             1,
				MetricName:     nil,
				ThresholdValue: float64Ptr(50.0),
				Operator:       strPtr(">"),
			},
		},
		{
			name: "nil threshold value",
			alert: &database.Alert{
				ID:             2,
				MetricName:     strPtr("test_metric"),
				ThresholdValue: nil,
				Operator:       strPtr(">"),
			},
		},
		{
			name: "nil operator",
			alert: &database.Alert{
				ID:             3,
				MetricName:     strPtr("test_metric"),
				ThresholdValue: float64Ptr(50.0),
				Operator:       nil,
			},
		},
		{
			name: "all nil",
			alert: &database.Alert{
				ID:             4,
				MetricName:     nil,
				ThresholdValue: nil,
				Operator:       nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should return early without panic when required fields are nil
			engine.checkAlertResolved(context.TODO(), tt.alert, nil)
		})
	}
}

// TestClassifyAbsentMetric covers the decision the cleaner makes when a
// metric returns no row for an active alert. Clearing on absence is only
// safe when the registry says absence is the recovery signal *and* the
// probe feeding the metric is currently reporting for the connection;
// every other combination leaves the alert active. See GitHub issue #407.
func TestClassifyAbsentMetric(t *testing.T) {
	const connID = 7

	fresh := []database.ProbeStaleness{
		{ConnectionID: connID, ProbeName: "pg_replication_slots", StalenessRatio: 0.4},
		{ConnectionID: connID, ProbeName: "pg_stat_activity", StalenessRatio: 1.0},
	}
	stale := []database.ProbeStaleness{
		{ConnectionID: connID, ProbeName: "pg_replication_slots", StalenessRatio: 8.0},
	}
	otherConnection := []database.ProbeStaleness{
		{ConnectionID: connID + 1, ProbeName: "pg_replication_slots", StalenessRatio: 0.2},
	}

	tests := []struct {
		name             string
		clearsWhenAbsent bool
		probe            string
		entries          []database.ProbeStaleness
		want             absentMetricVerdict
	}{
		{
			name:             "absence is not a recovery signal",
			clearsWhenAbsent: false,
			probe:            "pg_stat_database",
			entries:          fresh,
			want:             absentMetricNotAbsenceDriven,
		},
		{
			name:             "registry entry names no probe",
			clearsWhenAbsent: true,
			probe:            "",
			entries:          fresh,
			want:             absentMetricNoProbe,
		},
		{
			name:             "probe is reporting",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			entries:          fresh,
			want:             absentMetricClear,
		},
		{
			name:             "probe is at the freshness limit",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			entries: []database.ProbeStaleness{{
				ConnectionID:   connID,
				ProbeName:      "pg_replication_slots",
				StalenessRatio: probeFreshnessRatioLimit,
			}},
			want: absentMetricClear,
		},
		{
			name:             "probe has stalled",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			entries:          stale,
			want:             absentMetricProbeNotReporting,
		},
		{
			name:             "probe is absent from the staleness view",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			entries:          nil,
			want:             absentMetricProbeNotReporting,
		},
		{
			name:             "another probe is fresh but this one is not listed",
			clearsWhenAbsent: true,
			probe:            "spock_resolutions",
			entries:          fresh,
			want:             absentMetricProbeNotReporting,
		},
		{
			name:             "the probe is fresh on another connection only",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			entries:          otherConnection,
			want:             absentMetricProbeNotReporting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAbsentMetric(tt.clearsWhenAbsent, tt.probe, connID, tt.entries)
			if got != tt.want {
				t.Errorf("classifyAbsentMetric(%v, %q) = %v, want %v",
					tt.clearsWhenAbsent, tt.probe, got, tt.want)
			}
		})
	}
}
