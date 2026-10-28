/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package config

import (
	"reflect"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// TestDefaultScoringSettingsMatchConfigDefaults pins pkg/anomaly's
// shipped settings to the alerter's configuration defaults, so a
// component scoring with the former reaches the same verdict as an
// alerter running with an empty anomaly.tier1 section.
func TestDefaultScoringSettingsMatchConfigDefaults(t *testing.T) {
	got := NewConfig().Anomaly.Tier1.ScoringSettings()
	want := anomaly.DefaultScoringSettings()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tier1Config defaults = %+v\nwant anomaly.DefaultScoringSettings() = %+v", got, want)
	}
}

// TestScoringSettingsUsesEffectiveFloors checks that metric_floors
// overrides are laid over the built-in floors field by field, that a
// metric with only an override is included, and that the result does
// not alias the configuration.
func TestScoringSettingsUsesEffectiveFloors(t *testing.T) {
	tier1 := NewConfig().Anomaly.Tier1
	tier1.DefaultSensitivity = 4
	tier1.MaxZScore = 50
	tier1.MetricFloors = map[string]MetricFloorConfig{
		"pg_sys_cpu_usage_info.processor_time_percent": {MinValue: floatPtr(70)},
		"pg_stat_activity.count":                       {MinValue: floatPtr(0), MinStdDev: floatPtr(0)},
		"custom.metric":                                {MinStdDev: floatPtr(3)},
	}

	s := tier1.ScoringSettings()
	if s.DefaultSensitivity != 4 || s.MaxZScore != 50 {
		t.Errorf("DefaultSensitivity, MaxZScore = %v, %v; want 4, 50", s.DefaultSensitivity, s.MaxZScore)
	}
	cases := map[string]MetricFloor{
		"pg_sys_cpu_usage_info.processor_time_percent": {MinValue: 70, MinStdDev: 5},
		"pg_stat_activity.count":                       {},
		"custom.metric":                                {MinStdDev: 3},
		"pg_stat_database.temp_files_delta":            DefaultMetricFloor("pg_stat_database.temp_files_delta"),
	}
	for metric, want := range cases {
		if got := s.MetricFloor(metric); got != want {
			t.Errorf("MetricFloor(%q) = %+v, want %+v", metric, got, want)
		}
		if got := tier1.MetricFloor(metric); got != want {
			t.Errorf("Tier1Config.MetricFloor(%q) = %+v disagrees with ScoringSettings %+v", metric, got, want)
		}
	}
	if len(s.MetricFloors) != len(DefaultMetricFloorNames())+1 {
		t.Errorf("MetricFloors has %d entries, want %d", len(s.MetricFloors), len(DefaultMetricFloorNames())+1)
	}
}
