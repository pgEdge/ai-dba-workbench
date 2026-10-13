/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for the list_probes tool. They run against the
// Postgres instance named by TEST_AI_WORKBENCH_SERVER, reusing the schema
// and pool helper from tools_integration_test.go, and skip when that
// variable is unset.

package tools

import (
	"context"
	"strings"
	"testing"
)

// listProbesNames returns the probe names from a list_probes TSV response,
// keyed by name with the remainder of the row as the value.
func listProbesNames(t *testing.T, text string) map[string][]string {
	t.Helper()
	rows := make(map[string][]string)
	lines := strings.Split(text, "\n")
	headerSeen := false
	for _, line := range lines {
		if line == "name\tdescription\trow_count\tscope" {
			headerSeen = true
			continue
		}
		if !headerSeen || line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Fatalf("unexpected TSV row %q", line)
		}
		rows[fields[0]] = fields[1:]
	}
	if !headerSeen {
		t.Fatalf("response has no TSV header:\n%s", text)
	}
	return rows
}

// TestListProbes_ExcludesPartitionsNotProbes is the regression test for
// issue #644: the old name filter (NOT LIKE '%_p%') hid real probes such
// as pg_stat_replication, because '_' is a LIKE wildcard, whilst listing
// the weekly partitions it was meant to hide.
func TestListProbes_ExcludesPartitionsNotProbes(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		CREATE TABLE metrics.pg_stat_replication (
		    connection_id INTEGER NOT NULL,
		    collected_at TIMESTAMPTZ NOT NULL
		);
		COMMENT ON TABLE metrics.pg_stat_replication IS 'Replication status';

		CREATE TABLE metrics.pg_stat_database (
		    connection_id INTEGER NOT NULL,
		    collected_at TIMESTAMPTZ NOT NULL
		) PARTITION BY RANGE (collected_at);
		COMMENT ON TABLE metrics.pg_stat_database IS 'Database statistics';

		CREATE TABLE metrics.pg_stat_database_20261012
		    PARTITION OF metrics.pg_stat_database
		    FOR VALUES FROM ('2026-10-12 00:00:00+00')
		    TO ('2026-10-19 00:00:00+00');

		CREATE VIEW metrics.pg_probe_view AS
		    SELECT 1 AS x;
	`)
	if err != nil {
		t.Fatalf("failed to create probe tables: %v", err)
	}

	tool := ListProbesTool(pool)
	text := mustSuccess(t, tool, map[string]any{"__context": ctx})
	probes := listProbesNames(t, text)

	want := map[string][]string{
		"pg_connectivity":     {"", "0", "server"},
		"pg_stat_replication": {"Replication status", "0", "server"},
		"pg_stat_database":    {"Database statistics", "0", "database"},
	}
	for name, fields := range want {
		got, ok := probes[name]
		if !ok {
			t.Errorf("probe %q missing from list_probes output:\n%s", name, text)
			continue
		}
		if strings.Join(got, "\t") != strings.Join(fields, "\t") {
			t.Errorf("probe %q = %q, want %q", name, got, fields)
		}
	}

	for _, name := range []string{"pg_stat_database_20261012", "pg_probe_view"} {
		if _, ok := probes[name]; ok {
			t.Errorf("%q should not be listed as a probe:\n%s", name, text)
		}
	}

	if len(probes) != len(want) {
		t.Errorf("got %d probes, want %d:\n%s", len(probes), len(want), text)
	}
	if !strings.HasPrefix(text, "Found 3 metrics probes:") {
		t.Errorf("unexpected summary line:\n%s", text)
	}
}

// TestListProbes_NoProbeTables covers the empty-schema response.
func TestListProbes_NoProbeTables(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		`DROP TABLE metrics.pg_connectivity`); err != nil {
		t.Fatalf("failed to drop probe table: %v", err)
	}

	tool := ListProbesTool(pool)
	text := mustSuccess(t, tool, map[string]any{})
	if !strings.Contains(text, "No metrics probes found") {
		t.Errorf("expected the no-probes message, got:\n%s", text)
	}
}

// TestListProbes_QueryError covers the error response when the query
// cannot run.
func TestListProbes_QueryError(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := ListProbesTool(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := tool.Handler(map[string]any{"__context": ctx})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected an error response for a canceled context")
	}
	if len(resp.Content) == 0 ||
		!strings.Contains(resp.Content[0].Text, "Failed to query metrics probes") {
		t.Errorf("unexpected error response: %+v", resp.Content)
	}
}
