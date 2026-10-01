/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// overrideHandlersTestSchema creates the tables the probe, alert and
// channel override handlers read and write. It mirrors the production
// shape (collector/src/database/schema.go), limited to the columns those
// queries reference.
const overrideHandlersTestSchema = `
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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
    default_operator TEXT NOT NULL,
    default_threshold REAL NOT NULL,
    default_severity TEXT NOT NULL,
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

CREATE TABLE notification_channels (
    id BIGSERIAL PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    channel_type TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    is_estate_default BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE notification_channel_overrides (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('group', 'cluster', 'server')),
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE UNIQUE INDEX idx_nco_unique_server
    ON notification_channel_overrides(channel_id, connection_id)
    WHERE scope = 'server';
CREATE UNIQUE INDEX idx_nco_unique_cluster
    ON notification_channel_overrides(channel_id, cluster_id)
    WHERE scope = 'cluster';
CREATE UNIQUE INDEX idx_nco_unique_group
    ON notification_channel_overrides(channel_id, group_id)
    WHERE scope = 'group';
`

const overrideHandlersTestTeardown = `
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// overrideFixture holds the ids of the rows seeded for the override
// handler tests.
type overrideFixture struct {
	groupID, clusterID, connID int
	ruleID, channelID          int64
}

// newOverrideHandlersTestDatastore wires up a *database.Datastore against
// the TEST_AI_WORKBENCH_SERVER Postgres instance, seeds one group,
// cluster, connection, probe, alert rule and channel, and returns a
// cleanup that drops the schema and closes the pool.
func newOverrideHandlersTestDatastore(t *testing.T) (*database.Datastore, *pgxpool.Pool, overrideFixture, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping override handler integration test")
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

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), overrideHandlersTestTeardown); err != nil {
			t.Logf("override handlers teardown failed: %v", err)
		}
		pool.Close()
	}

	if _, err := pool.Exec(ctx, overrideHandlersTestSchema); err != nil {
		cleanup()
		t.Fatalf("Failed to create override handlers test schema: %v", err)
	}

	var f overrideFixture
	seed := []struct {
		sql  string
		args func() []any
		dest any
	}{
		{"INSERT INTO cluster_groups (name) VALUES ('Group') RETURNING id",
			func() []any { return nil }, &f.groupID},
		{"INSERT INTO clusters (group_id, name) VALUES ($1, 'Cluster') RETURNING id",
			func() []any { return []any{f.groupID} }, &f.clusterID},
		{"INSERT INTO connections (name, cluster_id) VALUES ('conn', $1) RETURNING id",
			func() []any { return []any{f.clusterID} }, &f.connID},
		{`INSERT INTO alert_rules (name, description, category, metric_name,
              default_operator, default_threshold, default_severity)
          VALUES ('high_cpu', 'CPU is high', 'system', 'cpu', '>', 90, 'warning')
          RETURNING id`,
			func() []any { return nil }, &f.ruleID},
		{`INSERT INTO notification_channels (channel_type, name, description)
          VALUES ('webhook', 'Hook', 'test hook') RETURNING id`,
			func() []any { return nil }, &f.channelID},
	}
	for _, s := range seed {
		if err := pool.QueryRow(ctx, s.sql, s.args()...).Scan(s.dest); err != nil {
			cleanup()
			t.Fatalf("Seed %q: %v", s.sql, err)
		}
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO probe_configs (name, description) VALUES ('pg_stat_activity', 'activity')
    `); err != nil {
		cleanup()
		t.Fatalf("Seed probe: %v", err)
	}

	return database.NewTestDatastore(pool), pool, f, cleanup
}

// overrideRequest serves one superuser request through the handler's
// registered routes and returns the recorder.
func overrideRequest(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req = withSuperuser(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// overrideMux registers a handler's routes on a fresh mux with a no-op
// auth wrapper.
func overrideMux(register func(*http.ServeMux, func(http.HandlerFunc) http.HandlerFunc)) *http.ServeMux {
	mux := http.NewServeMux()
	register(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	return mux
}

// scopeIDs maps each override scope to its seeded id.
func (f overrideFixture) scopeIDs() map[string]int {
	return map[string]int{"server": f.connID, "cluster": f.clusterID, "group": f.groupID}
}

// TestProbeOverrideHandler_Datastore drives list, upsert and delete for
// every scope against a real datastore, then the failure paths once the
// probe_configs table is gone.
func TestProbeOverrideHandler_Datastore(t *testing.T) {
	ds, pool, f, cleanup := newOverrideHandlersTestDatastore(t)
	defer cleanup()
	_, store, storeCleanup := createTestRBACHandler(t)
	defer storeCleanup()

	h := NewProbeOverrideHandler(ds, store, auth.NewRBACChecker(store))
	mux := overrideMux(h.RegisterRoutes)

	for scope, id := range f.scopeIDs() {
		t.Run(scope, func(t *testing.T) {
			base := fmt.Sprintf("/api/v1/probe-overrides/%s/%d", scope, id)

			rec := overrideRequest(mux, http.MethodPut, base+"/pg_stat_activity",
				`{"is_enabled":false,"collection_interval_seconds":30,"retention_days":7}`)
			assertStatus(t, rec, http.StatusOK)

			rec = overrideRequest(mux, http.MethodGet, base, "")
			assertStatus(t, rec, http.StatusOK)
			var overrides []database.ProbeOverride
			if err := json.Unmarshal(rec.Body.Bytes(), &overrides); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(overrides) != 1 || !overrides[0].HasOverride ||
				overrides[0].OverrideIntervalSeconds == nil ||
				*overrides[0].OverrideIntervalSeconds != 30 {
				t.Errorf("Unexpected overrides: %+v", overrides)
			}

			rec = overrideRequest(mux, http.MethodPut, base+"/pg_stat_activity",
				`{"is_enabled":true,"collection_interval_seconds":0,"retention_days":7}`)
			assertStatus(t, rec, http.StatusBadRequest)

			rec = overrideRequest(mux, http.MethodPut, base+"/pg_stat_activity", `{`)
			assertStatus(t, rec, http.StatusBadRequest)

			rec = overrideRequest(mux, http.MethodDelete, base+"/pg_stat_activity", "")
			assertStatus(t, rec, http.StatusOK)
		})
	}

	if _, err := pool.Exec(context.Background(), "DROP TABLE probe_configs CASCADE"); err != nil {
		t.Fatalf("Drop probe_configs: %v", err)
	}
	for scope, id := range f.scopeIDs() {
		base := fmt.Sprintf("/api/v1/probe-overrides/%s/%d", scope, id)
		assertStatus(t, overrideRequest(mux, http.MethodGet, base, ""),
			http.StatusInternalServerError)
		assertStatus(t, overrideRequest(mux, http.MethodDelete, base+"/pg_stat_activity", ""),
			http.StatusInternalServerError)
	}
}

// TestProbeOverrideHandler_Routing covers the URL parsing and method
// checks, which return before the datastore is used.
func TestProbeOverrideHandler_Routing(t *testing.T) {
	_, store, storeCleanup := createTestRBACHandler(t)
	defer storeCleanup()

	h := NewProbeOverrideHandler(&database.Datastore{}, store, auth.NewRBACChecker(store))
	if h.checkPermission == nil {
		t.Fatal("checkPermission not set with a non-nil RBAC checker")
	}
	mux := overrideMux(h.RegisterRoutes)

	tests := []struct {
		name, method, path string
		want               int
	}{
		{"missing scope id", http.MethodGet, "/api/v1/probe-overrides/server", http.StatusNotFound},
		{"empty scope id", http.MethodGet, "/api/v1/probe-overrides/server/", http.StatusNotFound},
		{"invalid scope", http.MethodGet, "/api/v1/probe-overrides/bogus/1", http.StatusBadRequest},
		{"invalid scope id", http.MethodGet, "/api/v1/probe-overrides/server/abc", http.StatusBadRequest},
		{"list wrong method", http.MethodPost, "/api/v1/probe-overrides/server/1", http.StatusMethodNotAllowed},
		{"empty probe name", http.MethodPut, "/api/v1/probe-overrides/server/1/", http.StatusBadRequest},
		{"probe wrong method", http.MethodPost, "/api/v1/probe-overrides/server/1/p", http.StatusMethodNotAllowed},
		{"too many segments", http.MethodGet, "/api/v1/probe-overrides/server/1/p/x", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertStatus(t, overrideRequest(mux, tt.method, tt.path, ""), tt.want)
		})
	}

	t.Run("not configured", func(t *testing.T) {
		mux := overrideMux(NewProbeOverrideHandler(nil, nil, nil).RegisterRoutes)
		assertStatus(t, overrideRequest(mux, http.MethodGet, "/api/v1/probe-overrides/server/1", ""),
			http.StatusServiceUnavailable)
	})

	t.Run("permission denied", func(t *testing.T) {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			req := httptest.NewRequest(method, "/api/v1/probe-overrides/server/1/p",
				strings.NewReader(`{}`))
			req = withUser(req, 999)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			assertStatus(t, rec, http.StatusForbidden)
		}
	})
}

// TestAlertOverrideHandler_Datastore drives list, upsert, delete and the
// override context against a real datastore, then the failure paths
// once the alert tables are gone.
func TestAlertOverrideHandler_Datastore(t *testing.T) {
	ds, pool, f, cleanup := newOverrideHandlersTestDatastore(t)
	defer cleanup()
	_, store, storeCleanup := createTestRBACHandler(t)
	defer storeCleanup()

	h := NewAlertOverrideHandler(ds, store, auth.NewRBACChecker(store))
	mux := overrideMux(h.RegisterRoutes)

	for scope, id := range f.scopeIDs() {
		t.Run(scope, func(t *testing.T) {
			base := fmt.Sprintf("/api/v1/alert-overrides/%s/%d", scope, id)
			rule := fmt.Sprintf("%s/%d", base, f.ruleID)

			rec := overrideRequest(mux, http.MethodPut, rule,
				`{"operator":">=","threshold":75,"severity":"critical","enabled":true}`)
			assertStatus(t, rec, http.StatusOK)

			rec = overrideRequest(mux, http.MethodGet, base, "")
			assertStatus(t, rec, http.StatusOK)
			var overrides []database.AlertOverride
			if err := json.Unmarshal(rec.Body.Bytes(), &overrides); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(overrides) != 1 || !overrides[0].HasOverride {
				t.Errorf("Unexpected overrides: %+v", overrides)
			}
		})
	}

	t.Run("context", func(t *testing.T) {
		rec := overrideRequest(mux, http.MethodGet,
			fmt.Sprintf("/api/v1/alert-overrides/context/%d/%d", f.connID, f.ruleID), "")
		assertStatus(t, rec, http.StatusOK)
		var oc database.OverrideContext
		if err := json.Unmarshal(rec.Body.Bytes(), &oc); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		for _, scope := range []string{"server", "cluster", "group"} {
			if oc.Overrides[scope] == nil {
				t.Errorf("Missing %s override in context", scope)
			}
		}

		rec = overrideRequest(mux, http.MethodGet,
			fmt.Sprintf("/api/v1/alert-overrides/context/%d/%d", f.connID+1000, f.ruleID), "")
		assertStatus(t, rec, http.StatusInternalServerError)
	})

	for scope, id := range f.scopeIDs() {
		rule := fmt.Sprintf("/api/v1/alert-overrides/%s/%d/%d", scope, id, f.ruleID)
		assertStatus(t, overrideRequest(mux, http.MethodDelete, rule, ""), http.StatusOK)
	}

	if _, err := pool.Exec(context.Background(),
		"DROP TABLE alert_thresholds CASCADE; DROP TABLE alert_rules CASCADE"); err != nil {
		t.Fatalf("Drop alert tables: %v", err)
	}
	for scope, id := range f.scopeIDs() {
		base := fmt.Sprintf("/api/v1/alert-overrides/%s/%d", scope, id)
		rule := fmt.Sprintf("%s/%d", base, f.ruleID)
		assertStatus(t, overrideRequest(mux, http.MethodGet, base, ""),
			http.StatusInternalServerError)
		assertStatus(t, overrideRequest(mux, http.MethodPut, rule,
			`{"operator":">","threshold":1,"severity":"info","enabled":true}`),
			http.StatusBadRequest)
		assertStatus(t, overrideRequest(mux, http.MethodDelete, rule, ""),
			http.StatusInternalServerError)
	}
}

// TestChannelOverrideHandler_Datastore drives list, upsert and delete
// for every scope against a real datastore, then the failure paths once
// the channel tables are gone.
func TestChannelOverrideHandler_Datastore(t *testing.T) {
	ds, pool, f, cleanup := newOverrideHandlersTestDatastore(t)
	defer cleanup()
	_, store, storeCleanup := createTestRBACHandler(t)
	defer storeCleanup()

	h := NewChannelOverrideHandler(ds, store, auth.NewRBACChecker(store))
	mux := overrideMux(h.RegisterRoutes)

	for scope, id := range f.scopeIDs() {
		t.Run(scope, func(t *testing.T) {
			base := fmt.Sprintf("/api/v1/channel-overrides/%s/%d", scope, id)
			channel := fmt.Sprintf("%s/%d", base, f.channelID)

			assertStatus(t, overrideRequest(mux, http.MethodPut, channel, `{"enabled":false}`),
				http.StatusOK)

			rec := overrideRequest(mux, http.MethodGet, base, "")
			assertStatus(t, rec, http.StatusOK)
			var overrides []database.ChannelOverride
			if err := json.Unmarshal(rec.Body.Bytes(), &overrides); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(overrides) != 1 || !overrides[0].HasOverride ||
				overrides[0].OverrideEnabled == nil || *overrides[0].OverrideEnabled {
				t.Errorf("Unexpected overrides: %+v", overrides)
			}

			assertStatus(t, overrideRequest(mux, http.MethodPut, channel, `{`),
				http.StatusBadRequest)
			assertStatus(t, overrideRequest(mux, http.MethodDelete, channel, ""),
				http.StatusOK)
		})
	}

	if _, err := pool.Exec(context.Background(),
		"DROP TABLE notification_channel_overrides CASCADE; DROP TABLE notification_channels CASCADE"); err != nil {
		t.Fatalf("Drop channel tables: %v", err)
	}
	for scope, id := range f.scopeIDs() {
		base := fmt.Sprintf("/api/v1/channel-overrides/%s/%d", scope, id)
		channel := fmt.Sprintf("%s/%d", base, f.channelID)
		assertStatus(t, overrideRequest(mux, http.MethodGet, base, ""),
			http.StatusInternalServerError)
		assertStatus(t, overrideRequest(mux, http.MethodPut, channel, `{"enabled":true}`),
			http.StatusBadRequest)
		assertStatus(t, overrideRequest(mux, http.MethodDelete, channel, ""),
			http.StatusInternalServerError)
	}
}
