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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

		CREATE TABLE metrics.pg_stat_database_20261019
		    PARTITION OF metrics.pg_stat_database
		    FOR VALUES FROM ('2026-10-19 00:00:00+00')
		    TO ('2026-10-26 00:00:00+00')
		    PARTITION BY LIST (connection_id);

		CREATE TABLE metrics.pg_stat_database_20261019_c1
		    PARTITION OF metrics.pg_stat_database_20261019
		    FOR VALUES IN (1);

		CREATE VIEW metrics.pg_probe_view AS
		    SELECT 1 AS x;

		INSERT INTO metrics.pg_stat_database VALUES
		    (1, '2026-10-13 00:00:00+00'),
		    (2, '2026-10-14 00:00:00+00'),
		    (3, '2026-10-15 00:00:00+00'),
		    (1, '2026-10-20 00:00:00+00'),
		    (1, '2026-10-21 00:00:00+00');
		INSERT INTO metrics.pg_stat_replication VALUES
		    (1, '2026-10-13 00:00:00+00'),
		    (1, '2026-10-14 00:00:00+00');
	`)
	if err != nil {
		t.Fatalf("failed to create probe tables: %v", err)
	}
	// The seeding backend reports its inserts to the statistics system
	// some time after they commit, adding them to n_live_tup. Wait until
	// they have all been reported before running ANALYZE, so that none
	// can be added on top of the count ANALYZE sets.
	leaves := map[string]int64{
		"pg_stat_database_20261012":    3,
		"pg_stat_database_20261019_c1": 2,
		"pg_stat_replication":          2,
	}
	flushDeadline := time.Now().Add(30 * time.Second)
	for name, want := range leaves {
		for {
			var inserted int64
			if err := pool.QueryRow(ctx, `
				SELECT COALESCE(n_tup_ins, 0) FROM pg_stat_user_tables
				WHERE schemaname = 'metrics' AND relname = $1`,
				name).Scan(&inserted); err != nil {
				t.Fatalf("failed to read statistics for %s: %v", name, err)
			}
			if inserted == want {
				break
			}
			if time.Now().After(flushDeadline) {
				t.Fatalf("inserts into %s not reported: n_tup_ins = %d, want %d",
					name, inserted, want)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	// ANALYZE sets n_live_tup for each leaf partition and plain table to
	// its row count, which is exact for tables this small.
	if _, err := pool.Exec(ctx, `ANALYZE metrics.pg_stat_database_20261012,
		metrics.pg_stat_database_20261019_c1, metrics.pg_stat_replication`); err != nil {
		t.Fatalf("failed to analyze probe tables: %v", err)
	}

	tool := ListProbesTool(pool)
	var text string
	var probes map[string][]string
	// From PostgreSQL 15 ANALYZE writes its counts straight into shared
	// statistics memory, so the first call sees them; on PostgreSQL 14
	// they travel through the statistics collector asynchronously, so
	// allow a few seconds for them to arrive.
	deadline := time.Now().Add(10 * time.Second)
	for {
		text = mustSuccess(t, tool, map[string]any{"__context": ctx})
		probes = listProbesNames(t, text)
		db, repl := probes["pg_stat_database"], probes["pg_stat_replication"]
		statsArrived := len(db) == 3 && db[1] == "5" &&
			len(repl) == 3 && repl[1] == "2"
		if statsArrived || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The parent's count is the sum over both levels of its partition
	// tree (three rows in one leaf, two in a nested leaf).
	want := map[string][]string{
		"pg_connectivity":     {"", "0", "server"},
		"pg_stat_replication": {"Replication status", "2", "server"},
		"pg_stat_database":    {"Database statistics", "5", "database"},
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

	for _, name := range []string{
		"pg_stat_database_20261012",
		"pg_stat_database_20261019",
		"pg_stat_database_20261019_c1",
		"pg_probe_view",
	} {
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

// TestListProbes_OmitsUnreadableTables checks that a table the datastore
// role cannot SELECT from is left out of the list.
func TestListProbes_OmitsUnreadableTables(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	ctx := context.Background()
	// Roles are cluster-wide, so give this one a unique name in case
	// another test run shares the instance.
	role := fmt.Sprintf("list_probes_reader_%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE ROLE %[1]s NOLOGIN;
		GRANT USAGE ON SCHEMA metrics TO %[1]s;
		GRANT SELECT ON metrics.pg_connectivity TO %[1]s;
		CREATE TABLE metrics.pg_restricted (
		    connection_id INTEGER NOT NULL,
		    collected_at TIMESTAMPTZ NOT NULL
		);`, role)); err != nil {
		t.Fatalf("failed to set up the restricted role: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), fmt.Sprintf(`
			DROP TABLE IF EXISTS metrics.pg_restricted;
			REVOKE ALL ON metrics.pg_connectivity FROM %[1]s;
			REVOKE ALL ON SCHEMA metrics FROM %[1]s;
			DROP ROLE IF EXISTS %[1]s;`, role)); err != nil {
			t.Logf("failed to drop the restricted role: %v", err)
		}
	}()

	cfg := pool.Config()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+role)
		return err
	}
	restricted, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create the restricted pool: %v", err)
	}
	defer restricted.Close()

	// The superuser pool sees both tables.
	all := listProbesNames(t, mustSuccess(t, ListProbesTool(pool),
		map[string]any{"__context": ctx}))
	if _, ok := all["pg_restricted"]; !ok {
		t.Fatalf("pg_restricted missing for a role that can read it: %v", all)
	}

	probes := listProbesNames(t, mustSuccess(t, ListProbesTool(restricted),
		map[string]any{"__context": ctx}))
	if _, ok := probes["pg_restricted"]; ok {
		t.Errorf("pg_restricted listed for a role that cannot read it")
	}
	if _, ok := probes["pg_connectivity"]; !ok {
		t.Errorf("pg_connectivity missing for a role that can read it: %v", probes)
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
