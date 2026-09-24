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
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// latestTablesProbe is a fixture probe shaped like pg_stat_all_tables, so
// that the test owns its rows and can drop the table afterwards without
// disturbing collected data in the test database.
const latestTablesProbe = "pg_stat_all_tables_latest_test"

// latestNoSchemaProbe is a fixture probe with a text dimension but no
// schemaname column, used to prove the schema filters are ignored for
// probes they cannot apply to.
const latestNoSchemaProbe = "pg_latest_noschema_test"

// latestNoDimensionProbe is a fixture probe with no text dimension column
// at all, which the latest-snapshot query cannot key a DISTINCT ON on.
const latestNoDimensionProbe = "pg_latest_nodim_test"

// latestFixtureRow is one sample inserted into the pg_stat_all_tables
// shaped fixture.
type latestFixtureRow struct {
	age      time.Duration
	database string
	schema   string
	relname  string
	liveTup  int64
}

// latestFixtureRows mirrors what the collector stores for a database whose
// statistics are all zero apart from two user tables: PostgreSQL reports
// n_live_tup = 0 for catalog relations that no VACUUM or ANALYZE has touched
// since the statistics were last reset, which is how issue #499's
// leaderboard ended up listing catalog tables with "0 rows". The older
// public.orders sample proves the DISTINCT ON keeps only the newest row,
// and the otherdb row proves the database filter applies.
var latestFixtureRows = []latestFixtureRow{
	{time.Minute, "appdb", "pg_catalog", "pg_attribute", 0},
	{time.Minute, "appdb", "pg_catalog", "pg_class", 0},
	{time.Minute, "appdb", "information_schema", "sql_features", 0},
	{time.Minute, "appdb", "pg_toast", "pg_toast_1255", 0},
	{time.Minute, "appdb", "pg_toast_temp_3", "pg_toast_16390", 0},
	{time.Minute, "appdb", "public", "orders", 500},
	{10 * time.Minute, "appdb", "public", "orders", 400},
	{time.Minute, "appdb", "sales", "customers", 0},
	{time.Minute, "appdb", "pg_temp_3", "scratch", 7},
	{time.Minute, "otherdb", "public", "elsewhere", 9999},
	// Outside the one-hour window, so never returned.
	{2 * time.Hour, "appdb", "public", "ancient", 123456},
}

// openLatestTestPool connects to the local test database or skips the test,
// following the gating convention of the other DB-backed tests.
func openLatestTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping latest snapshot test")
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
	return pool
}

// createLatestFixtureTable (re)creates a fixture probe table in the metrics
// schema, clears any cached column metadata for it and registers a cleanup
// that drops it again.
func createLatestFixtureTable(t *testing.T, pool *pgxpool.Pool, name, columns string) {
	t.Helper()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS metrics."`+name+`"`); err != nil {
		t.Fatalf("failed to drop fixture table %s: %v", name, err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE metrics."`+name+`" (`+columns+`)`); err != nil {
		t.Fatalf("failed to create fixture table %s: %v", name, err)
	}
	tableColumnCache.Delete(name)
	tableDBColCache.Delete(name)

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS metrics."`+name+`"`); err != nil {
			t.Logf("fixture teardown for %s failed: %v", name, err)
		}
		tableColumnCache.Delete(name)
		tableDBColCache.Delete(name)
	})
}

// setupLatestTablesFixture creates the pg_stat_all_tables shaped fixture and
// fills it with latestFixtureRows for connection 1.
func setupLatestTablesFixture(t *testing.T) *LatestSnapshotHandler {
	t.Helper()

	pool := openLatestTestPool(t)
	t.Cleanup(pool.Close)

	createLatestFixtureTable(t, pool, latestTablesProbe, `
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        database_name text NOT NULL,
        schemaname    text NOT NULL,
        relname       text NOT NULL,
        n_live_tup    bigint,
        table_size    bigint
    `)

	ctx := context.Background()
	now := time.Now().UTC()
	for _, r := range latestFixtureRows {
		if _, err := pool.Exec(ctx,
			`INSERT INTO metrics."`+latestTablesProbe+`"
             (connection_id, collected_at, database_name, schemaname,
              relname, n_live_tup, table_size)
             VALUES (1, $1, $2, $3, $4, $5, 8192)`,
			now.Add(-r.age), r.database, r.schema, r.relname,
			r.liveTup); err != nil {
			t.Fatalf("failed to insert a fixture row: %v", err)
		}
	}

	// A nil authStore makes the RBAC checker treat the caller as a
	// superuser, so the request reaches the query.
	return &LatestSnapshotHandler{datastore: database.NewTestDatastore(pool)}
}

// getLatest issues a GET against the latest-snapshot handler with the given
// query string and returns the recorder.
func getLatest(t *testing.T, h *LatestSnapshotHandler, query string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/latest?"+query, nil)
	rec := httptest.NewRecorder()
	h.handleLatestSnapshot(rec, req)
	return rec
}

// decodeLatest decodes a 200 latest-snapshot response, failing the test on
// any other status.
func decodeLatest(t *testing.T, rec *httptest.ResponseRecorder) LatestSnapshotResponse {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body %q)",
			rec.Code, rec.Body.String())
	}
	var resp LatestSnapshotResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

// qualifiedNames returns schemaname.relname for each response row, in
// response order.
func qualifiedNames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		schema, _ := row["schemaname"].(string)
		rel, _ := row["relname"].(string)
		names = append(names, schema+"."+rel)
	}
	return names
}

// TestLatestSnapshot_ExcludeSystemSchemas is the regression test for issue
// #499: the Table Leaderboard listed pg_catalog and information_schema
// tables with zero rows because the endpoint returned every schema. With
// exclude_system_schemas set, the catalog, information_schema and TOAST
// schemas (including pg_toast_temp_N) are dropped exactly as PostgreSQL
// drops them from pg_stat_user_tables, whilst user schemas, pg_temp_N
// included, remain.
func TestLatestSnapshot_ExcludeSystemSchemas(t *testing.T) {
	h := setupLatestTablesFixture(t)

	resp := decodeLatest(t, getLatest(t, h,
		"connection_id=1&probe_name="+latestTablesProbe+
			"&database_name=appdb&order_by=n_live_tup&order=desc"+
			"&limit=10&exclude_system_schemas=true"))

	got := strings.Join(qualifiedNames(resp.Rows), ",")
	want := "public.orders,pg_temp_3.scratch,sales.customers"
	if got != want {
		t.Errorf("rows = %s, want %s", got, want)
	}
	if resp.TotalCount != 3 {
		t.Errorf("total_count = %d, want 3", resp.TotalCount)
	}
	if live, _ := resp.Rows[0]["n_live_tup"].(float64); live != 500 {
		t.Errorf("public.orders n_live_tup = %v, want the newest sample 500",
			resp.Rows[0]["n_live_tup"])
	}
	for _, row := range resp.Rows {
		for _, internal := range []string{"connection_id", "collected_at", "inserted_at"} {
			if _, ok := row[internal]; ok {
				t.Errorf("row %v exposes internal column %s", row, internal)
			}
		}
	}
}

// TestLatestSnapshot_SystemSchemasIncludedByDefault proves the flag is
// opt-in: without it the endpoint keeps returning every schema, which the
// Vacuum Status section relies on because catalog bloat matters there.
// It also shows the #499 failure mode, where zero-valued catalog rows
// outrank a user table whose statistics are zero too.
func TestLatestSnapshot_SystemSchemasIncludedByDefault(t *testing.T) {
	h := setupLatestTablesFixture(t)

	for _, flag := range []string{"", "&exclude_system_schemas=false"} {
		t.Run("flag="+flag, func(t *testing.T) {
			resp := decodeLatest(t, getLatest(t, h,
				"connection_id=1&probe_name="+latestTablesProbe+
					"&database_name=appdb&order_by=n_live_tup&limit=100"+flag))

			if resp.TotalCount != 8 {
				t.Errorf("total_count = %d, want 8 (every appdb table)",
					resp.TotalCount)
			}
			names := qualifiedNames(resp.Rows)
			joined := "," + strings.Join(names, ",") + ","
			for _, sys := range []string{
				"pg_catalog.pg_attribute", "information_schema.sql_features",
				"pg_toast.pg_toast_1255", "pg_toast_temp_3.pg_toast_16390",
			} {
				if !strings.Contains(joined, ","+sys+",") {
					t.Errorf("rows %v missing system table %s", names, sys)
				}
			}
		})
	}
}

// TestLatestSnapshot_ExcludeSchemasCombinesWithSystemFilter checks that an
// explicit exclude_schemas list still applies, alone and alongside the
// system-schema flag.
func TestLatestSnapshot_ExcludeSchemasCombinesWithSystemFilter(t *testing.T) {
	h := setupLatestTablesFixture(t)

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "explicit list only",
			query: "&exclude_schemas=pg_catalog,+information_schema,,pg_toast,pg_toast_temp_3",
			want:  "public.orders,pg_temp_3.scratch,sales.customers",
		},
		{
			name:  "explicit list with system flag",
			query: "&exclude_schemas=sales&exclude_system_schemas=1",
			want:  "public.orders,pg_temp_3.scratch",
		},
		{
			name:  "none keyword",
			query: "&exclude_schemas=none&exclude_system_schemas=true",
			want:  "public.orders,pg_temp_3.scratch,sales.customers",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := decodeLatest(t, getLatest(t, h,
				"connection_id=1&probe_name="+latestTablesProbe+
					"&database_name=appdb&order_by=n_live_tup&order=DESC"+tt.query))
			if got := strings.Join(qualifiedNames(resp.Rows), ","); got != tt.want {
				t.Errorf("rows = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestLatestSnapshot_OrderingAndLimits covers ascending order, the default
// order_by, limit clamping and the unscoped (all databases) request.
func TestLatestSnapshot_OrderingAndLimits(t *testing.T) {
	h := setupLatestTablesFixture(t)

	t.Run("ascending with limit", func(t *testing.T) {
		resp := decodeLatest(t, getLatest(t, h,
			"connection_id=1&probe_name="+latestTablesProbe+
				"&database_name=appdb&order_by=n_live_tup&order=asc"+
				"&limit=1&exclude_system_schemas=true"))
		if got := strings.Join(qualifiedNames(resp.Rows), ","); got != "sales.customers" {
			t.Errorf("rows = %s, want sales.customers", got)
		}
		if resp.TotalCount != 3 {
			t.Errorf("total_count = %d, want 3", resp.TotalCount)
		}
	})

	t.Run("default order_by and clamped limit", func(t *testing.T) {
		// With no order_by the first dimension column (database_name)
		// ranks the rows, descending by default; limit=500 clamps to 100.
		resp := decodeLatest(t, getLatest(t, h,
			"connection_id=1&probe_name="+latestTablesProbe+
				"&limit=500&exclude_system_schemas=true"))
		if resp.TotalCount != 4 {
			t.Fatalf("total_count = %d, want 4 across both databases",
				resp.TotalCount)
		}
		if db, _ := resp.Rows[0]["database_name"].(string); db != "otherdb" {
			t.Errorf("first row database = %q, want otherdb", db)
		}
	})

	t.Run("collected_at falls back to a dimension column", func(t *testing.T) {
		resp := decodeLatest(t, getLatest(t, h,
			"connection_id=1&probe_name="+latestTablesProbe+
				"&order_by=collected_at&exclude_system_schemas=true"))
		if resp.TotalCount != 4 {
			t.Errorf("total_count = %d, want 4", resp.TotalCount)
		}
	})

	t.Run("no rows for another connection", func(t *testing.T) {
		resp := decodeLatest(t, getLatest(t, h,
			"connection_id=2&probe_name="+latestTablesProbe))
		if len(resp.Rows) != 0 || resp.TotalCount != 0 {
			t.Errorf("rows = %v, total = %d; want none",
				resp.Rows, resp.TotalCount)
		}
		if resp.Rows == nil {
			t.Error("rows must be an empty array, not null")
		}
	})
}

// TestLatestSnapshot_RequestErrors covers the validation failures that need
// a real datastore to reach.
func TestLatestSnapshot_RequestErrors(t *testing.T) {
	h := setupLatestTablesFixture(t)
	base := "connection_id=1&probe_name=" + latestTablesProbe

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"invalid probe name", "connection_id=1&probe_name=bad-name",
			"Invalid probe_name: must contain only letters, numbers, and underscores"},
		{"unknown probe", "connection_id=1&probe_name=zzz_no_such_probe",
			`Probe "zzz_no_such_probe" not found`},
		{"invalid excluded schema", base + "&exclude_schemas=public,bad%3Bname",
			`Invalid schema name in exclude_schemas: "bad;name"`},
		{"invalid order_by syntax", base + "&order_by=n_live_tup%3Bdrop",
			"Invalid order_by: must contain only letters, numbers, and underscores"},
		{"invalid order", base + "&order=sideways",
			"Invalid order: must be asc or desc"},
		{"invalid limit", base + "&limit=0",
			"Invalid limit: must be a positive integer"},
		{"non-numeric limit", base + "&limit=ten",
			"Invalid limit: must be a positive integer"},
		{"unknown order_by column", base + "&order_by=no_such_column",
			`Invalid order_by: column "no_such_column" does not exist in probe "` +
				latestTablesProbe + `" output`},
		{"internal order_by column", base + "&order_by=inserted_at",
			`Invalid order_by: column "inserted_at" does not exist in probe "` +
				latestTablesProbe + `" output`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := getLatest(t, h, tt.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d (body %q)",
					rec.Code, rec.Body.String())
			}
			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode error: %v", err)
			}
			if resp.Error != tt.wantErr {
				t.Errorf("error = %q, want %q", resp.Error, tt.wantErr)
			}
		})
	}
}

// TestLatestSnapshot_SchemaFiltersIgnoredWithoutSchemaColumn proves that the
// schema filters are dropped, rather than producing invalid SQL, for a probe
// with no schemaname column.
func TestLatestSnapshot_SchemaFiltersIgnoredWithoutSchemaColumn(t *testing.T) {
	pool := openLatestTestPool(t)
	t.Cleanup(pool.Close)

	createLatestFixtureTable(t, pool, latestNoSchemaProbe, `
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        datname       name NOT NULL,
        numbackends   integer
    `)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO metrics."`+latestNoSchemaProbe+`"
         VALUES (1, now(), 'appdb', 3), (1, now(), 'pg_catalog', 5)`); err != nil {
		t.Fatalf("failed to insert fixture rows: %v", err)
	}

	h := &LatestSnapshotHandler{datastore: database.NewTestDatastore(pool)}
	resp := decodeLatest(t, getLatest(t, h,
		"connection_id=1&probe_name="+latestNoSchemaProbe+
			"&database_name=appdb&exclude_system_schemas=true&exclude_schemas=public"))
	if resp.TotalCount != 1 {
		t.Fatalf("total_count = %d, want 1", resp.TotalCount)
	}
	if n, _ := resp.Rows[0]["numbackends"].(float64); n != 3 {
		t.Errorf("numbackends = %v, want 3", resp.Rows[0]["numbackends"])
	}
}

// TestLatestSnapshot_NoDimensionColumns covers a probe the endpoint cannot
// key: with no text dimension column there is nothing to DISTINCT ON.
func TestLatestSnapshot_NoDimensionColumns(t *testing.T) {
	pool := openLatestTestPool(t)
	t.Cleanup(pool.Close)

	createLatestFixtureTable(t, pool, latestNoDimensionProbe, `
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        value         bigint
    `)

	h := &LatestSnapshotHandler{datastore: database.NewTestDatastore(pool)}
	rec := getLatest(t, h,
		"connection_id=1&probe_name="+latestNoDimensionProbe+"&order_by=value")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d (body %q)",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no dimension columns found") {
		t.Errorf("unexpected body %q", rec.Body.String())
	}
}

// TestLatestSnapshot_ColumnCaches checks that the column and database-column
// caches are reused while fresh and refreshed once they pass their TTL.
func TestLatestSnapshot_ColumnCaches(t *testing.T) {
	pool := openLatestTestPool(t)
	t.Cleanup(pool.Close)
	createLatestFixtureTable(t, pool, latestNoSchemaProbe, `
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        datname       name NOT NULL
    `)
	ctx := context.Background()

	// A fresh entry is served from the cache without touching the
	// database, so a sentinel value comes straight back.
	tableColumnCache.Store(latestNoSchemaProbe, &columnCache{
		allColumns: []string{"sentinel"},
		colTypes:   map[string]string{"sentinel": "text"},
		cachedAt:   time.Now(),
	})
	cols, _, err := discoverAllColumnsCached(ctx, pool, latestNoSchemaProbe)
	if err != nil || len(cols) != 1 || cols[0] != "sentinel" {
		t.Fatalf("fresh cache: cols = %v, err = %v; want the sentinel", cols, err)
	}
	tableDBColCache.Store(latestNoSchemaProbe, &dbColCache{
		dbCol: "sentinel", cachedAt: time.Now(),
	})
	dbCol, err := resolveDatabaseColumnCached(ctx, pool, latestNoSchemaProbe)
	if err != nil || dbCol != "sentinel" {
		t.Fatalf("fresh cache: dbCol = %q, err = %v; want the sentinel", dbCol, err)
	}

	// An expired entry is rediscovered from information_schema.
	expired := time.Now().Add(-2 * columnCacheTTL)
	tableColumnCache.Store(latestNoSchemaProbe, &columnCache{
		allColumns: []string{"sentinel"}, cachedAt: expired,
	})
	cols, types, err := discoverAllColumnsCached(ctx, pool, latestNoSchemaProbe)
	if err != nil {
		t.Fatalf("expired cache: %v", err)
	}
	if strings.Join(cols, ",") != "connection_id,collected_at,datname" ||
		types["datname"] != "name" {
		t.Errorf("expired cache: cols = %v, types = %v", cols, types)
	}
	tableDBColCache.Store(latestNoSchemaProbe, &dbColCache{
		dbCol: "sentinel", cachedAt: expired,
	})
	if dbCol, err = resolveDatabaseColumnCached(ctx, pool, latestNoSchemaProbe); err != nil ||
		dbCol != "datname" {
		t.Errorf("expired cache: dbCol = %q, err = %v; want datname", dbCol, err)
	}
}

// TestLatestSnapshot_DatastoreFailures covers the internal-error responses
// raised when the datastore cannot answer the discovery queries.
func TestLatestSnapshot_DatastoreFailures(t *testing.T) {
	pool := openLatestTestPool(t)
	// A closed pool fails every query, which is the simplest way to reach
	// the handler's internal-error branches without a fake database.
	pool.Close()
	h := &LatestSnapshotHandler{datastore: database.NewTestDatastore(pool)}

	rec := getLatest(t, h, "connection_id=1&probe_name="+latestTablesProbe)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d (body %q)",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Failed to verify probe table") {
		t.Errorf("unexpected body %q", rec.Body.String())
	}

	ctx := context.Background()
	if _, _, err := discoverAllColumns(ctx, pool, latestTablesProbe); err == nil {
		t.Error("discoverAllColumns on a closed pool: expected an error")
	}
	tableColumnCache.Delete(latestTablesProbe)
	if _, _, err := discoverAllColumnsCached(ctx, pool, latestTablesProbe); err == nil {
		t.Error("discoverAllColumnsCached on a closed pool: expected an error")
	}
	tableDBColCache.Delete(latestTablesProbe)
	if _, err := resolveDatabaseColumnCached(ctx, pool, latestTablesProbe); err == nil {
		t.Error("resolveDatabaseColumnCached on a closed pool: expected an error")
	}
	if _, _, err := queryLatestSnapshot(ctx, pool, latestTablesProbe, 1, "", "",
		[]string{"relname"}, []string{"relname"}, map[string]string{"relname": "text"},
		"relname", "desc", 10, latestSchemaFilter{}); err == nil {
		t.Error("queryLatestSnapshot on a closed pool: expected an error")
	}
}
