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
	"github.com/jackc/pgx/v5/pgxpool"
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
	scope := featureCacheScope(connName, conn.Conn().Config().Database)
	key := featureCacheKey{connectionName: scope,
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
			scope := featureCacheScope(connName,
				conn.Conn().Config().Database)
			seed := map[string]bool{
				"pg_stat_statements_ext":             true,
				"pg_stat_statements_shared_blk_time": tc.sharedBlkTime,
				"pg_stat_statements_blk_read_time":   tc.blkReadTime,
			}
			for check, val := range seed {
				key := featureCacheKey{connectionName: scope, checkName: check}
				featureCache.Store(key, val)
				defer featureCache.Delete(key)
			}
			infoKey := featureCacheKey{connectionName: scope,
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
			//nosemgrep: go_sql_rule-concat-sqli -- fixed cleanup DDL; the only interpolated value is the extension's original schema name from pg_namespace, sanitized by pgx.Identifier
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
	// The column set follows the installed extension version, not the
	// server's: 1.11 (PostgreSQL 17) renamed blk_read_time to
	// shared_blk_read_time, and 1.8 (PostgreSQL 13) introduced toplevel
	// alongside the blk_read_time column the probe looks for.
	var extVersion string
	if err := conn.QueryRow(ctx, `
        SELECT extversion FROM pg_catalog.pg_extension
        WHERE extname = 'pg_stat_statements'
    `).Scan(&extVersion); err != nil {
		t.Fatalf("read extension version: %v", err)
	}
	switch {
	case extensionVersionAtLeast(extVersion, 1, 11):
		expectChecks("relocated view, extension "+extVersion, true, false)
	case extensionVersionAtLeast(extVersion, 1, 8):
		expectChecks("relocated view, extension "+extVersion, false, true)
	default:
		expectChecks("relocated view, extension "+extVersion, false, false)
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

// extensionVersionAtLeast reports whether a pg_extension.extversion string
// such as "1.11" is at least major.minor.
func extensionVersionAtLeast(version string, major, minor int) bool {
	var gotMajor, gotMinor int
	if _, err := fmt.Sscanf(version, "%d.%d", &gotMajor, &gotMinor); err != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

// newSecondaryDatabase creates an extra database on the integration
// server and returns a pool to it, dropping it when the test ends. It
// exists so a test can drive two pools that differ only in database,
// which is the shape the scheduler creates for a connection that
// monitors more than one database.
//
// Administrative work goes through a single short-lived connection and
// the returned pool is capped at one connection, because the shared
// integration server is close to max_connections once every package has
// opened its pools.
func newSecondaryDatabase(t *testing.T, suffix string) *pgxpool.Pool {
	t.Helper()

	base, ok := integrationConnString()
	if !ok {
		t.Skip("TEST_AI_WORKBENCH_SERVER (or TEST_DB_CONN) not set; " +
			"skipping integration test")
	}

	ctx := context.Background()
	adminConnStr := replaceProbeDatabase(base, "postgres")
	dbName := fmt.Sprintf("ai_workbench_probes_%s_%d", suffix,
		time.Now().UnixNano())

	// dbName is generated above from a fixed prefix and a timestamp, so
	// the statement carries no caller-supplied text.
	// Parsing through pgxpool drops any pool_* parameters the test DSN
	// carries, which a plain pgx.Connect would reject.
	adminCfg, err := pgxpool.ParseConfig(adminConnStr)
	if err != nil {
		t.Fatalf("parse admin config: %v", err)
	}
	adminExec := func(sql string) error {
		adminConn, err := pgx.ConnectConfig(ctx, adminCfg.ConnConfig)
		if err != nil {
			return err
		}
		defer func() { _ = adminConn.Close(ctx) }()
		_, err = adminConn.Exec(ctx, sql)
		return err
	}

	if err := adminExec(
		fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		t.Fatalf("create secondary db: %v", err)
	}
	t.Cleanup(func() {
		if err := adminExec(
			fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)); err != nil {
			t.Logf("drop secondary db %s: %v", dbName, err)
		}
	})

	cfg, parseErr := pgxpool.ParseConfig(replaceProbeDatabase(base, dbName))
	if parseErr != nil {
		t.Fatalf("parse secondary db config: %v", parseErr)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to secondary db: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestPgStatStatementsProbe_FeatureCacheIsPerDatabase drives Execute
// through two pools that share one connection name and differ only in
// database, which is exactly what the scheduler builds for a connection
// monitoring several databases. Every cached check must be keyed by
// database as well as connection, so a connection-only key on any one
// of them is caught here: the first database's answer would decide the
// query shape for the second.
func TestPgStatStatementsProbe_FeatureCacheIsPerDatabase(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	requirePgStatStatementsReadable(t, conn)

	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	otherPool := newSecondaryDatabase(t, "xdb")
	otherConn := acquireConn(t, otherPool)

	firstDB := conn.Conn().Config().Database
	secondDB := otherConn.Conn().Config().Database
	if firstDB == secondDB {
		t.Fatalf("the two pools must differ by database, both are %q",
			firstDB)
	}

	// The extension check: present in the first database and absent in
	// the second, so a shared answer would have the second database run
	// the pg_stat_statements query it cannot satisfy.
	t.Run("extension availability", func(t *testing.T) {
		const connName = "stmts-xdb-availability"
		for _, db := range []string{firstDB, secondDB} {
			key := featureCacheKey{
				connectionName: featureCacheScope(connName, db),
				checkName:      "pg_stat_statements_ext",
			}
			defer featureCache.Delete(key)
		}

		if _, err := p.Execute(ctx, connName, conn, pgVersion); err != nil {
			t.Fatalf("Execute against %s: %v", firstDB, err)
		}
		metrics, err := p.Execute(ctx, connName, otherConn, pgVersion)
		if err != nil {
			t.Fatalf("Execute against %s (no extension): %v", secondDB, err)
		}
		if len(metrics) != 0 {
			t.Errorf("Execute against %s returned %d rows; the first "+
				"database's cached extension check decided for it",
				secondDB, len(metrics))
		}
	})

	// The pg_stat_statements_info check, which #457 added and which must
	// be scoped like the rest. Seeding "view absent" for the first
	// database must not suppress stats_reset in the second.
	t.Run("stats info view", func(t *testing.T) {
		if _, err := otherConn.Exec(ctx,
			"CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
			t.Skipf("cannot install pg_stat_statements in %s: %v",
				secondDB, err)
		}
		requirePgStatStatementsReadable(t, otherConn)

		hasInfo, err := p.checkHasStatsInfoView(ctx, otherConn)
		if err != nil {
			t.Fatalf("checkHasStatsInfoView: %v", err)
		}
		if !hasInfo {
			t.Skip("this server's pg_stat_statements has no info view")
		}

		const connName = "stmts-xdb-info-view"
		for _, db := range []string{firstDB, secondDB} {
			scope := featureCacheScope(connName, db)
			for _, check := range []string{
				"pg_stat_statements_ext",
				"pg_stat_statements_shared_blk_time",
				"pg_stat_statements_blk_read_time",
				"pg_stat_statements_info_view",
			} {
				defer featureCache.Delete(featureCacheKey{
					connectionName: scope, checkName: check})
			}
		}

		// Say the info view is absent for the first database only.
		featureCache.Store(featureCacheKey{
			connectionName: featureCacheScope(connName, firstDB),
			checkName:      "pg_stat_statements_info_view",
		}, false)

		first, err := p.Execute(ctx, connName, conn, pgVersion)
		if err != nil {
			t.Fatalf("Execute against %s: %v", firstDB, err)
		}
		if len(first) == 0 {
			t.Fatalf("expected rows from %s", firstDB)
		}
		for _, m := range first {
			if m["stats_reset"] != nil {
				t.Fatalf("stats_reset = %v in %s, want NULL from the "+
					"seeded check", m["stats_reset"], firstDB)
			}
		}

		second, err := p.Execute(ctx, connName, otherConn, pgVersion)
		if err != nil {
			t.Fatalf("Execute against %s: %v", secondDB, err)
		}
		if len(second) == 0 {
			t.Fatalf("expected rows from %s", secondDB)
		}
		for _, m := range second {
			if _, isTime := m["stats_reset"].(time.Time); !isTime {
				t.Fatalf("stats_reset = %v (%T) in %s; the first "+
					"database's seeded info-view answer leaked across "+
					"the connection name", m["stats_reset"],
					m["stats_reset"], secondDB)
			}
		}

		// Each database must have left its own info-view answer behind:
		// a connection-only key would have stored just the one.
		for db, want := range map[string]bool{firstDB: false, secondDB: true} {
			got, ok := featureCache.Load(featureCacheKey{
				connectionName: featureCacheScope(connName, db),
				checkName:      "pg_stat_statements_info_view",
			})
			if !ok {
				t.Errorf("no cached info-view answer for %s; the check "+
					"is not keyed by database", db)
				continue
			}
			if got != want {
				t.Errorf("cached info-view answer for %s = %v, want %v",
					db, got, want)
			}
		}
	})
}

// TestPgStatStatementsProbe_ExecutePreThirteenFallback drives the query
// shape used for PostgreSQL 12 and earlier, which has neither timing
// column. The shared_blk_read_time answer is seeded false and the
// blk_read_time check is left to run for real, so on a modern server,
// where that column is also absent, Execute takes the oldest branch.
func TestPgStatStatementsProbe_ExecutePreThirteenFallback(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)
	requirePgStatStatementsReadable(t, conn)

	const connName = "stmts-pre-13"
	scope := featureCacheScope(connName, conn.Conn().Config().Database)
	for _, check := range []string{
		"pg_stat_statements_ext",
		"pg_stat_statements_shared_blk_time",
		"pg_stat_statements_blk_read_time",
		"pg_stat_statements_info_view",
	} {
		defer featureCache.Delete(featureCacheKey{
			connectionName: scope, checkName: check})
	}
	featureCache.Store(featureCacheKey{connectionName: scope,
		checkName: "pg_stat_statements_shared_blk_time"}, false)

	if hasOld, err := p.checkHasBlkReadTime(ctx, conn); err != nil {
		t.Fatalf("checkHasBlkReadTime: %v", err)
	} else if hasOld {
		t.Skip("this server still has blk_read_time; the pre-13 shape " +
			"is unreachable here")
	}

	metrics, err := p.Execute(ctx, connName, conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected at least one pg_stat_statements row")
	}
	for _, m := range metrics {
		if m["toplevel"] != true {
			t.Errorf("toplevel = %v, want true in the pre-13 shape",
				m["toplevel"])
		}
		if m["shared_blk_read_time"] != nil {
			t.Errorf("shared_blk_read_time = %v, want NULL in the "+
				"pre-13 shape", m["shared_blk_read_time"])
		}
	}
}

// TestPgStatStatementsProbe_ExecuteCachedCheckErrors shows that a failure
// from any of the cached feature checks aborts Execute rather than
// falling through to a query chosen on a bad answer. A non-boolean cache
// entry is the one failure that can be provoked deterministically,
// cachedCheck rejecting it for every check in turn.
func TestPgStatStatementsProbe_ExecuteCachedCheckErrors(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatStatementsProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)
	requirePgStatStatementsReadable(t, conn)

	database := conn.Conn().Config().Database
	cases := []struct {
		name  string
		check string
		// seed holds the boolean answers the earlier checks need for
		// Execute to reach the poisoned one.
		seed map[string]bool
	}{
		{"extension", "pg_stat_statements_ext", nil},
		{"shared_blk_time", "pg_stat_statements_shared_blk_time", nil},
		{"blk_read_time", "pg_stat_statements_blk_read_time",
			map[string]bool{"pg_stat_statements_shared_blk_time": false}},
		{"info_view", "pg_stat_statements_info_view",
			map[string]bool{"pg_stat_statements_shared_blk_time": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connName := "stmts-bad-cache-" + tc.name
			scope := featureCacheScope(connName, database)
			for _, check := range []string{
				"pg_stat_statements_ext",
				"pg_stat_statements_shared_blk_time",
				"pg_stat_statements_blk_read_time",
				"pg_stat_statements_info_view",
			} {
				defer featureCache.Delete(featureCacheKey{
					connectionName: scope, checkName: check})
			}
			featureCache.Store(featureCacheKey{connectionName: scope,
				checkName: "pg_stat_statements_ext"}, true)
			for check, val := range tc.seed {
				featureCache.Store(featureCacheKey{
					connectionName: scope, checkName: check}, val)
			}
			featureCache.Store(featureCacheKey{connectionName: scope,
				checkName: tc.check}, "not a bool")

			metrics, err := p.Execute(ctx, connName, conn, pgVersion)
			if err == nil {
				t.Fatalf("Execute returned %d rows, want an error from "+
					"the %s check", len(metrics), tc.check)
			}
			if !strings.Contains(err.Error(), tc.check) {
				t.Errorf("Execute error = %v, want it to name %s", err,
					tc.check)
			}
		})
	}
}
