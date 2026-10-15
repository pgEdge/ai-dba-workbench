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

	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// maskedProbe is a probe table holding a credential in a column that
// every route returning metrics must mask or leave out. The SQL is
// constant; the visible connection's ID is the only parameter.
type maskedProbe struct {
	name, create, insert string
	// hidden is a column that must not be returned at all.
	hidden string
}

var maskedProbes = []maskedProbe{
	{name: "pg_settings",
		create: `CREATE TABLE metrics.pg_settings (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, name text, setting text, reset_val text, boot_val text)`,
		insert: `INSERT INTO metrics.pg_settings VALUES ($1, now(), 'primary_conninfo',
            'host=192.0.2.1 password=example-secret', 'host=192.0.2.1 password=example-secret', '')`},
	{name: "pg_hba_file_rules",
		create: `CREATE TABLE metrics.pg_hba_file_rules (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, type text, auth_method text, options text[], error text)`,
		insert: `INSERT INTO metrics.pg_hba_file_rules VALUES ($1, now(), 'host', 'ldap',
            '{ldapserver=192.0.2.1,ldapbindpasswd=example-secret}', NULL),
            ($1, now(), 'host', NULL, NULL, 'authentication option not in name=value format: example-secret')`},
	{name: "pg_stat_activity",
		create: `CREATE TABLE metrics.pg_stat_activity (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, datname text, query text)`,
		insert: `INSERT INTO metrics.pg_stat_activity VALUES ($1, now(), 'appdb',
            'ALTER ROLE app PASSWORD ''example-secret''')`},
	{name: "pg_stat_statements",
		create: `CREATE TABLE metrics.pg_stat_statements (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, datname text, queryid bigint, query text)`,
		insert: `INSERT INTO metrics.pg_stat_statements VALUES ($1, now(), 'appdb', 1,
            'CREATE USER app WITH PASSWORD ''example-secret''')`},
	{name: "spock_exception_log",
		create: `CREATE TABLE metrics.spock_exception_log (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, id integer, local_tup jsonb, remote_old_tup jsonb,
            remote_new_tup jsonb, ddl_statement text, error_message text)`,
		insert: `INSERT INTO metrics.spock_exception_log VALUES ($1, now(), 1,
            '{"password": "example-secret"}', '{"api_key": "example-secret"}', '{"token": 31337}',
            'CREATE SUBSCRIPTION s CONNECTION ''host=192.0.2.1 password=example-secret'' PUBLICATION p',
            'could not connect to postgresql://app:example-secret@192.0.2.1/db')`},
	{name: "spock_resolutions",
		create: `CREATE TABLE metrics.spock_resolutions (connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL, relname text, local_tuple text, remote_tuple text)`,
		insert: `INSERT INTO metrics.spock_resolutions VALUES ($1, now(), 'users',
            '{"id": 1, "password": "example-secret"}', '{"id": 1, "secret": "example-secret"}')`},
	{name: "ds_masking_secret_probe", hidden: "api_token",
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
			drop := "DROP TABLE IF EXISTS metrics." + p.name
			for _, stmt := range []string{drop, p.create} {
				if _, err := f.pool.Exec(ctx, stmt); err != nil {
					t.Fatalf("%s: %v", stmt, err)
				}
			}
			t.Cleanup(func() {
				if _, err := f.pool.Exec(context.Background(), drop); err != nil {
					t.Logf("%s: %v", drop, err)
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
