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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/api"
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
}

var maskedProbes = []maskedProbe{
	{name: "pg_settings", drop: `DROP TABLE IF EXISTS metrics.pg_settings`,
		create: `CREATE TABLE metrics.pg_settings (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, name text, setting text, reset_val text, boot_val text)`,
		insert: `INSERT INTO metrics.pg_settings VALUES ($1, now(), 'primary_conninfo',
            'host=192.0.2.1 password=example-secret', 'host=192.0.2.1 password=example-secret', '')`},
	{name: "pg_hba_file_rules", drop: `DROP TABLE IF EXISTS metrics.pg_hba_file_rules`,
		create: `CREATE TABLE metrics.pg_hba_file_rules (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, type text, auth_method text, options text[], error text)`,
		insert: `INSERT INTO metrics.pg_hba_file_rules VALUES ($1, now(), 'host', 'ldap',
            '{ldapserver=192.0.2.1,ldapbindpasswd=example-secret}', NULL),
            ($1, now(), 'host', NULL, NULL, 'authentication option not in name=value format: example-secret')`},
	{name: "pg_stat_activity", drop: `DROP TABLE IF EXISTS metrics.pg_stat_activity`,
		create: `CREATE TABLE metrics.pg_stat_activity (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, datname text, query text)`,
		insert: `INSERT INTO metrics.pg_stat_activity VALUES ($1, now(), 'appdb',
            'ALTER ROLE app PASSWORD ''example-secret''')`},
	{name: "pg_stat_statements", drop: `DROP TABLE IF EXISTS metrics.pg_stat_statements`,
		create: `CREATE TABLE metrics.pg_stat_statements (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, datname text, queryid bigint, query text)`,
		insert: `INSERT INTO metrics.pg_stat_statements VALUES ($1, now(), 'appdb', 1,
            'CREATE USER app WITH PASSWORD ''example-secret''')`},
	{name: "spock_exception_log", drop: `DROP TABLE IF EXISTS metrics.spock_exception_log`,
		create: `CREATE TABLE metrics.spock_exception_log (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, id integer, local_tup jsonb, remote_old_tup jsonb,
            remote_new_tup jsonb, ddl_statement text, error_message text)`,
		insert: `INSERT INTO metrics.spock_exception_log VALUES ($1, now(), 1,
            '{"password": "example-secret"}', '{"api_key": "example-secret"}', '{"token": 31337}',
            'CREATE SUBSCRIPTION s CONNECTION ''host=192.0.2.1 password=example-secret'' PUBLICATION p',
            'could not connect to postgresql://app:example-secret@192.0.2.1/db')`},
	{name: "spock_resolutions", drop: `DROP TABLE IF EXISTS metrics.spock_resolutions`,
		create: `CREATE TABLE metrics.spock_resolutions (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, relname text, local_tuple text, remote_tuple text)`,
		insert: `INSERT INTO metrics.spock_resolutions VALUES ($1, now(), 'users',
            '{"id": 1, "password": "example-secret"}', '{"id": 1, "secret": "example-secret"}')`},
	{name: "ds_masking_secret_probe", drop: `DROP TABLE IF EXISTS metrics.ds_masking_secret_probe`, hidden: "api_token",
		create: `CREATE TABLE metrics.ds_masking_secret_probe (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, name text, api_token text, calls bigint)`,
		insert: `INSERT INTO metrics.ds_masking_secret_probe VALUES ($1, now(), 'app', 'example-secret', 1)`},
}

// TestMaskedProbesOnEveryRouteIntegration runs every probe that holds a
// credential through query_datastore, GET /api/v1/metrics/latest and the
// latest-rows mode of GET /api/v1/metrics/query, and checks none returns
// the credential.
func TestMaskedProbesOnEveryRouteIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	mux := http.NewServeMux()
	noAuth := func(h http.HandlerFunc) http.HandlerFunc { return h }
	api.NewLatestSnapshotHandler(f.ds, f.store).RegisterRoutes(mux, noAuth)
	api.NewMetricsHandler(f.ds, f.store).RegisterRoutes(mux, noAuth)

	for _, p := range maskedProbes {
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
// top-queries route returns masked statement text, and that a change to
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
	tool := GetTimelineEventsTool(f.ds, f.checker, database.NewVisibilityLister(f.ds))
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
