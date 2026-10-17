/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// maskedProbe is a probe table holding a credential in a column that
// every route returning metrics must mask or leave out. The SQL is
// constant; the visible connection's ID is the only parameter.
type maskedProbe struct {
	name, drop, create, insert string
	// hidden is a column that must not be returned at all.
	hidden string
	// statement, when set, seeds each quoteShiftStatements entry: $1 is
	// the visible connection's ID, $2 a distinct number and $3 the
	// statement.
	statement string
}

// maskedProbes seed their rows a minute in the past, so that a database
// clock slightly ahead of the server's cannot put them after the end of
// a query window.
var maskedProbes = []maskedProbe{
	{name: "pg_settings", drop: `DROP TABLE IF EXISTS metrics.pg_settings`,
		create: `CREATE TABLE metrics.pg_settings (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, name text, setting text, reset_val text, boot_val text)`,
		insert: `INSERT INTO metrics.pg_settings VALUES ($1, now() - interval '1 minute', 'primary_conninfo',
            'host=192.0.2.1 password=example-secret', 'host=192.0.2.1 password=example-secret', '')`},
	{name: "pg_hba_file_rules", drop: `DROP TABLE IF EXISTS metrics.pg_hba_file_rules`,
		create: `CREATE TABLE metrics.pg_hba_file_rules (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, type text, auth_method text, options text[], error text)`,
		insert: `INSERT INTO metrics.pg_hba_file_rules VALUES ($1, now() - interval '1 minute', 'host', 'ldap',
            '{ldapserver=192.0.2.1,ldapbindpasswd=example-secret}', NULL),
            ($1, now() - interval '1 minute', 'host', NULL, NULL, 'authentication option not in name=value format: example-secret')`},
	{name: "pg_stat_activity", drop: `DROP TABLE IF EXISTS metrics.pg_stat_activity`,
		create: `CREATE TABLE metrics.pg_stat_activity (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, datname text, query text)`,
		insert: `INSERT INTO metrics.pg_stat_activity VALUES ($1, now() - interval '1 minute', 'appdb',
            'ALTER ROLE app PASSWORD ''example-secret''')`,
		statement: `INSERT INTO metrics.pg_stat_activity VALUES ($1, now() - interval '1 minute', concat('appdb', $2::integer), $3)`},
	{name: "pg_stat_statements", drop: `DROP TABLE IF EXISTS metrics.pg_stat_statements`,
		create: `CREATE TABLE metrics.pg_stat_statements (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, datname text, queryid bigint, query text)`,
		insert: `INSERT INTO metrics.pg_stat_statements VALUES ($1, now() - interval '1 minute', 'appdb', 1,
            'CREATE USER app WITH PASSWORD ''example-secret''')`,
		statement: `INSERT INTO metrics.pg_stat_statements VALUES ($1, now() - interval '1 minute', 'appdb', $2, $3)`},
	{name: "spock_exception_log", drop: `DROP TABLE IF EXISTS metrics.spock_exception_log`,
		create: `CREATE TABLE metrics.spock_exception_log (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, id integer, local_tup jsonb, remote_old_tup jsonb,
            remote_new_tup jsonb, ddl_statement text, error_message text)`,
		insert: `INSERT INTO metrics.spock_exception_log VALUES ($1, now() - interval '1 minute', 1,
            '{"password": "example-secret"}', '{"api_key": "example-secret"}', '{"token": 31337}',
            'CREATE SUBSCRIPTION s CONNECTION ''host=192.0.2.1 password=example-secret'' PUBLICATION p',
            'could not connect to postgresql://app:example-secret@192.0.2.1/db')`},
	{name: "spock_resolutions", drop: `DROP TABLE IF EXISTS metrics.spock_resolutions`,
		create: `CREATE TABLE metrics.spock_resolutions (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, relname text, local_tuple text, remote_tuple text)`,
		insert: `INSERT INTO metrics.spock_resolutions VALUES ($1, now() - interval '1 minute', 'users',
            '{"id": 1, "password": "example-secret"}', '{"id": 1, "secret": "example-secret"}')`},
	{name: "ds_masking_secret_probe", drop: `DROP TABLE IF EXISTS metrics.ds_masking_secret_probe`, hidden: "api_token",
		create: `CREATE TABLE metrics.ds_masking_secret_probe (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, name text, api_token text, calls bigint)`,
		insert: `INSERT INTO metrics.ds_masking_secret_probe VALUES ($1, now() - interval '1 minute', 'app', 'example-secret', 1)`},
}

// probesToMask returns a maskedProbes fixture for every probe in
// metrics.ProbeRedactions, failing the test for one that has none, so a
// new masked probe cannot skip the route checks, followed by the
// fixtures for probes masked by column name alone.
func probesToMask(t *testing.T) []maskedProbe {
	t.Helper()
	byName := make(map[string]maskedProbe, len(maskedProbes))
	for _, p := range maskedProbes {
		byName[p.name] = p
	}
	names := make([]string, 0, len(metrics.ProbeRedactions))
	for name := range metrics.ProbeRedactions {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]maskedProbe, 0, len(maskedProbes))
	for _, name := range names {
		p, ok := byName[name]
		if !ok {
			t.Errorf("metrics.ProbeRedactions masks %q but maskedProbes has no fixture for it", name)
			continue
		}
		out = append(out, p)
		delete(byName, name)
	}
	for _, p := range maskedProbes {
		if _, ok := byName[p.name]; ok {
			out = append(out, p)
		}
	}
	return out
}

// TestMaskedProbesCoverProbeRedactions runs without a database, so a
// missing fixture fails every test run.
func TestMaskedProbesCoverProbeRedactions(t *testing.T) {
	if got := probesToMask(t); len(got) != len(maskedProbes) {
		t.Errorf("%d of %d fixtures used", len(got), len(maskedProbes))
	}
}

// TestMaskedProbesOnEveryRouteIntegration runs every probe that holds a
// credential through query_datastore, GET /api/v1/metrics/latest and the
// latest-rows mode of GET /api/v1/metrics/query, and checks none returns
// the credential, a quoteShiftStatements secret or the stored statement
// text the latest routes select beside a masked statement column.
func TestMaskedProbesOnEveryRouteIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	mux := http.NewServeMux()
	noAuth := func(h http.HandlerFunc) http.HandlerFunc { return h }
	api.NewLatestSnapshotHandler(f.ds, f.store).RegisterRoutes(mux, noAuth)
	api.NewMetricsHandler(f.ds, f.store).RegisterRoutes(mux, noAuth)

	for _, p := range probesToMask(t) {
		t.Run(p.name, func(t *testing.T) {
			if _, err := f.pool.Exec(ctx, p.drop); err != nil {
				t.Fatalf("%s: %v", p.drop, err)
			}
			if _, err := f.pool.Exec(ctx, p.create); err != nil {
				t.Fatalf("%s: %v", p.create, err)
			}
			t.Cleanup(func() {
				if _, err := f.pool.Exec(context.Background(), p.drop); err != nil {
					t.Logf("%s: %v", p.drop, err)
				}
			})
			if _, err := f.pool.Exec(ctx, p.insert, f.visible); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if p.statement != "" {
				for i, st := range quoteShiftStatements {
					if _, err := f.pool.Exec(ctx, p.statement, f.visible, 2+i, st.query); err != nil {
						t.Fatalf("seed %q: %v", st.query, err)
					}
				}
			}

			outputs := map[string]string{}
			for _, shape := range []string{"rows", "latest"} {
				text, isErr := call(t, f.tool(), superuserContext(), map[string]any{"source": p.name, "shape": shape})
				if isErr {
					t.Fatalf("query_datastore %s: %s", shape, text)
				}
				outputs["query_datastore "+shape] = text
			}
			params := "probe_name=" + p.name + "&connection_id=" + strconv.Itoa(f.visible)
			for route, target := range map[string]string{
				"metrics/latest": "/api/v1/metrics/latest?" + params,
				"metrics/query":  "/api/v1/metrics/query?limit=10&" + params,
			} {
				req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(superuserContext())
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s returned %d: %s", route, rec.Code, rec.Body.String())
				}
				outputs[route] = rec.Body.String()
			}

			for route, out := range outputs {
				if strings.Contains(out, "example-secret") || strings.Contains(out, "31337") {
					t.Errorf("%s returned a credential:\n%s", route, out)
				}
				// query_datastore masks statement text with the SQL
				// patterns alone, which miss these forms, as its
				// TextRedactionNote says; the latest routes also lex it.
				for _, st := range quoteShiftStatements {
					if !strings.HasPrefix(route, "query_datastore") && strings.Contains(out, st.secret) {
						t.Errorf("%s returned %s:\n%s", route, st.secret, out)
					}
				}
				if strings.Contains(out, "_raw_statement_") {
					t.Errorf("%s returned the stored statement text column:\n%s", route, out)
				}
				if p.hidden != "" {
					if strings.Contains(out, p.hidden) || !strings.Contains(out, "calls") {
						t.Errorf("%s should leave out %s only:\n%s", route, p.hidden, out)
					}
				} else if !strings.Contains(out, metrics.RedactedValue) {
					t.Errorf("%s should show the masked value:\n%s", route, out)
				}
			}
		})
	}
}

// collectorShapeTables are the tables, in the collector's shape, that
// the top-queries and timeline routes read. The SQL is constant.
var collectorShapeTables = []struct{ drop, create string }{
	{`DROP TABLE IF EXISTS metrics.pg_settings`,
		`CREATE TABLE metrics.pg_settings (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, name text, setting text)`},
	{`DROP TABLE IF EXISTS metrics.pg_stat_statements`,
		`CREATE TABLE metrics.pg_stat_statements (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, queryid bigint NOT NULL, userid bigint, dbid bigint,
            toplevel boolean NOT NULL DEFAULT TRUE, database_name text, query text NOT NULL,
            calls bigint NOT NULL DEFAULT 0, total_exec_time double precision NOT NULL DEFAULT 0,
            mean_exec_time double precision NOT NULL DEFAULT 0, min_exec_time double precision NOT NULL DEFAULT 0,
            max_exec_time double precision NOT NULL DEFAULT 0, rows bigint NOT NULL DEFAULT 0,
            shared_blks_hit bigint NOT NULL DEFAULT 0, shared_blks_read bigint NOT NULL DEFAULT 0)`},
	{`DROP TABLE IF EXISTS metrics.pg_stat_activity`,
		`CREATE TABLE metrics.pg_stat_activity (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, datid bigint, datname text, usesysid bigint,
            usename text, client_addr inet, client_hostname text, query_id bigint)`},
	{`DROP TABLE IF EXISTS metrics.pg_stat_database`,
		`CREATE TABLE metrics.pg_stat_database (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, database_name text NOT NULL, datid bigint, datname text)`},
}

// TestMaskedQueryTextAndSettingChangesIntegration checks that the
// top-queries route returns masked statement text, including the
// quoteShiftStatements, and that a change to
// a credential setting still appears on the timeline, through both
// GET /api/v1/timeline/events and get_timeline_events, with its old and
// new values masked (get_timeline_events lists no details).
func TestMaskedQueryTextAndSettingChangesIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	for _, tbl := range collectorShapeTables {
		if _, err := f.pool.Exec(ctx, tbl.drop); err != nil {
			t.Fatalf("%s: %v", tbl.drop, err)
		}
		if _, err := f.pool.Exec(ctx, tbl.create); err != nil {
			t.Fatalf("%s: %v", tbl.create, err)
		}
		drop := tbl.drop
		t.Cleanup(func() {
			if _, err := f.pool.Exec(context.Background(), drop); err != nil {
				t.Logf("%s: %v", drop, err)
			}
		})
	}
	seed := []string{
		`INSERT INTO metrics.pg_settings VALUES
            ($1, now() - interval '10 minutes', 'primary_conninfo', 'host=192.0.2.1 password=example-secret-old'),
            ($1, now() - interval '1 minute', 'primary_conninfo', 'host=192.0.2.1 password=example-secret'),
            ($1, now() - interval '10 minutes', 'work_mem', '4MB'),
            ($1, now() - interval '1 minute', 'work_mem', '8MB')`,
		`INSERT INTO metrics.pg_stat_statements (connection_id, collected_at, queryid, userid, dbid,
            database_name, query, calls, total_exec_time, mean_exec_time) VALUES
            ($1, now() - interval '6 minutes', 1001, 10, 100, 'appdb', 'ALTER ROLE app PASSWORD ''example-secret''', 0, 0, 0),
            ($1, now() - interval '1 minute', 1001, 10, 100, 'appdb', 'ALTER ROLE app PASSWORD ''example-secret''', 5, 50, 10)`,
	}
	for _, sql := range seed {
		if _, err := f.pool.Exec(ctx, sql, f.visible); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	for i, st := range quoteShiftStatements {
		if _, err := f.pool.Exec(ctx, `INSERT INTO metrics.pg_stat_statements (connection_id, collected_at,
            queryid, userid, dbid, database_name, query, calls, total_exec_time, mean_exec_time) VALUES
            ($1, now() - interval '6 minutes', $2, 10, 100, 'appdb', $3, 0, 0, 0),
            ($1, now() - interval '1 minute', $2, 10, 100, 'appdb', $3, 5, 50, 10)`,
			f.visible, 2001+i, st.query); err != nil {
			t.Fatalf("seed %q: %v", st.query, err)
		}
	}

	mux := http.NewServeMux()
	noAuth := func(h http.HandlerFunc) http.HandlerFunc { return h }
	api.NewPerfSummaryHandler(f.ds, f.store).RegisterRoutes(mux, noAuth)
	api.NewTimelineHandler(f.ds, f.store, f.checker).RegisterRoutes(mux, noAuth)

	conn := strconv.Itoa(f.visible)
	window := "&start_time=" + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) +
		"&end_time=" + time.Now().UTC().Format(time.RFC3339)
	outputs := map[string]string{}
	for route, target := range map[string]string{
		"metrics/top-queries": "/api/v1/metrics/top-queries?connection_id=" + conn,
		"timeline/events":     "/api/v1/timeline/events?event_types=config_change&connection_id=" + conn + window,
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(superuserContext())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", route, rec.Code, rec.Body.String())
		}
		outputs[route] = rec.Body.String()
	}
	tool := GetTimelineEventsTool(f.ds, f.checker, database.NewVisibilityLister(f.ds), nil)
	text, isErr := call(t, tool, superuserContext(), map[string]any{
		"connection_id": f.visible, "event_types": "config_change", "start_time": "1h"})
	if isErr {
		t.Fatalf("get_timeline_events: %s", text)
	}
	outputs["get_timeline_events"] = text

	for route, out := range outputs {
		if strings.Contains(out, "example-secret") {
			t.Errorf("%s returned a credential:\n%s", route, out)
		}
		for _, st := range quoteShiftStatements {
			if strings.Contains(out, st.secret) {
				t.Errorf("%s returned %s:\n%s", route, st.secret, out)
			}
		}
		// get_timeline_events lists events without their details.
		if route != "get_timeline_events" && !strings.Contains(out, metrics.RedactedValue) {
			t.Errorf("%s should show the masked value:\n%s", route, out)
		}
	}
	if !strings.Contains(outputs["get_timeline_events"], "Changed 2 settings") {
		t.Errorf("get_timeline_events should report the change:\n%s", outputs["get_timeline_events"])
	}
	if !strings.Contains(outputs["metrics/top-queries"], "ALTER ROLE app PASSWORD") {
		t.Errorf("top-queries should keep the statement text:\n%s", outputs["metrics/top-queries"])
	}
	if out := outputs["timeline/events"]; !strings.Contains(out, "primary_conninfo") || !strings.Contains(out, "8MB") {
		t.Errorf("timeline/events should report both setting changes:\n%s", out)
	}
}

// quoteShiftConninfo is a primary_conninfo value whose masking in SQL,
// if it rewrote only part of the value, would take the quote after
// "token=a" with the masked text and leave the password inside what
// libpq then reads as application_name's quoted value, where
// pgsettings.RedactValue does not look.
const quoteShiftConninfo = `application_name='app.token=a' password=$1,hunter2 options=' port=5432'`

// TestConninfoQuoteShiftIntegration is the regression test for the
// partial masking of a connection string setting (VULN-001): the
// latest routes and the timeline's configuration change details must
// either leave such a value as it is or mask it whole, so the password
// never reaches a response.
func TestConninfoQuoteShiftIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	for _, sql := range []string{`DROP TABLE IF EXISTS metrics.pg_settings`,
		`CREATE TABLE metrics.pg_settings (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, name text, setting text, reset_val text, boot_val text)`} {
		if _, err := f.pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DROP TABLE IF EXISTS metrics.pg_settings`); err != nil {
			t.Logf("drop: %v", err)
		}
	})
	if _, err := f.pool.Exec(ctx, `INSERT INTO metrics.pg_settings VALUES
        ($1, now() - interval '10 minutes', 'primary_conninfo', 'host=192.0.2.1', '', ''),
        ($1, now() - interval '1 minute', 'primary_conninfo', $2, $2, $2)`,
		f.visible, quoteShiftConninfo); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mux := http.NewServeMux()
	noAuth := func(h http.HandlerFunc) http.HandlerFunc { return h }
	api.NewLatestSnapshotHandler(f.ds, f.store).RegisterRoutes(mux, noAuth)
	api.NewMetricsHandler(f.ds, f.store).RegisterRoutes(mux, noAuth)
	api.NewTimelineHandler(f.ds, f.store, f.checker).RegisterRoutes(mux, noAuth)

	conn := strconv.Itoa(f.visible)
	params := "probe_name=pg_settings&connection_id=" + conn
	window := "&start_time=" + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) +
		"&end_time=" + time.Now().UTC().Format(time.RFC3339)
	for route, target := range map[string]string{
		"metrics/latest":  "/api/v1/metrics/latest?" + params,
		"metrics/query":   "/api/v1/metrics/query?limit=10&" + params,
		"timeline/events": "/api/v1/timeline/events?event_types=config_change&connection_id=" + conn + window,
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(superuserContext())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		out := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", route, rec.Code, out)
		}
		if strings.Contains(out, "hunter2") {
			t.Errorf("%s returned the password:\n%s", route, out)
		}
		if !strings.Contains(out, "primary_conninfo") || !strings.Contains(out, metrics.RedactedValue) {
			t.Errorf("%s should report the setting with its value masked:\n%s", route, out)
		}
	}

	// Neither latest route sorts on a setting's value.
	for _, target := range []string{
		"/api/v1/metrics/latest?order_by=setting&" + params,
		"/api/v1/metrics/query?limit=10&order_by=reset_val&" + params,
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(superuserContext())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "cannot be sorted on") {
			t.Errorf("%s: got %d (%s), want 400", target, rec.Code, rec.Body.String())
		}
	}
}

// quoteShiftStatements are statements, as a collector stored them
// before migration 26 scrubbed them, whose secret came back on the
// latest and top-queries routes when the server masked the text in SQL
// (RedactedText) before lexing it (GitHub issue #670).
var quoteShiftStatements = []struct{ query, secret string }{
	{`ALTER SYSTEM SET restore_command = 'PGPASSWORD=\''PSSLEAKsecret17 cp %f %p'`, "PSSLEAKsecret17"},
	{`ALTER SYSTEM SET primary_conninfo = 'host=h user=rep password=\''PSSLEAKsecret18'`, "PSSLEAKsecret18"},
	{`ALTER ROLE app PASSWORD '\''WFLEAKsecret19'`, "WFLEAKsecret19"},
}

// connectionErrorSecret is a driver error that quotes a connection
// string and its password, as connections.connection_error can.
const connectionErrorSecret = `failed to connect to postgresql://app:example-secret@192.0.2.1/db: ` +
	`host=192.0.2.1 user=app password=example-secret dbname=db`

// connectionErrorMembers are the connections the connection_error test
// seeds, one in each place a topology or hierarchy query reads the
// column from.
var connectionErrorMembers = []string{"manual-member", "persisted-member", "ungrouped"}

// TestMaskedConnectionErrorOnEveryRouteIntegration checks that the
// connection_error text, which can quote a password, is masked by
// list_connections, GET /api/v1/clusters (both the auto-detected and
// the persisted members of the topology) and the datastore's cluster
// hierarchy (issue #683). It runs on the collector's real schema, since
// the topology queries read most of it.
func TestMaskedConnectionErrorOnEveryRouteIntegration(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()
	ds := database.NewTestDatastore(pool)
	store, storeCleanup := newRBACRegressionTestStore(t)
	t.Cleanup(storeCleanup)
	checker := auth.NewRBACCheckerForDatastore(store, ds)

	var groupID, manualID, autoID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ('masking-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, 'manual-cluster') RETURNING id`,
		groupID).Scan(&manualID); err != nil {
		t.Fatalf("seed manual cluster: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name, auto_cluster_key) VALUES ($1, 'auto-cluster', 'spock:masking')
            RETURNING id`, groupID).Scan(&autoID); err != nil {
		t.Fatalf("seed auto cluster: %v", err)
	}
	// A manual cluster member, a member of an auto-detected cluster that
	// detection no longer finds it in, and a connection in no cluster.
	clusterOf := map[string]*int{"manual-member": &manualID, "persisted-member": &autoID, "ungrouped": nil}
	for _, name := range connectionErrorMembers {
		if _, err := pool.Exec(ctx, `INSERT INTO connections
            (name, host, port, database_name, username, owner_username, is_shared, is_monitored,
             cluster_id, membership_source, connection_error)
            VALUES ($1, '192.0.2.1', 5432, 'db', 'app', 'admin', TRUE, TRUE, $2, 'manual', $3)`,
			name, clusterOf[name], connectionErrorSecret); err != nil {
			t.Fatalf("seed connection %s: %v", name, err)
		}
	}

	outputs := map[string]string{}
	text, isErr := call(t, ListConnectionsTool(pool, checker, database.NewVisibilityLister(ds)),
		superuserContext(), map[string]any{})
	if isErr {
		t.Fatalf("list_connections: %s", text)
	}
	outputs["list_connections"] = text

	mux := http.NewServeMux()
	noAuth := func(h http.HandlerFunc) http.HandlerFunc { return h }
	api.NewClusterHandler(ds, store, checker).RegisterRoutes(mux, noAuth)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil).WithContext(superuserContext())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/clusters returned %d: %s", rec.Code, rec.Body.String())
	}
	outputs["GET /api/v1/clusters"] = rec.Body.String()

	hierarchy, err := ds.GetClusterHierarchy(ctx)
	if err != nil {
		t.Fatalf("GetClusterHierarchy: %v", err)
	}
	body, err := json.Marshal(hierarchy)
	if err != nil {
		t.Fatalf("marshal hierarchy: %v", err)
	}
	outputs["GetClusterHierarchy"] = string(body)

	for route, out := range outputs {
		if strings.Contains(out, "example-secret") {
			t.Errorf("%s returned a credential:\n%s", route, out)
		}
		for _, name := range connectionErrorMembers {
			if !strings.Contains(out, name) {
				t.Errorf("%s should list %s:\n%s", route, name, out)
			}
		}
		if got := strings.Count(out, metrics.RedactedValue); got < len(connectionErrorMembers) {
			t.Errorf("%s should show the masked value for every connection, found %d:\n%s", route, got, out)
		}
	}
}
