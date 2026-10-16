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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Several probes pick a query by which statistics views the monitored
// server has, so a test run against one PostgreSQL version only reaches
// the branches for that version. These tests reach the others on any
// version: a temporary view shadows the system view, since PostgreSQL
// searches the session's temporary schema before pg_catalog when it is
// not named in search_path, and a seeded feature-cache entry stands in
// for the existence check. CI runs the suite against PostgreSQL 14 to 18,
// and without this the coverage measured on the oldest version fell
// below the floor.

// shadowViews acquires a connection and creates the given temporary
// views on it. The views are discarded before the connection goes back
// to the pool.
func shadowViews(t *testing.T, views ...string) *pgxpool.Conn {
	t.Helper()
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "DISCARD TEMP"); err != nil {
			t.Errorf("failed to discard temporary views: %v", err)
		}
	})
	for _, v := range views {
		if _, err := conn.Exec(ctx, v); err != nil {
			t.Fatalf("failed to create temporary view: %v\nSQL: %s", err, v)
		}
	}
	return conn
}

// seedFeature records a feature-detection answer for connName on conn
// and removes it when the test ends.
func seedFeature(t *testing.T, connName string, conn *pgxpool.Conn, check string, value bool) {
	t.Helper()
	key := featureCacheKey{
		connectionName: connName,
		databaseName:   connectionDatabaseName(conn),
		checkName:      check,
	}
	featureCache.Store(key, value)
	t.Cleanup(func() { featureCache.Delete(key) })
}

// requireOneRow fails the test unless Execute returned exactly one row
// whose column col holds want.
func requireOneRow(t *testing.T, rows []map[string]any, err error, col string, want any) {
	t.Helper()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Execute() returned %d rows, want 1: %v", len(rows), rows)
	}
	if got := rows[0][col]; got != want {
		t.Errorf("%s = %#v, want %#v", col, got, want)
	}
}

func TestPgStatIOProbe_ExecuteVersionBranches(t *testing.T) {
	conn := shadowViews(t, `
        CREATE TEMP VIEW pg_stat_io AS SELECT
            'client backend'::text AS backend_type, 'relation'::text AS object,
            'normal'::text AS context, 7::bigint AS reads,
            0::double precision AS read_time, 0::bigint AS writes,
            0::double precision AS write_time, 0::bigint AS writebacks,
            0::double precision AS writeback_time, 0::bigint AS extends,
            0::double precision AS extend_time, 8192::bigint AS op_bytes,
            0::bigint AS hits, 0::bigint AS evictions, 0::bigint AS reuses,
            0::bigint AS fsyncs, 0::double precision AS fsync_time,
            now() AS stats_reset`)
	p := NewPgStatIOProbe(&ProbeConfig{Name: ProbeNamePgStatIO})

	for _, version := range []int{17, 18} {
		const connName = "io-version-branches"
		seedFeature(t, connName, conn, "pg_stat_io_exists", true)
		seedFeature(t, connName, conn, "pg_stat_slru_exists", false)
		rows, err := p.Execute(context.Background(), connName, conn, version)
		requireOneRow(t, rows, err, "reads", int64(7))
		if version == 18 && rows[0]["op_bytes"] != nil {
			t.Errorf("PG18 op_bytes = %v, want NULL", rows[0]["op_bytes"])
		}
	}
}

func TestPgStatCheckpointerProbe_ExecuteVersionBranches(t *testing.T) {
	conn := shadowViews(t,
		`CREATE TEMP VIEW pg_stat_checkpointer AS SELECT
            3::bigint AS num_timed, 1::bigint AS num_requested,
            0::bigint AS restartpoints_timed, 0::bigint AS restartpoints_req,
            0::bigint AS restartpoints_done, 0::double precision AS write_time,
            0::double precision AS sync_time, 0::bigint AS buffers_written,
            now() AS stats_reset`,
		`CREATE TEMP VIEW pg_stat_bgwriter AS SELECT
            5::bigint AS checkpoints_timed, 1::bigint AS checkpoints_req,
            0::double precision AS checkpoint_write_time,
            0::double precision AS checkpoint_sync_time,
            0::bigint AS buffers_checkpoint, 0::bigint AS buffers_clean,
            0::bigint AS maxwritten_clean, 0::bigint AS buffers_alloc,
            now() AS stats_reset`)
	p := NewPgStatCheckpointerProbe(&ProbeConfig{Name: ProbeNamePgStatCheckpointer})

	tests := []struct {
		connName string
		exists   bool
		want     int64
	}{
		{"checkpointer-view", true, 3},
		{"bgwriter-only", false, 5},
	}
	for _, tt := range tests {
		seedFeature(t, tt.connName, conn, "pg_stat_checkpointer_exists", tt.exists)
		rows, err := p.Execute(context.Background(), tt.connName, conn, 16)
		requireOneRow(t, rows, err, "num_timed", tt.want)
	}
}

func TestPgStatRecoveryPrefetchProbe_ExecuteWithView(t *testing.T) {
	conn := shadowViews(t, `
        CREATE TEMP VIEW pg_stat_recovery_prefetch AS SELECT
            now() AS stats_reset, 11::bigint AS prefetch, 0::bigint AS hit,
            0::bigint AS skip_init, 0::bigint AS skip_new, 0::bigint AS skip_fpw,
            0::bigint AS skip_rep, 0::integer AS wal_distance,
            0::integer AS block_distance, 0::integer AS io_depth`)
	const connName = "recovery-prefetch-view"
	seedFeature(t, connName, conn, "pg_stat_recovery_prefetch_exists", true)

	p := NewPgStatRecoveryPrefetchProbe(&ProbeConfig{Name: ProbeNamePgStatRecoveryPrefetch})
	rows, err := p.Execute(context.Background(), connName, conn, 15)
	requireOneRow(t, rows, err, "prefetch", int64(11))
}

func TestPgIdentFileMappingsProbe_ExecuteWithView(t *testing.T) {
	conn := shadowViews(t, `
        CREATE TEMP VIEW pg_ident_file_mappings AS SELECT
            1::integer AS map_number, '/etc/pg_ident.conf'::text AS file_name,
            4::integer AS line_number, 'map'::text AS map_name,
            'sysuser'::text AS sys_name, 'app'::text AS pg_username,
            NULL::text AS error`)
	const connName = "ident-mappings-view"
	seedFeature(t, connName, conn, "pg_ident_file_mappings_exists", true)

	p := NewPgIdentFileMappingsProbe(&ProbeConfig{Name: ProbeNamePgIdentFileMappings})
	for _, version := range []int{15, 16} {
		rows, err := p.Execute(context.Background(), connName, conn, version)
		requireOneRow(t, rows, err, "pg_username", "app")
	}
}

func TestPgStatWalProbe_ExecuteVersionBranches(t *testing.T) {
	conn := shadowViews(t, `
        CREATE TEMP VIEW pg_stat_wal AS SELECT
            9::bigint AS wal_records, 0::bigint AS wal_fpi,
            0::numeric AS wal_bytes, 0::bigint AS wal_buffers_full,
            0::bigint AS wal_write, 0::bigint AS wal_sync,
            0::double precision AS wal_write_time,
            0::double precision AS wal_sync_time, now() AS stats_reset`)
	p := NewPgStatWalProbe(&ProbeConfig{Name: ProbeNamePgStatWAL})

	for _, version := range []int{17, 18} {
		rows, err := p.Execute(context.Background(), "wal-version-branches", conn, version)
		requireOneRow(t, rows, err, "wal_records", int64(9))
	}
}
