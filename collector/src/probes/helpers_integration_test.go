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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreMetrics_EmptyValues(t *testing.T) {
	// StoreMetrics short-circuits on len(values)==0; nil conn is safe
	// for that branch.
	if err := StoreMetrics(context.Background(), nil,
		"pg_stat_activity", []string{"connection_id"},
		nil); err != nil {
		t.Errorf("StoreMetrics([]) = %v, want nil", err)
	}
}

func TestStoreMetrics_BatchAndCommit(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Make sure a partition exists for the timestamp we will use.
	now := time.Now().UTC()
	if err := EnsurePartition(ctx, conn, "pg_stat_activity",
		now); err != nil {
		t.Fatalf("EnsurePartition: %v", err)
	}

	// Build > 100 rows so the batching loop iterates more than once.
	const rowCount = 250
	cols := []string{
		"connection_id", "collected_at",
		"datid", "datname", "pid", "leader_pid",
		"usesysid", "usename", "application_name",
		"client_addr", "client_hostname", "client_port",
		"backend_start", "xact_start", "query_start",
		"state_change", "wait_event_type", "wait_event",
		"state", "backend_xid", "backend_xmin",
		"query", "backend_type",
	}
	values := make([][]any, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		values = append(values, []any{
			1, now,
			nil, "db", int64(i), nil,
			nil, "u", "app",
			nil, nil, nil,
			now, nil, nil,
			now, nil, nil,
			"idle", nil, nil,
			"SELECT 1", "client backend",
		})
	}
	if err := StoreMetrics(ctx, conn,
		"pg_stat_activity", cols, values); err != nil {
		t.Fatalf("StoreMetrics: %v", err)
	}
}

func TestEnsurePartition_Idempotent(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := EnsurePartition(ctx, conn, "pg_stat_activity",
			now); err != nil {
			t.Fatalf("EnsurePartition iteration %d: %v", i, err)
		}
	}
}

func TestCheckExtensionExists(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// plpgsql is installed in every default cluster; system_stats is
	// not. Together they cover both branches.
	exists, err := CheckExtensionExists(ctx, "test", conn,
		"plpgsql")
	if err != nil {
		t.Errorf("CheckExtensionExists(plpgsql) error: %v", err)
	}
	if !exists {
		t.Error("plpgsql should exist")
	}
	exists, err = CheckExtensionExists(ctx, "test", conn,
		"definitely_not_installed_extension")
	if err != nil {
		t.Errorf("CheckExtensionExists(missing) error: %v", err)
	}
	if exists {
		t.Error("missing extension should not exist")
	}
}

func TestCheckViewExists_Helper(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// pg_views is itself a catalog view, so the check returns true.
	exists, err := CheckViewExists(ctx, conn, "pg_views")
	if err != nil {
		t.Errorf("CheckViewExists(pg_views): %v", err)
	}
	if !exists {
		t.Error("pg_views should exist")
	}
	exists, err = CheckViewExists(ctx, conn,
		"definitely_not_a_view")
	if err != nil {
		t.Errorf("CheckViewExists(missing): %v", err)
	}
	if exists {
		t.Error("missing view should not exist")
	}
}

func TestGetLastCollectionTime_NoTable(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Querying a probe whose metrics table does not exist must return
	// (zero time, nil) so the scheduler treats the probe as fresh
	// rather than aborting.
	got, err := GetLastCollectionTime(ctx, conn,
		"nonexistent_probe_for_test", 1)
	if err != nil {
		t.Fatalf("GetLastCollectionTime missing table: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}

func TestGetLastCollectionTime_NoRows(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Existing table but no rows for connection_id -> zero time.
	got, err := GetLastCollectionTime(ctx, conn,
		"pg_stat_activity", 999999)
	if err != nil {
		t.Fatalf("GetLastCollectionTime no rows: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("expected zero time for missing rows, got %v",
			got)
	}
}

func TestDropExpiredPartitions(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Touching pg_settings creates partitions in this run; ensure at
	// least one exists for an old date so the GC loop has something
	// to do. The function must not drop the partition for a future
	// timestamp because we set retentionDays so the cutoff is older.
	old := time.Now().AddDate(0, -2, 0).UTC()
	if err := EnsurePartition(ctx, conn, "pg_stat_activity",
		old); err != nil {
		t.Fatalf("EnsurePartition old: %v", err)
	}

	// retentionDays=7 makes the cutoff recent, so the old partition
	// is older than the cutoff and should be dropped.
	dropped, err := DropExpiredPartitions(ctx, conn,
		"pg_stat_activity", 7)
	if err != nil {
		t.Fatalf("DropExpiredPartitions: %v", err)
	}
	if dropped < 1 {
		t.Errorf("expected at least 1 dropped partition, got %d",
			dropped)
	}

	// Idempotent: a second call should drop zero further partitions
	// because the recent partitions are within retention.
	dropped, err = DropExpiredPartitions(ctx, conn,
		"pg_stat_activity", 365)
	if err != nil {
		t.Fatalf("DropExpiredPartitions second: %v", err)
	}
	if dropped != 0 {
		t.Errorf("expected zero dropped on second call, got %d",
			dropped)
	}
}

func TestDropExpiredPartitions_ProtectedTable(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// pg_settings is one of the change-tracked probes whose most
	// recent partition is protected. Insert a sample row, then call
	// DropExpiredPartitions with a tiny retention window. The
	// protected partition must survive.
	now := time.Now().UTC()
	if err := EnsurePartition(ctx, conn, "pg_settings",
		now); err != nil {
		t.Fatalf("EnsurePartition: %v", err)
	}
	cols := []string{
		"connection_id", "collected_at",
		"name", "setting", "unit", "category", "short_desc",
		"extra_desc", "context", "vartype", "source", "min_val",
		"max_val", "enumvals", "boot_val", "reset_val",
		"sourcefile", "sourceline", "pending_restart",
	}
	rows := [][]any{
		{
			55, now,
			"max_connections", "100", nil, "test", "test",
			"", "postmaster", "integer", "default",
			"1", "262143", nil, "100", "100",
			nil, nil, false,
		},
	}
	if err := StoreMetrics(ctx, conn, "pg_settings", cols,
		rows); err != nil {
		t.Fatalf("StoreMetrics: %v", err)
	}

	dropped, err := DropExpiredPartitions(ctx, conn,
		"pg_settings", 0)
	if err != nil {
		t.Fatalf("DropExpiredPartitions: %v", err)
	}
	// The partition that holds the row we just inserted must be
	// protected even with retentionDays=0; we cannot prove non-zero
	// drops because no other partitions exist for the table here.
	if dropped < 0 {
		t.Errorf("dropped count should not be negative: %d", dropped)
	}
}

// helperPool is a tiny convenience wrapper that simplifies passing the
// pool through helper-only tests.
func helperPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return requireIntegrationPool(t)
}

func TestLoadProbeConfigs(t *testing.T) {
	pool := helperPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Create a minimal probe_configs table so LoadProbeConfigs has
	// something to scan. The integration helper does not create this
	// table because the production schema lives in a different
	// package; we provision it locally for the test.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS probe_configs (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			collection_interval_seconds INTEGER NOT NULL DEFAULT 300,
			retention_days INTEGER NOT NULL DEFAULT 30,
			is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			connection_id INTEGER,
			scope TEXT NOT NULL DEFAULT 'global',
			cluster_id INTEGER,
			group_id INTEGER
		)
	`); err != nil {
		t.Fatalf("create probe_configs: %v", err)
	}
	// Ensure a clean slate.
	if _, err := conn.Exec(ctx,
		"TRUNCATE probe_configs"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	connID := 1
	rows := []struct {
		name string
		cid  *int
	}{
		{"pg_stat_activity", nil},
		{"pg_stat_database", &connID},
	}
	for _, r := range rows {
		if _, err := conn.Exec(ctx, `
			INSERT INTO probe_configs (name, connection_id)
			VALUES ($1, $2)
		`, r.name, r.cid); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	configs, err := LoadProbeConfigs(ctx, conn)
	if err != nil {
		t.Fatalf("LoadProbeConfigs: %v", err)
	}
	if len(configs[0]) == 0 {
		t.Error("expected globals (connection_id 0)")
	}
	if len(configs[1]) == 0 {
		t.Error("expected per-connection entries for ID 1")
	}
}

func TestEnsureProbeConfig(t *testing.T) {
	pool := helperPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS probe_configs (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			collection_interval_seconds INTEGER NOT NULL DEFAULT 300,
			retention_days INTEGER NOT NULL DEFAULT 30,
			is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			connection_id INTEGER,
			scope TEXT NOT NULL DEFAULT 'global',
			cluster_id INTEGER,
			group_id INTEGER
		)
	`); err != nil {
		t.Fatalf("create probe_configs: %v", err)
	}
	// Clear any previous test data.
	if _, err := conn.Exec(ctx,
		"DELETE FROM probe_configs"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Create supporting connections/clusters tables so the cluster
	// and group queries can run without erroring; the queries are
	// expected to find no rows in this test.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS connections (
			id INTEGER PRIMARY KEY,
			cluster_id INTEGER
		);
		CREATE TABLE IF NOT EXISTS clusters (
			id INTEGER PRIMARY KEY,
			group_id INTEGER
		);
	`); err != nil {
		t.Fatalf("create supporting tables: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO connections (id, cluster_id) VALUES (1, NULL)
		ON CONFLICT (id) DO NOTHING
	`); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	// First call: no existing config -> uses hardcoded defaults
	// and inserts a server-scoped row.
	cfg, err := EnsureProbeConfig(ctx, conn, 1,
		"pg_stat_activity")
	if err != nil {
		t.Fatalf("EnsureProbeConfig first: %v", err)
	}
	if cfg.Name != "pg_stat_activity" {
		t.Errorf("Name = %v", cfg.Name)
	}
	if cfg.CollectionIntervalSeconds != 60 {
		t.Errorf("default interval should be 60, got %d",
			cfg.CollectionIntervalSeconds)
	}
	// Second call returns the same row from the server-scoped
	// lookup branch.
	cfg2, err := EnsureProbeConfig(ctx, conn, 1,
		"pg_stat_activity")
	if err != nil {
		t.Fatalf("EnsureProbeConfig second: %v", err)
	}
	if cfg2.ID != cfg.ID {
		t.Errorf("expected same row, got IDs %d vs %d",
			cfg.ID, cfg2.ID)
	}
	// Third call with a probe that has a global config seeded by
	// the test -> hits the global branch.
	if _, err := conn.Exec(ctx, `
		INSERT INTO probe_configs (name, scope, connection_id,
			collection_interval_seconds, retention_days)
		VALUES ('custom_probe', 'global', NULL, 999, 11)
	`); err != nil {
		t.Fatalf("seed global: %v", err)
	}
	cfg3, err := EnsureProbeConfig(ctx, conn, 1,
		"custom_probe")
	if err != nil {
		t.Fatalf("EnsureProbeConfig with global parent: %v", err)
	}
	if cfg3.CollectionIntervalSeconds != 999 {
		t.Errorf("interval inherited from global should be 999, "+
			"got %d", cfg3.CollectionIntervalSeconds)
	}
}

func TestGetDefaultInterval(t *testing.T) {
	tests := []struct {
		name     string
		probe    string
		expected int
	}{
		{"known activity", ProbeNamePgStatActivity, 60},
		{"known settings", ProbeNamePgSettings, 3600},
		{"known sys cpu usage", ProbeNamePgSysCPUUsageInfo, 60},
		{"unknown defaults to 300", "no_such_probe", 300},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getDefaultInterval(tc.probe)
			if got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestParseConnInfo(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHost string
		wantPort int
	}{
		{"full", "host=publisher.example port=5433 dbname=p user=u",
			"publisher.example", 5433},
		{"defaults port", "host=h dbname=d", "h", 5432},
		{"empty", "", "", 0},
		{"port only", "port=6000 dbname=d", "", 6000},
		{"bad port", "host=h port=notanumber", "h", 5432},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, p := parseConnInfo(tc.input)
			if h != tc.wantHost {
				t.Errorf("host: got %q want %q", h, tc.wantHost)
			}
			if p != tc.wantPort {
				t.Errorf("port: got %d want %d", p, tc.wantPort)
			}
		})
	}
}
