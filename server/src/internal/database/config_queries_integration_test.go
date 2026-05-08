/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// configQueriesTestSchema mirrors the production schema for the config
// query family: probe_configs, alert_rules, alert_thresholds, plus the
// cluster hierarchy parents and a metrics schema sufficient for
// resolveConnectionHierarchy to walk through its happy and dismissed
// paths.
const configQueriesTestSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    auto_group_key VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    auto_cluster_key VARCHAR(255) UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    replication_type VARCHAR(50),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT clusters_group_name_unique UNIQUE (group_id, name)
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    host VARCHAR(255) NOT NULL DEFAULT 'localhost',
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT 'postgres',
    username VARCHAR(255) NOT NULL DEFAULT 'postgres',
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    role VARCHAR(50) DEFAULT 'primary',
    connection_error TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(20) NOT NULL DEFAULT 'auto',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER,
    status VARCHAR(50) NOT NULL DEFAULT 'active'
);

CREATE TABLE probe_configs (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    collection_interval_seconds INTEGER NOT NULL DEFAULT 60,
    retention_days INTEGER NOT NULL DEFAULT 28,
    scope TEXT NOT NULL DEFAULT 'global',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    user_modified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT probe_configs_scope_check
        CHECK (scope IN ('global', 'group', 'cluster', 'server'))
);

CREATE UNIQUE INDEX idx_probe_configs_unique_global
    ON probe_configs(name) WHERE scope = 'global';
CREATE UNIQUE INDEX idx_probe_configs_unique_server
    ON probe_configs(name, connection_id) WHERE scope = 'server';
CREATE UNIQUE INDEX idx_probe_configs_unique_cluster
    ON probe_configs(name, cluster_id) WHERE scope = 'cluster';
CREATE UNIQUE INDEX idx_probe_configs_unique_group
    ON probe_configs(name, group_id) WHERE scope = 'group';

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL,
    category TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    metric_unit TEXT,
    default_operator TEXT NOT NULL CHECK (default_operator IN ('>', '>=', '<', '<=', '==', '!=')),
    default_threshold REAL NOT NULL,
    default_severity TEXT NOT NULL CHECK (default_severity IN ('info', 'warning', 'critical')),
    default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    required_extension TEXT,
    is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alert_thresholds (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    operator TEXT NOT NULL,
    threshold REAL NOT NULL,
    severity TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_alert_thresholds_unique_server
    ON alert_thresholds(rule_id, connection_id, COALESCE(database_name, ''))
    WHERE scope = 'server';
CREATE UNIQUE INDEX idx_alert_thresholds_unique_cluster
    ON alert_thresholds(rule_id, cluster_id) WHERE scope = 'cluster';
CREATE UNIQUE INDEX idx_alert_thresholds_unique_group
    ON alert_thresholds(rule_id, group_id) WHERE scope = 'group';

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_connectivity (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_node_role (
    connection_id INTEGER NOT NULL,
    primary_role TEXT,
    upstream_host TEXT,
    upstream_port INTEGER,
    has_spock BOOLEAN,
    spock_node_name TEXT,
    binary_standby_count INTEGER,
    is_streaming_standby BOOLEAN,
    publisher_host TEXT,
    publisher_port INTEGER,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_server_info (
    connection_id INTEGER NOT NULL,
    server_version TEXT,
    system_identifier BIGINT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_sys_os_info (
    connection_id INTEGER NOT NULL,
    name TEXT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_extension (
    connection_id INTEGER NOT NULL,
    extname TEXT NOT NULL,
    extversion TEXT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const configQueriesTestTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// configFixture seeds the parent IDs and a default group used by
// resolveConnectionHierarchy. The CleanupConn flag lets each test pick
// up a different connection if it needs one without parent rows.
type configFixture struct {
	GroupID        int
	DefaultGroupID int
	ClusterID      int
	ConnID         int
}

// newConfigQueriesTestDatastore builds a *Datastore against the
// TEST_AI_WORKBENCH_SERVER instance with the config schema. It also
// seeds parent rows and a default group required by hierarchy
// resolution.
func newConfigQueriesTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, configFixture, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping config queries integration test")
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
	if _, err := pool.Exec(ctx, configQueriesTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create config queries test schema: %v", err)
	}

	var defaultGroupID, groupID, clusterID, connID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO cluster_groups (name, description, is_shared, is_default)
        VALUES ('Servers/Clusters', 'default', TRUE, TRUE)
        RETURNING id`).Scan(&defaultGroupID); err != nil {
		pool.Close()
		t.Fatalf("seed default group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ('grp-1') RETURNING id`).Scan(&groupID); err != nil {
		pool.Close()
		t.Fatalf("seed group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, 'cls-1') RETURNING id`,
		groupID).Scan(&clusterID); err != nil {
		pool.Close()
		t.Fatalf("seed cluster: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES ($1, 'conn-1') RETURNING id`,
		clusterID).Scan(&connID); err != nil {
		pool.Close()
		t.Fatalf("seed connection: %v", err)
	}

	ds := NewTestDatastore(pool)
	cleanup := func() {
		if _, err := pool.Exec(context.Background(), configQueriesTestTeardown); err != nil {
			t.Logf("config queries teardown failed: %v", err)
		}
		pool.Close()
	}
	return ds, pool, configFixture{
		GroupID:        groupID,
		DefaultGroupID: defaultGroupID,
		ClusterID:      clusterID,
		ConnID:         connID,
	}, cleanup
}

// seedProbeConfigs inserts a global probe and a server-scoped probe so
// the GetProbeConfigs both-branches behavior can be asserted.
func seedProbeConfigs(t *testing.T, pool *pgxpool.Pool, connID int) (globalID int, serverID int) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `
        INSERT INTO probe_configs (connection_id, is_enabled, name, description,
                                   collection_interval_seconds, retention_days, scope)
        VALUES (NULL, TRUE, 'pg_stat_activity', 'desc', 60, 7, 'global')
        RETURNING id`).Scan(&globalID); err != nil {
		t.Fatalf("seed global probe: %v", err)
	}
	if err := pool.QueryRow(ctx, `
        INSERT INTO probe_configs (connection_id, is_enabled, name, description,
                                   collection_interval_seconds, retention_days, scope)
        VALUES ($1, FALSE, 'pg_stat_database', 'sdesc', 30, 14, 'server')
        RETURNING id`, connID).Scan(&serverID); err != nil {
		t.Fatalf("seed server probe: %v", err)
	}
	return globalID, serverID
}

func seedAlertRule(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
        INSERT INTO alert_rules (name, description, category, metric_name,
                                 default_operator, default_threshold,
                                 default_severity, default_enabled)
        VALUES ($1, 'rule desc', 'cat', 'metric', '>', 10.0, 'warning', TRUE)
        RETURNING id`, name).Scan(&id)
	if err != nil {
		t.Fatalf("seed alert rule: %v", err)
	}
	return id
}

// TestGetProbeConfigsByScope exercises both branches of GetProbeConfigs:
// nil (global) and explicit connection ID.
func TestGetProbeConfigsByScope(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	seedProbeConfigs(t, pool, fx.ConnID)

	// nil -> global
	globals, err := ds.GetProbeConfigs(ctx, nil)
	if err != nil {
		t.Fatalf("global: %v", err)
	}
	if len(globals) != 1 || globals[0].Name != "pg_stat_activity" {
		t.Errorf("global mismatch: %+v", globals)
	}

	// scoped -> server
	servers, err := ds.GetProbeConfigs(ctx, &fx.ConnID)
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "pg_stat_database" {
		t.Errorf("scoped mismatch: %+v", servers)
	}

	// non-existent connection -> empty (not nil)
	other := 999999
	none, err := ds.GetProbeConfigs(ctx, &other)
	if err != nil {
		t.Fatalf("none: %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("expected non-nil empty slice, got %+v", none)
	}
}

// TestGetProbeConfigByID exercises both Get success and not-found paths.
func TestGetProbeConfigByID(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	globalID, _ := seedProbeConfigs(t, pool, fx.ConnID)

	got, err := ds.GetProbeConfig(ctx, int64(globalID))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "pg_stat_activity" {
		t.Errorf("name = %q, want pg_stat_activity", got.Name)
	}

	if _, err := ds.GetProbeConfig(ctx, 999999); !errors.Is(err, ErrProbeConfigNotFound) {
		t.Errorf("expected ErrProbeConfigNotFound, got %v", err)
	}
}

// TestUpdateProbeConfig exercises validation, partial-merge and not-found
// branches of UpdateProbeConfig.
func TestUpdateProbeConfig(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	globalID, _ := seedProbeConfigs(t, pool, fx.ConnID)

	// validation
	bad := -1
	if _, err := ds.UpdateProbeConfig(ctx, int64(globalID), ProbeConfigUpdate{
		CollectionIntervalSeconds: &bad,
	}); err == nil {
		t.Error("expected validation error for negative interval")
	}
	if _, err := ds.UpdateProbeConfig(ctx, int64(globalID), ProbeConfigUpdate{
		RetentionDays: &bad,
	}); err == nil {
		t.Error("expected validation error for negative retention")
	}

	// merge: update only retention, leave others intact.
	enabled := false
	interval := 120
	retention := 21
	got, err := ds.UpdateProbeConfig(ctx, int64(globalID), ProbeConfigUpdate{
		IsEnabled:                 &enabled,
		CollectionIntervalSeconds: &interval,
		RetentionDays:             &retention,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.IsEnabled || got.CollectionIntervalSeconds != 120 || got.RetentionDays != 21 {
		t.Errorf("update did not propagate: %+v", got)
	}

	// merge with no fields set just refreshes timestamps.
	if _, err := ds.UpdateProbeConfig(ctx, int64(globalID), ProbeConfigUpdate{}); err != nil {
		t.Fatalf("noop update: %v", err)
	}

	// not found
	if _, err := ds.UpdateProbeConfig(ctx, 99999, ProbeConfigUpdate{}); !errors.Is(err, ErrProbeConfigNotFound) {
		t.Errorf("expected ErrProbeConfigNotFound, got %v", err)
	}
}

// TestAlertRuleQueries exercises GetAlertRules, GetAlertRule (success +
// not-found) and UpdateAlertRule (validation + happy + not-found).
func TestAlertRuleQueries(t *testing.T) {
	ds, pool, _, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	id1 := seedAlertRule(t, pool, "rule-a")
	id2 := seedAlertRule(t, pool, "rule-b")

	rules, err := ds.GetAlertRules(ctx)
	if err != nil {
		t.Fatalf("GetAlertRules: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}

	got, err := ds.GetAlertRule(ctx, id1)
	if err != nil {
		t.Fatalf("GetAlertRule: %v", err)
	}
	if got.Name != "rule-a" {
		t.Errorf("name = %q", got.Name)
	}

	if _, err := ds.GetAlertRule(ctx, 99999); !errors.Is(err, ErrAlertRuleNotFound) {
		t.Errorf("expected ErrAlertRuleNotFound, got %v", err)
	}

	// validation
	badOp := "INVALID"
	if _, err := ds.UpdateAlertRule(ctx, id1, AlertRuleUpdate{DefaultOperator: &badOp}); err == nil {
		t.Error("expected operator validation error")
	}
	badSev := "EXTREME"
	if _, err := ds.UpdateAlertRule(ctx, id1, AlertRuleUpdate{DefaultSeverity: &badSev}); err == nil {
		t.Error("expected severity validation error")
	}

	// happy path - all fields set.
	op := "<"
	thr := 99.9
	sev := "critical"
	en := false
	updated, err := ds.UpdateAlertRule(ctx, id1, AlertRuleUpdate{
		DefaultOperator:  &op,
		DefaultThreshold: &thr,
		DefaultSeverity:  &sev,
		DefaultEnabled:   &en,
	})
	if err != nil {
		t.Fatalf("UpdateAlertRule: %v", err)
	}
	// Threshold is REAL in Postgres (single-precision); compare with
	// a tolerance to absorb the float32 -> float64 rounding.
	if updated.DefaultOperator != "<" ||
		updated.DefaultThreshold < 99.89 || updated.DefaultThreshold > 99.91 ||
		updated.DefaultSeverity != "critical" || updated.DefaultEnabled {
		t.Errorf("update did not propagate: %+v", updated)
	}

	// no-op merge path
	if _, err := ds.UpdateAlertRule(ctx, id2, AlertRuleUpdate{}); err != nil {
		t.Fatalf("noop update: %v", err)
	}

	// not found
	if _, err := ds.UpdateAlertRule(ctx, 99999, AlertRuleUpdate{}); !errors.Is(err, ErrAlertRuleNotFound) {
		t.Errorf("expected ErrAlertRuleNotFound, got %v", err)
	}
}

// TestAlertOverridesAndThresholds exercises GetAlertOverridesFor* at all
// three scopes and UpsertAlertThreshold + DeleteAlertThreshold across
// scopes plus their validation/invalid-scope branches.
func TestAlertOverridesAndThresholds(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := seedAlertRule(t, pool, "rule-x")

	// validation
	if err := ds.UpsertAlertThreshold(ctx, "server", fx.ConnID, ruleID, AlertThresholdUpdate{
		Operator: "BAD", Threshold: 1, Severity: "warning", Enabled: true,
	}); err == nil {
		t.Error("expected operator validation error")
	}
	if err := ds.UpsertAlertThreshold(ctx, "server", fx.ConnID, ruleID, AlertThresholdUpdate{
		Operator: ">", Threshold: 1, Severity: "EXTREME", Enabled: true,
	}); err == nil {
		t.Error("expected severity validation error")
	}
	// invalid scope
	if err := ds.UpsertAlertThreshold(ctx, "weird", 1, ruleID, AlertThresholdUpdate{
		Operator: ">", Threshold: 1, Severity: "info", Enabled: true,
	}); err == nil {
		t.Error("expected invalid scope error")
	}

	// happy path: insert at every scope
	for _, c := range []struct {
		scope string
		id    int
	}{
		{"server", fx.ConnID},
		{"cluster", fx.ClusterID},
		{"group", fx.GroupID},
	} {
		if err := ds.UpsertAlertThreshold(ctx, c.scope, c.id, ruleID, AlertThresholdUpdate{
			Operator: ">=", Threshold: 50, Severity: "info", Enabled: true,
		}); err != nil {
			t.Errorf("upsert %s: %v", c.scope, err)
		}
		// upsert again (update branch)
		if err := ds.UpsertAlertThreshold(ctx, c.scope, c.id, ruleID, AlertThresholdUpdate{
			Operator: ">", Threshold: 75, Severity: "critical", Enabled: false,
		}); err != nil {
			t.Errorf("re-upsert %s: %v", c.scope, err)
		}
	}

	// GetAlertOverridesForServer
	srv, err := ds.GetAlertOverridesForServer(ctx, fx.ConnID)
	if err != nil {
		t.Fatalf("server overrides: %v", err)
	}
	if len(srv) != 1 || !srv[0].HasOverride {
		t.Errorf("server overrides mismatch: %+v", srv)
	}

	// GetAlertOverridesForCluster
	cls, err := ds.GetAlertOverridesForCluster(ctx, fx.ClusterID)
	if err != nil {
		t.Fatalf("cluster overrides: %v", err)
	}
	if len(cls) != 1 || !cls[0].HasOverride {
		t.Errorf("cluster overrides mismatch: %+v", cls)
	}

	// GetAlertOverridesForGroup
	grp, err := ds.GetAlertOverridesForGroup(ctx, fx.GroupID)
	if err != nil {
		t.Fatalf("group overrides: %v", err)
	}
	if len(grp) != 1 || !grp[0].HasOverride {
		t.Errorf("group overrides mismatch: %+v", grp)
	}

	// Connection without overrides surfaces HasOverride=false.
	var connID2 int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES ($1, 'conn-2') RETURNING id`,
		fx.ClusterID).Scan(&connID2); err != nil {
		t.Fatalf("seed conn-2: %v", err)
	}
	noov, err := ds.GetAlertOverridesForServer(ctx, connID2)
	if err != nil {
		t.Fatalf("no-override server: %v", err)
	}
	if len(noov) != 1 || noov[0].HasOverride {
		t.Errorf("expected HasOverride=false: %+v", noov)
	}

	// Delete and confirm.
	if err := ds.DeleteAlertThreshold(ctx, "server", fx.ConnID, ruleID); err != nil {
		t.Errorf("delete server: %v", err)
	}
	if err := ds.DeleteAlertThreshold(ctx, "cluster", fx.ClusterID, ruleID); err != nil {
		t.Errorf("delete cluster: %v", err)
	}
	if err := ds.DeleteAlertThreshold(ctx, "group", fx.GroupID, ruleID); err != nil {
		t.Errorf("delete group: %v", err)
	}
	if err := ds.DeleteAlertThreshold(ctx, "weird", 1, ruleID); err == nil {
		t.Error("expected invalid scope on delete")
	}
}

// TestProbeOverridesAndUpserts exercises every scope branch of
// GetProbeOverridesFor*, UpsertProbeOverride and DeleteProbeOverride
// including validation and invalid-scope branches.
func TestProbeOverridesAndUpserts(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	// Seed a global probe row to drive the LEFT JOIN in the
	// GetProbeOverridesFor* queries.
	seedProbeConfigs(t, pool, fx.ConnID)

	// validation: collection interval and retention must be positive.
	if err := ds.UpsertProbeOverride(ctx, "server", fx.ConnID, "pg_stat_activity",
		ProbeOverrideUpdate{IsEnabled: true, CollectionIntervalSeconds: 0, RetentionDays: 1}); err == nil {
		t.Error("expected interval validation error")
	}
	if err := ds.UpsertProbeOverride(ctx, "server", fx.ConnID, "pg_stat_activity",
		ProbeOverrideUpdate{IsEnabled: true, CollectionIntervalSeconds: 30, RetentionDays: 0}); err == nil {
		t.Error("expected retention validation error")
	}
	if err := ds.UpsertProbeOverride(ctx, "weird", 1, "pg_stat_activity",
		ProbeOverrideUpdate{IsEnabled: true, CollectionIntervalSeconds: 30, RetentionDays: 7}); err == nil {
		t.Error("expected invalid scope error")
	}

	for _, c := range []struct {
		scope string
		id    int
	}{
		{"server", fx.ConnID},
		{"cluster", fx.ClusterID},
		{"group", fx.GroupID},
	} {
		if err := ds.UpsertProbeOverride(ctx, c.scope, c.id, "pg_stat_activity",
			ProbeOverrideUpdate{IsEnabled: true, CollectionIntervalSeconds: 30, RetentionDays: 7}); err != nil {
			t.Errorf("upsert %s: %v", c.scope, err)
		}
		// re-upsert -> update branch
		if err := ds.UpsertProbeOverride(ctx, c.scope, c.id, "pg_stat_activity",
			ProbeOverrideUpdate{IsEnabled: false, CollectionIntervalSeconds: 60, RetentionDays: 14}); err != nil {
			t.Errorf("re-upsert %s: %v", c.scope, err)
		}
	}

	srv, err := ds.GetProbeOverridesForServer(ctx, fx.ConnID)
	if err != nil {
		t.Fatalf("server probes: %v", err)
	}
	if len(srv) == 0 || !srv[0].HasOverride {
		t.Errorf("server probe overrides mismatch: %+v", srv)
	}

	cls, err := ds.GetProbeOverridesForCluster(ctx, fx.ClusterID)
	if err != nil {
		t.Fatalf("cluster probes: %v", err)
	}
	if len(cls) == 0 || !cls[0].HasOverride {
		t.Errorf("cluster probe overrides mismatch: %+v", cls)
	}

	grp, err := ds.GetProbeOverridesForGroup(ctx, fx.GroupID)
	if err != nil {
		t.Fatalf("group probes: %v", err)
	}
	if len(grp) == 0 || !grp[0].HasOverride {
		t.Errorf("group probe overrides mismatch: %+v", grp)
	}

	// Delete each scope; non-existing key delete is a no-op.
	if err := ds.DeleteProbeOverride(ctx, "server", fx.ConnID, "pg_stat_activity"); err != nil {
		t.Errorf("delete server: %v", err)
	}
	if err := ds.DeleteProbeOverride(ctx, "cluster", fx.ClusterID, "pg_stat_activity"); err != nil {
		t.Errorf("delete cluster: %v", err)
	}
	if err := ds.DeleteProbeOverride(ctx, "group", fx.GroupID, "pg_stat_activity"); err != nil {
		t.Errorf("delete group: %v", err)
	}
	if err := ds.DeleteProbeOverride(ctx, "weird", 1, "pg_stat_activity"); err == nil {
		t.Error("expected invalid scope")
	}
}

// TestGetOverrideContext_DirectHierarchy exercises the happy path
// where the connection -> cluster -> group lookup succeeds via the
// direct join (no auto-detection fallback). It then asserts each
// override level surfaces correctly.
func TestGetOverrideContext_DirectHierarchy(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := seedAlertRule(t, pool, "rule-ctx")

	// Insert an override at each scope.
	if err := ds.UpsertAlertThreshold(ctx, "server", fx.ConnID, ruleID, AlertThresholdUpdate{
		Operator: ">", Threshold: 1, Severity: "info", Enabled: true,
	}); err != nil {
		t.Fatalf("server upsert: %v", err)
	}
	if err := ds.UpsertAlertThreshold(ctx, "cluster", fx.ClusterID, ruleID, AlertThresholdUpdate{
		Operator: ">", Threshold: 2, Severity: "warning", Enabled: true,
	}); err != nil {
		t.Fatalf("cluster upsert: %v", err)
	}
	if err := ds.UpsertAlertThreshold(ctx, "group", fx.GroupID, ruleID, AlertThresholdUpdate{
		Operator: ">", Threshold: 3, Severity: "critical", Enabled: true,
	}); err != nil {
		t.Fatalf("group upsert: %v", err)
	}

	got, err := ds.GetOverrideContext(ctx, fx.ConnID, ruleID)
	if err != nil {
		t.Fatalf("GetOverrideContext: %v", err)
	}
	if got.Hierarchy.ConnectionID != fx.ConnID {
		t.Errorf("ConnectionID = %d, want %d", got.Hierarchy.ConnectionID, fx.ConnID)
	}
	if got.Hierarchy.ClusterID == nil || *got.Hierarchy.ClusterID != fx.ClusterID {
		t.Errorf("ClusterID mismatch: %v", got.Hierarchy.ClusterID)
	}
	if got.Hierarchy.GroupID == nil || *got.Hierarchy.GroupID != fx.GroupID {
		t.Errorf("GroupID mismatch: %v", got.Hierarchy.GroupID)
	}
	if got.Overrides["server"] == nil || got.Overrides["server"].Threshold != 1 {
		t.Errorf("server override missing/wrong: %+v", got.Overrides["server"])
	}
	if got.Overrides["cluster"] == nil || got.Overrides["cluster"].Threshold != 2 {
		t.Errorf("cluster override missing/wrong: %+v", got.Overrides["cluster"])
	}
	if got.Overrides["group"] == nil || got.Overrides["group"].Threshold != 3 {
		t.Errorf("group override missing/wrong: %+v", got.Overrides["group"])
	}

	// Connection lookup failure -> error returned.
	if _, err := ds.GetOverrideContext(ctx, 999999, ruleID); err == nil {
		t.Error("expected error for missing connection")
	}

	// Rule lookup failure -> error returned.
	if _, err := ds.GetOverrideContext(ctx, fx.ConnID, 999999); err == nil {
		t.Error("expected error for missing rule")
	}
}

// TestGetOverrideContext_OrphanConnection covers the path where the
// connection has no cluster_id and auto-detection cannot produce a
// hierarchy. The result should still hold the connection info without
// cluster/group overrides.
func TestGetOverrideContext_OrphanConnection(t *testing.T) {
	ds, pool, _, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := seedAlertRule(t, pool, "rule-orphan")

	// Insert a connection with NULL cluster_id (orphan).
	var orphanID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES (NULL, 'orphan') RETURNING id`).Scan(&orphanID); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	got, err := ds.GetOverrideContext(ctx, orphanID, ruleID)
	if err != nil {
		t.Fatalf("GetOverrideContext: %v", err)
	}
	if got.Hierarchy.ConnectionID != orphanID {
		t.Errorf("ConnectionID mismatch")
	}
	if got.Hierarchy.ClusterID != nil {
		t.Errorf("expected ClusterID nil, got %v", got.Hierarchy.ClusterID)
	}
	if got.Overrides["server"] != nil {
		t.Errorf("server override expected nil")
	}
	if got.Overrides["cluster"] != nil {
		t.Errorf("cluster override expected nil")
	}
	if got.Overrides["group"] != nil {
		t.Errorf("group override expected nil")
	}
}

// TestResolveConnectionHierarchy_NoMatch covers the resolveConnection
// Hierarchy branch that returns all-nil when no auto-detected cluster
// contains the target connection. We trigger this via
// GetOverrideContext on an orphan connection: the direct join returns
// hierarchy.ClusterID=nil, then the auto-detection pass also fails,
// leaving the cluster/group fields nil.
func TestResolveConnectionHierarchy_NoMatch(t *testing.T) {
	ds, pool, _, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := seedAlertRule(t, pool, "rule-noauto")

	// Orphan connection with no metrics -> auto-detection produces
	// no clusters, so resolveConnectionHierarchy returns all-nils.
	var connID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES (NULL, 'no-auto') RETURNING id`).Scan(&connID); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	got, err := ds.GetOverrideContext(ctx, connID, ruleID)
	if err != nil {
		t.Fatalf("GetOverrideContext: %v", err)
	}
	if got.Hierarchy.ClusterID != nil {
		t.Errorf("expected ClusterID nil, got %v", got.Hierarchy.ClusterID)
	}
}

// TestResolveConnectionHierarchy_AutoDetectInsert drives
// resolveConnectionHierarchy through the auto-detection happy path:
// a connection with no cluster_id is grouped into a Spock cluster by
// auto-detection, the clusters table has no matching auto_cluster_key,
// and the function inserts a new row, then re-selects it. This test
// covers the ErrNoRows -> INSERT branch in resolveConnectionHierarchy.
func TestResolveConnectionHierarchy_AutoDetectInsert(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	ruleID := seedAlertRule(t, pool, "rule-auto")

	// Insert two spock-detected connections sharing the prefix "pg17".
	// We update the existing fx.ConnID with proper host info, then add
	// a sibling. The auto-detector groups them into spock:pg17.
	if _, err := pool.Exec(ctx, `
        UPDATE connections SET name='pg17-n1', host='10.0.0.1', port=5432,
            cluster_id=NULL, role='primary'
        WHERE id = $1`, fx.ConnID); err != nil {
		t.Fatalf("update conn-1: %v", err)
	}

	var sibID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO connections (name, host, port, role, cluster_id)
        VALUES ('pg17-n2', '10.0.0.2', 5432, 'primary', NULL)
        RETURNING id`).Scan(&sibID); err != nil {
		t.Fatalf("insert sibling: %v", err)
	}

	// Seed metrics required by getAllConnectionsWithRoles for both
	// connections so the auto-detector sees recent data.
	for _, cid := range []int{fx.ConnID, sibID} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO metrics.pg_connectivity (connection_id) VALUES ($1)`, cid); err != nil {
			t.Fatalf("connectivity: %v", err)
		}
		if _, err := pool.Exec(ctx, `
            INSERT INTO metrics.pg_node_role
                (connection_id, primary_role, has_spock, spock_node_name)
            VALUES ($1, 'spock', TRUE, $2)`,
			cid, "n"+string(rune('0'+cid))); err != nil {
			t.Fatalf("node role: %v", err)
		}
	}

	got, err := ds.GetOverrideContext(ctx, fx.ConnID, ruleID)
	if err != nil {
		t.Fatalf("GetOverrideContext: %v", err)
	}
	if got.Hierarchy.ClusterID == nil {
		t.Fatalf("expected ClusterID populated by auto-detection, got nil")
	}

	// A new clusters row with auto_cluster_key='spock:pg17' should
	// have been inserted by resolveConnectionHierarchy.
	var inserted int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM clusters WHERE auto_cluster_key = 'spock:pg17'`,
	).Scan(&inserted); err != nil {
		t.Fatalf("expected inserted cluster row: %v", err)
	}
	if *got.Hierarchy.ClusterID != inserted {
		t.Errorf("ClusterID = %d, want inserted=%d", *got.Hierarchy.ClusterID, inserted)
	}

	// Now update the dismissed flag on the inserted cluster to TRUE
	// and call again. resolveConnectionHierarchy must return all-nil
	// (case (c) in the function's comment).
	if _, err := pool.Exec(ctx,
		`UPDATE clusters SET dismissed = TRUE WHERE id = $1`, inserted); err != nil {
		t.Fatalf("dismiss cluster: %v", err)
	}
	got2, err := ds.GetOverrideContext(ctx, fx.ConnID, ruleID)
	if err != nil {
		t.Fatalf("GetOverrideContext (dismissed): %v", err)
	}
	if got2.Hierarchy.ClusterID != nil {
		t.Errorf("expected ClusterID nil after dismissal, got %v", got2.Hierarchy.ClusterID)
	}

	// Undismiss and call once more to exercise the live-row path
	// (no INSERT, no dismissed branch).
	if _, err := pool.Exec(ctx,
		`UPDATE clusters SET dismissed = FALSE WHERE id = $1`, inserted); err != nil {
		t.Fatalf("undismiss: %v", err)
	}
	got3, err := ds.GetOverrideContext(ctx, fx.ConnID, ruleID)
	if err != nil {
		t.Fatalf("GetOverrideContext (live): %v", err)
	}
	if got3.Hierarchy.ClusterID == nil || *got3.Hierarchy.ClusterID != inserted {
		t.Errorf("expected ClusterID=%d on live path, got %v",
			inserted, got3.Hierarchy.ClusterID)
	}
}

// TestProbeConfigErrorPaths forces SQL failures by dropping the
// underlying tables and confirming each helper bubbles up an error.
func TestProbeConfigErrorPaths(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	// Drop probe_configs to force GetProbeConfigs/Get/Update errors.
	if _, err := pool.Exec(ctx, `DROP TABLE probe_configs`); err != nil {
		t.Fatalf("drop probe_configs: %v", err)
	}

	if _, err := ds.GetProbeConfigs(ctx, nil); err == nil {
		t.Error("expected error from GetProbeConfigs (global)")
	}
	if _, err := ds.GetProbeConfigs(ctx, &fx.ConnID); err == nil {
		t.Error("expected error from GetProbeConfigs (scoped)")
	}
	if _, err := ds.GetProbeConfig(ctx, 1); err == nil {
		t.Error("expected error from GetProbeConfig")
	}
	one := 1
	if _, err := ds.UpdateProbeConfig(ctx, 1, ProbeConfigUpdate{
		CollectionIntervalSeconds: &one,
	}); err == nil {
		t.Error("expected error from UpdateProbeConfig")
	}
	if _, err := ds.GetProbeOverridesForServer(ctx, fx.ConnID); err == nil {
		t.Error("expected error from GetProbeOverridesForServer")
	}
	if _, err := ds.GetProbeOverridesForCluster(ctx, fx.ClusterID); err == nil {
		t.Error("expected error from GetProbeOverridesForCluster")
	}
	if _, err := ds.GetProbeOverridesForGroup(ctx, fx.GroupID); err == nil {
		t.Error("expected error from GetProbeOverridesForGroup")
	}
	if err := ds.UpsertProbeOverride(ctx, "server", fx.ConnID, "x",
		ProbeOverrideUpdate{IsEnabled: true, CollectionIntervalSeconds: 1, RetentionDays: 1}); err == nil {
		t.Error("expected error from UpsertProbeOverride")
	}
	if err := ds.DeleteProbeOverride(ctx, "server", fx.ConnID, "x"); err == nil {
		t.Error("expected error from DeleteProbeOverride")
	}
}

// TestAlertRuleErrorPaths forces SQL failures for the alert rule and
// alert threshold helpers.
func TestAlertRuleErrorPaths(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE alert_thresholds`); err != nil {
		t.Fatalf("drop alert_thresholds: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE alert_rules`); err != nil {
		t.Fatalf("drop alert_rules: %v", err)
	}

	if _, err := ds.GetAlertRules(ctx); err == nil {
		t.Error("expected error from GetAlertRules")
	}
	if _, err := ds.GetAlertRule(ctx, 1); err == nil {
		t.Error("expected error from GetAlertRule")
	}
	if _, err := ds.UpdateAlertRule(ctx, 1, AlertRuleUpdate{}); err == nil {
		t.Error("expected error from UpdateAlertRule")
	}
	if _, err := ds.GetAlertOverridesForServer(ctx, fx.ConnID); err == nil {
		t.Error("expected error from GetAlertOverridesForServer")
	}
	if _, err := ds.GetAlertOverridesForCluster(ctx, fx.ClusterID); err == nil {
		t.Error("expected error from GetAlertOverridesForCluster")
	}
	if _, err := ds.GetAlertOverridesForGroup(ctx, fx.GroupID); err == nil {
		t.Error("expected error from GetAlertOverridesForGroup")
	}
	if err := ds.UpsertAlertThreshold(ctx, "server", fx.ConnID, 1,
		AlertThresholdUpdate{Operator: ">", Threshold: 1, Severity: "info", Enabled: true}); err == nil {
		t.Error("expected error from UpsertAlertThreshold")
	}
	if err := ds.DeleteAlertThreshold(ctx, "server", fx.ConnID, 1); err == nil {
		t.Error("expected error from DeleteAlertThreshold")
	}
}
