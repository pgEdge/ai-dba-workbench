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
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// These tests store rows as a collector that did not redact secrets
// would have, and check the REST endpoints that return them strip the
// secrets on the way out (GitHub issue #643). The values are fake.

const (
	redactionSettingsCreateSQL = `CREATE TABLE metrics.pg_settings (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        name          text NOT NULL,
        setting       text,
        reset_val     text,
        boot_val      text
    )`
	redactionSettingsDropSQL = `DROP TABLE IF EXISTS metrics.pg_settings CASCADE`

	redactionStatementsCreateSQL = `CREATE TABLE metrics.pg_stat_statements (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        database_name text NOT NULL,
        queryid       bigint NOT NULL,
        query         text
    )`
	redactionStatementsDropSQL = `DROP TABLE IF EXISTS metrics.pg_stat_statements CASCADE`
)

// TestLatestSnapshot_RedactsSecrets checks /api/v1/metrics/latest strips
// the password from a stored primary_conninfo and from stored statement
// text, leaving other values alone.
func TestLatestSnapshot_RedactsSecrets(t *testing.T) {
	pool := openLatestTestPool(t)
	t.Cleanup(pool.Close)

	createLatestFixtureTable(t, pool, "pg_settings",
		redactionSettingsCreateSQL, redactionSettingsDropSQL)
	createLatestFixtureTable(t, pool, "pg_stat_statements",
		redactionStatementsCreateSQL, redactionStatementsDropSQL)

	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Minute)
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_settings
            (connection_id, collected_at, name, setting, reset_val, boot_val)
        VALUES
            (1, $1, 'primary_conninfo', 'host=192.0.2.1 password=s3cret',
             'host=192.0.2.1 password=********', ''),
            (1, $1, 'work_mem', '4096', '4096', '4096')
    `, now); err != nil {
		t.Fatalf("failed to seed pg_settings: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_stat_statements
            (connection_id, collected_at, database_name, queryid, query)
        VALUES
            (1, $1, 'appdb', 1, 'ALTER ROLE app PASSWORD ''hunter2'''),
            (1, $1, 'appdb', 2, 'SELECT * FROM orders WHERE id = $1')
    `, now); err != nil {
		t.Fatalf("failed to seed pg_stat_statements: %v", err)
	}

	h := &LatestSnapshotHandler{
		datastore: database.NewTestDatastore(pool),
		authStore: newTestAuthStore(t),
	}

	settings := decodeLatest(t, getLatest(t, h,
		"connection_id=1&probe_name=pg_settings&order_by=name&order=asc"))
	if len(settings.Rows) != 2 {
		t.Fatalf("pg_settings rows = %d, want 2", len(settings.Rows))
	}
	// A stored password is masked with the whole connection string; a
	// value the collector already redacted is returned as stored.
	wantSettings := map[string]map[string]string{
		"primary_conninfo": {"setting": "********", "reset_val": "host=192.0.2.1 password=********"},
		"work_mem":         {"setting": "4096", "reset_val": "4096"},
	}
	for _, row := range settings.Rows {
		name, _ := row["name"].(string)
		for col, want := range wantSettings[name] {
			if got := row[col]; got != want {
				t.Errorf("%s.%s = %#v, want %q", name, col, got, want)
			}
		}
	}

	statements := decodeLatest(t, getLatest(t, h,
		"connection_id=1&probe_name=pg_stat_statements"))
	if len(statements.Rows) != 2 {
		t.Fatalf("pg_stat_statements rows = %d, want 2", len(statements.Rows))
	}
	wantQueries := map[string]bool{
		"ALTER ROLE app PASSWORD '********'": true,
		"SELECT * FROM orders WHERE id = $1": true,
	}
	for _, row := range statements.Rows {
		if q, _ := row["query"].(string); !wantQueries[q] {
			t.Errorf("unexpected query %q", q)
		}
	}
}

// TestHandleTopQueries_RedactsStatementText checks the top-queries
// endpoint strips a password from stored statement text.
func TestHandleTopQueries_RedactsStatementText(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	const connID = 4643
	ctx := context.Background()
	latest := time.Now().UTC().Add(-time.Minute)
	baseline := latest.Add(-2 * time.Minute)
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, dbid, userid, database_name,
         query, calls, total_exec_time, mean_exec_time, min_exec_time,
         max_exec_time, rows, shared_blks_hit, shared_blks_read)
        VALUES
        ($1, $2, 777, 16384, 16500, 'appdb',
         'CREATE USER MAPPING FOR app SERVER s OPTIONS (password ''hunter2'')',
         2, 10, 5, 4, 6, 0, 0, 0),
        ($1, $3, 777, 16384, 16500, 'appdb',
         'CREATE USER MAPPING FOR app SERVER s OPTIONS (password ''hunter2'')',
         0, 0, 0, 0, 0, 0, 0, 0)`,
		connID, latest, baseline); err != nil {
		t.Fatalf("failed to seed pg_stat_statements: %v", err)
	}

	req := withSuperuser(httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/top-queries?connection_id=4643", nil))
	rec := httptest.NewRecorder()
	h.handleTopQueries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("response leaks the password: %s", rec.Body.String())
	}
	var rows []TopQueryRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	want := "CREATE USER MAPPING FOR app SERVER s OPTIONS (password '********')"
	if len(rows) != 1 || rows[0].Query != want {
		t.Fatalf("rows = %+v, want one row with query %q", rows, want)
	}
}
