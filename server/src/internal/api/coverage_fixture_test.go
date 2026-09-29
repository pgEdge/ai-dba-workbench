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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// covFixtureSchema holds every table the configuration, override,
// blackout and notification channel handlers read. It is created inside
// a per-test schema (selected through search_path) so these tests never
// collide with the public-schema fixtures other tests in this package
// create and drop.
const covFixtureSchema = `
CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    auto_group_key VARCHAR(255),
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    role VARCHAR(50) DEFAULT 'primary',
    connection_error TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(20) NOT NULL DEFAULT 'auto',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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
    CHECK (scope IN ('global', 'group', 'cluster', 'server'))
);
CREATE UNIQUE INDEX idx_cov_probe_global ON probe_configs(name) WHERE scope = 'global';
CREATE UNIQUE INDEX idx_cov_probe_server ON probe_configs(name, connection_id) WHERE scope = 'server';
CREATE UNIQUE INDEX idx_cov_probe_cluster ON probe_configs(name, cluster_id) WHERE scope = 'cluster';
CREATE UNIQUE INDEX idx_cov_probe_group ON probe_configs(name, group_id) WHERE scope = 'group';

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
CREATE UNIQUE INDEX idx_cov_at_server ON alert_thresholds(rule_id, connection_id, COALESCE(database_name, '')) WHERE scope = 'server';
CREATE UNIQUE INDEX idx_cov_at_cluster ON alert_thresholds(rule_id, cluster_id) WHERE scope = 'cluster';
CREATE UNIQUE INDEX idx_cov_at_group ON alert_thresholds(rule_id, group_id) WHERE scope = 'group';

CREATE TABLE blackouts (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    reason TEXT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (end_time > start_time),
    CHECK (scope IN ('estate', 'group', 'cluster', 'server'))
);

CREATE TABLE blackout_schedules (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    name TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    duration_minutes INTEGER NOT NULL CHECK (duration_minutes > 0),
    timezone TEXT NOT NULL DEFAULT 'UTC',
    reason TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (scope IN ('estate', 'group', 'cluster', 'server'))
);

CREATE TABLE notification_channels (
    id BIGSERIAL PRIMARY KEY,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    channel_type TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    webhook_url_encrypted TEXT,
    endpoint_url TEXT,
    http_method TEXT DEFAULT 'POST',
    headers_json JSONB DEFAULT '{}',
    auth_type TEXT,
    auth_credentials_encrypted TEXT,
    smtp_host TEXT,
    smtp_port INTEGER DEFAULT 587,
    smtp_username TEXT,
    smtp_password_encrypted TEXT,
    smtp_use_tls BOOLEAN DEFAULT TRUE,
    from_address TEXT,
    from_name TEXT,
    telegram_bot_token_encrypted TEXT,
    telegram_chat_id TEXT,
    template_alert_fire TEXT,
    template_alert_clear TEXT,
    template_reminder TEXT,
    reminder_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    reminder_interval_hours INTEGER DEFAULT 24,
    is_estate_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE email_recipients (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    email_address TEXT NOT NULL,
    display_name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE notification_channel_overrides (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('group', 'cluster', 'server')),
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_cov_nco_server ON notification_channel_overrides(channel_id, connection_id) WHERE scope = 'server';
CREATE UNIQUE INDEX idx_cov_nco_cluster ON notification_channel_overrides(channel_id, cluster_id) WHERE scope = 'cluster';
CREATE UNIQUE INDEX idx_cov_nco_group ON notification_channel_overrides(channel_id, group_id) WHERE scope = 'group';
`

// covOwner is the username that owns the visible fixture connection.
const covOwner = "cov_owner"

// covFixture is an isolated, seeded datastore for handler coverage
// tests. The "visible" hierarchy (group, cluster, connection) is owned
// by covOwner; the "hidden" hierarchy belongs to another user and is
// not shared, so a restricted covOwner caller cannot see it.
type covFixture struct {
	ds   *database.Datastore
	pool *pgxpool.Pool

	groupID       int
	clusterID     int
	connID        int
	hiddenGroupID int
	hiddenCluster int
	hiddenConnID  int
	ruleID        int64
	probeName     string
	channelID     int64
}

// covTestDSN returns the test database DSN or skips the test.
func covTestDSN(t *testing.T) string {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	dsn := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if dsn == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}
	return dsn
}

// covFixtureSchemaName is the schema each fixture test builds its tables
// in. The tests in this package do not run in parallel, so one fixed
// name is enough; it is dropped before and after each test.
const covFixtureSchemaName = "api_cov_fixture"

// covMissingSchemaName names a schema that is never created, so a pool
// whose search_path points at it fails every unqualified query.
const covMissingSchemaName = "api_cov_missing"

// covPoolForSchema opens a small pool whose sessions resolve
// unqualified table names in the given schema only.
func covPoolForSchema(t *testing.T, dsn, schema string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	return pool
}

// newCovFixture creates an isolated schema, the tables above and the
// seed rows, and drops the schema when the test ends.
func newCovFixture(t *testing.T) *covFixture {
	t.Helper()
	dsn := covTestDSN(t)
	ctx := context.Background()

	admin := covPoolForSchema(t, dsn, "public")
	if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS api_cov_fixture CASCADE"); err != nil {
		admin.Close()
		t.Fatalf("drop stale schema: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA api_cov_fixture"); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	pool := covPoolForSchema(t, dsn, covFixtureSchemaName)
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS api_cov_fixture CASCADE")
		admin.Close()
	})

	if _, err := pool.Exec(ctx, covFixtureSchema); err != nil {
		t.Fatalf("create fixture tables: %v", err)
	}

	f := &covFixture{
		ds:        database.NewTestDatastoreWithSecret(pool, channelTestServerSecret),
		pool:      pool,
		probeName: "pg_stat_activity",
	}

	scan := func(dst any, q string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, q, args...).Scan(dst); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	scan(&f.groupID, `INSERT INTO cluster_groups (name) VALUES ('visible group') RETURNING id`)
	scan(&f.hiddenGroupID, `INSERT INTO cluster_groups (name) VALUES ('hidden group') RETURNING id`)
	scan(&f.clusterID, `INSERT INTO clusters (group_id, name) VALUES ($1, 'visible cluster') RETURNING id`, f.groupID)
	scan(&f.hiddenCluster, `INSERT INTO clusters (group_id, name) VALUES ($1, 'hidden cluster') RETURNING id`, f.hiddenGroupID)
	scan(&f.connID, `INSERT INTO connections (name, owner_username, cluster_id) VALUES ('visible', $1, $2) RETURNING id`,
		covOwner, f.clusterID)
	scan(&f.hiddenConnID, `INSERT INTO connections (name, owner_username, cluster_id) VALUES ('hidden', 'someone_else', $1) RETURNING id`,
		f.hiddenCluster)
	scan(&f.ruleID, `INSERT INTO alert_rules (name, description, category, metric_name, default_operator, default_threshold, default_severity)
        VALUES ('cov_rule', 'Coverage rule', 'test', 'cov.metric', '>', 90, 'warning') RETURNING id`)
	if _, err := pool.Exec(ctx, `INSERT INTO probe_configs (name, description, scope) VALUES ($1, 'Activity probe', 'global')`,
		f.probeName); err != nil {
		t.Fatalf("seed probe config: %v", err)
	}
	scan(&f.channelID, `INSERT INTO notification_channels (channel_type, name, owner_username)
        VALUES ('slack', 'cov channel', 'channel_admin') RETURNING id`)
	return f
}

// newBrokenDatastore returns a datastore whose search_path points at a
// schema that does not exist, so every query fails with "relation does
// not exist". It drives the handlers' datastore-error branches.
func newBrokenDatastore(t *testing.T) *database.Datastore {
	t.Helper()
	dsn := covTestDSN(t)
	pool := covPoolForSchema(t, dsn, covMissingSchemaName)
	t.Cleanup(pool.Close)
	return database.NewTestDatastoreWithSecret(pool, channelTestServerSecret)
}

// covRestrictedUser creates a non-superuser holding perms and returns a
// request decorator that authenticates as that user under covOwner, so
// the permission gate passes but connection visibility is restricted
// to the fixture's visible hierarchy.
func covRestrictedUser(t *testing.T, store *auth.AuthStore, perms ...string) func(*http.Request) *http.Request {
	t.Helper()
	var userID int64
	if len(perms) == 0 {
		if err := store.CreateUser(covOwner, "Password1234", "", "", ""); err != nil {
			t.Fatalf("create user: %v", err)
		}
		id, err := store.GetUserID(covOwner)
		if err != nil {
			t.Fatalf("get user id: %v", err)
		}
		userID = id
	} else {
		userID = setupUserWithPermissions(t, store, covOwner, perms)
	}
	return func(r *http.Request) *http.Request {
		return withUsername(withUser(r, userID), covOwner)
	}
}

// covDo issues a request against h after applying each decorator.
func covDo(h http.HandlerFunc, method, path, body string, decorate ...func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, d := range decorate {
		req = d(req)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// covIdentityWrapper is an authWrapper that adds no authentication.
func covIdentityWrapper(h http.HandlerFunc) http.HandlerFunc { return h }

// covExpect fails the test when rec does not carry the wanted status.
func covExpect(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, want, rec.Body.String())
	}
}

// covAssertRouteRegistered checks that path is served by mux with a
// handler that is not the mux's own 404.
func covAssertRouteRegistered(t *testing.T, mux *http.ServeMux, path string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if _, pattern := mux.Handler(req); pattern == "" {
		t.Fatalf("no route registered for %s", path)
	}
}

// covOverrideSpec describes one of the scope-override handlers (alert,
// probe and channel overrides share the same route shape).
type covOverrideSpec struct {
	prefix      string // route prefix, with trailing slash
	item        string // a valid item segment (rule ID, probe name, channel ID)
	badItem     string // an item segment the router rejects with 400
	okBody      string // a PUT body the datastore accepts
	invalidBody string // a PUT body the datastore rejects
}

// runOverrideCoverage drives the routing, visibility, upsert and delete
// paths of a scope-override handler. serve is backed by f, broken by a
// datastore whose queries all fail, and restricted authenticates as the
// covOwner user who holds the handler's management permission.
func runOverrideCoverage(t *testing.T, f *covFixture, serve, broken http.HandlerFunc,
	restricted func(*http.Request) *http.Request, spec covOverrideSpec) {
	t.Helper()
	p := spec.prefix
	itoa := func(i int) string { return strconv.Itoa(i) }
	scopes := []struct {
		scope     string
		visible   int
		invisible int
	}{
		{"server", f.connID, f.hiddenConnID},
		{"cluster", f.clusterID, f.hiddenCluster},
		{"group", f.groupID, f.hiddenGroupID},
	}

	t.Run("routing", func(t *testing.T) {
		cases := []struct {
			method, path string
			want         int
		}{
			{http.MethodGet, p, http.StatusNotFound},
			{http.MethodGet, p + "server", http.StatusNotFound},
			{http.MethodGet, p + "bogus/1", http.StatusBadRequest},
			{http.MethodGet, p + "server/x", http.StatusBadRequest},
			{http.MethodPost, p + "server/1", http.StatusMethodNotAllowed},
			{http.MethodGet, p + "server/1/" + spec.item, http.StatusMethodNotAllowed},
			{http.MethodPut, p + "server/1/" + spec.badItem, http.StatusBadRequest},
			{http.MethodGet, p + "server/1/a/b", http.StatusNotFound},
		}
		for _, tc := range cases {
			covExpect(t, covDo(serve, tc.method, tc.path, "", withSuperuser), tc.want)
		}
	})

	for _, s := range scopes {
		base := p + s.scope + "/"
		t.Run(s.scope, func(t *testing.T) {
			// Superuser listing, upsert (twice, to hit the conflict
			// update), validation failures and delete.
			covExpect(t, covDo(serve, http.MethodGet, base+itoa(s.visible), "", withSuperuser), http.StatusOK)
			itemURL := base + itoa(s.visible) + "/" + spec.item
			covExpect(t, covDo(serve, http.MethodPut, itemURL, spec.okBody, withSuperuser), http.StatusOK)
			covExpect(t, covDo(serve, http.MethodPut, itemURL, spec.okBody, withSuperuser), http.StatusOK)
			covExpect(t, covDo(serve, http.MethodGet, base+itoa(s.visible), "", restricted), http.StatusOK)
			covExpect(t, covDo(serve, http.MethodPut, itemURL, "{", withSuperuser), http.StatusBadRequest)
			if spec.invalidBody != "" {
				covExpect(t, covDo(serve, http.MethodPut, itemURL, spec.invalidBody, withSuperuser), http.StatusBadRequest)
			}
			covExpect(t, covDo(serve, http.MethodDelete, itemURL, "", withSuperuser), http.StatusOK)

			// A restricted caller cannot see the other user's scope.
			covExpect(t, covDo(serve, http.MethodGet, base+itoa(s.invisible), "", restricted), http.StatusNotFound)

			// Permission gate on writes.
			noPerm := func(r *http.Request) *http.Request { return withUser(r, 424242) }
			covExpect(t, covDo(serve, http.MethodPut, itemURL, spec.okBody, noPerm), http.StatusForbidden)
			covExpect(t, covDo(serve, http.MethodDelete, itemURL, "", noPerm), http.StatusForbidden)

			// Datastore failures.
			covExpect(t, covDo(broken, http.MethodGet, base+"1", "", withSuperuser), http.StatusInternalServerError)
			covExpect(t, covDo(broken, http.MethodGet, base+"1", "", restricted), http.StatusInternalServerError)
			covExpect(t, covDo(broken, http.MethodDelete, base+"1/"+spec.item, "", withSuperuser),
				http.StatusInternalServerError)
		})
	}
}
