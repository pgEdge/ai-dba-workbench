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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// searchPathSchema is the schema the search-path test places first on
// its pool's search_path, standing in for a role-named schema such as
// the one a role named coldfront owns.
const searchPathSchema = "ai_wb_ds_search_path"

// searchPathConnectionID is the one connection the schema holds. It is
// far above any serial ID the shared test database hands out, so it
// names no row of a connections table in public.
const searchPathConnectionID = 987654

// searchPathFixture creates the schema and the connections table in it,
// unqualified as the collector's migrations create it, once the
// search_path names the schema first, with the columns the source
// catalog permits. It seeds one live connection.
const searchPathFixture = `
DROP SCHEMA IF EXISTS ai_wb_ds_search_path CASCADE;
CREATE SCHEMA ai_wb_ds_search_path;
CREATE TABLE connections (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    host VARCHAR(255),
    hostaddr VARCHAR(255),
    port INTEGER,
    database_name VARCHAR(255),
    username VARCHAR(255),
    sslmode VARCHAR(32),
    is_monitored BOOLEAN,
    is_shared BOOLEAN,
    enabled BOOLEAN,
    owner_username VARCHAR(255),
    cluster_id INTEGER,
    role VARCHAR(64),
    membership_source VARCHAR(16),
    connection_error TEXT,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);
INSERT INTO connections (id, name) VALUES (987654, 'search-path-conn');
`

// TestQueryDatastoreFollowsSearchPathIntegration checks that
// query_datastore reads the Workbench's ordinary tables through the
// role's search_path rather than from public. The collector's migrations
// create them unqualified, so on a datastore whose role has a schema of
// its own first on the search_path (a role named coldfront owns one
// named coldfront) they live there, and a query naming
// public.connections failed with 42P01. public.connections exists here
// too, created by newToolsTestPool, but holds neither the seeded name
// nor the seeded ID, so a query that read it would fail the assertions
// below rather than pass by accident.
func TestQueryDatastoreFollowsSearchPathIntegration(t *testing.T) {
	base, _, cleanup := newToolsTestPool(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	cfg, err := pgxpool.ParseConfig(os.Getenv("TEST_AI_WORKBENCH_SERVER"))
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = searchPathSchema + ", public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect with search_path: %v", err)
	}
	t.Cleanup(func() {
		if _, err := base.Exec(context.Background(),
			"DROP SCHEMA IF EXISTS ai_wb_ds_search_path CASCADE"); err != nil {
			t.Logf("drop search_path schema: %v", err)
		}
		pool.Close()
	})

	if _, err := pool.Exec(ctx, searchPathFixture); err != nil {
		t.Fatalf("create search_path fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, dsProbeSchema); err != nil {
		t.Fatalf("probe schema: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO metrics.pg_sys_os_info (connection_id, collected_at, datname, numbackends, xact_commit)
         VALUES ($1, now() - interval '5 minutes', 'app', 1, 1)`,
		searchPathConnectionID); err != nil {
		t.Fatalf("seed probe row: %v", err)
	}

	tool := QueryDatastoreTool(pool, testRBACChecker(t), nil, nil)
	admin := superuserContext()
	id := fmt.Sprint(searchPathConnectionID)

	t.Run("configuration source", func(t *testing.T) {
		rows := mustQuery(t, tool, admin, map[string]any{
			"source": "connections", "columns": []any{"id", "name"},
		})
		assertColumn(t, rows, "name", "search-path-conn")
		assertColumn(t, rows, "id", id)
	})

	// A caller who may read every connection has each named ID checked
	// with datastoreConnectionsExist, and the rows kept to live
	// connections by liveConnectionCondition; both read connections.
	t.Run("metric source with connection_ids", func(t *testing.T) {
		rows := mustQuery(t, tool, admin, map[string]any{
			"source":         "pg_sys_os_info",
			"connection_ids": []any{float64(searchPathConnectionID)},
		})
		assertColumn(t, rows, "connection_id", id)
	})

	t.Run("unknown connection is still refused", func(t *testing.T) {
		text, isErr := call(t, tool, admin, map[string]any{
			"source":         "pg_sys_os_info",
			"connection_ids": []any{float64(searchPathConnectionID + 1)},
		})
		if !isErr || strings.Contains(text, "Results (") {
			t.Errorf("an unknown connection was not refused: %s", text)
		}
	})
}
