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

// newSpockResolutionsProbeForTest constructs a probe with a minimal,
// deterministic configuration suitable for unit and integration tests.
// The returned probe is database-scoped, matching production behavior.
// Mirrors newSpockExceptionLogProbeForTest for symmetry.
func newSpockResolutionsProbeForTest() *SpockResolutionsProbe {
	return NewSpockResolutionsProbe(&ProbeConfig{
		Name:                      ProbeNameSpockResolutions,
		CollectionIntervalSeconds: 60,
		RetentionDays:             7,
		IsEnabled:                 true,
	})
}

// TestSpockResolutionsProbe_Surface verifies the probe's static
// metadata: name, table name, scope, and that GetQuery returns SQL
// that targets spock.resolutions over the rolling 15-minute window
// described in the design spec. The query must surface the columns
// the alerter relies on (log_time, conflict_type, conflict_resolution)
// and the rolling-window predicate.
func TestSpockResolutionsProbe_Surface(t *testing.T) {
	p := newSpockResolutionsProbeForTest()

	if got := p.GetName(); got != ProbeNameSpockResolutions {
		t.Errorf("GetName() = %q, want %q",
			got, ProbeNameSpockResolutions)
	}
	if got := p.GetTableName(); got != ProbeNameSpockResolutions {
		t.Errorf("GetTableName() = %q, want %q",
			got, ProbeNameSpockResolutions)
	}
	if !p.IsDatabaseScoped() {
		t.Error("IsDatabaseScoped() = false, want true " +
			"(spock_resolutions is a per-database catalog)")
	}

	q := p.GetQuery()
	for _, s := range []string{
		"spock.resolutions",
		"log_time",
		"conflict_type",
		"conflict_resolution",
		"interval '15 minutes'",
	} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing substring %q; query was:\n%s",
				s, q)
		}
	}
}

// TestSpockResolutionsProbe_StoreEmpty asserts that Store returns
// nil with a nil/empty metrics slice and never touches the
// datastore connection. This is the no-op fast path that lets the
// scheduler call Store unconditionally even when Execute returned
// nothing (e.g. Spock not installed, or no resolutions in the window).
func TestSpockResolutionsProbe_StoreEmpty(t *testing.T) {
	p := newSpockResolutionsProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v, want nil", err)
	}
	// Empty slice should also be a no-op.
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		[]map[string]any{}); err != nil {
		t.Errorf("Store([]) = %v, want nil", err)
	}
}

// TestSpockResolutionsProbe_ExecuteWhenSpockAbsent verifies the
// graceful-skip behavior: when the Spock extension is not installed
// in the connected database, Execute returns (nil, nil) rather than
// raising an error or attempting to query a missing catalog. This
// branch is hit on every collection cycle for non-Spock databases
// and must not log noise or churn the connection pool.
//
// The test is skipped when Spock is already installed on the host
// integration database; the integration pool is created fresh per
// process today, but the guard keeps this test correct on hosts where
// Spock is part of the base image.
func TestSpockResolutionsProbe_ExecuteWhenSpockAbsent(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	if spockAlreadyInstalled(t, ctx, conn) {
		t.Skip("spock extension already installed; cannot exercise " +
			"the spock-absent branch")
	}

	p := newSpockResolutionsProbeForTest()

	metrics, err := p.Execute(ctx, "no-spock", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if metrics != nil {
		t.Errorf("Execute() returned %d metrics; want nil "+
			"(integration database has no Spock extension)",
			len(metrics))
	}
}

// TestSpockResolutionsProbe_GetExtensionName documents the
// extension dependency advertised to the scheduler so operator-
// facing reasons ("Spock not installed") stay in sync with the
// runtime guard inside Execute.
func TestSpockResolutionsProbe_GetExtensionName(t *testing.T) {
	p := newSpockResolutionsProbeForTest()
	if got := p.GetExtensionName(); got != "spock" {
		t.Errorf("GetExtensionName() = %q, want %q", got, "spock")
	}
}

// TestSpockResolutionsProbe_ExecuteWithStubSpock drives the Spock-
// installed branch of Execute by registering a stub spock extension
// and creating a minimal spock.resolutions table that matches the
// columns the probe selects. With the schema in place Execute should
// succeed and return an empty (non-nil) slice when no rows fall
// inside the rolling 15-minute window. We then insert a single row
// and verify it round-trips through Execute and Store.
//
// xid columns in the source table use TEXT here (rather than the
// catalog's xid type) because the probe casts xid::text in its query;
// using TEXT in the stub keeps the round-trip free of pgx-side type
// negotiation while still validating the column ordering.
func TestSpockResolutionsProbe_ExecuteWithStubSpock(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	if spockAlreadyInstalled(t, ctx, conn) {
		t.Skip("spock extension already installed; this test " +
			"creates a stub spock and would otherwise need to " +
			"destroy a real one to clean up")
	}

	// Track whether this test created the spock schema. If a stale
	// schema exists from a previous crashed run (or a future setup
	// step pre-creates it for unrelated reasons) the cleanup must
	// leave it alone rather than drop a structure it does not own.
	var createdSchema bool
	if err := conn.QueryRow(ctx, `
		SELECT NOT EXISTS (
			SELECT 1 FROM pg_namespace WHERE nspname = 'spock'
		)
	`).Scan(&createdSchema); err != nil {
		t.Fatalf("check spock schema existence: %v", err)
	}

	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS spock`,
		`CREATE TABLE IF NOT EXISTS spock.resolutions (
			id INTEGER NOT NULL,
			node_name NAME NOT NULL,
			log_time TIMESTAMPTZ NOT NULL,
			relname TEXT,
			idxname TEXT,
			conflict_type TEXT,
			conflict_resolution TEXT,
			local_origin INTEGER,
			local_tuple TEXT,
			local_xid XID,
			local_timestamp TIMESTAMPTZ,
			remote_origin INTEGER,
			remote_tuple TEXT,
			remote_xid XID,
			remote_timestamp TIMESTAMPTZ,
			remote_lsn PG_LSN
		)`,
		`INSERT INTO pg_extension (oid, extname, extowner,
			extnamespace, extrelocatable, extversion,
			extconfig, extcondition)
			SELECT (SELECT MAX(oid::oid::int)
				FROM pg_extension) + 1, 'spock', 10,
				(SELECT oid FROM pg_namespace
					WHERE nspname='spock'),
				TRUE, '1.0', NULL, NULL
			WHERE NOT EXISTS (SELECT 1 FROM pg_extension
				WHERE extname='spock')`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("setup spock stub: %v\n%s", err, s)
		}
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(ctx,
			"DELETE FROM pg_extension "+
				"WHERE extname='spock'"); err != nil {
			t.Logf("cleanup pg_extension spock row: %v", err)
		}
		if !createdSchema {
			// The schema was already there; leave it alone so a
			// crashed previous run or unrelated test fixture does
			// not lose its structure to this cleanup. In that
			// situation we still drop the table the test created
			// so subsequent runs find a clean schema.
			if _, err := conn.Exec(ctx,
				"DROP TABLE IF EXISTS spock.resolutions"); err != nil {
				t.Logf("cleanup spock.resolutions table: %v", err)
			}
			return
		}
		if _, err := conn.Exec(ctx,
			"DROP SCHEMA spock CASCADE"); err != nil {
			t.Logf("cleanup spock schema: %v", err)
		}
	})

	p := newSpockResolutionsProbeForTest()

	// Empty-window case: no rows yet, but the query and scan path
	// must succeed and return a non-error result.
	metrics, err := p.Execute(ctx, "with-spock", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute (empty window): %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("Execute (empty window) returned %d metrics, "+
			"want 0", len(metrics))
	}

	// Insert a synthetic row that falls inside the 15-minute window.
	// xid and pg_lsn values use the cast-friendly literal forms so the
	// probe's xid::text / pg_lsn::text projections produce stable text
	// representations.
	if _, err := conn.Exec(ctx, `
		INSERT INTO spock.resolutions (
			id, node_name, log_time,
			relname, idxname,
			conflict_type, conflict_resolution,
			local_origin, local_tuple, local_xid, local_timestamp,
			remote_origin, remote_tuple, remote_xid, remote_timestamp,
			remote_lsn)
		VALUES (
			1, 'n1', now(),
			'public.t', 't_pkey',
			'update_update', 'keep_local',
			16384, 'a=1', '12345'::xid, now(),
			16385, 'a=2', '12346'::xid, now(),
			'0/16B6300'::pg_lsn)
	`); err != nil {
		t.Fatalf("insert resolutions row: %v", err)
	}

	metrics, err = p.Execute(ctx, "with-spock", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute (one row): %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("Execute returned %d rows, want 1", len(metrics))
	}
	if got := metrics[0]["conflict_type"]; got != "update_update" {
		t.Errorf("conflict_type = %v, want %q",
			got, "update_update")
	}
	if got := metrics[0]["conflict_resolution"]; got != "keep_local" {
		t.Errorf("conflict_resolution = %v, want %q",
			got, "keep_local")
	}
	// xid and pg_lsn must arrive as text after the explicit cast so
	// the COPY/INSERT pipeline can write them into the destination
	// TEXT columns without further coercion.
	if got, ok := metrics[0]["local_xid"].(string); !ok ||
		got != "12345" {
		t.Errorf("local_xid = %v (%T), want \"12345\" (string)",
			metrics[0]["local_xid"], metrics[0]["local_xid"])
	}
	if got, ok := metrics[0]["remote_lsn"].(string); !ok ||
		got != "0/16B6300" {
		t.Errorf("remote_lsn = %v (%T), want \"0/16B6300\" (string)",
			metrics[0]["remote_lsn"], metrics[0]["remote_lsn"])
	}

	// The scheduler injects _database_name on every row before
	// Store is called. Mirror that here so Store can populate the
	// destination database_name column without errors.
	for i := range metrics {
		metrics[i]["_database_name"] = "stub-db"
	}

	// Store the row to metrics.spock_resolutions to exercise the
	// happy path of the COPY/INSERT pipeline. The integration helper
	// schema includes this table so partition creation and the
	// column ordering above are both verified end-to-end.
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Confirm at least one row landed in the destination table.
	var stored int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM metrics.spock_resolutions
		 WHERE connection_id = 1
	`).Scan(&stored); err != nil {
		t.Fatalf("count stored rows: %v", err)
	}
	if stored != 1 {
		t.Errorf("metrics.spock_resolutions count = %d, want 1",
			stored)
	}
}

// TestSpockResolutionsProbe_ExecuteCheckExtensionError exercises the
// CheckExtensionExists error branch by passing a canceled context.
// pgxpool returns an error for any query attempted with a canceled
// context; the probe must wrap and return that error rather than
// swallowing it.
func TestSpockResolutionsProbe_ExecuteCheckExtensionError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// Force the underlying *pgx.Conn into a state that fails the
	// next query. Canceling the context is the cheapest way to
	// guarantee QueryRow returns an error without disturbing the
	// shared pool.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	p := newSpockResolutionsProbeForTest()
	if _, err := p.Execute(cancelCtx, "canceled", conn,
		pgVersion); err == nil {
		t.Fatal("Execute with canceled context: expected error, " +
			"got nil")
	}
}

// TestSpockResolutionsProbe_StoreMissingDatabaseName drives the
// guard that surfaces a clear error when the scheduler-injected
// _database_name key is missing from a metric row. database_name is
// part of the destination primary key (the spock.resolutions.id
// sequence is per-database, so the discriminator is essential), and
// a missing value is a programming error in the scheduler rather
// than a runtime data condition the probe can recover from.
func TestSpockResolutionsProbe_StoreMissingDatabaseName(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)

	metrics := []map[string]any{
		{
			// _database_name deliberately omitted so Store hits the
			// guarded error path before issuing any SQL.
			"id":                  1,
			"node_name":           "n1",
			"log_time":            time.Now(),
			"relname":             "public.t",
			"idxname":             "t_pkey",
			"conflict_type":       "update_update",
			"conflict_resolution": "keep_local",
			"local_origin":        int32(16384),
			"local_tuple":         "a=1",
			"local_xid":           "12345",
			"local_timestamp":     time.Now(),
			"remote_origin":       int32(16385),
			"remote_tuple":        "a=2",
			"remote_xid":          "12346",
			"remote_timestamp":    time.Now(),
			"remote_lsn":          "0/16B6300",
		},
	}

	p := newSpockResolutionsProbeForTest()
	err := p.Store(context.Background(), conn, 1, time.Now(), metrics)
	if err == nil {
		t.Fatal("Store(no _database_name) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "database_name not found") {
		t.Errorf("Store error = %q; want it to mention "+
			"'database_name not found'", err.Error())
	}
}

// TestSpockResolutionsProbe_StoreEnsurePartitionError drives the
// EnsurePartition error branch of Store by passing a canceled
// context with a real datastore connection. EnsurePartition issues
// SQL through the connection, which fails immediately when the
// context is already canceled, causing Store to surface the wrapped
// "failed to ensure partition" error.
func TestSpockResolutionsProbe_StoreEnsurePartitionError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	metrics := []map[string]any{
		{
			"id":                  1,
			"node_name":           "n1",
			"log_time":            time.Now(),
			"relname":             "public.t",
			"idxname":             "t_pkey",
			"conflict_type":       "update_update",
			"conflict_resolution": "keep_local",
			"local_origin":        int32(16384),
			"local_tuple":         "a=1",
			"local_xid":           "12345",
			"local_timestamp":     time.Now(),
			"remote_origin":       int32(16385),
			"remote_tuple":        "a=2",
			"remote_xid":          "12346",
			"remote_timestamp":    time.Now(),
			"remote_lsn":          "0/16B6300",
		},
	}

	p := newSpockResolutionsProbeForTest()
	err := p.Store(cancelCtx, conn, 1, time.Now(), metrics)
	if err == nil {
		t.Fatal("Store(canceled ctx) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to ensure partition") {
		t.Errorf("Store error = %q; want it to mention "+
			"'failed to ensure partition'", err.Error())
	}
}
