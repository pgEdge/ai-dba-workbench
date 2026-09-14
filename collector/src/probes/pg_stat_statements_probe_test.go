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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
		if _, cleanupErr := conn.Exec(ctx, `
			DO $$
			BEGIN
				BEGIN
					CREATE EXTENSION IF NOT EXISTS
						pg_stat_statements;
				EXCEPTION WHEN OTHERS THEN
					NULL;
				END;
			END$$`); cleanupErr != nil {
			t.Logf("restore pg_stat_statements: %v", cleanupErr)
		}
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
	requirePgStatStatementsReadable(t, conn)

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

	// Use a connection_id and database_name that are unique to this
	// test so the row-count assertions below are not affected by
	// other tests sharing the integration database.
	const (
		testConnID   = 9911
		testDatabase = "stmts_dedup_testdb"
	)

	// Build three rows: one with NULL queryid (should be skipped), two
	// with the same uniqueness key (one should be deduped).
	common := map[string]any{
		"_database_name":      testDatabase,
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
	if err := p.Store(ctx, conn, testConnID, time.Now().UTC(),
		rows); err != nil {
		t.Fatalf("Store synthetic: %v", err)
	}

	// After the first Store call we should have exactly two rows
	// (queryid=99 and queryid=100) for our private connection_id;
	// the nil-queryid row was skipped and the duplicate queryid=99
	// was deduped.
	var total, q99, q100 int64
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE queryid = 99),
			COUNT(*) FILTER (WHERE queryid = 100)
		FROM metrics.pg_stat_statements
		WHERE connection_id = $1 AND database_name = $2
	`, testConnID, testDatabase).Scan(&total, &q99, &q100); err != nil {
		t.Fatalf("count rows after first Store: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 rows after first Store, got %d", total)
	}
	if q99 != 1 {
		t.Errorf("expected exactly 1 row for queryid=99, got %d",
			q99)
	}
	if q100 != 1 {
		t.Errorf("expected exactly 1 row for queryid=100, got %d",
			q100)
	}

	// Calling Store with only NULL queryid rows must short-circuit
	// without attempting to write a partition row.
	allNil := []map[string]any{mkRow(nil), mkRow(nil)}
	if err := p.Store(ctx, conn, testConnID, time.Now().UTC(),
		allNil); err != nil {
		t.Fatalf("Store all-nil queryid: %v", err)
	}

	// The all-nil call must not have inserted anything; the row
	// counts should be unchanged.
	var totalAfter int64
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM metrics.pg_stat_statements
		WHERE connection_id = $1 AND database_name = $2
	`, testConnID, testDatabase).Scan(&totalAfter); err != nil {
		t.Fatalf("count rows after all-nil Store: %v", err)
	}
	if totalAfter != total {
		t.Errorf("all-nil Store inserted rows: before=%d after=%d",
			total, totalAfter)
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

	expectChecks := func(label string, wantShared, wantBlk bool) {
		t.Helper()
		gotShared, err := p.checkHasSharedBlkTime(ctx, conn)
		if err != nil {
			t.Fatalf("%s: checkHasSharedBlkTime: %v", label, err)
		}
		gotBlk, err := p.checkHasBlkReadTime(ctx, conn)
		if err != nil {
			t.Fatalf("%s: checkHasBlkReadTime: %v", label, err)
		}
		if gotShared != wantShared || gotBlk != wantBlk {
			t.Errorf("%s: shared=%t blk=%t, want shared=%t blk=%t",
				label, gotShared, gotBlk, wantShared, wantBlk)
		}
	}

	// Move the extension into a schema that is not on the default search
	// path, as a relocated install or a managed service does, restoring
	// its original schema afterwards. The checks must find the view
	// exactly when the probe's own unqualified query would, and never by
	// assuming pg_catalog (#439).
	var originalSchema string
	err := conn.QueryRow(ctx, `
        SELECT n.nspname
        FROM pg_catalog.pg_extension e
        JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
        WHERE e.extname = 'pg_stat_statements'
    `).Scan(&originalSchema)
	installed := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("look up extension schema: %v", err)
	}

	if _, err := conn.Exec(ctx, "CREATE SCHEMA pgss_relocated"); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		restore := []string{"RESET search_path"}
		if installed {
			restore = append(restore, fmt.Sprintf(
				"ALTER EXTENSION pg_stat_statements SET SCHEMA %s",
				pgx.Identifier{originalSchema}.Sanitize()))
		} else {
			restore = append(restore,
				"DROP EXTENSION IF EXISTS pg_stat_statements")
		}
		restore = append(restore,
			"DROP SCHEMA IF EXISTS pgss_relocated CASCADE")
		for _, stmt := range restore {
			if _, err := conn.Exec(ctx, stmt); err != nil {
				t.Logf("cleanup %q: %v", stmt, err)
			}
		}
	})
	relocate := "CREATE EXTENSION pg_stat_statements SCHEMA pgss_relocated"
	if installed {
		relocate = "ALTER EXTENSION pg_stat_statements SET SCHEMA pgss_relocated"
	}
	if _, err := conn.Exec(ctx, relocate); err != nil {
		t.Skipf("skipping: pg_stat_statements cannot be relocated here: %v", err)
	}

	// Off the search path the view is invisible to the probe's query, so
	// both checks are false and neither errors.
	if _, err := conn.Exec(ctx, "SET search_path TO public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	expectChecks("extension off the search path", false, false)

	if _, err := conn.Exec(ctx,
		"SET search_path TO pgss_relocated, public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	switch pgVersion := detectPgVersion(t, conn); {
	case pgVersion >= 17:
		expectChecks("relocated view on PostgreSQL 17+", true, false)
	case pgVersion >= 13:
		expectChecks("relocated view on PostgreSQL 13-16", false, true)
	default:
		expectChecks("relocated view on PostgreSQL 12", false, false)
	}
}
