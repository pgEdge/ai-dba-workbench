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
	"strings"
	"testing"
)

// TestBaselineMetrics checks the shape of the definition list: sorted,
// non-empty, and every entry complete and consistent with Metric.
func TestBaselineMetrics(t *testing.T) {
	defs := BaselineMetrics()
	if len(defs) == 0 {
		t.Fatal("BaselineMetrics() is empty")
	}
	names := make([]string, len(defs))
	for i, def := range defs {
		names[i] = def.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("BaselineMetrics() is not sorted by name: %v", names)
	}

	for _, def := range defs {
		if strings.TrimSpace(def.HistoricalSQL) == "" {
			t.Errorf("%s has no HistoricalSQL", def.Name)
		}
		if def.ProbeName == "" {
			t.Errorf("%s has no ProbeName", def.Name)
		}
		if !strings.Contains(def.HistoricalSQL, "metrics."+def.ProbeName) {
			t.Errorf("%s HistoricalSQL does not read metrics.%s", def.Name, def.ProbeName)
		}
		if !strings.Contains(def.HistoricalSQL, "$1") {
			t.Errorf("%s HistoricalSQL does not bind the lookback as $1", def.Name)
		}
		if !strings.Contains(def.HistoricalSQL, "deleted_at IS NULL") {
			t.Errorf("%s HistoricalSQL does not exclude deleted connections", def.Name)
		}
		if def.PerDatabase != def.HistoricalScan.PerDatabase() {
			t.Errorf("%s PerDatabase = %v, but its scan kind says %v",
				def.Name, def.PerDatabase, def.HistoricalScan.PerDatabase())
		}

		got, ok := Metric(def.Name)
		if !ok {
			t.Errorf("Metric(%q) missed a listed metric", def.Name)
		} else if got != def {
			t.Errorf("Metric(%q) = %+v, want %+v", def.Name, got, def)
		}
	}
}

// TestMetricMiss checks that unknown and empty names are reported as
// absent with a zero definition.
func TestMetricMiss(t *testing.T) {
	for _, name := range []string{"", "no.such.metric", "pg_settings.max_connections", "metric_staleness"} {
		if def, ok := Metric(name); ok || def != (MetricDef{}) {
			t.Errorf("Metric(%q) = (%+v, %v), want a zero miss", name, def, ok)
		}
	}
}

// TestHistoricalScanPerDatabase pins which scan kinds carry a database.
func TestHistoricalScanPerDatabase(t *testing.T) {
	cases := map[HistoricalScan]bool{
		HistoricalScanBasic:            false,
		HistoricalScanWithDB:           true,
		HistoricalScanWithDBAndSamples: true,
		HistoricalScanBasicWithSamples: false,
		HistoricalScan(99):             false,
	}
	for scan, want := range cases {
		if got := scan.PerDatabase(); got != want {
			t.Errorf("HistoricalScan(%d).PerDatabase() = %v, want %v", scan, got, want)
		}
	}

	// A per-database metric must report as one.
	def, ok := Metric("pg_stat_database.cache_hit_ratio")
	if !ok || !def.PerDatabase {
		t.Errorf("pg_stat_database.cache_hit_ratio = (%+v, %v), want a per-database metric", def, ok)
	}
}

// TestSharedFragmentsEmbedded checks that the SQL fragments shared with
// the alerter's latest-value queries are embedded in the historical
// queries that need them.
func TestSharedFragmentsEmbedded(t *testing.T) {
	cases := map[string]string{
		"pg_sys_cpu_usage_info.processor_time_percent": CPUBusyPercentExpr,
		"pg_sys_disk_info.used_percent":                PseudoFilesystemExclusion,
	}
	for name, fragment := range cases {
		def, ok := Metric(name)
		if !ok {
			t.Fatalf("Metric(%q) missed", name)
		}
		if !strings.Contains(def.HistoricalSQL, fragment) {
			t.Errorf("%s HistoricalSQL does not embed its shared fragment", name)
		}
	}
}
