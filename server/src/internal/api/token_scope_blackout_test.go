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

// tokenScopeBlackoutSchema is the subset of the production schema the
// blackout handlers read and write, as in the database package's
// blackout integration tests.
const tokenScopeBlackoutSchema = `
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
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
    CONSTRAINT blackouts_scope_check
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
    CONSTRAINT blackout_schedules_scope_check
        CHECK (scope IN ('estate', 'group', 'cluster', 'server'))
);

INSERT INTO cluster_groups (id, name) VALUES (1, 'g');
INSERT INTO clusters (id, group_id, name) VALUES (1, 1, 'c');
INSERT INTO connections (id, cluster_id, name)
    VALUES (5, 1, 'five'), (6, 1, 'six'), (7, 1, 'seven');

INSERT INTO blackouts (id, scope, connection_id, cluster_id, reason,
                       start_time, end_time, created_by) VALUES
    (1, 'server', 5, NULL, 'r', NOW() - INTERVAL '1 hour',
     NOW() + INTERVAL '1 hour', 'admin'),
    (2, 'server', 6, NULL, 'r', NOW() - INTERVAL '1 hour',
     NOW() + INTERVAL '1 hour', 'admin'),
    (3, 'cluster', NULL, 1, 'r', NOW() - INTERVAL '1 hour',
     NOW() + INTERVAL '1 hour', 'admin');

INSERT INTO blackout_schedules (id, scope, connection_id, cluster_id, name,
                                cron_expression, duration_minutes, reason,
                                created_by) VALUES
    (1, 'server', 5, NULL, 'five', '0 * * * *', 30, 'r', 'admin'),
    (2, 'server', 6, NULL, 'six', '0 * * * *', 30, 'r', 'admin'),
    (3, 'cluster', NULL, 1, 'cluster', '0 * * * *', 30, 'r', 'admin');
`

const tokenScopeBlackoutTeardown = `
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// newTokenScopeBlackoutHandler returns a BlackoutHandler over a freshly
// seeded blackout schema, the pool behind it, and the shared callers.
func newTokenScopeBlackoutHandler(t *testing.T) (*BlackoutHandler,
	*pgxpool.Pool, map[string]scopeCaller) {

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
	if _, err := pool.Exec(ctx, tokenScopeBlackoutSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create test schema: %v", err)
	}

	_, store, cleanup := createTestRBACHandler(t)
	t.Cleanup(func() {
		cleanup()
		_, _ = pool.Exec(context.Background(), tokenScopeBlackoutTeardown)
		pool.Close()
	})

	session, unscoped, wildcard, narrowed, readOnly := scopedCallers(t, store)
	callers := map[string]scopeCaller{
		"session": session, "unscoped": unscoped, "wildcard": wildcard,
		"narrowed": narrowed, "readOnly": readOnly,
	}
	h := NewBlackoutHandler(database.NewTestDatastore(pool), store,
		auth.NewRBACChecker(store))
	return h, pool, callers
}

// rowExists reports whether the table holds a row with the given id.
func rowExists(t *testing.T, pool *pgxpool.Pool, table string, id int64) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM "+table+" WHERE id = $1)",
		id).Scan(&exists); err != nil {
		t.Fatalf("Failed to query %s: %v", table, err)
	}
	return exists
}

// TestBlackoutWritesByIDRespectTokenScope covers the blackout and
// schedule writes that address an existing record by id, where the
// target comes from the stored record rather than the request.
func TestBlackoutWritesByIDRespectTokenScope(t *testing.T) {
	const scheduleBody = `{"scope":"server","connection_id":5,"name":"n",` +
		`"cron_expression":"0 * * * *","duration_minutes":30,` +
		`"timezone":"UTC","reason":"r"}`

	type write func(h *BlackoutHandler, w http.ResponseWriter,
		c scopeCaller, id int64)

	deleteBlackout := func(h *BlackoutHandler, w http.ResponseWriter,
		c scopeCaller, id int64) {
		h.deleteBlackout(w, newScopeRequest(c, http.MethodDelete, ""), id)
	}
	stopBlackout := func(h *BlackoutHandler, w http.ResponseWriter,
		c scopeCaller, id int64) {
		h.stopBlackout(w, newScopeRequest(c, http.MethodPost, ""), id)
	}
	updateBlackout := func(h *BlackoutHandler, w http.ResponseWriter,
		c scopeCaller, id int64) {
		h.updateBlackout(w, newScopeRequest(c, http.MethodPut,
			`{"reason":"changed"}`), id)
	}
	deleteSchedule := func(h *BlackoutHandler, w http.ResponseWriter,
		c scopeCaller, id int64) {
		h.deleteBlackoutSchedule(w, newScopeRequest(c, http.MethodDelete, ""), id)
	}
	updateSchedule := func(h *BlackoutHandler, w http.ResponseWriter,
		c scopeCaller, id int64) {
		h.updateBlackoutSchedule(w, newScopeRequest(c, http.MethodPut,
			scheduleBody), id)
	}

	moveScheduleOut := func(h *BlackoutHandler, w http.ResponseWriter,
		c scopeCaller, id int64) {
		h.updateBlackoutSchedule(w, newScopeRequest(c, http.MethodPut,
			strings.Replace(scheduleBody, `"connection_id":5`,
				`"connection_id":6`, 1)), id)
	}

	cases := []struct {
		name   string
		fn     write
		table  string
		id     int64
		caller string
		want   int
	}{
		{"delete blackout in scope", deleteBlackout, "blackouts", 1, "narrowed", http.StatusOK},
		{"delete blackout read-only", deleteBlackout, "blackouts", 1, "readOnly", http.StatusForbidden},
		{"delete blackout out of scope", deleteBlackout, "blackouts", 2, "narrowed", http.StatusForbidden},
		{"delete cluster blackout narrowed", deleteBlackout, "blackouts", 3, "narrowed", http.StatusForbidden},
		{"delete cluster blackout wildcard", deleteBlackout, "blackouts", 3, "wildcard", http.StatusOK},
		{"delete missing blackout", deleteBlackout, "blackouts", 999, "narrowed", http.StatusNotFound},
		{"stop blackout in scope", stopBlackout, "blackouts", 1, "narrowed", http.StatusOK},
		{"stop cluster blackout narrowed", stopBlackout, "blackouts", 3, "narrowed", http.StatusForbidden},
		{"stop cluster blackout session", stopBlackout, "blackouts", 3, "session", http.StatusOK},
		{"update blackout in scope", updateBlackout, "blackouts", 1, "narrowed", http.StatusOK},
		{"update blackout out of scope", updateBlackout, "blackouts", 2, "narrowed", http.StatusForbidden},
		{"delete schedule in scope", deleteSchedule, "blackout_schedules", 1, "narrowed", http.StatusOK},
		{"delete schedule out of scope", deleteSchedule, "blackout_schedules", 2, "narrowed", http.StatusForbidden},
		{"delete cluster schedule narrowed", deleteSchedule, "blackout_schedules", 3, "narrowed", http.StatusForbidden},
		{"delete cluster schedule unscoped", deleteSchedule, "blackout_schedules", 3, "unscoped", http.StatusOK},
		{"delete missing schedule", deleteSchedule, "blackout_schedules", 999, "narrowed", http.StatusNotFound},
		{"update schedule in scope", updateSchedule, "blackout_schedules", 1, "narrowed", http.StatusOK},
		// The body moves the schedule onto connection 5, which the token
		// holds, but the schedule is on connection 6 today.
		{"move schedule from out of scope", updateSchedule, "blackout_schedules", 2, "narrowed", http.StatusForbidden},
		{"move cluster schedule", updateSchedule, "blackout_schedules", 3, "narrowed", http.StatusForbidden},
		{"move cluster schedule wildcard", updateSchedule, "blackout_schedules", 3, "wildcard", http.StatusOK},
		// The schedule is on connection 5 today, but the body would move
		// it onto connection 6, which the token does not hold.
		{"move schedule out of scope", moveScheduleOut, "blackout_schedules", 1, "narrowed", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, pool, callers := newTokenScopeBlackoutHandler(t)
			rec := httptest.NewRecorder()

			tc.fn(h, rec, callers[tc.caller], tc.id)

			if rec.Code != tc.want {
				t.Fatalf("Expected %d, got %d: %s", tc.want, rec.Code,
					rec.Body.String())
			}
			if tc.want == http.StatusForbidden {
				assertOutOfTokenScope(t, rec)
				if !rowExists(t, pool, tc.table, tc.id) {
					t.Errorf("Expected %s row %d to survive a refusal",
						tc.table, tc.id)
				}
			}
		})
	}
}

// TestBlackoutWritesByIDFastPath checks that a caller covering every
// connection skips the lookup, so a nil datastore is reached only by the
// write itself.
func TestBlackoutWritesByIDFastPath(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	session, unscoped, wildcard, _, _ := scopedCallers(t, store)
	h := NewBlackoutHandler(nil, store, auth.NewRBACChecker(store))

	for _, caller := range []scopeCaller{session, unscoped, wildcard} {
		t.Run(caller.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := newScopeRequest(caller, http.MethodDelete, "")
			if !h.requireBlackoutInTokenScope(rec, r, 1) {
				t.Errorf("Expected the blackout check to pass: %s",
					rec.Body.String())
			}
			if !h.requireBlackoutScheduleInTokenScope(rec, r, 1) {
				t.Errorf("Expected the schedule check to pass: %s",
					rec.Body.String())
			}
		})
	}
}
