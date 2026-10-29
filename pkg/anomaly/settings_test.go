/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package anomaly

import (
	"sort"
	"testing"
)

// TestDefaultScoringSettings pins the scalar defaults; the alerter's
// config package checks the whole value against its own defaults.
func TestDefaultScoringSettings(t *testing.T) {
	s := DefaultScoringSettings()
	if s.MaxZScore != 100 || s.DefaultSensitivity != 3 {
		t.Errorf("MaxZScore, DefaultSensitivity = %v, %v; want 100, 3",
			s.MaxZScore, s.DefaultSensitivity)
	}
	if s.VarianceFloor != (VarianceFloorConfig{RelativePct: 0.05, AbsoluteFloor: 0.001}) {
		t.Errorf("VarianceFloor = %+v", s.VarianceFloor)
	}
	if s.Warmup != shippedWarmup {
		t.Errorf("Warmup = %+v, want %+v", s.Warmup, shippedWarmup)
	}
	if len(s.MetricFloors) != len(DefaultMetricFloorNames()) {
		t.Errorf("MetricFloors has %d entries, want %d",
			len(s.MetricFloors), len(DefaultMetricFloorNames()))
	}

	// Each call returns a fresh map, so a caller cannot change the
	// built-in floors by writing to it.
	s.MetricFloors["pg_stat_activity.count"] = MetricFloor{}
	if DefaultMetricFloor("pg_stat_activity.count") == (MetricFloor{}) {
		t.Error("writing to the returned map changed the built-in floors")
	}
}

// TestScoringSettingsMetricFloor checks lookups, including a miss and a
// nil map.
func TestScoringSettingsMetricFloor(t *testing.T) {
	s := DefaultScoringSettings()
	if got := s.MetricFloor("pg_stat_activity.count"); got != (MetricFloor{MinValue: 10, MinStdDev: 5}) {
		t.Errorf("pg_stat_activity.count floor = %+v", got)
	}
	if got := s.MetricFloor("no.such.metric"); got != (MetricFloor{}) {
		t.Errorf("unknown metric floor = %+v, want zero", got)
	}
	if got := (ScoringSettings{}).MetricFloor("pg_stat_activity.count"); got != (MetricFloor{}) {
		t.Errorf("nil map floor = %+v, want zero", got)
	}
}

// TestDefaultMetricFloors pins a sample of the built-in floors and the
// shape of the name list.
func TestDefaultMetricFloors(t *testing.T) {
	cases := map[string]MetricFloor{
		"pg_sys_cpu_usage_info.processor_time_percent": {MinValue: 50, MinStdDev: 5},
		"pg_stat_database.temp_files_delta":            {MinValue: 10, MinStdDev: 10},
		"pg_replication_slots.max_retained_bytes":      {MinValue: 256 << 20, MinStdDev: 64 << 20},
		"pg_stat_database.deadlocks_delta":             {MinStdDev: 1},
		"pg_stat_database.cache_hit_ratio":             {},
	}
	for metric, want := range cases {
		if got := DefaultMetricFloor(metric); got != want {
			t.Errorf("DefaultMetricFloor(%q) = %+v, want %+v", metric, got, want)
		}
	}

	names := DefaultMetricFloorNames()
	if len(names) != 19 {
		t.Errorf("DefaultMetricFloorNames() returned %d names, want 19", len(names))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("DefaultMetricFloorNames() is not sorted: %v", names)
	}
	for _, name := range names {
		if DefaultMetricFloor(name) == (MetricFloor{}) {
			t.Errorf("%s is listed but has no floor", name)
		}
	}
}
