/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package metrics

import (
	"strings"
	"testing"
	"time"
)

// mountFilterStart and mountFilterEnd bound every window used below; the
// exact instants do not matter because the assertions are about clause text
// and argument positions rather than about the data a query would return.
var (
	mountFilterStart = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mountFilterEnd   = time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
)

// TestMetricQueryClausesMountPoint covers the mount_point dimension added so
// that per-mount probes such as pg_sys_disk_info can be scoped to one real
// filesystem instead of averaged across every mount.
func TestMetricQueryClausesMountPoint(t *testing.T) {
	t.Run("binds mount_point after the fixed args", func(t *testing.T) {
		parts := metricQueryClauses("pg_sys_disk_info", 1,
			mountFilterStart, mountFilterEnd, time.Minute,
			MetricFilters{MountPoint: "/var/lib/postgresql"})

		if got := parts.where(); !strings.Contains(got, "mount_point = $5") {
			t.Errorf("where() missing mount_point clause: %q", got)
		}
		if len(parts.args) != 5 {
			t.Fatalf("expected 5 args, got %d: %v", len(parts.args), parts.args)
		}
		if parts.args[4] != "/var/lib/postgresql" {
			t.Errorf("expected mount_point arg %q, got %v",
				"/var/lib/postgresql", parts.args[4])
		}
	})

	t.Run("omitted leaves the SQL and args unchanged", func(t *testing.T) {
		// A probe without a mount_point column must produce byte-identical
		// SQL to the pre-change builder, so charts that never send the
		// parameter keep their existing plans.
		without := metricQueryClauses("pg_stat_all_tables", 1,
			mountFilterStart, mountFilterEnd, time.Minute,
			MetricFilters{SchemaName: "public"})
		withEmpty := metricQueryClauses("pg_stat_all_tables", 1,
			mountFilterStart, mountFilterEnd, time.Minute,
			MetricFilters{SchemaName: "public", MountPoint: ""})

		if without.where() != withEmpty.where() {
			t.Errorf("empty MountPoint changed SQL:\n%s\n---\n%s",
				without.where(), withEmpty.where())
		}
		if strings.Contains(withEmpty.where(), "mount_point") {
			t.Error("empty MountPoint must not add a mount_point clause")
		}
		if len(without.args) != len(withEmpty.args) {
			t.Errorf("empty MountPoint changed arg count: %d vs %d",
				len(without.args), len(withEmpty.args))
		}
	})

	t.Run("sits between index_name and queryid", func(t *testing.T) {
		// The ordering is load bearing: mount_point must increment argNum so
		// that the queryid clause, which deliberately does not, still lands
		// on the last position. Binding every dimension at once proves the
		// placeholder run has no gap or collision.
		queryID := int64(42)
		parts := metricQueryClauses("pg_stat_all_indexes", 1,
			mountFilterStart, mountFilterEnd, time.Minute,
			MetricFilters{
				DatabaseName:   "northwind",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
				TableName:      "orders",
				IndexName:      "orders_pkey",
				MountPoint:     "/srv/data",
				QueryID:        &queryID,
			})

		got := parts.where()
		for _, want := range []string{
			`"database_name" = $5`,
			"schemaname = $6",
			"relname = $7",
			"indexrelname = $8",
			"mount_point = $9",
			"queryid = $10",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("where() missing %q: %q", want, got)
			}
		}
		if len(parts.args) != 10 {
			t.Fatalf("expected 10 args, got %d: %v", len(parts.args), parts.args)
		}
		if parts.args[8] != "/srv/data" {
			t.Errorf("expected mount_point at arg 9, got %v", parts.args[8])
		}
		if parts.args[9] != queryID {
			t.Errorf("expected queryid at arg 10, got %v", parts.args[9])
		}
	})

	t.Run("reaches the lookback clause", func(t *testing.T) {
		// Rate and delta queries pull a predecessor sample from before the
		// window start through lookbackWhere; without the filter there the
		// predecessor could come from a different mount and the first
		// bucket's delta would be meaningless.
		parts := metricQueryClauses("pg_sys_disk_info", 1,
			mountFilterStart, mountFilterEnd, time.Minute,
			MetricFilters{MountPoint: "/srv/data"})

		got := parts.lookbackWhere("pg_sys_disk_info")
		if !strings.Contains(got, "mount_point = $5") {
			t.Errorf("lookbackWhere missing mount_point clause: %q", got)
		}
	})
}

// TestBuildMetricsQueryMountPoint exercises the mount filter through the
// public bucketed builder, which is the path the Disk Space chart uses.
func TestBuildMetricsQueryMountPoint(t *testing.T) {
	query, args, err := BuildMetricsQuery(
		"pg_sys_disk_info",
		[]string{"used_space", "free_space"},
		map[string]string{"used_space": "bigint", "free_space": "bigint"},
		1, mountFilterStart, mountFilterEnd, 60, "avg",
		MetricFilters{MountPoint: "/"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(query, "mount_point = $5") {
		t.Errorf("query should filter by mount_point, got:\n%s", query)
	}
	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(args), args)
	}
	if args[4] != "/" {
		t.Errorf("expected mount_point arg %q, got %v", "/", args[4])
	}
}

// TestBuildDerivedMetricsQueryMountPoint proves the derived path, which
// appends one more argument after the filter values, still numbers that
// trailing argument correctly with the mount filter present.
func TestBuildDerivedMetricsQueryMountPoint(t *testing.T) {
	query, args, err := BuildDerivedMetricsQuery(
		"pg_sys_disk_info",
		[]DerivedMetric{{
			OutputName: "used_space_delta",
			BaseColumn: "used_space",
			Kind:       DerivedDelta,
		}},
		nil, nil, 1, mountFilterStart, mountFilterEnd, 60, "avg",
		MetricFilters{MountPoint: "/srv/data"}, testMaxElapsed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(query, "mount_point = $5") {
		t.Errorf("query should filter by mount_point, got:\n%s", query)
	}
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}
	if args[4] != "/srv/data" {
		t.Errorf("expected mount_point arg %q, got %v", "/srv/data", args[4])
	}
}

// TestBuildLatestRowsQueryMountPoint covers the latest-row path, which
// builds its own WHERE clause rather than sharing metricQueryClauses, so the
// filter has to be added in both places or the mount selector would scope
// the chart but not the KPI tile behind it.
func TestBuildLatestRowsQueryMountPoint(t *testing.T) {
	t.Run("binds mount_point after the connection ids", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			"pg_sys_disk_info",
			[]string{"mount_point", "used_space"},
			map[string]string{"mount_point": "text", "used_space": "bigint"},
			[]int{4},
			MetricFilters{MountPoint: "/srv/data"},
			"collected_at", "desc", 10,
		)

		if !strings.Contains(query, "mount_point = $2") {
			t.Errorf("query should filter by mount_point, got:\n%s", query)
		}
		if len(args) != 3 {
			t.Fatalf("expected 3 args, got %d: %v", len(args), args)
		}
		if args[1] != "/srv/data" {
			t.Errorf("expected mount_point arg %q, got %v", "/srv/data", args[1])
		}
		if args[2] != 10 {
			t.Errorf("expected the limit to stay last, got %v", args[2])
		}
	})

	t.Run("orders before queryid alongside every other filter", func(t *testing.T) {
		queryID := int64(7)
		query, args := buildLatestRowsQuery(
			"pg_stat_all_indexes",
			[]string{"indexrelname", "idx_scan"},
			map[string]string{"indexrelname": "name", "idx_scan": "bigint"},
			[]int{2, 3},
			MetricFilters{
				DatabaseName:   "northwind",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
				TableName:      "orders",
				IndexName:      "orders_pkey",
				MountPoint:     "/srv/data",
				QueryID:        &queryID,
			},
			"idx_scan", "asc", 5,
		)

		for _, want := range []string{
			"connection_id IN ($1, $2)",
			`"database_name" = $3`,
			"schemaname = $4",
			"relname = $5",
			"indexrelname = $6",
			"mount_point = $7",
			"queryid = $8",
			"LIMIT $9",
		} {
			if !strings.Contains(query, want) {
				t.Errorf("query missing %q, got:\n%s", want, query)
			}
		}
		if len(args) != 9 {
			t.Fatalf("expected 9 args, got %d: %v", len(args), args)
		}
		if args[6] != "/srv/data" {
			t.Errorf("expected mount_point at arg 7, got %v", args[6])
		}
	})

	t.Run("omitted adds no clause", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			"pg_stat_all_tables",
			[]string{"relname", "seq_scan"},
			map[string]string{"relname": "name", "seq_scan": "bigint"},
			[]int{1},
			MetricFilters{MountPoint: ""},
			"collected_at", "desc", 1,
		)
		if strings.Contains(query, "mount_point = $") {
			t.Errorf("empty MountPoint must not add a clause, got:\n%s", query)
		}
		if len(args) != 2 {
			t.Errorf("expected 2 args, got %d: %v", len(args), args)
		}
	})
}
