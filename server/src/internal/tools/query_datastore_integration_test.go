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
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// Integration tests for query_datastore and describe_datastore against
// the local test datastore. Two connections are seeded, one shared (which
// a signed-in non-superuser can read) and one private to another user,
// with rows behind every scoping rule, so each test can show the caller
// gets exactly the rows for the connection they can read.
//
// The configuration tables are created from the source catalog itself,
// one column per permitted column, because the server module cannot
// import the collector's schema manager. That proves the scoping against
// a real planner and real rows, but not that the catalog matches the
// production schema; TestConfigurationSources* in datastore_catalog_test.go
// and the collector's schema are the check on that.

// dsProbeSchema is a metric source with two entity rows per connection.
const dsProbeSchema = `
CREATE TABLE metrics.ds_probe (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    datname TEXT,
    numbackends INTEGER,
    xact_commit BIGINT
);
COMMENT ON TABLE metrics.ds_probe IS 'Test probe for query_datastore.';
`

// dsFixtureFunctions creates the helper functions newDatastoreFixture
// builds the catalog's tables with. The table, column and type names come
// from the catalog, so they are passed as parameters and quoted by
// format's %I; a type name is first resolved with to_regtype, and the
// statement uses the resolved name.
const dsFixtureFunctions = `
CREATE OR REPLACE FUNCTION public.ds_fixture_drop_tables(tbls text[])
RETURNS void LANGUAGE plpgsql AS $fn$
BEGIN
    IF cardinality(tbls) > 0 THEN
        EXECUTE format('DROP TABLE IF EXISTS %s CASCADE',
            (SELECT string_agg(format('public.%I', tbl), ', ') FROM unnest(tbls) AS tbl));
    END IF;
END
$fn$;

CREATE OR REPLACE FUNCTION public.ds_fixture_create_table(tbl text, cols text[], types text[])
RETURNS void LANGUAGE plpgsql AS $fn$
DECLARE
    i integer;
    typ regtype;
BEGIN
    EXECUTE format('CREATE TABLE IF NOT EXISTS public.%I ()', tbl);
    FOR i IN 1 .. coalesce(cardinality(cols), 0) LOOP
        typ := to_regtype(types[i]);
        IF typ IS NULL THEN
            RAISE EXCEPTION 'unknown type % for column %.%', types[i], tbl, cols[i];
        END IF;
        EXECUTE format('ALTER TABLE public.%I ADD COLUMN IF NOT EXISTS %I %s', tbl, cols[i], typ::text);
    END LOOP;
END
$fn$;
`

// dsFixtureFunctionsDrop removes the functions dsFixtureFunctions creates.
const dsFixtureFunctionsDrop = `
DROP FUNCTION IF EXISTS public.ds_fixture_drop_tables(text[]);
DROP FUNCTION IF EXISTS public.ds_fixture_create_table(text, text[], text[]);
`

// fixtureStatement is one constant seed statement and its parameters.
type fixtureStatement struct {
	sql  string
	args []any
}

// datastoreFixture holds the seeded datastore and the IDs the tests
// assert on.
type datastoreFixture struct {
	pool    *pgxpool.Pool
	ds      *database.Datastore
	checker *auth.RBACChecker
	store   *auth.AuthStore
	visible int
	hidden  int
}

// newDatastoreFixture builds the catalog's configuration tables and the
// test probe, and seeds them.
func newDatastoreFixture(t *testing.T) *datastoreFixture {
	t.Helper()
	pool, ds, cleanup := newToolsTestPool(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, dsFixtureFunctions); err != nil {
		t.Fatalf("create fixture functions: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), dsFixtureFunctionsDrop); err != nil {
			t.Logf("drop fixture functions: %v", err)
		}
	})

	var tables, stale []string
	for _, src := range configurationSources() {
		tables = append(tables, src.Table)
		// newToolsTestPool has just recreated these with serial ids.
		if src.Table != "connections" && src.Table != "metric_baselines" {
			stale = append(stale, src.Table)
		}
	}
	// Another package's tests may have left the real schema behind in a
	// shared database (CI uses one), with different column definitions
	// such as identity ids; start from the catalog's own shape.
	if _, err := pool.Exec(ctx, `SELECT public.ds_fixture_drop_tables($1)`, stale); err != nil {
		t.Fatalf("drop stale datastore tables: %v", err)
	}
	for _, src := range configurationSources() {
		cols := make([]string, 0, len(src.Columns))
		types := make([]string, 0, len(src.Columns))
		for _, c := range src.Columns {
			cols = append(cols, c.Name)
			types = append(types, c.DataType)
		}
		if _, err := pool.Exec(ctx, `SELECT public.ds_fixture_create_table($1, $2, $3)`,
			src.Table, cols, types); err != nil {
			t.Fatalf("schema for %q: %v", src.Table, err)
		}
	}
	// Cleanups run last-in first-out, so these tables go before the
	// helper functions and the pool.
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `SELECT public.ds_fixture_drop_tables($1)`, tables); err != nil {
			t.Logf("drop datastore fixture tables: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, dsProbeSchema); err != nil {
		t.Fatalf("probe schema: %v", err)
	}

	store, storeCleanup := newRBACRegressionTestStore(t)
	t.Cleanup(storeCleanup)

	f := &datastoreFixture{
		pool:    pool,
		ds:      ds,
		checker: auth.NewRBACCheckerForDatastore(store, ds),
		store:   store,
		visible: seedBaselineConnection(t, pool, "visible-conn", true, ""),
		hidden:  seedBaselineConnection(t, pool, "hidden-conn", false, "someone-else"),
	}

	ids := []any{f.visible, f.hidden}
	seed := []fixtureStatement{
		{sql: `INSERT INTO cluster_groups (id, name) VALUES (1, 'visible-group'), (2, 'hidden-group')`},
		{sql: `INSERT INTO clusters (id, group_id, name) VALUES (10, 1, 'visible-cluster'), (20, 2, 'hidden-cluster')`},
		{sql: `UPDATE connections SET cluster_id = 10 WHERE id = $1`, args: []any{f.visible}},
		{sql: `UPDATE connections SET cluster_id = 20 WHERE id = $1`, args: []any{f.hidden}},
		{sql: `INSERT INTO alerts (id, alert_type, connection_id, title, status, triggered_at) VALUES
            (1, 'threshold', $1, 'visible alert', 'active', now() - interval '10 minutes'),
            (2, 'threshold', $2, 'hidden alert', 'active', now() - interval '10 minutes'),
            (3, 'system', NULL, 'system alert', 'active', now() - interval '10 minutes')`, args: ids},
		{sql: `INSERT INTO alert_acknowledgments (id, alert_id, acknowledged_by, message) VALUES
            (1, 1, 'alice', 'visible ack'), (2, 2, 'alice', 'hidden ack'), (3, 3, 'alice', 'system ack')`},
		{sql: `INSERT INTO blackouts (id, scope, connection_id, cluster_id, group_id, reason, start_time) VALUES
            (1, 'estate', NULL, NULL, NULL, 'estate', now()),
            (2, 'server', $1, NULL, NULL, 'visible server', now()),
            (3, 'server', $2, NULL, NULL, 'hidden server', now()),
            (4, 'cluster', NULL, 10, NULL, 'visible cluster', now()),
            (5, 'cluster', NULL, 20, NULL, 'hidden cluster', now()),
            (6, 'group', NULL, NULL, 1, 'visible group', now()),
            (7, 'group', NULL, NULL, 2, 'hidden group', now()),
            (8, 'cluster', NULL, NULL, NULL, 'malformed', now())`, args: ids},
		{sql: `INSERT INTO alert_rules (id, name, metric_name) VALUES (1, 'rule one', 'm1'), (2, 'rule two', 'm2')`},
		{sql: `INSERT INTO probe_configs (id, name, scope, connection_id, cluster_id, group_id) VALUES
            (1, 'global probe', 'global', NULL, NULL, NULL),
            (2, 'visible server probe', 'server', $1, NULL, NULL),
            (3, 'hidden server probe', 'server', $2, NULL, NULL),
            (4, 'visible cluster probe', 'cluster', NULL, 10, NULL),
            (5, 'hidden group probe', 'group', NULL, NULL, 2),
            (6, 'malformed probe', 'server', NULL, NULL, NULL)`, args: ids},
		{sql: `INSERT INTO alert_thresholds (id, rule_id, scope, connection_id, cluster_id, group_id, database_name) VALUES
            (1, 1, 'server', $1, NULL, NULL, 'visible server'),
            (2, 1, 'server', $2, NULL, NULL, 'hidden server'),
            (3, 1, 'cluster', NULL, 20, NULL, 'hidden cluster'),
            (4, 1, 'group', NULL, NULL, 1, 'visible group')`, args: ids},
		{sql: `INSERT INTO metric_baselines (connection_id, metric_name, period_type) VALUES
            ($1, 'visible_metric', 'all'), ($2, 'hidden_metric', 'all')`, args: ids},
		{sql: `INSERT INTO metrics.ds_probe (connection_id, collected_at, datname, numbackends, xact_commit) VALUES
            ($1, now() - interval '50 minutes', 'app', 1, 100),
            ($1, now() - interval '20 minutes', 'app', 3, 150),
            ($1, now() - interval '20 minutes', 'postgres', 2, 10),
            ($2, now() - interval '20 minutes', 'app', 9, 999),
            ($1, now() - interval '3 hours', 'app', 7, 50)`, args: ids},
	}
	for _, stmt := range seed {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed %q: %v", stmt.sql, err)
		}
	}
	return f
}

// tool returns query_datastore with the fixture's real checker and
// visibility lister.
func (f *datastoreFixture) tool() Tool {
	return QueryDatastoreTool(f.pool, f.checker, database.NewVisibilityLister(f.ds))
}

// userCtx is a signed-in non-superuser with no grants, who can read the
// shared connection only.
func (f *datastoreFixture) userCtx() context.Context {
	return nonSuperuserContextInt(4242, "restricted")
}

// call runs tool with args under ctx and returns the response text and
// error flag.
func call(t *testing.T, tool Tool, ctx context.Context, args map[string]any) (string, bool) {
	t.Helper()
	args["__context"] = ctx
	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatalf("empty response: %+v", resp)
	}
	return resp.Content[0].Text, resp.IsError
}

// mustQuery runs a query that must succeed and returns its result rows,
// keyed by column name.
func mustQuery(t *testing.T, tool Tool, ctx context.Context, args map[string]any) []map[string]string {
	t.Helper()
	text, isErr := call(t, tool, ctx, args)
	if isErr {
		t.Fatalf("query %v failed: %s", args, text)
	}
	return parseResultRows(t, text)
}

// parseResultRows reads the TSV block that follows the "Results (...):"
// line of a query_datastore response.
func parseResultRows(t *testing.T, text string) []map[string]string {
	t.Helper()
	idx := strings.Index(text, "Results (")
	if idx < 0 {
		t.Fatalf("no results in response: %s", text)
	}
	body := text[idx:]
	nl := strings.Index(body, "\n")
	if nl < 0 {
		t.Fatalf("no result header in response: %s", text)
	}
	lines := strings.Split(body[nl+1:], "\n")
	header := strings.Split(lines[0], "\t")
	var rows []map[string]string
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		values := strings.Split(line, "\t")
		row := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(values) {
				row[h] = values[i]
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// column returns one column of rows, sorted.
func column(rows []map[string]string, name string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r[name])
	}
	slices.Sort(out)
	return out
}

func assertColumn(t *testing.T, rows []map[string]string, name string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := column(rows, name); !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func TestQueryDatastoreScopesEveryRuleIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	tool := f.tool()
	user := f.userCtx()
	admin := superuserContext()
	vis, hid := fmt.Sprint(f.visible), fmt.Sprint(f.hidden)

	t.Run("metric source", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "ds_probe"})
		assertColumn(t, rows, "connection_id", vis, vis, vis)
		rows = mustQuery(t, tool, admin, map[string]any{"source": "ds_probe"})
		assertColumn(t, rows, "connection_id", vis, vis, vis, hid)
	})

	t.Run("connections", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "connections", "columns": []any{"id", "name"}})
		assertColumn(t, rows, "name", "visible-conn")
		rows = mustQuery(t, tool, admin, map[string]any{"source": "connections", "columns": []any{"name"}})
		assertColumn(t, rows, "name", "hidden-conn", "visible-conn")
	})

	t.Run("connection source", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "metric_baselines"})
		assertColumn(t, rows, "metric_name", "visible_metric")
	})

	t.Run("alert", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "alerts", "columns": []any{"title"}})
		assertColumn(t, rows, "title", "system alert", "visible alert")
		rows = mustQuery(t, tool, user, map[string]any{
			"source": "alerts", "columns": []any{"title"}, "connection_ids": []any{float64(f.visible)}})
		assertColumn(t, rows, "title", "visible alert")
		rows = mustQuery(t, tool, admin, map[string]any{"source": "alerts", "columns": []any{"title"}, "time_start": "1h"})
		assertColumn(t, rows, "title", "hidden alert", "system alert", "visible alert")
	})

	t.Run("via_alert", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "alert_acknowledgments", "columns": []any{"message"}})
		assertColumn(t, rows, "message", "system ack", "visible ack")
	})

	t.Run("hierarchy", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "blackouts", "columns": []any{"reason"}})
		assertColumn(t, rows, "reason", "estate", "visible cluster", "visible group", "visible server")
		rows = mustQuery(t, tool, admin, map[string]any{"source": "blackouts", "columns": []any{"reason"}})
		if len(rows) != 8 {
			t.Errorf("superuser should see every blackout, got %v", column(rows, "reason"))
		}
	})

	// probe_configs and alert_thresholds share the blackouts rule; these
	// cases pin that their columns are wired to it too.
	t.Run("hierarchy probe_configs and alert_thresholds", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "probe_configs", "columns": []any{"name"}})
		assertColumn(t, rows, "name", "global probe", "visible cluster probe", "visible server probe")
		rows = mustQuery(t, tool, admin, map[string]any{"source": "probe_configs", "columns": []any{"name"}})
		if len(rows) != 6 {
			t.Errorf("superuser should see every probe config, got %v", column(rows, "name"))
		}
		rows = mustQuery(t, tool, user, map[string]any{"source": "alert_thresholds", "columns": []any{"database_name"}})
		assertColumn(t, rows, "database_name", "visible group", "visible server")
		rows = mustQuery(t, tool, admin, map[string]any{"source": "alert_thresholds", "columns": []any{"database_name"}})
		if len(rows) != 4 {
			t.Errorf("superuser should see every threshold, got %v", column(rows, "database_name"))
		}
	})

	t.Run("cluster and group", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "clusters", "columns": []any{"name"}})
		assertColumn(t, rows, "name", "visible-cluster")
		rows = mustQuery(t, tool, user, map[string]any{"source": "cluster_groups", "columns": []any{"name"}})
		assertColumn(t, rows, "name", "visible-group")
	})

	t.Run("global", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "alert_rules", "columns": []any{"name"}})
		assertColumn(t, rows, "name", "rule one", "rule two")
	})

	t.Run("hidden connection named", func(t *testing.T) {
		text, isErr := call(t, tool, user, map[string]any{
			"source": "ds_probe", "connection_ids": []any{float64(f.hidden)}})
		if !isErr || !strings.Contains(text, "connection not found or not accessible.") {
			t.Fatalf("expected the generic denial, got: %s", text)
		}
		if strings.Contains(text, "hidden-conn") || !strings.Contains(text, "visible-conn") {
			t.Errorf("denial should list only visible connections: %s", text)
		}
		// A connection that does not exist gets the same text.
		missing, _ := call(t, tool, user, map[string]any{
			"source": "ds_probe", "connection_ids": []any{float64(99999)}})
		if missing != text {
			t.Errorf("missing and hidden connections should be indistinguishable:\n%s\n%s", missing, text)
		}
	})
}

func TestQueryDatastoreEmptyVisibleSetIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	if _, err := f.pool.Exec(context.Background(), "UPDATE connections SET is_shared = FALSE, owner_username = 'someone-else'"); err != nil {
		t.Fatal(err)
	}
	tool := f.tool()
	user := f.userCtx()

	// Global sources still return everything.
	rows := mustQuery(t, tool, user, map[string]any{"source": "alert_rules"})
	if len(rows) != 2 {
		t.Errorf("alert_rules: got %d rows, want 2", len(rows))
	}
	// Connection-scoped sources return nothing, rather than an error or
	// every row.
	for _, source := range []string{"ds_probe", "connections", "metric_baselines", "clusters", "cluster_groups"} {
		text, isErr := call(t, tool, user, map[string]any{"source": source})
		if isErr || !strings.Contains(text, "Results (0 rows)") {
			t.Errorf("%s: expected no rows, got: %s", source, text)
		}
	}
	// Estate-wide blackouts and system alerts are not about any
	// connection, so they still show.
	rows = mustQuery(t, tool, user, map[string]any{"source": "blackouts", "columns": []any{"reason"}})
	assertColumn(t, rows, "reason", "estate")
	rows = mustQuery(t, tool, user, map[string]any{"source": "alerts", "columns": []any{"title"}})
	assertColumn(t, rows, "title", "system alert")
}

func TestQueryDatastoreTokenScopeIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	if err := f.store.CreateUser("root", "Password1234", "Root", "", ""); err != nil {
		t.Fatal(err)
	}
	userID, err := f.store.GetUserID("root")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := f.store.AsActor(auth.SystemActor()).CreateToken("root", "scoped token", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(token.ID,
		[]auth.ScopedConnection{{ConnectionID: f.visible, AccessLevel: "read"}}, true); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(superuserContext(), auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.TokenIDContextKey, token.ID)

	// A superuser's token scoped to one connection reads that connection
	// alone.
	rows := mustQuery(t, f.tool(), ctx, map[string]any{"source": "connections", "columns": []any{"name"}})
	assertColumn(t, rows, "name", "visible-conn")
}

func TestQueryDatastoreShapesIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	tool := f.tool()
	user := f.userCtx()
	admin := superuserContext()
	vis := fmt.Sprint(f.visible)

	t.Run("latest", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{
			"source": "ds_probe", "shape": "latest", "columns": []any{"connection_id", "datname", "xact_commit"}})
		if len(rows) != 2 {
			t.Fatalf("latest: want one row per database, got %v", rows)
		}
		for _, r := range rows {
			if r["connection_id"] != vis {
				t.Errorf("latest returned a hidden connection: %v", r)
			}
			if r["datname"] == "app" && r["xact_commit"] != "150" {
				t.Errorf("latest app row should be the newest, got %v", r)
			}
		}
	})

	t.Run("aggregate with buckets", func(t *testing.T) {
		rows := mustQuery(t, tool, admin, map[string]any{
			"source": "ds_probe", "shape": "aggregate", "aggregation": "max",
			"columns": []any{"xact_commit"}, "group_by": []any{"connection_id"},
			"time_start": "1h", "buckets": float64(2)})
		if len(rows) != 3 {
			t.Fatalf("expected two buckets for the visible connection and one for the hidden, got %v", rows)
		}
		for _, r := range rows {
			if r["bucket_time"] == "" {
				t.Errorf("row lacks a bucket: %v", r)
			}
		}
		rows = mustQuery(t, tool, user, map[string]any{
			"source": "ds_probe", "shape": "aggregate", "aggregation": "max",
			"columns": []any{"xact_commit"}, "group_by": []any{"connection_id"},
			"time_start": "1h", "buckets": float64(2)})
		assertColumn(t, rows, "max_xact_commit", "100", "150")
	})

	t.Run("aggregate count", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{
			"source": "ds_probe", "shape": "aggregate", "aggregation": "count", "group_by": []any{"datname"}})
		if len(rows) != 2 {
			t.Fatalf("count by datname: %v", rows)
		}
	})

	t.Run("paging", func(t *testing.T) {
		args := func(page int) map[string]any {
			return map[string]any{"source": "ds_probe", "columns": []any{"xact_commit"}, "page_size": float64(2),
				"page": float64(page), "order_by": []any{map[string]any{"column": "xact_commit"}}}
		}
		text, isErr := call(t, tool, user, args(1))
		if isErr || !strings.Contains(text, "Results (rows 1-2, more available - use page=2 for the next page)") {
			t.Fatalf("page 1: %s", text)
		}
		assertColumn(t, parseResultRows(t, text), "xact_commit", "10", "100")
		text, _ = call(t, tool, user, args(2))
		if !strings.Contains(text, "Results (rows 3-3):") {
			t.Fatalf("page 2: %s", text)
		}
		assertColumn(t, parseResultRows(t, text), "xact_commit", "150")
	})

	t.Run("explain", func(t *testing.T) {
		text, isErr := call(t, tool, admin, map[string]any{
			"source": "ds_probe", "explain": true,
			"filters": []any{map[string]any{"column": "datname", "op": "=", "value": "app"}}})
		if isErr || !strings.HasPrefix(text, "Datastore Query Plan: ds_probe") || !strings.Contains(text, "Scan") {
			t.Fatalf("explain: %s", text)
		}
		if strings.Contains(text, "\t") {
			t.Errorf("plan should be plain text: %q", text)
		}
	})

	t.Run("time window", func(t *testing.T) {
		rows := mustQuery(t, tool, user, map[string]any{"source": "ds_probe", "time_start": "4h", "columns": []any{"xact_commit"}})
		assertColumn(t, rows, "xact_commit", "10", "100", "150", "50")
	})
}

func TestQueryDatastoreFailuresIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	user := f.userCtx()

	runCase := func(name string, tool Tool, ctx context.Context, args map[string]any, want string) {
		t.Run(name, func(t *testing.T) {
			text, isErr := call(t, tool, ctx, args)
			if !isErr || !strings.Contains(text, want) {
				t.Errorf("expected error containing %q, got (error=%v): %s", want, isErr, text)
			}
		})
	}

	runCase("visibility failure", QueryDatastoreTool(f.pool, f.checker, failingLister()), user,
		map[string]any{"source": "alerts"}, "Failed to resolve accessible connections")
	runCase("unknown source", f.tool(), user,
		map[string]any{"source": "email_recipients"}, "call describe_datastore")
	runCase("builder rejection", f.tool(), user,
		map[string]any{"source": "connections", "columns": []any{"password_encrypted"}}, "unknown column")

	done, cancel := context.WithCancel(user)
	cancel()
	runCase("lookup failure", f.tool(), done, map[string]any{"source": "ds_probe"}, "Failed to look up the source")
	runCase("execution failure", f.tool(), done, map[string]any{"source": "alerts"}, "")
	runCase("explain failure", f.tool(), done, map[string]any{"source": "alerts", "explain": true}, "")
	runCase("explain restricted", f.tool(), user, map[string]any{"source": "alerts", "explain": true},
		"explain is available only to callers who can access every connection")

	// A nil checker sees nothing, so a connection-scoped source is empty
	// rather than unscoped.
	text, isErr := call(t, QueryDatastoreTool(f.pool, nil, nil), superuserContext(), map[string]any{"source": "connections"})
	if isErr || !strings.Contains(text, "Results (0 rows)") {
		t.Errorf("nil checker: %s", text)
	}
}

func TestDescribeDatastoreIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	tool := DescribeDatastoreTool(f.pool)
	ctx := superuserContext()

	text, isErr := call(t, tool, ctx, map[string]any{})
	if isErr {
		t.Fatalf("list: %s", text)
	}
	for _, want := range []string{
		"alerts\tconfiguration\ttriggered_at\talert\t",
		"blackouts\tconfiguration\tstart_time\thierarchy\t",
		"alert_rules\tconfiguration\t\tglobal\t",
		"ds_probe\tmetric\tcollected_at\tconnection\tTest probe for query_datastore.",
		"pg_connectivity\tmetric\tcollected_at\tconnection\tMetrics collected by the pg_connectivity probe.",
		"- via_alert: ",
		"Scoping rules:",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("list lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "email_recipients") || strings.Contains(text, "notification_channels") {
		t.Errorf("list names a table query_datastore refuses:\n%s", text)
	}
	if blank, _ := call(t, tool, ctx, map[string]any{"source": "  "}); blank != text {
		t.Error("a blank source should list every source")
	}

	text, isErr = call(t, tool, ctx, map[string]any{"source": "ds_probe"})
	if isErr {
		t.Fatalf("describe probe: %s", text)
	}
	for _, want := range []string{
		"Source: ds_probe\nKind: metric\n", "Time column: collected_at",
		"Shape latest returns the newest row per: connection_id, datname",
		"xact_commit\tbigint\tmetric", "datname\ttext\tdimension",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("probe description lacks %q:\n%s", want, text)
		}
	}

	text, isErr = call(t, tool, ctx, map[string]any{"source": "connections"})
	if isErr || !strings.Contains(text, "Time column: none") || !strings.Contains(text, "Scoping: connection") {
		t.Fatalf("describe connections: %s", text)
	}
	if cols := text[strings.Index(text, "Columns ("):]; strings.Contains(cols, "password") || strings.Contains(cols, "sslkey") {
		t.Errorf("connections description offers a credential column:\n%s", text)
	}
	if strings.Contains(text, "Shape latest") {
		t.Errorf("a configuration source has no latest shape:\n%s", text)
	}

	for _, tc := range []struct {
		name string
		args map[string]any
		ctx  context.Context
		want string
	}{
		{"unknown source", map[string]any{"source": "email_recipients"}, ctx, "call describe_datastore"},
		{"non-string source", map[string]any{"source": float64(1)}, ctx, "source must be a string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := call(t, tool, tc.ctx, tc.args)
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("got (error=%v) %s, want %q", isErr, text, tc.want)
			}
		})
	}

	done, cancel := context.WithCancel(ctx)
	cancel()
	if text, isErr := call(t, tool, done, map[string]any{}); !isErr || !strings.Contains(text, "Failed to list the datastore sources") {
		t.Errorf("list failure: %s", text)
	}
	if text, isErr := call(t, tool, done, map[string]any{"source": "ds_probe"}); !isErr || !strings.Contains(text, "Failed to look up the source") {
		t.Errorf("lookup failure: %s", text)
	}

	// Without a context the handler falls back to the background one.
	resp, err := tool.Handler(map[string]any{"source": "alert_rules"})
	if err != nil || resp.IsError {
		t.Errorf("no context: %v %+v", err, resp)
	}
}
