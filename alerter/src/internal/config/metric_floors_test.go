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
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func floatPtr(v float64) *float64 { return &v }

func TestDefaultMetricFloors(t *testing.T) {
	// The floors issue #617 named explicitly.
	cases := map[string]MetricFloor{
		"pg_sys_cpu_usage_info.processor_time_percent": {MinValue: 50, MinStdDev: 5},
		"pg_stat_activity.count":                       {MinValue: 10, MinStdDev: 5},
		"pg_stat_database.temp_files_delta":            {MinValue: 10, MinStdDev: 10},
		"pg_stat_database.deadlocks_delta":             {MinStdDev: 1},
		"no_such_metric":                               {},
	}
	for metric, want := range cases {
		if got := DefaultMetricFloor(metric); got != want {
			t.Errorf("DefaultMetricFloor(%q) = %+v, want %+v", metric, got, want)
		}
	}

	names := DefaultMetricFloorNames()
	if len(names) != len(defaultMetricFloors) {
		t.Fatalf("DefaultMetricFloorNames() returned %d names, want %d",
			len(names), len(defaultMetricFloors))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("DefaultMetricFloorNames() is not sorted: %v", names)
	}
	for _, name := range names {
		floor := DefaultMetricFloor(name)
		if floor.MinValue < 0 || floor.MinStdDev <= 0 {
			t.Errorf("default floor for %s = %+v; every default needs a positive MinStdDev", name, floor)
		}
	}

	// A fresh config carries no overrides, so it resolves to the defaults.
	cfg := NewConfig()
	if got := cfg.Anomaly.Tier1.MetricFloor("pg_stat_activity.count"); got != (MetricFloor{MinValue: 10, MinStdDev: 5}) {
		t.Errorf("NewConfig floor = %+v", got)
	}
}

func TestMetricFloorOverrides(t *testing.T) {
	const cpu = "pg_sys_cpu_usage_info.processor_time_percent"
	cases := []struct {
		name     string
		metric   string
		override MetricFloorConfig
		want     MetricFloor
	}{
		{"min_value only keeps default min_stddev", cpu,
			MetricFloorConfig{MinValue: floatPtr(70)}, MetricFloor{MinValue: 70, MinStdDev: 5}},
		{"min_stddev only keeps default min_value", cpu,
			MetricFloorConfig{MinStdDev: floatPtr(8)}, MetricFloor{MinValue: 50, MinStdDev: 8}},
		{"zeros disable both", cpu,
			MetricFloorConfig{MinValue: floatPtr(0), MinStdDev: floatPtr(0)}, MetricFloor{}},
		{"empty entry keeps defaults", cpu,
			MetricFloorConfig{}, MetricFloor{MinValue: 50, MinStdDev: 5}},
		{"metric without defaults", "custom_metric",
			MetricFloorConfig{MinValue: floatPtr(3)}, MetricFloor{MinValue: 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier1 := Tier1Config{MetricFloors: map[string]MetricFloorConfig{tc.metric: tc.override}}
			if got := tier1.MetricFloor(tc.metric); got != tc.want {
				t.Errorf("MetricFloor(%q) = %+v, want %+v", tc.metric, got, tc.want)
			}
		})
	}
}

func TestValidateMetricFloors(t *testing.T) {
	cases := []struct {
		name    string
		floors  map[string]MetricFloorConfig
		wantErr string
	}{
		{"nil map", nil, ""},
		{"valid", map[string]MetricFloorConfig{
			"m": {MinValue: floatPtr(1), MinStdDev: floatPtr(0)},
		}, ""},
		{"empty name", map[string]MetricFloorConfig{
			" ": {MinValue: floatPtr(1)},
		}, "keys must be non-empty metric names"},
		{"negative min_value", map[string]MetricFloorConfig{
			"m": {MinValue: floatPtr(-1)},
		}, "metric_floors.m.min_value"},
		{"NaN min_stddev", map[string]MetricFloorConfig{
			"m": {MinStdDev: floatPtr(math.NaN())},
		}, "metric_floors.m.min_stddev"},
		{"infinite min_stddev", map[string]MetricFloorConfig{
			"m": {MinStdDev: floatPtr(math.Inf(1))},
		}, "metric_floors.m.min_stddev"},
		{"first bad entry in name order", map[string]MetricFloorConfig{
			"b": {MinValue: floatPtr(-1)},
			"a": {MinStdDev: floatPtr(-1)},
		}, "metric_floors.a.min_stddev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.Anomaly.Tier1.MetricFloors = tc.floors
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadMetricFloorsFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerter.yaml")
	yamlText := `
anomaly:
  tier1:
    metric_floors:
      pg_sys_cpu_usage_info.processor_time_percent:
        min_value: 70
      pg_stat_activity.count:
        min_value: 0
        min_stddev: 0
`
	if err := os.WriteFile(path, []byte(yamlText), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile() = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}

	tier1 := cfg.Anomaly.Tier1
	if got := tier1.MetricFloor("pg_sys_cpu_usage_info.processor_time_percent"); got != (MetricFloor{MinValue: 70, MinStdDev: 5}) {
		t.Errorf("CPU floor = %+v, want min_value 70 with the default min_stddev", got)
	}
	if got := tier1.MetricFloor("pg_stat_activity.count"); got != (MetricFloor{}) {
		t.Errorf("session count floor = %+v, want both disabled", got)
	}
	if got := tier1.MetricFloor("pg_stat_database.temp_files_delta"); got != DefaultMetricFloor("pg_stat_database.temp_files_delta") {
		t.Errorf("temp files floor = %+v, want the default", got)
	}
}
