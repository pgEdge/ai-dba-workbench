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
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
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

// lookbackPredicate matches a historical query's lookback on
// collected_at, capturing the column as written.
var lookbackPredicate = regexp.MustCompile(
	`((?:\w+\.)?collected_at) > NOW\(\) - INTERVAL '1 day' \* \$1`)

// TestHistoricalUpperBound checks that every lookback on collected_at is
// followed at once by the $2 upper bound on the same column, so a reader
// of a past window can keep each inner scan off the present (GitHub
// issue #776).
func TestHistoricalUpperBound(t *testing.T) {
	for _, def := range BaselineMetrics() {
		sql := def.HistoricalSQL
		matches := lookbackPredicate.FindAllStringSubmatchIndex(sql, -1)
		if len(matches) == 0 {
			t.Errorf("%s HistoricalSQL has no lookback on collected_at", def.Name)
		}
		if got, want := strings.Count(sql, "$2"), len(matches); got != want {
			t.Errorf("%s HistoricalSQL binds $2 %d times, want once per lookback (%d)",
				def.Name, got, want)
		}
		for _, m := range matches {
			column := sql[m[2]:m[3]]
			rest := strings.TrimLeft(sql[m[1]:], " \t\n")
			if !strings.HasPrefix(rest, "AND "+column+" <= $2") {
				t.Errorf("%s HistoricalSQL lookback on %s has no $2 upper bound beside it",
					def.Name, column)
			}
		}
	}
}

// TestBucketWidthMatchesSQL keeps BucketWidth in step with the SQL: a
// query that buckets by hour declares an hour, a query that buckets by
// any other unit is caught until a width for it is declared, and a
// query that does not bucket declares none.
func TestBucketWidthMatchesSQL(t *testing.T) {
	for _, def := range BaselineMetrics() {
		hourly := strings.Contains(def.HistoricalSQL, "date_trunc('hour'")
		buckets := strings.Contains(def.HistoricalSQL, "date_trunc(")
		switch {
		case hourly && def.BucketWidth != time.Hour:
			t.Errorf("%s buckets by hour but BucketWidth = %v", def.Name, def.BucketWidth)
		case buckets && !hourly:
			t.Errorf("%s buckets with a unit BucketWidth does not yet cover", def.Name)
		case !buckets && def.BucketWidth != 0:
			t.Errorf("%s reads raw samples but BucketWidth = %v", def.Name, def.BucketWidth)
		}
	}
}

// TestBucketStart pins the bucket start for raw and hourly definitions,
// including a time given in a zone other than UTC.
func TestBucketStart(t *testing.T) {
	plus530 := time.FixedZone("plus0530", 5*3600+1800)
	tests := []struct {
		name  string
		width time.Duration
		in    time.Time
		want  time.Time
	}{
		{"raw keeps the time", 0, time.Date(2026, 3, 1, 10, 30, 15, 0, time.UTC),
			time.Date(2026, 3, 1, 10, 30, 15, 0, time.UTC)},
		{"hourly mid-hour", time.Hour, time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC),
			time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
		{"hourly on the hour", time.Hour, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
		// 16:00 at +05:30 is 10:30 UTC, whose UTC hour starts at 10:00;
		// a local truncation would give 09:30 UTC instead.
		{"hourly in a half-hour zone", time.Hour, time.Date(2026, 3, 1, 16, 0, 0, 0, plus530),
			time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MetricDef{BucketWidth: tt.width}.BucketStart(tt.in)
			if !got.Equal(tt.want) || got.Location() != time.UTC {
				t.Errorf("BucketStart(%v) = %v, want %v in UTC", tt.in, got, tt.want)
			}
		})
	}
}
