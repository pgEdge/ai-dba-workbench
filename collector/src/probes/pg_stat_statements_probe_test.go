/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newPgStatStatementsProbeForTest() *PgStatStatementsProbe {
	return NewPgStatStatementsProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatStatements,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatStatementsProbe_Surface(t *testing.T) {
	p := newPgStatStatementsProbeForTest()
	if p.GetName() != ProbeNamePgStatStatements {
		t.Errorf("GetName()")
	}
	if !p.IsDatabaseScoped() {
		t.Error("pg_stat_statements is database-scoped")
	}
	if p.GetExtensionName() != "pg_stat_statements" {
		t.Errorf("GetExtensionName() = %v", p.GetExtensionName())
	}
	q := p.GetQuery()
	for _, s := range []string{"queryid", "calls", "total_exec_time",
		"pg_stat_statements"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

func TestPgStatStatementsProbe_StoreEmpty(t *testing.T) {
	p := newPgStatStatementsProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatStatementsProbe_ExecuteExtensionMissing(t *testing.T) {
	// Force the missing-extension branch by dropping pg_stat_statements
	// for the duration of this test. If the install in setup failed
	// (i.e. the server lacks shared_preload_libraries), the DROP is a
	// no-op. Restoring the extension is best-effort.
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	if _, err := conn.Exec(ctx,
		"DROP EXTENSION IF EXISTS pg_stat_statements"); err != nil {
		t.Fatalf("drop extension: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(ctx, `
			DO $$
			BEGIN
				BEGIN
					CREATE EXTENSION IF NOT EXISTS
						pg_stat_statements;
				EXCEPTION WHEN OTHERS THEN
					NULL;
				END;
			END$$`)
	})

	metrics, err := p.Execute(ctx, "stmts-noext", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute (no extension): %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("expected 0 rows when extension is missing, got %d",
			len(metrics))
	}
	// Cache path
	if _, err := p.Execute(ctx, "stmts-noext", conn,
		pgVersion); err != nil {
		t.Fatalf("Execute cached: %v", err)
	}
}

func TestPgStatStatementsProbe_ExecuteWithExtension(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// Skip if the extension query path would fail (the server lacks
	// shared_preload_libraries=pg_stat_statements).
	var ok bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_extension
			WHERE extname='pg_stat_statements')
	`).Scan(&ok); err != nil {
		t.Fatalf("check extension: %v", err)
	}
	if !ok {
		t.Skip("pg_stat_statements extension not available")
	}
	if _, err := conn.Exec(ctx,
		"SELECT count(*) FROM pg_stat_statements LIMIT 1"); err != nil {
		t.Skipf("pg_stat_statements view not queryable: %v", err)
	}

	metrics, err := p.Execute(ctx, "stmts-with-ext", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// We do not require non-empty results; we just need the path to
	// run.
	for _, m := range metrics {
		m["_database_name"] = "testdb"
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatStatementsProbe_StoreSyntheticAndDedup(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()

	// Build three rows: one with NULL queryid (should be skipped), two
	// with the same uniqueness key (one should be deduped).
	common := map[string]any{
		"_database_name":      "testdb",
		"userid":              int64(10),
		"dbid":                int64(20),
		"toplevel":            true,
		"query":               "SELECT 1",
		"calls":               int64(1),
		"total_exec_time":     1.0,
		"mean_exec_time":      1.0,
		"min_exec_time":       1.0,
		"max_exec_time":       1.0,
		"stddev_exec_time":    0.0,
		"rows":                int64(1),
		"shared_blks_hit":     int64(0),
		"shared_blks_read":    int64(0),
		"shared_blks_dirtied": int64(0),
		"shared_blks_written": int64(0),
		"local_blks_hit":      int64(0),
		"local_blks_read":     int64(0),
		"local_blks_dirtied":  int64(0),
		"local_blks_written":  int64(0),
		"temp_blks_read":      int64(0),
		"temp_blks_written":   int64(0),
	}
	mkRow := func(queryid any) map[string]any {
		row := make(map[string]any, len(common)+1)
		for k, v := range common {
			row[k] = v
		}
		row["queryid"] = queryid
		return row
	}

	rows := []map[string]any{
		mkRow(nil),        // skipped: NULL queryid
		mkRow(int64(99)),  // stored
		mkRow(int64(99)),  // duplicate: skipped
		mkRow(int64(100)), // stored
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		rows); err != nil {
		t.Fatalf("Store synthetic: %v", err)
	}

	// Calling Store with only NULL queryid rows must short-circuit
	// without attempting to write a partition row.
	allNil := []map[string]any{mkRow(nil), mkRow(nil)}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		allNil); err != nil {
		t.Fatalf("Store all-nil queryid: %v", err)
	}
}

func TestPgStatStatementsProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()

	err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		[]map[string]any{
			{"queryid": int64(1), "userid": int64(10),
				"dbid": int64(20), "toplevel": true},
		})
	if err == nil ||
		!strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("expected database_name error, got %v", err)
	}
}

func TestPgStatStatementsProbe_CheckColumnHelpers(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()

	// The helpers query information_schema and tolerate the case where
	// the view does not exist (returns false).
	if _, err := p.checkHasSharedBlkTime(ctx, conn); err != nil {
		t.Errorf("checkHasSharedBlkTime: %v", err)
	}
	if _, err := p.checkHasBlkReadTime(ctx, conn); err != nil {
		t.Errorf("checkHasBlkReadTime: %v", err)
	}
}
