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
		"pg_stat_statements", "NULL::timestamptz AS stats_reset"} {
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
	// Every row must carry stats_reset, and on a server whose extension
	// exposes pg_stat_statements_info it must be a real timestamp.
	hasInfo, err := p.checkHasStatsInfoView(ctx, conn)
	if err != nil {
		t.Fatalf("checkHasStatsInfoView: %v", err)
	}
	for _, m := range metrics {
		v, present := m["stats_reset"]
		if !present {
			t.Fatal("stats_reset column missing from Execute result")
		}
		if hasInfo {
			if _, isTime := v.(time.Time); !isTime {
				t.Errorf("stats_reset = %T (%v), want time.Time", v, v)
			}
		} else if v != nil {
			t.Errorf("stats_reset = %v, want NULL without the info view", v)
		}
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

// TestPgStatStatementsProbe_ExecuteWithoutStatsInfoView drives the
// NULL::timestamptz branch by seeding the feature cache to say the
// pg_stat_statements_info view is absent, as it is on pg_stat_statements
// before 1.9, so the query shape for older extensions is exercised on a
// modern server.
func TestPgStatStatementsProbe_ExecuteWithoutStatsInfoView(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)
	requirePgStatStatementsReadable(t, conn)

	const connName = "stmts-no-info-view"
	key := featureCacheKey{connectionName: connName,
		checkName: "pg_stat_statements_info_view"}
	featureCache.Store(key, false)
	defer featureCache.Delete(key)

	metrics, err := p.Execute(ctx, connName, conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected at least one pg_stat_statements row")
	}
	for _, m := range metrics {
		v, present := m["stats_reset"]
		if !present {
			t.Fatal("stats_reset column missing from Execute result")
		}
		if v != nil {
			t.Errorf("stats_reset = %v, want NULL when the info view is absent", v)
		}
	}
}

// TestPgStatStatementsProbe_ExecuteTimingColumnVariants drives the two
// query shapes selected by the timing-column checks (PostgreSQL 17+ and
// 13-16) by seeding the feature cache, so every variant is shown to carry
// the stats_reset column. On a server that lacks the old blk_read_time
// column the 13-16 shape fails at execution; the test then only requires
// that the failure is the missing column rather than a malformed query.
func TestPgStatStatementsProbe_ExecuteTimingColumnVariants(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)
	requirePgStatStatementsReadable(t, conn)

	cases := []struct {
		name          string
		sharedBlkTime bool
		blkReadTime   bool
		missingColumn string
	}{
		{"pg17_shared_blk_time", true, false, "shared_blk_read_time"},
		{"pg13_16_blk_read_time", false, true, "blk_read_time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connName := "stmts-variant-" + tc.name
			seed := map[string]bool{
				"pg_stat_statements_ext":             true,
				"pg_stat_statements_shared_blk_time": tc.sharedBlkTime,
				"pg_stat_statements_blk_read_time":   tc.blkReadTime,
			}
			for check, val := range seed {
				key := featureCacheKey{connectionName: connName, checkName: check}
				featureCache.Store(key, val)
				defer featureCache.Delete(key)
			}
			infoKey := featureCacheKey{connectionName: connName,
				checkName: "pg_stat_statements_info_view"}
			defer featureCache.Delete(infoKey)

			metrics, err := p.Execute(ctx, connName, conn, pgVersion)
			if err != nil {
				if !strings.Contains(err.Error(), tc.missingColumn) {
					t.Fatalf("Execute: %v", err)
				}
				t.Logf("server lacks %s; query shape still exercised",
					tc.missingColumn)
				return
			}
			for _, m := range metrics {
				if _, present := m["stats_reset"]; !present {
					t.Fatal("stats_reset column missing from Execute result")
				}
			}
		})
	}
}

func TestStatsResetSelect(t *testing.T) {
	if got := statsResetSelect(true); !strings.Contains(got,
		"FROM pg_stat_statements_info") ||
		!strings.HasSuffix(got, "AS stats_reset") {
		t.Errorf("statsResetSelect(true) = %q", got)
	}
	if got := statsResetSelect(false); got != "NULL::timestamptz AS stats_reset" {
		t.Errorf("statsResetSelect(false) = %q", got)
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
	statsReset := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
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
		"stats_reset":         statsReset,
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

	// stats_reset must round-trip through Store unchanged.
	var storedReset time.Time
	if err := conn.QueryRow(ctx, `
		SELECT stats_reset FROM metrics.pg_stat_statements
		WHERE connection_id = $1 AND database_name = $2 AND queryid = 99
	`, testConnID, testDatabase).Scan(&storedReset); err != nil {
		t.Fatalf("read stats_reset: %v", err)
	}
	if !storedReset.Equal(statsReset) {
		t.Errorf("stats_reset = %v, want %v", storedReset, statsReset)
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

	// The helpers query information_schema and tolerate the case where
	// the view does not exist (returns false).
	if _, err := p.checkHasSharedBlkTime(ctx, conn); err != nil {
		t.Errorf("checkHasSharedBlkTime: %v", err)
	}
	if _, err := p.checkHasBlkReadTime(ctx, conn); err != nil {
		t.Errorf("checkHasBlkReadTime: %v", err)
	}

	// The info-view check must agree with the catalog: the view exists
	// exactly when the installed extension is 1.9 or later.
	hasInfo, err := p.checkHasStatsInfoView(ctx, conn)
	if err != nil {
		t.Fatalf("checkHasStatsInfoView: %v", err)
	}
	var want bool
	if err := conn.QueryRow(ctx,
		`SELECT to_regclass('pg_stat_statements_info') IS NOT NULL`,
	).Scan(&want); err != nil {
		t.Fatalf("to_regclass: %v", err)
	}
	if hasInfo != want {
		t.Errorf("checkHasStatsInfoView = %v, catalog says %v", hasInfo, want)
	}
}
