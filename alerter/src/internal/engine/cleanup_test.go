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
			engine.checkAlertResolved(context.TODO(), tt.alert, nil, nil)
		})
	}
}

// TestClassifyAbsentMetric covers the decision the cleaner makes when a
// metric returns no row for an active alert. Clearing on absence is only
// safe when the registry says absence is the recovery signal *and* the
// probe feeding the metric collected inside the window that metric's
// latest query reads; every other combination leaves the alert active.
// See GitHub issue #407.
func TestClassifyAbsentMetric(t *testing.T) {
	const connID = 7

	// The slots metrics read a fifteen minute window, so a collection
	// four minutes ago is inside it and one forty minutes ago is not.
	const slotWindow = 15 * time.Minute

	fresh := []database.ProbeStaleness{
		{ConnectionID: connID, ProbeName: "pg_replication_slots", IsAvailable: true,
			SinceCollected: 4 * time.Minute},
		{ConnectionID: connID, ProbeName: "pg_stat_activity", IsAvailable: true,
			SinceCollected: 1 * time.Minute},
	}
	stale := []database.ProbeStaleness{
		{ConnectionID: connID, ProbeName: "pg_replication_slots", IsAvailable: true,
			SinceCollected: 40 * time.Minute},
	}
	otherConnection := []database.ProbeStaleness{
		{ConnectionID: connID + 1, ProbeName: "pg_replication_slots", IsAvailable: true,
			SinceCollected: 30 * time.Second},
	}

	tests := []struct {
		name             string
		clearsWhenAbsent bool
		probe            string
		window           time.Duration
		entries          []database.ProbeStaleness
		age              time.Duration
		want             absentMetricVerdict
	}{
		{
			name:             "absence is not a recovery signal",
			clearsWhenAbsent: false,
			probe:            "pg_stat_database",
			window:           0,
			entries:          fresh,
			want:             absentMetricNotAbsenceDriven,
		},
		{
			name:             "registry entry names no probe",
			clearsWhenAbsent: true,
			probe:            "",
			window:           slotWindow,
			entries:          fresh,
			want:             absentMetricNoProbe,
		},
		{
			name:             "registry entry declares no window",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           0,
			entries:          fresh,
			want:             absentMetricNoWindow,
		},
		{
			name:             "probe collected inside the window",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries:          fresh,
			want:             absentMetricClear,
		},
		{
			name:             "probe collected exactly on the window edge",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID: connID,
				ProbeName:    "pg_replication_slots", IsAvailable: true,
				SinceCollected: slotWindow,
			}},
			want: absentMetricClear,
		},
		{
			name:             "probe collected just outside the window",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID: connID,
				ProbeName:    "pg_replication_slots", IsAvailable: true,
				SinceCollected: slotWindow + time.Second,
			}},
			want: absentMetricProbeNotReporting,
		},
		{
			// A probe configured to run every hour is well within its
			// interval two minutes after collecting, so the old ratio
			// gate said it was current, yet its fifteen minute window
			// empties long before the next collection lands. The
			// verdict must follow the window, not the interval.
			name:             "interval is longer than the window",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID: connID,
				ProbeName:    "pg_replication_slots", IsAvailable: true,
				CollectionInterval: 3600,
				StalenessRatio:     0.5,
				SinceCollected:     30 * time.Minute,
			}},
			want: absentMetricProbeNotReporting,
		},
		{
			// The mirror image: a probe running every ten seconds is a
			// hundred intervals late two minutes after collecting, and
			// the old ratio gate refused to clear, but its window is
			// still wide open and the absence is genuine.
			name:             "probe is many intervals late but inside the window",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID: connID,
				ProbeName:    "pg_replication_slots", IsAvailable: true,
				CollectionInterval: 10,
				StalenessRatio:     12.0,
				SinceCollected:     2 * time.Minute,
			}},
			want: absentMetricClear,
		},
		{
			// The snapshot said 14m58s when it was read, but the pass
			// has been running for ten seconds since, so the probe is
			// really 15m08s late and outside the window. Judging the
			// frozen figure would clear here, which is #407 in
			// miniature.
			name:             "probe was inside the window at the read but is outside it now",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID: connID,
				ProbeName:    "pg_replication_slots", IsAvailable: true,
				SinceCollected: slotWindow - 2*time.Second,
			}},
			age:  10 * time.Second,
			want: absentMetricProbeNotReporting,
		},
		{
			name:             "probe is still inside the window after the pass has aged",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID: connID,
				ProbeName:    "pg_replication_slots", IsAvailable: true,
				SinceCollected: slotWindow - 30*time.Second,
			}},
			age:  10 * time.Second,
			want: absentMetricClear,
		},
		{
			// Issue #465 keeps unavailable probes in the staleness
			// view so the staleness alert cleaner can tell a fault
			// from operator action, which means this gate now sees
			// rows it never used to. An unavailable probe has stopped
			// collecting whatever its last collection says, so a
			// last_collected still inside the window must not clear
			// the alert, exactly as it did not when the query
			// filtered the row out altogether.
			name:             "probe is unavailable but collected inside the window",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID:   connID,
				ProbeName:      "pg_replication_slots",
				IsAvailable:    false,
				SinceCollected: 1 * time.Minute,
			}},
			want: absentMetricProbeNotReporting,
		},
		{
			name:             "probe is unavailable and outside the window",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries: []database.ProbeStaleness{{
				ConnectionID:   connID,
				ProbeName:      "pg_replication_slots",
				IsAvailable:    false,
				SinceCollected: 40 * time.Minute,
			}},
			want: absentMetricProbeNotReporting,
		},
		{
			name:             "probe has stalled",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries:          stale,
			want:             absentMetricProbeNotReporting,
		},
		{
			name:             "probe is absent from the staleness view",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries:          nil,
			want:             absentMetricProbeNotReporting,
		},
		{
			name:             "another probe is fresh but this one is not listed",
			clearsWhenAbsent: true,
			probe:            "spock_resolutions",
			window:           5 * time.Minute,
			entries:          fresh,
			want:             absentMetricProbeNotReporting,
		},
		{
			name:             "the probe is fresh on another connection only",
			clearsWhenAbsent: true,
			probe:            "pg_replication_slots",
			window:           slotWindow,
			entries:          otherConnection,
			want:             absentMetricProbeNotReporting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAbsentMetric(tt.clearsWhenAbsent, tt.probe, tt.window,
				connID, tt.entries, tt.age)
			if got != tt.want {
				t.Errorf("classifyAbsentMetric(%v, %q, %s, age %s) = %v, want %v",
					tt.clearsWhenAbsent, tt.probe, tt.window, tt.age, got, tt.want)
			}
		})
	}
}

// TestProbeStalenessSnapshotAge pins the snapshot's clock handling: an
// unloaded snapshot has no age, a loaded one ages from the moment before
// its read, and currentStalenessRatio brings a frozen ratio forward by
// that age without dividing by a missing interval.
func TestProbeStalenessSnapshotAge(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	now := base
	s := &probeStalenessSnapshot{now: func() time.Time { return now }}

	if got := s.age(); got != 0 {
		t.Errorf("age of an unloaded snapshot = %s, want 0", got)
	}

	s.loaded = true
	s.readAt = base
	now = base.Add(10 * time.Second)
	if got := s.age(); got != 10*time.Second {
		t.Errorf("age = %s, want 10s", got)
	}

	entry := database.ProbeStaleness{
		CollectionInterval: 60,
		StalenessRatio:     2.9,
		SinceCollected:     174 * time.Second,
	}
	if got := currentStalenessRatio(entry, 10*time.Second); got < 3.06 || got > 3.07 {
		t.Errorf("currentStalenessRatio = %v, want 184/60", got)
	}
	entry.CollectionInterval = 0
	if got := currentStalenessRatio(entry, 10*time.Second); got != 2.9 {
		t.Errorf("currentStalenessRatio with no interval = %v, want the frozen 2.9", got)
	}

	if (&probeStalenessSnapshot{}).clock().IsZero() {
		t.Error("the default clock returned the zero time")
	}
}
