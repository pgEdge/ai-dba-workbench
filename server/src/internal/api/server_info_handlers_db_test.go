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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	collectordb "github.com/pgedge/ai-workbench/collector/src/database"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// serverInfoSchemaSuffix names the scratch database the server info
// queries are tested in, migrated to the collector's real schema so the
// queries are checked against the columns the collector creates. It is
// separate from the shared test database, whose metrics tables other
// tests in this package create with other shapes.
const serverInfoSchemaSuffix = "_api_server_info"

// serverInfoSchemaStatementsQuery returns the scratch database's name and
// the statements that drop and create it.
const serverInfoSchemaStatementsQuery = `
        SELECT n,
            format('DROP DATABASE IF EXISTS %I WITH (FORCE)', n),
            format('CREATE DATABASE %I', n)
        FROM (SELECT current_database() || $1 AS n) s
    `

// newServerInfoSchemaPool creates the scratch database, migrates it to the
// collector's latest schema and returns a pool on it, dropped when the
// test ends.
func newServerInfoSchemaPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping server info schema test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(admin.Close)

	var name, dropSQL, createSQL string
	if err := admin.QueryRow(ctx, serverInfoSchemaStatementsQuery, serverInfoSchemaSuffix).
		Scan(&name, &dropSQL, &createSQL); err != nil {
		t.Fatalf("name the scratch database: %v", err)
	}
	if len(name) > 63 {
		t.Fatalf("scratch database name %q is longer than 63 bytes", name)
	}
	if _, err := admin.Exec(ctx, dropSQL); err != nil {
		t.Fatalf("drop stale %s: %v", name, err)
	}
	if _, err := admin.Exec(ctx, createSQL); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), dropSQL); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection to %s: %v", name, err)
	}
	defer conn.Release()
	if err := collectordb.NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("migrate collector schema: %v", err)
	}
	return pool
}

// seedServerInfo inserts one connection and an older and a newer sample
// of every probe the server info queries read, and returns the
// connection's ID.
func seedServerInfo(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	ctx := context.Background()
	var id int
	if err := pool.QueryRow(ctx, `
        INSERT INTO connections (name, host, port, database_name, username, owner_username)
        VALUES ('server-info', '192.0.2.10', 5432, 'postgres', 'monitor', 'test-owner')
        RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	// The collector creates each week's partition as it writes; a
	// default partition stands in for them here. The table names are
	// constants.
	for _, table := range []string{"pg_sys_os_info", "pg_sys_cpu_info", "pg_sys_memory_info",
		"pg_sys_disk_info", "pg_server_info", "pg_database", "pg_extension", "pg_settings"} {
		// nosemgrep: go_sql_rule-concat-sqli
		if _, err := pool.Exec(ctx, "CREATE TABLE metrics."+table+"_default PARTITION OF metrics."+
			table+" DEFAULT"); err != nil {
			t.Fatalf("create the default partition of %s: %v", table, err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, at := range []time.Time{now.Add(-time.Hour), now} {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO metrics.pg_sys_os_info (connection_id, collected_at, name, version, architecture, host_name)
              VALUES ($1, $2, 'Linux', '6.1', 'x86_64', 'db1')`, nil},
			{`INSERT INTO metrics.pg_sys_cpu_info (connection_id, collected_at, model_name, no_of_cores, logical_processor, clock_speed_hz)
              VALUES ($1, $2, 'CPU', 4, 8, 3000000000)`, nil},
			{`INSERT INTO metrics.pg_sys_memory_info (connection_id, collected_at, total_memory, used_memory, free_memory, swap_total, swap_used)
              VALUES ($1, $2, 1000, 600, 400, 100, 10)`, nil},
			{`INSERT INTO metrics.pg_sys_disk_info (connection_id, collected_at, mount_point, file_system_type, total_space, used_space, free_space)
              VALUES ($1, $2, '/', 'ext4', 100, 60, 40), ($1, $2, '/data', NULL, 200, 20, 180)`, nil},
			{`INSERT INTO metrics.pg_server_info (connection_id, collected_at, server_version, cluster_name, data_directory, max_connections, max_wal_senders, max_replication_slots)
              VALUES ($1, $2, '18.0', 'main', '/var/lib/pg', 100, 10, 10)`, nil},
			{`INSERT INTO metrics.pg_database (connection_id, collected_at, datname, database_size_bytes, encoding, datconnlimit, datistemplate)
              VALUES ($1, $2, 'app', 8192, 6, -1, false), ($1, $2, 'other', 4096, NULL, 5, false)`, nil},
			{`INSERT INTO metrics.pg_extension (connection_id, collected_at, extname, extversion, schema_name, database_name)
              VALUES ($1, $2, 'plpgsql', '1.0', 'pg_catalog', 'app')`, nil},
			{`INSERT INTO metrics.pg_settings (connection_id, collected_at, name, setting, unit, category)
              VALUES ($1, $2, 'work_mem', '4096', 'kB', 'Resource Usage'),
                     ($1, $2, 'shared_buffers', '16384', '8kB', 'Resource Usage'),
                     ($1, $2, 'port', '5432', NULL, 'Connections')`, nil},
		}
		for _, s := range stmts {
			if _, err := pool.Exec(ctx, s.sql, id, at); err != nil {
				t.Fatalf("seed %q: %v", s.sql, err)
			}
		}
	}
	return id
}

// TestServerInfoQueries runs every server info query against the
// collector's real schema. The key settings query binds each setting
// name as $n::text, the only IN list a pgEdge ColdFront datastore can
// type (issue #755).
func TestServerInfoQueries(t *testing.T) {
	pool := newServerInfoSchemaPool(t)
	id := seedServerInfo(t, pool)
	ctx := context.Background()
	h := &ServerInfoHandler{}

	sys := &SystemInfo{}
	var collectedAt *time.Time
	h.queryOSInfo(ctx, pool, id, sys, &collectedAt)
	h.queryCPUInfo(ctx, pool, id, sys)
	h.queryMemoryInfo(ctx, pool, id, sys)
	h.queryDiskInfo(ctx, pool, id, sys)
	if collectedAt == nil || sys.OSName == nil || *sys.OSName != "Linux" {
		t.Errorf("OS info = %+v at %v", sys, collectedAt)
	}
	if sys.CPUCores == nil || *sys.CPUCores != 4 || sys.MemoryTotalBytes == nil || *sys.MemoryTotalBytes != 1000 {
		t.Errorf("CPU and memory info = %+v", sys)
	}
	if len(sys.Disks) != 2 || sys.Disks[0].FilesystemType != "ext4" || sys.Disks[1].FilesystemType != "" {
		t.Errorf("disks = %+v, want the latest sample's two mounts", sys.Disks)
	}

	pg := &PostgreSQLInfo{}
	h.queryServerInfo(ctx, pool, id, pg)
	if pg.Version == nil || *pg.Version != "18.0" {
		t.Errorf("server info = %+v", pg)
	}

	dbs := h.queryDatabases(ctx, pool, id)
	if len(dbs) != 2 || dbs[0].Name != "app" || dbs[0].Encoding == nil || dbs[1].Encoding != nil {
		t.Errorf("databases = %+v", dbs)
	}
	exts := h.queryExtensions(ctx, pool, id)
	if len(exts) != 1 {
		t.Errorf("extensions = %+v", exts)
	}

	settings := h.queryKeySettings(ctx, pool, id)
	if len(settings) != 2 {
		t.Fatalf("key settings = %+v, want work_mem and shared_buffers only", settings)
	}
	for _, s := range settings {
		if s.Name != "work_mem" && s.Name != "shared_buffers" {
			t.Errorf("unexpected setting %+v", s)
		}
	}

	// A connection with no samples reads nothing and fails nothing.
	empty := &SystemInfo{}
	var none *time.Time
	h.queryOSInfo(ctx, pool, id+1000, empty, &none)
	h.queryCPUInfo(ctx, pool, id+1000, empty)
	h.queryMemoryInfo(ctx, pool, id+1000, empty)
	if none != nil || empty.OSName != nil || empty.CPUCores != nil {
		t.Errorf("a connection with no samples read %+v", empty)
	}
	pgEmpty := &PostgreSQLInfo{}
	h.queryServerInfo(ctx, pool, id+1000, pgEmpty)
	if pgEmpty.Version != nil {
		t.Errorf("a connection with no samples read %+v", pgEmpty)
	}
	if got := h.queryKeySettings(ctx, pool, id+1000); len(got) != 0 || got == nil {
		t.Errorf("no settings should be an empty, non-nil list: %#v", got)
	}

	t.Run("handler", func(t *testing.T) {
		handler := &ServerInfoHandler{
			datastore:   database.NewTestDatastore(pool),
			rbacChecker: newTestRBACChecker(t),
			cache:       make(map[int]*aiCacheEntry),
		}
		mux := http.NewServeMux()
		handler.RegisterRoutes(mux, func(f http.HandlerFunc) http.HandlerFunc { return f })
		for _, tc := range []struct {
			path   string
			status int
		}{
			{"/api/v1/server-info/" + strconv.Itoa(id), http.StatusOK},
			{"/api/v1/server-info/", http.StatusBadRequest},
			{"/api/v1/server-info/abc", http.StatusBadRequest},
		} {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, withSuperuser(httptest.NewRequest(http.MethodGet, tc.path, nil)))
			if w.Code != tc.status {
				t.Fatalf("GET %s = %d, want %d: %s", tc.path, w.Code, tc.status, w.Body.String())
			}
			if tc.status != http.StatusOK {
				continue
			}
			var resp ServerInfoResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.CollectedAt == nil || len(resp.Databases) != 2 || len(resp.KeySettings) != 2 ||
				len(resp.Databases[0].Extensions) != 1 {
				t.Errorf("response = %+v", resp)
			}
		}

		unconfigured := http.NewServeMux()
		(&ServerInfoHandler{}).RegisterRoutes(unconfigured, func(f http.HandlerFunc) http.HandlerFunc { return f })
		w := httptest.NewRecorder()
		unconfigured.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/server-info/1", nil))
		if w.Code == http.StatusOK {
			t.Errorf("an unconfigured handler answered %d", w.Code)
		}
	})

	// A failing pool yields empty results rather than an error.
	closed, err := pgxpool.New(ctx, pool.Config().ConnString())
	if err != nil {
		t.Fatalf("open a second pool: %v", err)
	}
	closed.Close()
	h.queryDiskInfo(ctx, closed, id, &SystemInfo{})
	if got := h.queryDatabases(ctx, closed, id); len(got) != 0 {
		t.Errorf("databases on a closed pool = %+v", got)
	}
	if got := h.queryExtensions(ctx, closed, id); len(got) != 0 {
		t.Errorf("extensions on a closed pool = %+v", got)
	}
	if got := h.queryKeySettings(ctx, closed, id); len(got) != 0 {
		t.Errorf("settings on a closed pool = %+v", got)
	}

	// The remaining cases replace metrics.pg_settings with a view over its
	// rows, as a pgEdge ColdFront datastore does (issue #755), shaped to
	// fail part-way through reading. The scratch database is dropped when
	// the test ends.
	if _, err := pool.Exec(ctx, `
        CREATE TABLE pg_settings_rows AS SELECT * FROM metrics.pg_settings;
        DROP TABLE metrics.pg_settings CASCADE`); err != nil {
		t.Fatalf("move the settings rows aside: %v", err)
	}
	for _, tc := range []struct {
		name, view string
		want       []string
	}{
		{
			// work_mem's category cannot scan into a string, and it sorts
			// first (shared_buffers' NULL category sorts last). pgx closes
			// the rows on a scan error, so the read ends there and the
			// list is empty, not nil.
			name: "a row that does not scan ends the read",
			view: `CREATE OR REPLACE VIEW metrics.pg_settings AS
                SELECT connection_id, collected_at, name, setting, unit,
                       CASE WHEN name = 'work_mem' THEN ARRAY[category] END AS category
                FROM pg_settings_rows`,
			want: []string{},
		},
		{
			// work_mem's division by zero fails at execution, which
			// surfaces through rows.Err, not Query; the sort reads every
			// row first, so none is returned.
			name: "an execution failure gives an empty list",
			view: `CREATE OR REPLACE VIEW metrics.pg_settings AS
                SELECT connection_id, collected_at, name,
                       CASE WHEN name = 'work_mem' THEN (1 / (length(name) - 8))::text
                            ELSE setting END AS setting,
                       unit, category
                FROM pg_settings_rows`,
			want: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `DROP VIEW IF EXISTS metrics.pg_settings`); err != nil {
				t.Fatalf("drop the view: %v", err)
			}
			if _, err := pool.Exec(ctx, tc.view); err != nil {
				t.Fatalf("create the view: %v", err)
			}
			// Drop the connections' cached statements, planned against the
			// old relation's result type.
			pool.Reset()
			got := h.queryKeySettings(ctx, pool, id)
			names := []string{}
			for _, s := range got {
				names = append(names, s.Name)
			}
			if got == nil || strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Errorf("key settings = %#v, want %v", got, tc.want)
			}
		})
	}
}
