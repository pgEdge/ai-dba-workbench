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
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These integration tests exercise the DB-executing latest-row query
// functions against a real PostgreSQL instance. They follow the gating
// convention used by the api package: they connect to the instance named
// by TEST_AI_WORKBENCH_SERVER, skip cleanly when it is unset or when
// SKIP_DB_TESTS is set, and skip on any connection or ping failure. The
// Server CI jobs set TEST_AI_WORKBENCH_SERVER, so these run in CI and are
// skipped locally when no database is available.

const (
	latestRowsTestProbe    = "pg_stat_all_tables_latest_test"
	latestRowsInternalOnly = "internal_only_latest_test"
	latestRowsNoEntityKey  = "pg_sys_metric_latest_test"
)

// newLatestRowsTestPool connects to the test database named by
// TEST_AI_WORKBENCH_SERVER. It skips the calling test when the env var is
// missing, SKIP_DB_TESTS is set, or the connection cannot be established.
// The returned cleanup closes the pool.
func newLatestRowsTestPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping metrics query integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	return pool, pool.Close
}

// fixtureRow describes one row inserted into the latest-rows fixture table.
type fixtureRow struct {
	collectedAt time.Time
	dbName      string
	schema      string
	relname     string
	nLiveTup    int64
	nDeadTup    int64
	seqScan     int64
	someRatio   float64
	lastVacuum  any
	replayLag   any
	nanMetric   float64
}

// setupLatestRowsFixture creates the metrics schema (if absent) and a probe
// table with a representative column mix: bookkeeping columns, dimension
// columns, numeric metrics, a timestamp column, an interval column, a
// numeric column, and a double-precision column able to hold NaN. It
// inserts connection-1 rows for three distinct entities and returns a
// cleanup that drops the table.
//
// The public.orders entity has three samples and models the stale-latest-row
// regression: its most recent sample (200) is numerically smaller than an
// older sample (300), so any query that ranks by the historical maximum of
// n_live_tup would wrongly surface the older 300 row as "latest".
//
//	A: orders    now-2m, n_live_tup 100, last_vacuum set, replay_lag 1s, nan 1.0
//	B: orders    now-1m, n_live_tup 300, last_vacuum set, replay_lag NULL, nan NaN
//	C: orders    now,    n_live_tup 200, last_vacuum NULL, replay_lag NULL, nan 2.0
//	D: customers now-90s, n_live_tup 400
//	E: customers now-30s, n_live_tup 500 (customers' latest sample)
//	F: products  now-45s, n_live_tup 50  (products' only sample)
func setupLatestRowsFixture(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, latestRowsTestProbe)

	ddl := `CREATE TABLE metrics."` + latestRowsTestProbe + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        database_name text,
        schemaname    name,
        relname       name,
        n_live_tup    bigint,
        n_dead_tup    bigint,
        seq_scan      bigint,
        some_ratio    numeric,
        last_vacuum   timestamp with time zone,
        replay_lag    interval,
        nan_metric    double precision
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	rows := []fixtureRow{
		{
			collectedAt: now.Add(-2 * time.Minute),
			dbName:      "northwind", schema: "public", relname: "orders",
			nLiveTup: 100, nDeadTup: 10, seqScan: 5, someRatio: 1.25,
			lastVacuum: now.Add(-time.Hour),
			replayLag:  pgtype.Interval{Microseconds: 1_000_000, Valid: true},
			nanMetric:  1.0,
		},
		{
			collectedAt: now.Add(-1 * time.Minute),
			dbName:      "northwind", schema: "public", relname: "orders",
			nLiveTup: 300, nDeadTup: 30, seqScan: 15, someRatio: 3.5,
			lastVacuum: now.Add(-30 * time.Minute),
			replayLag:  nil,
			nanMetric:  math.NaN(),
		},
		{
			collectedAt: now,
			dbName:      "northwind", schema: "public", relname: "orders",
			nLiveTup: 200, nDeadTup: 20, seqScan: 10, someRatio: 2.0,
			lastVacuum: nil,
			replayLag:  nil,
			nanMetric:  2.0,
		},
		{
			collectedAt: now.Add(-90 * time.Second),
			dbName:      "northwind", schema: "public", relname: "customers",
			nLiveTup: 400, nDeadTup: 5, seqScan: 2, someRatio: 1.0,
			lastVacuum: nil, replayLag: nil, nanMetric: 3.0,
		},
		{
			collectedAt: now.Add(-30 * time.Second),
			dbName:      "northwind", schema: "public", relname: "customers",
			nLiveTup: 500, nDeadTup: 8, seqScan: 4, someRatio: 1.5,
			lastVacuum: nil, replayLag: nil, nanMetric: 4.0,
		},
		{
			collectedAt: now.Add(-45 * time.Second),
			dbName:      "northwind", schema: "public", relname: "products",
			nLiveTup: 50, nDeadTup: 1, seqScan: 1, someRatio: 0.5,
			lastVacuum: nil, replayLag: nil, nanMetric: 5.0,
		},
	}

	insert := `INSERT INTO metrics."` + latestRowsTestProbe + `"
        (connection_id, collected_at, database_name, schemaname, relname,
         n_live_tup, n_dead_tup, seq_scan, some_ratio, last_vacuum,
         replay_lag, nan_metric)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	for i, r := range rows {
		_, err := pool.Exec(ctx, insert,
			1, r.collectedAt, r.dbName, r.schema, r.relname,
			r.nLiveTup, r.nDeadTup, r.seqScan, r.someRatio, r.lastVacuum,
			r.replayLag, r.nanMetric)
		if err != nil {
			dropTable(ctx, pool, latestRowsTestProbe)
			t.Fatalf("failed to insert fixture row %d: %v", i, err)
		}
	}

	return func() { dropTable(context.Background(), pool, latestRowsTestProbe) }
}

// dropTable removes a probe table from the metrics schema, ignoring errors.
func dropTable(ctx context.Context, pool *pgxpool.Pool, name string) {
	// name is a compile-time-constant test probe name, QuoteIdentifier-wrapped;
	// DROP TABLE cannot bind an identifier as a placeholder, so the interpolation
	// is safe and this Opengrep SQLi flag is a false positive.
	// nosemgrep: go_sql_rule-concat-sqli
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS metrics."+QuoteIdentifier(name)+" CASCADE")
}

func TestQueryLatestRows_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupLatestRowsFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("happy path newest entity row", func(t *testing.T) {
		result, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, MetricFilters{}, "", "desc", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 row, got %d", len(result))
		}
		row := result[0]

		// Internal bookkeeping columns must be excluded.
		for _, col := range []string{"connection_id", "collected_at", "inserted_at"} {
			if _, ok := row[col]; ok {
				t.Errorf("internal column %q must not appear in result", col)
			}
		}

		// Default order_by is collected_at; the entity with the newest
		// sample is orders (collected_at now, n_live_tup 200).
		if got := toInt64(t, row["n_live_tup"]); got != 200 {
			t.Errorf("n_live_tup = %v, want 200 (orders' newest row)", row["n_live_tup"])
		}
		if row["relname"] != "orders" {
			t.Errorf("relname = %v, want orders", row["relname"])
		}
		if _, ok := row["seq_scan"]; !ok {
			t.Error("expected seq_scan column in result")
		}
	})

	// This is the exact regression the fix targets: a single entity whose
	// most recent sample (200) is numerically smaller than an older sample
	// (300). TableDetail.tsx requests limit=1 & order_by=n_live_tup desc, so
	// the buggy "rank across all history by n_live_tup" query returned the
	// stale 300 row (with its stale last_vacuum) forever. The corrected query
	// must return the newest sample (200) regardless of order_by.
	t.Run("single entity returns newest row not historical max", func(t *testing.T) {
		filters := MetricFilters{SchemaName: "public", TableName: "orders"}
		result, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, filters, "n_live_tup", "desc", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 row for orders, got %d", len(result))
		}
		row := result[0]
		if got := toInt64(t, row["n_live_tup"]); got != 200 {
			t.Errorf("n_live_tup = %d, want 200 (newest), not 300 (historical max)", got)
		}
		// Row C (the newest) has a NULL last_vacuum; the stale historical
		// max row B had last_vacuum set, so a non-nil value here would prove
		// the stale row leaked through.
		if row["last_vacuum"] != nil {
			t.Errorf("last_vacuum = %v, want nil (newest row's value)", row["last_vacuum"])
		}
	})

	t.Run("multi-entity ranked by each entity latest sample", func(t *testing.T) {
		result, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, MetricFilters{}, "n_live_tup", "desc", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// One row per distinct entity (orders, customers, products), ranked
		// by each entity's own latest n_live_tup DESC: customers 500,
		// orders 200 (NOT its historical 300), products 50.
		if len(result) != 3 {
			t.Fatalf("expected 3 entity rows, got %d", len(result))
		}
		wantVal := []int64{500, 200, 50}
		wantRel := []string{"customers", "orders", "products"}
		for i := range wantVal {
			if got := toInt64(t, result[i]["n_live_tup"]); got != wantVal[i] {
				t.Errorf("row %d n_live_tup = %d, want %d", i, got, wantVal[i])
			}
			if result[i]["relname"] != wantRel[i] {
				t.Errorf("row %d relname = %v, want %s", i, result[i]["relname"], wantRel[i])
			}
		}
	})

	t.Run("multi-entity default order by collected_at desc", func(t *testing.T) {
		result, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, MetricFilters{}, "", "desc", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 entity rows, got %d", len(result))
		}
		// Per-entity latest by collected_at DESC: orders (now, 200),
		// customers (now-30s, 500), products (now-45s, 50).
		want := []int64{200, 500, 50}
		for i, w := range want {
			if got := toInt64(t, result[i]["n_live_tup"]); got != w {
				t.Errorf("row %d n_live_tup = %d, want %d", i, got, w)
			}
		}
	})

	t.Run("limit clamped above max still returns one row per entity", func(t *testing.T) {
		result, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, MetricFilters{}, "", "asc", maxLatestRowLimit+10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 entity rows, got %d", len(result))
		}
		// Per-entity latest by collected_at ASC: products (now-45s, 50),
		// customers (now-30s, 500), orders (now, 200).
		if got := toInt64(t, result[0]["n_live_tup"]); got != 50 {
			t.Errorf("first row n_live_tup = %d, want 50 (oldest latest sample)", got)
		}
	})

	t.Run("database filter resolves and matches", func(t *testing.T) {
		result, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, MetricFilters{DatabaseName: "northwind"}, "", "desc", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 entity rows for northwind, got %d", len(result))
		}

		none, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, MetricFilters{DatabaseName: "no_such_db"}, "", "desc", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(none) != 0 {
			t.Fatalf("expected 0 rows for unknown database, got %d", len(none))
		}
	})

	t.Run("unknown order_by rejected before SQL runs", func(t *testing.T) {
		_, err := QueryLatestRows(ctx, pool, latestRowsTestProbe,
			[]int{1}, MetricFilters{}, "not_a_real_column", "desc", 1)
		if err == nil {
			t.Fatal("expected error for unknown order_by column")
		}
	})

	t.Run("missing probe rejected", func(t *testing.T) {
		_, err := QueryLatestRows(ctx, pool, "does_not_exist_probe",
			[]int{1}, MetricFilters{}, "", "desc", 1)
		if err == nil {
			t.Fatal("expected error for missing probe")
		}
	})
}

// setupNoEntityKeyFixture creates a probe table with no text/name dimension
// columns, only bookkeeping columns and numeric metrics. It models a
// system-level probe such as pg_sys_cpu_info where the whole per-connection
// series is a single logical entity keyed solely on connection_id.
//
// The three connection-1 samples are crafted so the newest sample (by
// collected_at) is NOT the historical maximum of cpu_user: an older sample
// peaks at 99 while the newest reads 50. A query that ranked the full history
// by cpu_user DESC (the original staleness bug) would surface the stale 99;
// the corrected query reduces to the newest sample first and must return 50.
func setupNoEntityKeyFixture(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, latestRowsNoEntityKey)

	ddl := `CREATE TABLE metrics."` + latestRowsNoEntityKey + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        cpu_user      double precision
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create no-entity-key fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	samples := []struct {
		collectedAt time.Time
		cpuUser     float64
	}{
		{now.Add(-2 * time.Minute), 90.0},
		{now.Add(-1 * time.Minute), 99.0}, // historical maximum
		{now, 50.0},                       // newest sample, but not the max
	}

	insert := `INSERT INTO metrics."` + latestRowsNoEntityKey + `"
        (connection_id, collected_at, cpu_user) VALUES ($1, $2, $3)`
	for i, s := range samples {
		if _, err := pool.Exec(ctx, insert, 1, s.collectedAt, s.cpuUser); err != nil {
			dropTable(ctx, pool, latestRowsNoEntityKey)
			t.Fatalf("failed to insert no-entity-key fixture row %d: %v", i, err)
		}
	}

	return func() { dropTable(context.Background(), pool, latestRowsNoEntityKey) }
}

// TestQueryLatestRows_NoEntityKeyIntegration exercises the DISTINCT ON
// (connection_id) path for a probe with no text/name entity-key columns. It
// proves that ranking by a real metric column still returns the newest sample
// by collected_at, not the historical maximum of that column.
func TestQueryLatestRows_NoEntityKeyIntegration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupNoEntityKeyFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	// order_by=cpu_user desc, limit=1. Under the old fallback the query
	// ranked the whole history by cpu_user DESC and returned the stale 99;
	// the corrected query reduces to the newest sample (50) first.
	t.Run("metric order_by returns newest sample not historical max", func(t *testing.T) {
		result, err := QueryLatestRows(ctx, pool, latestRowsNoEntityKey,
			[]int{1}, MetricFilters{}, "cpu_user", "desc", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 row, got %d", len(result))
		}
		if got := toInt64(t, result[0]["cpu_user"]); got != 50 {
			t.Errorf("cpu_user = %d, want 50 (newest sample), not 99 (historical max)", got)
		}
	})

	// The default order_by (collected_at) must also return the newest sample.
	t.Run("default order_by returns newest sample", func(t *testing.T) {
		result, err := QueryLatestRows(ctx, pool, latestRowsNoEntityKey,
			[]int{1}, MetricFilters{}, "", "desc", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 row, got %d", len(result))
		}
		if got := toInt64(t, result[0]["cpu_user"]); got != 50 {
			t.Errorf("cpu_user = %d, want 50 (newest sample)", got)
		}
	})
}

func TestDiscoverLatestRowColumns_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupLatestRowsFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("resolves output and order_by columns", func(t *testing.T) {
		filters := MetricFilters{}
		outputCols, colTypes, orderCol, err := discoverLatestRowColumns(
			ctx, pool, latestRowsTestProbe, "n_live_tup", &filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if orderCol != "n_live_tup" {
			t.Errorf("orderCol = %q, want n_live_tup", orderCol)
		}
		for _, col := range []string{"connection_id", "collected_at", "inserted_at"} {
			if contains(outputCols, col) {
				t.Errorf("output columns must exclude internal column %q", col)
			}
		}
		for _, col := range []string{"relname", "n_live_tup", "last_vacuum", "replay_lag"} {
			if !contains(outputCols, col) {
				t.Errorf("output columns should include %q", col)
			}
		}
		// colTypes must carry the data types the entity-key detection relies
		// on so the caller can build the DISTINCT ON grouping.
		if colTypes["relname"] != "name" {
			t.Errorf("colTypes[relname] = %q, want name", colTypes["relname"])
		}
		if colTypes["n_live_tup"] != "bigint" {
			t.Errorf("colTypes[n_live_tup] = %q, want bigint", colTypes["n_live_tup"])
		}
	})

	t.Run("resolves database filter column", func(t *testing.T) {
		filters := MetricFilters{DatabaseName: "northwind"}
		_, _, _, err := discoverLatestRowColumns(
			ctx, pool, latestRowsTestProbe, "", &filters)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filters.DatabaseColumn != "database_name" {
			t.Errorf("DatabaseColumn = %q, want database_name", filters.DatabaseColumn)
		}
	})

	t.Run("unknown order_by rejected", func(t *testing.T) {
		filters := MetricFilters{}
		_, _, _, err := discoverLatestRowColumns(
			ctx, pool, latestRowsTestProbe, "nope", &filters)
		if err == nil {
			t.Fatal("expected error for unknown order_by")
		}
	})

	t.Run("missing probe rejected", func(t *testing.T) {
		filters := MetricFilters{}
		_, _, _, err := discoverLatestRowColumns(
			ctx, pool, "does_not_exist_probe", "", &filters)
		if err == nil {
			t.Fatal("expected error for missing probe")
		}
	})

	t.Run("probe with only internal columns rejected", func(t *testing.T) {
		dropTable(ctx, pool, latestRowsInternalOnly)
		internalDDL := `CREATE TABLE metrics."` + latestRowsInternalOnly + `" (
            connection_id integer NOT NULL,
            collected_at  timestamp with time zone NOT NULL,
            inserted_at   timestamp without time zone NOT NULL DEFAULT now()
        )`
		if _, err := pool.Exec(ctx, internalDDL); err != nil {
			t.Fatalf("failed to create internal-only table: %v", err)
		}
		defer dropTable(ctx, pool, latestRowsInternalOnly)

		filters := MetricFilters{}
		_, _, _, err := discoverLatestRowColumns(
			ctx, pool, latestRowsInternalOnly, "", &filters)
		if err == nil {
			t.Fatal("expected error for probe with no output columns")
		}
	})

	t.Run("canceled context surfaces query error", func(t *testing.T) {
		cctx, cancel := context.WithCancel(context.Background())
		cancel()
		filters := MetricFilters{}
		_, _, _, err := discoverLatestRowColumns(
			cctx, pool, latestRowsTestProbe, "", &filters)
		if err == nil {
			t.Fatal("expected error from canceled context")
		}
	})
}

func TestScanLatestRows_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupLatestRowsFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("normalizes real scanned values", func(t *testing.T) {
		outputCols := []string{
			"relname", "some_ratio", "last_vacuum", "replay_lag", "nan_metric",
		}
		query := `SELECT relname, some_ratio, last_vacuum, replay_lag, nan_metric
            FROM metrics."` + latestRowsTestProbe + `"
            WHERE connection_id = 1 AND relname = 'orders'
            ORDER BY collected_at ASC`
		rows, err := pool.Query(ctx, query)
		if err != nil {
			t.Fatalf("failed to query fixture: %v", err)
		}
		result, err := scanLatestRows(rows, outputCols)
		if err != nil {
			t.Fatalf("scanLatestRows error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(result))
		}

		// Row A: numeric, timestamp (RFC3339 string), interval normalization.
		rowA := result[0]
		if got, ok := rowA["some_ratio"].(float64); !ok || got != 1.25 {
			t.Errorf("some_ratio = %v (%T), want float64 1.25", rowA["some_ratio"], rowA["some_ratio"])
		}
		ts, ok := rowA["last_vacuum"].(string)
		if !ok {
			t.Fatalf("last_vacuum = %v (%T), want RFC3339 string", rowA["last_vacuum"], rowA["last_vacuum"])
		}
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Errorf("last_vacuum %q is not RFC3339: %v", ts, err)
		}
		if got, ok := rowA["replay_lag"].(float64); !ok || got != 1.0 {
			t.Errorf("replay_lag = %v (%T), want float64 1.0", rowA["replay_lag"], rowA["replay_lag"])
		}

		// Row B: NaN normalizes to JSON null.
		if rowB := result[1]; rowB["nan_metric"] != nil {
			t.Errorf("nan_metric = %v, want nil (NaN sanitized)", rowB["nan_metric"])
		}

		// Row C: NULL timestamp/interval normalize to nil.
		rowC := result[2]
		if rowC["last_vacuum"] != nil {
			t.Errorf("NULL last_vacuum = %v, want nil", rowC["last_vacuum"])
		}
		if rowC["replay_lag"] != nil {
			t.Errorf("NULL replay_lag = %v, want nil", rowC["replay_lag"])
		}
	})

	t.Run("scan error when destinations mismatch columns", func(t *testing.T) {
		rows, err := pool.Query(ctx, "SELECT 1, 2")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		_, err = scanLatestRows(rows, []string{"only_one"})
		if err == nil {
			t.Fatal("expected scan error for mismatched destination count")
		}
	})
}

func TestGetProbeAllColumns_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupLatestRowsFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("returns columns in ordinal order with types", func(t *testing.T) {
		cols, colTypes, err := GetProbeAllColumns(ctx, pool, latestRowsTestProbe)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantOrder := []string{
			"connection_id", "collected_at", "inserted_at", "database_name",
			"schemaname", "relname", "n_live_tup", "n_dead_tup", "seq_scan",
			"some_ratio", "last_vacuum", "replay_lag", "nan_metric",
		}
		if len(cols) != len(wantOrder) {
			t.Fatalf("got %d columns %v, want %d", len(cols), cols, len(wantOrder))
		}
		for i, w := range wantOrder {
			if cols[i] != w {
				t.Errorf("column %d = %q, want %q (ordinal order)", i, cols[i], w)
			}
		}

		wantTypes := map[string]string{
			"connection_id": "integer",
			"collected_at":  "timestamp with time zone",
			"inserted_at":   "timestamp without time zone",
			"database_name": "text",
			"schemaname":    "name",
			"relname":       "name",
			"n_live_tup":    "bigint",
			"some_ratio":    "numeric",
			"last_vacuum":   "timestamp with time zone",
			"replay_lag":    "interval",
			"nan_metric":    "double precision",
		}
		for col, want := range wantTypes {
			if got := colTypes[col]; got != want {
				t.Errorf("colTypes[%q] = %q, want %q", col, got, want)
			}
		}
	})

	t.Run("canceled context surfaces query error", func(t *testing.T) {
		cctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := GetProbeAllColumns(cctx, pool, latestRowsTestProbe)
		if err == nil {
			t.Fatal("expected error from canceled context")
		}
	})
}

// contains reports whether s is present in the slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// toInt64 coerces a scanned/normalized value to int64 for assertions,
// accepting the int64 and float64 shapes pgx may yield for bigint columns.
func toInt64(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		t.Fatalf("value %v (%T) is not a numeric type", v, v)
		return 0
	}
}
