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
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// covBlackoutEnv bundles a seeded fixture with handlers that share one
// auth store, so the restricted decorator's grants apply to both the
// working and the broken handler.
type covBlackoutEnv struct {
	f          *covFixture
	serve      *BlackoutHandler
	broken     *BlackoutHandler
	restricted func(*http.Request) *http.Request
}

func newCovBlackoutEnv(t *testing.T) *covBlackoutEnv {
	t.Helper()
	f := newCovFixture(t)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	return &covBlackoutEnv{
		f:          f,
		serve:      NewBlackoutHandler(f.ds, store, checker),
		broken:     NewBlackoutHandler(newBrokenDatastore(t), store, checker),
		restricted: covRestrictedUser(t, store, auth.PermManageBlackouts),
	}
}

func (e *covBlackoutEnv) mux() *http.ServeMux {
	mux := http.NewServeMux()
	e.serve.RegisterRoutes(mux, covIdentityWrapper)
	return mux
}

// covScopeBodies returns scope JSON fragments for every scope, keyed by
// a label; "visible" entries belong to covOwner's hierarchy.
func covScopeBodies(f *covFixture) []struct {
	label   string
	json    string
	visible bool
} {
	return []struct {
		label   string
		json    string
		visible bool
	}{
		{"estate", `"scope": "estate"`, true},
		{"server", fmt.Sprintf(`"scope": "server", "connection_id": %d`, f.connID), true},
		{"hidden server", fmt.Sprintf(`"scope": "server", "connection_id": %d`, f.hiddenConnID), false},
		{"cluster", fmt.Sprintf(`"scope": "cluster", "cluster_id": %d`, f.clusterID), true},
		{"hidden cluster", fmt.Sprintf(`"scope": "cluster", "cluster_id": %d`, f.hiddenCluster), false},
		{"group", fmt.Sprintf(`"scope": "group", "group_id": %d`, f.groupID), true},
		{"hidden group", fmt.Sprintf(`"scope": "group", "group_id": %d`, f.hiddenGroupID), false},
	}
}

// covCreatedID decodes the "id" field of a creation response.
func covCreatedID(t *testing.T, body []byte) string {
	t.Helper()
	m := decodeRaw(t, body)
	id, ok := m["id"].(float64)
	if !ok {
		t.Fatalf("response has no id: %s", string(body))
	}
	return strconv.FormatInt(int64(id), 10)
}

// covTotal decodes the "total_count" field of a list response.
func covTotal(t *testing.T, body []byte) int {
	t.Helper()
	m := decodeRaw(t, body)
	n, ok := m["total_count"].(float64)
	if !ok {
		t.Fatalf("response has no total_count: %s", string(body))
	}
	return int(n)
}

func TestBlackoutHandler_RegisterRoutesNotConfigured(t *testing.T) {
	mux := http.NewServeMux()
	NewBlackoutHandler(nil, nil, nil).RegisterRoutes(mux, covIdentityWrapper)
	for _, p := range []string{"/api/v1/blackouts", "/api/v1/blackouts/1",
		"/api/v1/blackout-schedules", "/api/v1/blackout-schedules/1"} {
		covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, p, ""), http.StatusServiceUnavailable)
	}
}

func TestBlackoutHandler_Routing(t *testing.T) {
	e := newCovBlackoutEnv(t)
	serve := e.mux().ServeHTTP
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodPatch, "/api/v1/blackouts", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/blackouts/", http.StatusNotFound},
		{http.MethodGet, "/api/v1/blackouts/abc", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/blackouts/1/stop", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/blackouts/1/x/y", http.StatusNotFound},
		{http.MethodPatch, "/api/v1/blackouts/1", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/api/v1/blackout-schedules", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/blackout-schedules/", http.StatusNotFound},
		{http.MethodGet, "/api/v1/blackout-schedules/abc", http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/blackout-schedules/1", http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			covExpect(t, covDo(serve, tc.method, tc.path, "", withSuperuser), tc.want)
		})
	}
}

func TestBlackoutHandler_BlackoutLifecycle(t *testing.T) {
	e := newCovBlackoutEnv(t)
	f := e.f
	serve := e.mux().ServeHTTP
	now := time.Now().UTC()
	start := now.Add(-time.Hour).Format(time.RFC3339)
	end := now.Add(time.Hour).Format(time.RFC3339)
	times := fmt.Sprintf(`"reason": "maintenance", "start_time": %q, "end_time": %q`, start, end)
	su := func(r *http.Request) *http.Request { return withUsername(withSuperuser(r), "cov_admin") }

	ids := map[string]string{}
	visibleCount := 0
	for _, s := range covScopeBodies(f) {
		rec := covDo(serve, http.MethodPost, "/api/v1/blackouts", "{"+s.json+", "+times+"}", su)
		covExpect(t, rec, http.StatusCreated)
		ids[s.label] = covCreatedID(t, rec.Body.Bytes())
		if s.visible {
			visibleCount++
		}
	}

	t.Run("create validation", func(t *testing.T) {
		bad := []string{
			`{`,
			`{"scope": "bogus", ` + times + `}`,
			`{"scope": "estate", "reason": "r", "start_time": "nope", "end_time": "` + end + `"}`,
			`{"scope": "estate", "reason": "r", "start_time": "` + start + `", "end_time": "nope"}`,
			`{"scope": "estate", "reason": "r", "start_time": "` + end + `", "end_time": "` + start + `"}`,
		}
		for _, body := range bad {
			covExpect(t, covDo(serve, http.MethodPost, "/api/v1/blackouts", body, withSuperuser), http.StatusBadRequest)
		}
		// A connection that does not exist violates the foreign key.
		covExpect(t, covDo(serve, http.MethodPost, "/api/v1/blackouts",
			`{"scope": "server", "connection_id": 999999, `+times+`}`, withSuperuser), http.StatusInternalServerError)
		covExpect(t, covDo(serve, http.MethodPost, "/api/v1/blackouts", `{"scope": "estate", `+times+`}`,
			func(r *http.Request) *http.Request { return withUser(r, 424242) }), http.StatusForbidden)
	})

	t.Run("list", func(t *testing.T) {
		rec := covDo(serve, http.MethodGet, "/api/v1/blackouts", "", withSuperuser)
		covExpect(t, rec, http.StatusOK)
		if got := covTotal(t, rec.Body.Bytes()); got != len(ids) {
			t.Fatalf("superuser total = %d, want %d", got, len(ids))
		}
		filtered := fmt.Sprintf("/api/v1/blackouts?scope=server&group_id=%d&cluster_id=%d&connection_id=%d&active=true",
			f.groupID, f.clusterID, f.connID)
		covExpect(t, covDo(serve, http.MethodGet, filtered, "", withSuperuser), http.StatusOK)

		rec = covDo(serve, http.MethodGet, "/api/v1/blackouts", "", e.restricted)
		covExpect(t, rec, http.StatusOK)
		if got := covTotal(t, rec.Body.Bytes()); got != visibleCount {
			t.Fatalf("restricted total = %d, want %d", got, visibleCount)
		}
		rec = covDo(serve, http.MethodGet, "/api/v1/blackouts?limit=2&offset=1", "", e.restricted)
		covExpect(t, rec, http.StatusOK)
		if list, _ := decodeRaw(t, rec.Body.Bytes())["blackouts"].([]any); len(list) != 2 {
			t.Fatalf("paginated page has %d entries, want 2", len(list))
		}
		rec = covDo(serve, http.MethodGet, "/api/v1/blackouts?offset=500", "", e.restricted)
		covExpect(t, rec, http.StatusOK)
		if list, _ := decodeRaw(t, rec.Body.Bytes())["blackouts"].([]any); len(list) != 0 {
			t.Fatalf("offset past the end returned %d entries", len(list))
		}
	})

	t.Run("get", func(t *testing.T) {
		for _, s := range covScopeBodies(f) {
			url := "/api/v1/blackouts/" + ids[s.label]
			covExpect(t, covDo(serve, http.MethodGet, url, "", withSuperuser), http.StatusOK)
			want := http.StatusOK
			if !s.visible {
				want = http.StatusNotFound
			}
			covExpect(t, covDo(serve, http.MethodGet, url, "", e.restricted), want)
		}
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackouts/999999", "", withSuperuser), http.StatusNotFound)
	})

	t.Run("update", func(t *testing.T) {
		url := "/api/v1/blackouts/" + ids["server"]
		newEnd := now.Add(2 * time.Hour).Format(time.RFC3339)
		covExpect(t, covDo(serve, http.MethodPut, url, `{"reason": "extended", "end_time": "`+newEnd+`"}`, withSuperuser),
			http.StatusOK)
		covExpect(t, covDo(serve, http.MethodPut, url, `{"reason": "reason only"}`, withSuperuser), http.StatusOK)
		covExpect(t, covDo(serve, http.MethodPut, url, `{"end_time": "nope"}`, withSuperuser), http.StatusBadRequest)
		covExpect(t, covDo(serve, http.MethodPut, url, `{"end_time": "2000-01-01T00:00:00Z"}`, withSuperuser),
			http.StatusBadRequest)
		covExpect(t, covDo(serve, http.MethodPut, url, `{`, withSuperuser), http.StatusBadRequest)
		covExpect(t, covDo(serve, http.MethodPut, "/api/v1/blackouts/999999", `{"reason": "x"}`, withSuperuser),
			http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodPut, url, `{}`,
			func(r *http.Request) *http.Request { return withUser(r, 424242) }), http.StatusForbidden)
	})

	t.Run("stop and delete", func(t *testing.T) {
		url := "/api/v1/blackouts/" + ids["estate"]
		covExpect(t, covDo(serve, http.MethodPost, url+"/stop", "", withSuperuser), http.StatusOK)
		covExpect(t, covDo(serve, http.MethodPost, "/api/v1/blackouts/999999/stop", "", withSuperuser),
			http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodPost, url+"/stop", "",
			func(r *http.Request) *http.Request { return withUser(r, 424242) }), http.StatusForbidden)
		covExpect(t, covDo(serve, http.MethodDelete, url, "", withSuperuser), http.StatusOK)
		covExpect(t, covDo(serve, http.MethodDelete, url, "", withSuperuser), http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodDelete, url, "",
			func(r *http.Request) *http.Request { return withUser(r, 424242) }), http.StatusForbidden)
	})

	t.Run("datastore errors", func(t *testing.T) {
		b := e.broken
		body := `{"scope": "estate", ` + times + `}`
		covExpect(t, covDo(b.handleBlackouts, http.MethodGet, "/api/v1/blackouts", "", withSuperuser),
			http.StatusInternalServerError)
		covExpect(t, covDo(b.handleBlackouts, http.MethodGet, "/api/v1/blackouts", "", e.restricted),
			http.StatusInternalServerError)
		covExpect(t, covDo(b.handleBlackouts, http.MethodPost, "/api/v1/blackouts", body, withSuperuser),
			http.StatusInternalServerError)
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			covExpect(t, covDo(b.handleBlackoutSubpath, method, "/api/v1/blackouts/1", `{}`, withSuperuser),
				http.StatusInternalServerError)
		}
		covExpect(t, covDo(b.handleBlackoutSubpath, http.MethodPost, "/api/v1/blackouts/1/stop", "", withSuperuser),
			http.StatusInternalServerError)
	})

	t.Run("visibility lookup errors", func(t *testing.T) {
		// Dropping clusters breaks the group membership lookup whilst
		// leaving the blackouts and connections readable.
		if _, err := f.pool.Exec(context.Background(), "DROP TABLE clusters CASCADE"); err != nil {
			t.Fatalf("drop clusters: %v", err)
		}
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackouts", "", e.restricted),
			http.StatusInternalServerError)
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackouts/"+ids["group"], "", e.restricted),
			http.StatusInternalServerError)

		// Dropping connections breaks the visible-set resolution.
		if _, err := f.pool.Exec(context.Background(), "DROP TABLE connections CASCADE"); err != nil {
			t.Fatalf("drop connections: %v", err)
		}
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackouts/"+ids["group"], "", e.restricted),
			http.StatusInternalServerError)
	})
}

func TestBlackoutHandler_ScheduleLifecycle(t *testing.T) {
	e := newCovBlackoutEnv(t)
	f := e.f
	serve := e.mux().ServeHTTP
	fields := `"name": "nightly", "cron_expression": "0 2 * * *", "duration_minutes": 60, "reason": "backup"`
	su := func(r *http.Request) *http.Request { return withUsername(withSuperuser(r), "cov_admin") }

	ids := map[string]string{}
	visibleCount := 0
	for i, s := range covScopeBodies(f) {
		extra := ""
		if i%2 == 1 {
			extra = `, "timezone": "Europe/London", "enabled": false`
		}
		rec := covDo(serve, http.MethodPost, "/api/v1/blackout-schedules", "{"+s.json+", "+fields+extra+"}", su)
		covExpect(t, rec, http.StatusCreated)
		ids[s.label] = covCreatedID(t, rec.Body.Bytes())
		if s.visible {
			visibleCount++
		}
	}

	invalid := []string{
		`{`,
		`{"scope": "bogus", ` + fields + `}`,
		`{"scope": "estate", "cron_expression": "* * * * *", "duration_minutes": 5}`,
		`{"scope": "estate", "name": "n", "duration_minutes": 5}`,
		`{"scope": "estate", "name": "n", "cron_expression": "* * * * *", "duration_minutes": 0}`,
	}

	t.Run("create validation", func(t *testing.T) {
		for _, body := range invalid {
			covExpect(t, covDo(serve, http.MethodPost, "/api/v1/blackout-schedules", body, withSuperuser),
				http.StatusBadRequest)
		}
		covExpect(t, covDo(serve, http.MethodPost, "/api/v1/blackout-schedules",
			`{"scope": "server", "connection_id": 999999, `+fields+`}`, withSuperuser), http.StatusInternalServerError)
		covExpect(t, covDo(serve, http.MethodPost, "/api/v1/blackout-schedules", `{"scope": "estate", `+fields+`}`,
			func(r *http.Request) *http.Request { return withUser(r, 424242) }), http.StatusForbidden)
	})

	t.Run("list", func(t *testing.T) {
		rec := covDo(serve, http.MethodGet, "/api/v1/blackout-schedules", "", withSuperuser)
		covExpect(t, rec, http.StatusOK)
		if got := covTotal(t, rec.Body.Bytes()); got != len(ids) {
			t.Fatalf("superuser total = %d, want %d", got, len(ids))
		}
		filtered := fmt.Sprintf("/api/v1/blackout-schedules?scope=server&group_id=%d&cluster_id=%d&connection_id=%d&enabled=true",
			f.groupID, f.clusterID, f.connID)
		covExpect(t, covDo(serve, http.MethodGet, filtered, "", withSuperuser), http.StatusOK)

		rec = covDo(serve, http.MethodGet, "/api/v1/blackout-schedules", "", e.restricted)
		covExpect(t, rec, http.StatusOK)
		if got := covTotal(t, rec.Body.Bytes()); got != visibleCount {
			t.Fatalf("restricted total = %d, want %d", got, visibleCount)
		}
		rec = covDo(serve, http.MethodGet, "/api/v1/blackout-schedules?limit=2&offset=1", "", e.restricted)
		covExpect(t, rec, http.StatusOK)
		if list, _ := decodeRaw(t, rec.Body.Bytes())["schedules"].([]any); len(list) != 2 {
			t.Fatalf("paginated page has %d entries, want 2", len(list))
		}
		rec = covDo(serve, http.MethodGet, "/api/v1/blackout-schedules?offset=500", "", e.restricted)
		covExpect(t, rec, http.StatusOK)
	})

	t.Run("get", func(t *testing.T) {
		for _, s := range covScopeBodies(f) {
			url := "/api/v1/blackout-schedules/" + ids[s.label]
			covExpect(t, covDo(serve, http.MethodGet, url, "", withSuperuser), http.StatusOK)
			want := http.StatusOK
			if !s.visible {
				want = http.StatusNotFound
			}
			covExpect(t, covDo(serve, http.MethodGet, url, "", e.restricted), want)
		}
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackout-schedules/999999", "", withSuperuser),
			http.StatusNotFound)
	})

	t.Run("update", func(t *testing.T) {
		url := "/api/v1/blackout-schedules/" + ids["server"]
		body := fmt.Sprintf(`{"scope": "server", "connection_id": %d, %s}`, f.connID, fields)
		covExpect(t, covDo(serve, http.MethodPut, url, body, withSuperuser), http.StatusOK)
		withTZ := fmt.Sprintf(`{"scope": "server", "connection_id": %d, %s, "timezone": "UTC", "enabled": false}`,
			f.connID, fields)
		covExpect(t, covDo(serve, http.MethodPut, url, withTZ, withSuperuser), http.StatusOK)
		for _, bad := range invalid {
			covExpect(t, covDo(serve, http.MethodPut, url, bad, withSuperuser), http.StatusBadRequest)
		}
		covExpect(t, covDo(serve, http.MethodPut, "/api/v1/blackout-schedules/999999", body, withSuperuser),
			http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodPut, url, body,
			func(r *http.Request) *http.Request { return withUser(r, 424242) }), http.StatusForbidden)
	})

	t.Run("delete", func(t *testing.T) {
		url := "/api/v1/blackout-schedules/" + ids["estate"]
		covExpect(t, covDo(serve, http.MethodDelete, url, "", withSuperuser), http.StatusOK)
		covExpect(t, covDo(serve, http.MethodDelete, url, "", withSuperuser), http.StatusNotFound)
		covExpect(t, covDo(serve, http.MethodDelete, url, "",
			func(r *http.Request) *http.Request { return withUser(r, 424242) }), http.StatusForbidden)
	})

	t.Run("datastore errors", func(t *testing.T) {
		b := e.broken
		body := `{"scope": "estate", ` + fields + `}`
		covExpect(t, covDo(b.handleBlackoutSchedules, http.MethodGet, "/api/v1/blackout-schedules", "", withSuperuser),
			http.StatusInternalServerError)
		covExpect(t, covDo(b.handleBlackoutSchedules, http.MethodGet, "/api/v1/blackout-schedules", "", e.restricted),
			http.StatusInternalServerError)
		covExpect(t, covDo(b.handleBlackoutSchedules, http.MethodPost, "/api/v1/blackout-schedules", body, withSuperuser),
			http.StatusInternalServerError)
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			covExpect(t, covDo(b.handleBlackoutScheduleSubpath, method, "/api/v1/blackout-schedules/1", body, withSuperuser),
				http.StatusInternalServerError)
		}
	})

	t.Run("visibility lookup errors", func(t *testing.T) {
		if _, err := f.pool.Exec(context.Background(), "DROP TABLE clusters CASCADE"); err != nil {
			t.Fatalf("drop clusters: %v", err)
		}
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackout-schedules", "", e.restricted),
			http.StatusInternalServerError)
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackout-schedules/"+ids["group"], "", e.restricted),
			http.StatusInternalServerError)
		if _, err := f.pool.Exec(context.Background(), "DROP TABLE connections CASCADE"); err != nil {
			t.Fatalf("drop connections: %v", err)
		}
		covExpect(t, covDo(serve, http.MethodGet, "/api/v1/blackout-schedules/"+ids["group"], "", e.restricted),
			http.StatusInternalServerError)
	})
}
