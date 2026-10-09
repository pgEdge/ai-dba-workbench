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
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Connection scope on probe configs, alert rules, overrides and
// notification channels (issue #471)
//
// A probe config names one connection or none; an alert rule's defaults,
// and a notification channel and its recipients, reach every connection.
// These tests check each gate against the shared callers and exercise
// the handlers behind it against a seeded schema.
// =============================================================================

// estateGatesSchema is the subset of the production schema the probe
// config, alert rule and override handlers read and write, as in the
// database package's config and notification integration tests. The
// channel overrides table omits its foreign key to notification_channels,
// which these tests do not create.
const estateGatesSchema = `
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
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
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL
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

CREATE TABLE notification_channel_overrides (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('group', 'cluster', 'server')),
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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

INSERT INTO cluster_groups (id, name) VALUES (1, 'g');
INSERT INTO clusters (id, group_id, name) VALUES (1, 1, 'c');
INSERT INTO connections (id, cluster_id, name)
    VALUES (5, 1, 'five'), (6, 1, 'six'), (7, 1, 'seven');

INSERT INTO probe_configs (id, connection_id, name, description, scope) VALUES
    (1, NULL, 'pg_stat_activity', 'global', 'global'),
    (2, 5, 'pg_stat_activity', 'five', 'server'),
    (3, 6, 'pg_stat_activity', 'six', 'server');

INSERT INTO alert_rules (id, name, description, category, metric_name,
                         default_operator, default_threshold,
                         default_severity) VALUES
    (1, 'rule', 'd', 'cat', 'metric', '>', 10, 'warning');

SELECT setval('probe_configs_id_seq', 100);
SELECT setval('alert_rules_id_seq', 100);
`

const estateGatesTeardown = `
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// estateGatesDropAll removes every table the handlers under test use,
// so that each of their queries fails.
const estateGatesDropAll = `
DROP TABLE notification_channel_overrides CASCADE;
DROP TABLE alert_thresholds CASCADE;
DROP TABLE alert_rules CASCADE;
DROP TABLE probe_configs CASCADE;
`

// estateGates holds the handlers under test, over one seeded schema.
type estateGates struct {
	pool          *pgxpool.Pool
	probeConfig   *ProbeConfigHandler
	alertRule     *AlertRuleHandler
	alertOverride *AlertOverrideHandler
	probeOverride *ProbeOverrideHandler
	chanOverride  *ChannelOverrideHandler
	callers       map[string]scopeCaller
}

// newEstateGates seeds estateGatesSchema and builds the handlers over it
// with the shared callers, plus the anonymous caller.
func newEstateGates(t *testing.T) *estateGates {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping datastore integration test")
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
	if _, err := pool.Exec(ctx, estateGatesSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create test schema: %v", err)
	}

	_, store, cleanup := createTestRBACHandler(t)
	t.Cleanup(func() {
		cleanup()
		_, _ = pool.Exec(context.Background(), estateGatesTeardown)
		pool.Close()
	})

	session, unscoped, wildcard, narrowed, readOnly := scopedCallers(t, store)
	ds := database.NewTestDatastore(pool)
	checker := auth.NewRBACChecker(store)
	return &estateGates{
		pool:          pool,
		probeConfig:   NewProbeConfigHandler(ds, store, checker),
		alertRule:     NewAlertRuleHandler(ds, store, checker),
		alertOverride: NewAlertOverrideHandler(ds, store, checker),
		probeOverride: NewProbeOverrideHandler(ds, store, checker),
		chanOverride:  NewChannelOverrideHandler(ds, store, checker),
		callers: map[string]scopeCaller{
			"session": session, "unscoped": unscoped, "wildcard": wildcard,
			"narrowed": narrowed, "readOnly": readOnly,
			"anonymous": anonymousCaller,
		},
	}
}

// probeConfigEnabled reports whether the probe config is enabled.
func probeConfigEnabled(t *testing.T, pool *pgxpool.Pool, id int64) bool {
	t.Helper()
	var enabled bool
	if err := pool.QueryRow(context.Background(),
		"SELECT is_enabled FROM probe_configs WHERE id = $1",
		id).Scan(&enabled); err != nil {
		t.Fatalf("Failed to read probe config %d: %v", id, err)
	}
	return enabled
}

// alertRuleThreshold returns the alert rule's default threshold.
func alertRuleThreshold(t *testing.T, pool *pgxpool.Pool, id int64) float64 {
	t.Helper()
	var threshold float64
	if err := pool.QueryRow(context.Background(),
		"SELECT default_threshold FROM alert_rules WHERE id = $1",
		id).Scan(&threshold); err != nil {
		t.Fatalf("Failed to read alert rule %d: %v", id, err)
	}
	return threshold
}

// TestUpdateProbeConfigRespectsTokenScope covers the probe config gate:
// a config on one connection needs that connection at read_write, and a
// global config needs every connection.
func TestUpdateProbeConfigRespectsTokenScope(t *testing.T) {
	const disable = `{"is_enabled":false}`
	cases := []struct {
		name   string
		id     int64
		caller string
		body   string
		drop   bool
		want   int
	}{
		{"server config narrowed", 2, "narrowed", disable, false, http.StatusOK},
		{"server config wildcard", 2, "wildcard", disable, false, http.StatusOK},
		{"server config read-only", 2, "readOnly", disable, false, http.StatusForbidden},
		{"other server config narrowed", 3, "narrowed", disable, false, http.StatusForbidden},
		{"global config narrowed", 1, "narrowed", disable, false, http.StatusForbidden},
		{"global config read-only", 1, "readOnly", disable, false, http.StatusForbidden},
		{"global config unscoped", 1, "unscoped", disable, false, http.StatusOK},
		{"global config session", 1, "session", disable, false, http.StatusOK},
		{"missing config narrowed", 999, "narrowed", disable, false, http.StatusNotFound},
		{"missing config session", 999, "session", disable, false, http.StatusNotFound},
		{"lookup fails", 2, "narrowed", disable, true, http.StatusInternalServerError},
		{"bad body", 2, "session", `{`, false, http.StatusBadRequest},
		{"invalid interval", 2, "narrowed",
			`{"collection_interval_seconds":0}`, false, http.StatusBadRequest},
		{"no permission", 2, "anonymous", disable, false, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newEstateGates(t)
			if tc.drop {
				mustExec(t, g.pool, estateGatesDropAll)
			}
			rec := httptest.NewRecorder()
			g.probeConfig.updateProbeConfig(rec, newScopeRequest(
				g.callers[tc.caller], http.MethodPut, tc.body), tc.id)

			expectStatus(t, rec, tc.want)
			if tc.want == http.StatusForbidden && tc.caller != "anonymous" {
				assertOutOfTokenScope(t, rec)
				if !probeConfigEnabled(t, g.pool, tc.id) {
					t.Errorf("Expected probe config %d to survive a refusal", tc.id)
				}
			}
			if tc.want == http.StatusOK && probeConfigEnabled(t, g.pool, tc.id) {
				t.Errorf("Expected probe config %d to be disabled", tc.id)
			}
		})
	}
}

// TestProbeConfigFastPath checks that a caller covering every connection
// skips the probe config lookup, so a nil datastore is never reached.
func TestProbeConfigFastPath(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	session, unscoped, wildcard, _, _ := scopedCallers(t, store)
	h := NewProbeConfigHandler(nil, store, auth.NewRBACChecker(store))

	for _, caller := range []scopeCaller{session, unscoped, wildcard} {
		t.Run(caller.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := newScopeRequest(caller, http.MethodPut, "")
			if !h.requireProbeConfigInTokenScope(rec, r, 1) {
				t.Errorf("Expected the probe config check to pass: %s",
					rec.Body.String())
			}
		})
	}
}

// TestProbeConfigReads covers listing and fetching probe configs.
func TestProbeConfigReads(t *testing.T) {
	cases := []struct {
		name  string
		query string
		id    int64
		drop  bool
		want  int
	}{
		{"list all", "", 0, false, http.StatusOK},
		{"list for connection", "?connection_id=5", 0, false, http.StatusOK},
		{"list bad connection", "?connection_id=x", 0, false, http.StatusBadRequest},
		{"list fails", "", 0, true, http.StatusInternalServerError},
		{"get", "", 2, false, http.StatusOK},
		{"get missing", "", 999, false, http.StatusNotFound},
		{"get fails", "", 2, true, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newEstateGates(t)
			if tc.drop {
				mustExec(t, g.pool, estateGatesDropAll)
			}
			rec := httptest.NewRecorder()
			req := withSuperuser(httptest.NewRequest(http.MethodGet,
				"/"+tc.query, nil))
			if tc.id == 0 {
				g.probeConfig.listProbeConfigs(rec, req)
			} else {
				g.probeConfig.getProbeConfig(rec, req, tc.id)
			}
			expectStatus(t, rec, tc.want)
		})
	}
}

// TestUpdateAlertRuleRespectsTokenScope covers the alert rule gate: an
// alert rule's defaults reach every connection, so only a caller
// covering every connection may change one.
func TestUpdateAlertRuleRespectsTokenScope(t *testing.T) {
	const raise = `{"default_threshold":20}`
	cases := []struct {
		name   string
		id     int64
		caller string
		body   string
		want   int
	}{
		{"session", 1, "session", raise, http.StatusOK},
		{"unscoped", 1, "unscoped", raise, http.StatusOK},
		{"wildcard", 1, "wildcard", raise, http.StatusOK},
		{"narrowed", 1, "narrowed", raise, http.StatusForbidden},
		{"read-only", 1, "readOnly", raise, http.StatusForbidden},
		{"no permission", 1, "anonymous", raise, http.StatusForbidden},
		{"bad body", 1, "session", `{`, http.StatusBadRequest},
		{"invalid operator", 1, "session", `{"default_operator":"~"}`, http.StatusBadRequest},
		{"missing rule", 999, "wildcard", raise, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newEstateGates(t)
			rec := httptest.NewRecorder()
			g.alertRule.updateAlertRule(rec, newScopeRequest(
				g.callers[tc.caller], http.MethodPut, tc.body), tc.id)

			expectStatus(t, rec, tc.want)
			if tc.want == http.StatusForbidden && tc.caller != "anonymous" {
				assertOutOfTokenScope(t, rec)
				if got := alertRuleThreshold(t, g.pool, tc.id); got != 10 {
					t.Errorf("Expected the threshold to stay 10, got %v", got)
				}
			}
			if tc.want == http.StatusOK {
				if got := alertRuleThreshold(t, g.pool, tc.id); got != 20 {
					t.Errorf("Expected the threshold to become 20, got %v", got)
				}
			}
		})
	}
}

// TestAlertRuleReads covers listing and fetching alert rules.
func TestAlertRuleReads(t *testing.T) {
	cases := []struct {
		name string
		id   int64
		drop bool
		want int
	}{
		{"list", 0, false, http.StatusOK},
		{"list fails", 0, true, http.StatusInternalServerError},
		{"get", 1, false, http.StatusOK},
		{"get missing", 999, false, http.StatusNotFound},
		{"get fails", 1, true, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newEstateGates(t)
			if tc.drop {
				mustExec(t, g.pool, estateGatesDropAll)
			}
			rec := httptest.NewRecorder()
			req := withSuperuser(httptest.NewRequest(http.MethodGet, "/", nil))
			if tc.id == 0 {
				g.alertRule.listAlertRules(rec, req)
			} else {
				g.alertRule.getAlertRule(rec, req, tc.id)
			}
			expectStatus(t, rec, tc.want)
		})
	}
}

// TestOverrideWritesAgainstDatastore covers upsert and delete on the
// alert, probe and channel override handlers past the scope gate: a
// successful write, a malformed body, a datastore refusal and a failed
// query.
func TestOverrideWritesAgainstDatastore(t *testing.T) {
	type write func(g *estateGates, w http.ResponseWriter, r *http.Request,
		scope string, scopeID int)

	const alertBody = `{"operator":">","threshold":1,"severity":"warning","enabled":true}`
	const probeBody = `{"is_enabled":false,"collection_interval_seconds":30,"retention_days":7}`
	const channelBody = `{"enabled":false}`

	alertUpsert := func(g *estateGates, w http.ResponseWriter, r *http.Request, s string, id int) {
		g.alertOverride.upsertOverride(w, r, s, id, 1)
	}
	alertDelete := func(g *estateGates, w http.ResponseWriter, r *http.Request, s string, id int) {
		g.alertOverride.deleteOverride(w, r, s, id, 1)
	}
	probeUpsert := func(g *estateGates, w http.ResponseWriter, r *http.Request, s string, id int) {
		g.probeOverride.upsertOverride(w, r, s, id, "pg_stat_activity")
	}
	probeDelete := func(g *estateGates, w http.ResponseWriter, r *http.Request, s string, id int) {
		g.probeOverride.deleteOverride(w, r, s, id, "pg_stat_activity")
	}
	channelUpsert := func(g *estateGates, w http.ResponseWriter, r *http.Request, s string, id int) {
		g.chanOverride.upsertOverride(w, r, s, id, 1)
	}
	channelDelete := func(g *estateGates, w http.ResponseWriter, r *http.Request, s string, id int) {
		g.chanOverride.deleteOverride(w, r, s, id, 1)
	}

	cases := []struct {
		name   string
		fn     write
		caller string
		scope  string
		body   string
		drop   bool
		want   int
	}{
		{"alert upsert narrowed", alertUpsert, "narrowed", "server", alertBody, false, http.StatusOK},
		{"alert upsert cluster session", alertUpsert, "session", "cluster", alertBody, false, http.StatusOK},
		{"alert upsert bad body", alertUpsert, "narrowed", "server", `{`, false, http.StatusBadRequest},
		{"alert upsert refused", alertUpsert, "narrowed", "server",
			`{"operator":"~","severity":"warning"}`, false, http.StatusBadRequest},
		{"alert upsert fails", alertUpsert, "narrowed", "server", alertBody, true, http.StatusBadRequest},
		{"alert upsert no permission", alertUpsert, "anonymous", "server", alertBody, false, http.StatusForbidden},
		{"alert upsert out of scope", alertUpsert, "readOnly", "server", alertBody, false, http.StatusForbidden},
		{"alert delete narrowed", alertDelete, "narrowed", "server", "", false, http.StatusOK},
		{"alert delete fails", alertDelete, "narrowed", "server", "", true, http.StatusInternalServerError},
		{"alert delete no permission", alertDelete, "anonymous", "server", "", false, http.StatusForbidden},
		{"alert delete out of scope", alertDelete, "narrowed", "group", "", false, http.StatusForbidden},

		{"probe upsert narrowed", probeUpsert, "narrowed", "server", probeBody, false, http.StatusOK},
		{"probe upsert group wildcard", probeUpsert, "wildcard", "group", probeBody, false, http.StatusOK},
		{"probe upsert bad body", probeUpsert, "narrowed", "server", `{`, false, http.StatusBadRequest},
		{"probe upsert refused", probeUpsert, "narrowed", "server",
			`{"collection_interval_seconds":0,"retention_days":7}`, false, http.StatusBadRequest},
		{"probe upsert fails", probeUpsert, "narrowed", "server", probeBody, true, http.StatusBadRequest},
		{"probe upsert no permission", probeUpsert, "anonymous", "server", probeBody, false, http.StatusForbidden},
		{"probe upsert out of scope", probeUpsert, "narrowed", "cluster", probeBody, false, http.StatusForbidden},
		{"probe delete narrowed", probeDelete, "narrowed", "server", "", false, http.StatusOK},
		{"probe delete fails", probeDelete, "narrowed", "server", "", true, http.StatusInternalServerError},
		{"probe delete no permission", probeDelete, "anonymous", "server", "", false, http.StatusForbidden},
		{"probe delete out of scope", probeDelete, "readOnly", "server", "", false, http.StatusForbidden},

		{"channel upsert narrowed", channelUpsert, "narrowed", "server", channelBody, false, http.StatusOK},
		{"channel upsert cluster unscoped", channelUpsert, "unscoped", "cluster", channelBody, false, http.StatusOK},
		{"channel upsert bad body", channelUpsert, "narrowed", "server", `{`, false, http.StatusBadRequest},
		{"channel upsert fails", channelUpsert, "narrowed", "server", channelBody, true, http.StatusBadRequest},
		{"channel upsert no permission", channelUpsert, "anonymous", "server", channelBody, false, http.StatusForbidden},
		{"channel upsert out of scope", channelUpsert, "narrowed", "group", channelBody, false, http.StatusForbidden},
		{"channel delete narrowed", channelDelete, "narrowed", "server", "", false, http.StatusOK},
		{"channel delete fails", channelDelete, "narrowed", "server", "", true, http.StatusInternalServerError},
		{"channel delete no permission", channelDelete, "anonymous", "server", "", false, http.StatusForbidden},
		{"channel delete out of scope", channelDelete, "readOnly", "server", "", false, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newEstateGates(t)
			if tc.drop {
				mustExec(t, g.pool, estateGatesDropAll)
			}
			rec := httptest.NewRecorder()
			method := http.MethodPut
			if tc.body == "" {
				method = http.MethodDelete
			}
			// Connection 5, and cluster and group 1, all exist.
			scopeID := 5
			if tc.scope != "server" {
				scopeID = 1
			}
			tc.fn(g, rec, newScopeRequest(g.callers[tc.caller], method,
				tc.body), tc.scope, scopeID)

			expectStatus(t, rec, tc.want)
			if tc.want == http.StatusForbidden && tc.caller != "anonymous" {
				assertOutOfTokenScope(t, rec)
			}
		})
	}
}

// TestAlertRuleRouting drives the alert rule routes end to end over the
// seeded schema.
func TestAlertRuleRouting(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"list", http.MethodGet, "/api/v1/alert-rules", "", http.StatusOK},
		{"get", http.MethodGet, "/api/v1/alert-rules/1", "", http.StatusOK},
		{"update", http.MethodPut, "/api/v1/alert-rules/1",
			`{"default_threshold":20}`, http.StatusOK},
		{"wrong method", http.MethodDelete, "/api/v1/alert-rules/1", "",
			http.StatusMethodNotAllowed},
		{"unknown subpath", http.MethodGet, "/api/v1/alert-rules/1/x", "",
			http.StatusNotFound},
		{"empty id", http.MethodGet, "/api/v1/alert-rules/", "",
			http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newEstateGates(t)
			mux := http.NewServeMux()
			g.alertRule.RegisterRoutes(mux, func(next http.HandlerFunc) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					next(w, withSuperuser(r))
				}
			})
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path,
				strings.NewReader(tc.body)))
			expectStatus(t, rec, tc.want)
		})
	}
}
