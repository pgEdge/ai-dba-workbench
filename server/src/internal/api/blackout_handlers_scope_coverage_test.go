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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// The fixed SQL below adjusts the seeded blackout schema for individual
// tests. Each test seeds a fresh schema, so none of it leaks between
// tests.
const (
	// blackoutExtraRows adds a group blackout and schedule, an estate
	// blackout and schedule, and a blackout that has already ended, then
	// moves the id sequences past the seeded ids so that creates succeed.
	blackoutExtraRows = `
INSERT INTO blackouts (id, scope, connection_id, group_id, reason,
                       start_time, end_time, created_by) VALUES
    (4, 'group', NULL, 1, 'r', NOW() - INTERVAL '1 hour',
     NOW() + INTERVAL '1 hour', 'admin'),
    (5, 'estate', NULL, NULL, 'r', NOW() - INTERVAL '1 hour',
     NOW() + INTERVAL '1 hour', 'admin'),
    (6, 'server', 5, NULL, 'r', NOW() - INTERVAL '2 hours',
     NOW() - INTERVAL '1 hour', 'admin');

INSERT INTO blackout_schedules (id, scope, connection_id, group_id, name,
                                cron_expression, duration_minutes, reason,
                                created_by) VALUES
    (4, 'group', NULL, 1, 'group', '0 * * * *', 30, 'r', 'admin'),
    (5, 'estate', NULL, NULL, 'estate', '0 * * * *', 30, 'r', 'admin');

SELECT setval('blackouts_id_seq', 100);
SELECT setval('blackout_schedules_id_seq', 100);
`

	// blackoutDropConnections removes the connections table, so that a
	// cluster or group visibility lookup fails whilst the blackout
	// tables themselves still answer.
	blackoutDropConnections = `DROP TABLE connections CASCADE;`

	// blackoutDropBlackouts removes both blackout tables, so that every
	// blackout and schedule query fails.
	blackoutDropBlackouts = `
DROP TABLE blackout_schedules CASCADE;
DROP TABLE blackouts CASCADE;
`

	// blackoutFailWrites makes every UPDATE of either blackout table
	// fail, whilst reads still succeed.
	blackoutFailWrites = `
CREATE OR REPLACE FUNCTION blackout_cov_fail() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'blackout coverage: write refused';
END;
$$;
CREATE TRIGGER blackout_cov_fail BEFORE UPDATE ON blackouts
    FOR EACH ROW EXECUTE FUNCTION blackout_cov_fail();
CREATE TRIGGER blackout_cov_fail BEFORE UPDATE ON blackout_schedules
    FOR EACH ROW EXECUTE FUNCTION blackout_cov_fail();
`

	// blackoutFailWritesTeardown removes the trigger function, which
	// the table teardown leaves behind.
	blackoutFailWritesTeardown = `DROP FUNCTION IF EXISTS blackout_cov_fail() CASCADE;`
)

// anonymousCaller presents a request with no identity, which holds no
// admin permission.
var anonymousCaller = scopeCaller{name: "anonymous",
	wrap: func(r *http.Request) *http.Request { return r }}

// mustExec runs fixed SQL against the test pool.
func mustExec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("Failed to adjust the test schema: %v", err)
	}
}

// newBlackoutCoverageHandler seeds the blackout schema plus the extra
// rows, then applies any further fixed SQL.
func newBlackoutCoverageHandler(t *testing.T, sql ...string) (*BlackoutHandler,
	*pgxpool.Pool, map[string]scopeCaller) {

	t.Helper()
	h, pool, callers := newTokenScopeBlackoutHandler(t)
	mustExec(t, pool, blackoutExtraRows)
	for _, s := range sql {
		mustExec(t, pool, s)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), blackoutFailWritesTeardown)
	})
	return h, pool, callers
}

// closeAuthStore closes the handler's auth store, so that reading a
// token's connection scope fails. The test's own cleanup closes it
// again, which is harmless.
func closeAuthStore(t *testing.T, h *BlackoutHandler) {
	t.Helper()
	if err := h.authStore.Close(); err != nil {
		t.Fatalf("Failed to close the auth store: %v", err)
	}
}

// expectStatus fails unless the recorder holds the wanted status.
func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("Expected %d, got %d: %s", want, rec.Code, rec.Body.String())
	}
}

// TestBlackoutListVisibility covers listing blackouts and schedules for
// callers with every connection and for a token narrowed to two.
func TestBlackoutListVisibility(t *testing.T) {
	cases := []struct {
		name      string
		schedules bool
		caller    string
		query     string
		wantTotal int
		wantPage  int
	}{
		{"blackouts session", false, "session", "", 6, 6},
		{"blackouts session filtered", false, "session",
			"?scope=server&connection_id=5&active=true", 1, 1},
		// Connection 6's blackout is hidden; the cluster and group
		// blackouts contain connection 5, and the estate one is global.
		{"blackouts narrowed", false, "narrowed", "", 5, 5},
		{"blackouts narrowed paged", false, "narrowed",
			"?limit=2&offset=1&group_id=1&cluster_id=1", 0, 0},
		{"blackouts narrowed second page", false, "narrowed",
			"?limit=2&offset=4", 5, 1},
		{"schedules session", true, "session", "", 5, 5},
		{"schedules session filtered", true, "session",
			"?scope=server&connection_id=6&enabled=true", 1, 1},
		{"schedules narrowed", true, "narrowed", "", 4, 4},
		{"schedules narrowed paged", true, "narrowed",
			"?limit=1&offset=1&group_id=1&cluster_id=1", 0, 0},
		{"schedules narrowed second page", true, "narrowed",
			"?limit=3&offset=3", 4, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, callers := newBlackoutCoverageHandler(t)
			rec := httptest.NewRecorder()
			req := callers[tc.caller].wrap(httptest.NewRequest(
				http.MethodGet, "/"+tc.query, nil))
			if tc.schedules {
				h.listBlackoutSchedules(rec, req)
			} else {
				h.listBlackouts(rec, req)
			}
			expectStatus(t, rec, http.StatusOK)

			var total, page int
			if tc.schedules {
				var res database.BlackoutScheduleListResult
				if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				total, page = res.TotalCount, len(res.Schedules)
			} else {
				var res database.BlackoutListResult
				if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				total, page = res.TotalCount, len(res.Blackouts)
			}
			if total != tc.wantTotal || page != tc.wantPage {
				t.Errorf("Expected total %d and page %d, got %d and %d",
					tc.wantTotal, tc.wantPage, total, page)
			}
		})
	}
}

// TestBlackoutListFailures covers the 500 answers from the list
// endpoints.
func TestBlackoutListFailures(t *testing.T) {
	cases := []struct {
		name      string
		schedules bool
		caller    string
		sql       string
		closeAuth bool
	}{
		{"blackouts scope unreadable", false, "narrowed", "", true},
		{"blackouts query fails for session", false, "session", blackoutDropBlackouts, false},
		{"blackouts query fails for narrowed", false, "narrowed", blackoutDropBlackouts, false},
		{"blackouts visibility fails", false, "narrowed", blackoutDropConnections, false},
		{"schedules scope unreadable", true, "narrowed", "", true},
		{"schedules query fails for session", true, "session", blackoutDropBlackouts, false},
		{"schedules query fails for narrowed", true, "narrowed", blackoutDropBlackouts, false},
		{"schedules visibility fails", true, "narrowed", blackoutDropConnections, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sql []string
			if tc.sql != "" {
				sql = append(sql, tc.sql)
			}
			h, _, callers := newBlackoutCoverageHandler(t, sql...)
			if tc.closeAuth {
				closeAuthStore(t, h)
			}
			rec := httptest.NewRecorder()
			req := callers[tc.caller].wrap(httptest.NewRequest(
				http.MethodGet, "/", nil))
			if tc.schedules {
				h.listBlackoutSchedules(rec, req)
			} else {
				h.listBlackouts(rec, req)
			}
			expectStatus(t, rec, http.StatusInternalServerError)
		})
	}
}

// TestBlackoutGetVisibility covers fetching one blackout or schedule.
func TestBlackoutGetVisibility(t *testing.T) {
	cases := []struct {
		name      string
		schedules bool
		caller    string
		id        int64
		sql       string
		closeAuth bool
		want      int
	}{
		{"blackout session", false, "session", 2, "", false, http.StatusOK},
		{"blackout narrowed in scope", false, "narrowed", 1, "", false, http.StatusOK},
		{"blackout narrowed cluster", false, "narrowed", 3, "", false, http.StatusOK},
		{"blackout narrowed hidden", false, "narrowed", 2, "", false, http.StatusNotFound},
		{"blackout missing", false, "session", 999, "", false, http.StatusNotFound},
		{"blackout scope unreadable", false, "narrowed", 1, "", true, http.StatusInternalServerError},
		{"blackout visibility fails", false, "narrowed", 4, blackoutDropConnections, false, http.StatusInternalServerError},
		{"schedule session", true, "session", 2, "", false, http.StatusOK},
		{"schedule narrowed in scope", true, "narrowed", 1, "", false, http.StatusOK},
		{"schedule narrowed group", true, "narrowed", 4, "", false, http.StatusOK},
		{"schedule narrowed hidden", true, "narrowed", 2, "", false, http.StatusNotFound},
		{"schedule missing", true, "session", 999, "", false, http.StatusNotFound},
		{"schedule scope unreadable", true, "narrowed", 1, "", true, http.StatusInternalServerError},
		{"schedule visibility fails", true, "narrowed", 3, blackoutDropConnections, false, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sql []string
			if tc.sql != "" {
				sql = append(sql, tc.sql)
			}
			h, _, callers := newBlackoutCoverageHandler(t, sql...)
			if tc.closeAuth {
				closeAuthStore(t, h)
			}
			rec := httptest.NewRecorder()
			req := callers[tc.caller].wrap(httptest.NewRequest(
				http.MethodGet, "/", nil))
			if tc.schedules {
				h.getBlackoutSchedule(rec, req, tc.id)
			} else {
				h.getBlackout(rec, req, tc.id)
			}
			expectStatus(t, rec, tc.want)
		})
	}
}

// TestPaginateBlackoutsAndSchedules covers the in-memory pagination
// edges.
func TestPaginateBlackoutsAndSchedules(t *testing.T) {
	blackouts := make([]database.Blackout, 4)
	schedules := make([]database.BlackoutSchedule, 4)
	cases := []struct {
		name          string
		offset, limit int
		want          int
	}{
		{"negative offset", -3, 2, 2},
		{"offset past end", 4, 2, 0},
		{"no limit", 1, 0, 3},
		{"limit past end", 3, 5, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(paginateBlackouts(blackouts, tc.offset,
				tc.limit)); got != tc.want {
				t.Errorf("paginateBlackouts returned %d, want %d", got, tc.want)
			}
			if got := len(paginateSchedules(schedules, tc.offset,
				tc.limit)); got != tc.want {
				t.Errorf("paginateSchedules returned %d, want %d", got, tc.want)
			}
		})
	}
}

// TestBlackoutWritesCoverage covers the blackout and schedule writes
// beyond the scope gate: permission, body and validation refusals,
// successful writes and datastore failures.
func TestBlackoutWritesCoverage(t *testing.T) {
	const blackoutBody = `{"scope":"server","connection_id":5,"reason":"r",` +
		`"start_time":"2026-01-01T00:00:00Z","end_time":"2026-01-02T00:00:00Z"}`
	const scheduleBody = `{"scope":"server","connection_id":5,"name":"n",` +
		`"cron_expression":"0 * * * *","duration_minutes":30,"reason":"r",` +
		`"enabled":false}`

	type call func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request)

	create := func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request) {
		h.createBlackout(w, r)
	}
	update := func(id int64) call {
		return func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request) {
			h.updateBlackout(w, r, id)
		}
	}
	del := func(id int64) call {
		return func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request) {
			h.deleteBlackout(w, r, id)
		}
	}
	stop := func(id int64) call {
		return func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request) {
			h.stopBlackout(w, r, id)
		}
	}
	createSchedule := func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request) {
		h.createBlackoutSchedule(w, r)
	}
	updateSchedule := func(id int64) call {
		return func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request) {
			h.updateBlackoutSchedule(w, r, id)
		}
	}
	deleteSchedule := func(id int64) call {
		return func(h *BlackoutHandler, w http.ResponseWriter, r *http.Request) {
			h.deleteBlackoutSchedule(w, r, id)
		}
	}

	cases := []struct {
		name   string
		fn     call
		caller string
		method string
		body   string
		sql    string
		want   int
	}{
		// Creating a blackout.
		{"create forbidden", create, "anonymous", http.MethodPost, blackoutBody, "", http.StatusForbidden},
		{"create bad body", create, "session", http.MethodPost, `{`, "", http.StatusBadRequest},
		{"create bad start", create, "session", http.MethodPost,
			`{"scope":"estate","reason":"r","start_time":"x","end_time":"2026-01-02T00:00:00Z"}`,
			"", http.StatusBadRequest},
		{"create end before start", create, "session", http.MethodPost,
			`{"scope":"estate","reason":"r","start_time":"2026-01-02T00:00:00Z",` +
				`"end_time":"2026-01-01T00:00:00Z"}`, "", http.StatusBadRequest},
		{"create by token", create, "narrowed", http.MethodPost, blackoutBody, "", http.StatusCreated},
		{"create fails", create, "session", http.MethodPost, blackoutBody,
			blackoutDropBlackouts, http.StatusInternalServerError},

		// Updating a blackout.
		{"update forbidden", update(1), "anonymous", http.MethodPut, `{}`, "", http.StatusForbidden},
		{"update bad body", update(1), "session", http.MethodPut, `{`, "", http.StatusBadRequest},
		{"update missing", update(999), "session", http.MethodPut, `{}`, "", http.StatusNotFound},
		{"update bad end", update(1), "session", http.MethodPut,
			`{"end_time":"x"}`, "", http.StatusBadRequest},
		{"update end before start", update(1), "session", http.MethodPut,
			`{"end_time":"2000-01-01T00:00:00Z"}`, "", http.StatusBadRequest},
		{"update end time", update(1), "session", http.MethodPut,
			`{"reason":"later","end_time":"2099-01-01T00:00:00Z"}`, "", http.StatusOK},
		{"update fails", update(1), "session", http.MethodPut, `{"reason":"x"}`,
			blackoutFailWrites, http.StatusInternalServerError},
		// A cluster blackout the narrowed token can see is refused with
		// 403 rather than hidden.
		{"update visible out of scope", update(3), "narrowed", http.MethodPut,
			`{"reason":"x"}`, "", http.StatusForbidden},

		// Deleting and stopping a blackout.
		{"delete forbidden", del(1), "anonymous", http.MethodDelete, "", "", http.StatusForbidden},
		{"delete missing for session", del(999), "session", http.MethodDelete, "", "", http.StatusNotFound},
		{"delete lookup fails", del(1), "narrowed", http.MethodDelete, "",
			blackoutDropBlackouts, http.StatusInternalServerError},
		{"stop forbidden", stop(1), "anonymous", http.MethodPost, "", "", http.StatusForbidden},
		{"stop ended", stop(6), "narrowed", http.MethodPost, "", "", http.StatusNotFound},
		{"stop out of scope", stop(2), "narrowed", http.MethodPost, "", "", http.StatusNotFound},
		{"stop fails", stop(1), "session", http.MethodPost, "",
			blackoutFailWrites, http.StatusInternalServerError},

		// Creating a schedule.
		{"create schedule forbidden", createSchedule, "anonymous", http.MethodPost,
			scheduleBody, "", http.StatusForbidden},
		{"create schedule bad body", createSchedule, "session", http.MethodPost,
			`{`, "", http.StatusBadRequest},
		{"create schedule bad scope", createSchedule, "session", http.MethodPost,
			`{"scope":"server"}`, "", http.StatusBadRequest},
		{"create schedule out of scope", createSchedule, "narrowed", http.MethodPost,
			`{"scope":"estate","name":"n"}`, "", http.StatusForbidden},
		{"create schedule no name", createSchedule, "session", http.MethodPost,
			`{"scope":"estate"}`, "", http.StatusBadRequest},
		{"create schedule no cron", createSchedule, "session", http.MethodPost,
			`{"scope":"estate","name":"n"}`, "", http.StatusBadRequest},
		{"create schedule no duration", createSchedule, "session", http.MethodPost,
			`{"scope":"estate","name":"n","cron_expression":"0 * * * *"}`,
			"", http.StatusBadRequest},
		{"create schedule by token", createSchedule, "narrowed", http.MethodPost,
			scheduleBody, "", http.StatusCreated},
		{"create schedule fails", createSchedule, "session", http.MethodPost,
			scheduleBody, blackoutDropBlackouts, http.StatusInternalServerError},

		// Updating a schedule.
		{"update schedule forbidden", updateSchedule(1), "anonymous", http.MethodPut,
			scheduleBody, "", http.StatusForbidden},
		{"update schedule bad body", updateSchedule(1), "session", http.MethodPut,
			`{`, "", http.StatusBadRequest},
		{"update schedule bad scope", updateSchedule(1), "session", http.MethodPut,
			`{"scope":"nowhere"}`, "", http.StatusBadRequest},
		{"update schedule no name", updateSchedule(1), "session", http.MethodPut,
			`{"scope":"estate"}`, "", http.StatusBadRequest},
		{"update schedule no cron", updateSchedule(1), "session", http.MethodPut,
			`{"scope":"estate","name":"n"}`, "", http.StatusBadRequest},
		{"update schedule no duration", updateSchedule(1), "session", http.MethodPut,
			`{"scope":"estate","name":"n","cron_expression":"0 * * * *"}`,
			"", http.StatusBadRequest},
		{"update schedule missing", updateSchedule(999), "session", http.MethodPut,
			scheduleBody, "", http.StatusNotFound},
		{"update schedule lookup missing", updateSchedule(999), "narrowed", http.MethodPut,
			scheduleBody, "", http.StatusNotFound},
		{"update schedule fails", updateSchedule(1), "session", http.MethodPut,
			scheduleBody, blackoutFailWrites, http.StatusInternalServerError},
		// The group schedule is visible to the narrowed token, so moving
		// it is refused with 403.
		{"update visible schedule out of scope", updateSchedule(4), "narrowed",
			http.MethodPut, scheduleBody, "", http.StatusForbidden},

		// Deleting a schedule.
		{"delete schedule forbidden", deleteSchedule(1), "anonymous", http.MethodDelete,
			"", "", http.StatusForbidden},
		{"delete schedule missing for session", deleteSchedule(999), "session",
			http.MethodDelete, "", "", http.StatusNotFound},
		{"delete schedule lookup fails", deleteSchedule(1), "narrowed",
			http.MethodDelete, "", blackoutDropBlackouts, http.StatusInternalServerError},
		{"delete schedule read-only visible", deleteSchedule(1), "readOnly",
			http.MethodDelete, "", "", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sql []string
			if tc.sql != "" {
				sql = append(sql, tc.sql)
			}
			h, _, callers := newBlackoutCoverageHandler(t, sql...)
			callers["anonymous"] = anonymousCaller
			rec := httptest.NewRecorder()

			tc.fn(h, rec, newScopeRequest(callers[tc.caller], tc.method, tc.body))

			expectStatus(t, rec, tc.want)
			if tc.want == http.StatusForbidden && tc.caller != "anonymous" {
				assertOutOfTokenScope(t, rec)
			}
		})
	}
}

// TestRefuseOutOfScope covers each answer refuseOutOfScope can give.
func TestRefuseOutOfScope(t *testing.T) {
	visibleFn := func(ok bool, err error) func(context.Context,
		map[int]bool) (bool, error) {
		return func(context.Context, map[int]bool) (bool, error) {
			return ok, err
		}
	}
	cases := []struct {
		name      string
		caller    string
		closeAuth bool
		visible   bool
		err       error
		want      int
	}{
		{"hidden", "narrowed", false, false, nil, http.StatusNotFound},
		{"visible", "narrowed", false, true, nil, http.StatusForbidden},
		// A caller who can see every connection is never told 404.
		{"sees everything", "session", false, false, nil, http.StatusForbidden},
		{"visibility fails", "narrowed", false, false,
			errors.New("lookup failed"), http.StatusInternalServerError},
		{"scope unreadable", "narrowed", true, true, nil, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, callers := newTokenScopeBlackoutHandler(t)
			if tc.closeAuth {
				closeAuthStore(t, h)
			}
			rec := httptest.NewRecorder()
			r := newScopeRequest(callers[tc.caller], http.MethodDelete, "")

			h.refuseOutOfScope(rec, r, "Blackout not found",
				visibleFn(tc.visible, tc.err))

			expectStatus(t, rec, tc.want)
			if tc.want == http.StatusForbidden {
				assertOutOfTokenScope(t, rec)
			}
		})
	}
}
