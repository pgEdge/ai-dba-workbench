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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Connection scope on the admin-gated sibling endpoints (issue #471 review)
//
// Blackouts, blackout schedules, alert, probe and channel overrides and
// cluster topology are gated on an admin permission, which says nothing
// about the connections a token was issued for. These tests check that
// a superuser's token narrowed to one connection may change that
// connection and nothing wider, whilst sessions, unscoped tokens and
// wildcard tokens keep the access they had.
// =============================================================================

// scopeCaller builds a request context for one kind of caller.
type scopeCaller struct {
	name string
	wrap func(*http.Request) *http.Request
}

// scopedCallers creates the callers the tests share, against the given
// store: a superuser session, and superuser tokens that are unscoped,
// wildcard at read_write, limited to connections 5 and 7 at read_write,
// and limited to connection 5 read-only.
func scopedCallers(t *testing.T, store *auth.AuthStore) (session, unscoped,
	wildcard, narrowed, readOnly scopeCaller) {

	t.Helper()

	tokenWith := func(name string, conns []auth.ScopedConnection) scopeCaller {
		id := mustCreateScopedToken(t, store, name, nil)
		if conns != nil {
			if err := store.SetTokenConnectionScope(id, conns); err != nil {
				t.Fatalf("SetTokenConnectionScope failed: %v", err)
			}
		}
		return scopeCaller{name: name, wrap: func(r *http.Request) *http.Request {
			return withSuperuserToken(r, id)
		}}
	}

	session = scopeCaller{name: "session", wrap: withSuperuser}
	unscoped = tokenWith("svc-unscoped", nil)
	wildcard = tokenWith("svc-wildcard", []auth.ScopedConnection{
		{ConnectionID: auth.ConnectionIDAll, AccessLevel: auth.AccessLevelReadWrite},
	})
	narrowed = tokenWith("svc-narrowed", []auth.ScopedConnection{
		{ConnectionID: 5, AccessLevel: auth.AccessLevelReadWrite},
		{ConnectionID: 7, AccessLevel: auth.AccessLevelReadWrite},
	})
	readOnly = tokenWith("svc-read-only", []auth.ScopedConnection{
		{ConnectionID: 5, AccessLevel: auth.AccessLevelRead},
	})
	return session, unscoped, wildcard, narrowed, readOnly
}

// newScopeRequest builds a request with a JSON body for the caller.
func newScopeRequest(caller scopeCaller, method, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "/", nil)
	} else {
		req = httptest.NewRequest(method, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	return caller.wrap(req)
}

// assertOutOfTokenScope fails unless the response is the connection
// scope refusal.
func assertOutOfTokenScope(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), targetOutOfTokenScope) {
		t.Errorf("Expected the connection scope refusal, got %s",
			rec.Body.String())
	}
}

// scopeCase is one request that the named callers must pass or be
// refused on.
type scopeCase struct {
	name    string
	call    func(w http.ResponseWriter, caller scopeCaller)
	allowed []scopeCaller
	refused []scopeCaller
}

// runScopeCases checks each case: allowed callers must get past the
// gates (a nil datastore may panic beyond them, which is recovered), and
// refused callers must receive the connection scope refusal.
func runScopeCases(t *testing.T, cases []scopeCase) {
	t.Helper()
	for _, tc := range cases {
		for _, caller := range tc.allowed {
			t.Run(tc.name+"/allowed/"+caller.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				assertGatePassed(t, rec, func() { tc.call(rec, caller) })
			})
		}
		for _, caller := range tc.refused {
			t.Run(tc.name+"/refused/"+caller.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				tc.call(rec, caller)
				assertOutOfTokenScope(t, rec)
			})
		}
	}
}

// TestTargetInTokenScope checks the scope rule shared by blackouts,
// schedules and overrides.
func TestTargetInTokenScope(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	session, unscoped, wildcard, narrowed, readOnly := scopedCallers(t, store)
	rc := auth.NewRBACChecker(store)

	five, six := 5, 6
	cases := []struct {
		name         string
		caller       scopeCaller
		scope        string
		connectionID *int
		want         bool
	}{
		{"session estate", session, "estate", nil, true},
		{"unscoped group", unscoped, "group", nil, true},
		{"wildcard cluster", wildcard, "cluster", nil, true},
		{"narrowed server in scope", narrowed, "server", &five, true},
		{"narrowed server out of scope", narrowed, "server", &six, false},
		{"narrowed cluster", narrowed, "cluster", nil, false},
		{"narrowed group", narrowed, "group", nil, false},
		{"narrowed estate", narrowed, "estate", nil, false},
		{"narrowed server without id", narrowed, "server", nil, false},
		{"read-only server", readOnly, "server", &five, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.caller.wrap(httptest.NewRequest(http.MethodGet, "/",
				nil)).Context()
			if got := targetInTokenScope(ctx, rc, tc.scope,
				tc.connectionID); got != tc.want {
				t.Errorf("targetInTokenScope = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("nil store passes", func(t *testing.T) {
		if !targetInTokenScope(context.Background(), auth.NewRBACChecker(nil),
			"estate", nil) {
			t.Error("Expected a checker without a store to pass")
		}
	})
}

// TestOverrideWritesRespectTokenScope covers upsert and delete on the
// alert, probe and channel override handlers.
func TestOverrideWritesRespectTokenScope(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	session, unscoped, wildcard, narrowed, readOnly := scopedCallers(t, store)
	checker := auth.NewRBACChecker(store)

	alert := NewAlertOverrideHandler(nil, store, checker)
	probe := NewProbeOverrideHandler(nil, store, checker)
	channel := NewChannelOverrideHandler(nil, store, checker)

	type overrideWrite func(w http.ResponseWriter, r *http.Request,
		scope string, scopeID int)
	writes := map[string]overrideWrite{
		"alert upsert": func(w http.ResponseWriter, r *http.Request, s string, id int) {
			alert.upsertOverride(w, r, s, id, 1)
		},
		"alert delete": func(w http.ResponseWriter, r *http.Request, s string, id int) {
			alert.deleteOverride(w, r, s, id, 1)
		},
		"probe upsert": func(w http.ResponseWriter, r *http.Request, s string, id int) {
			probe.upsertOverride(w, r, s, id, "pg_stat_activity")
		},
		"probe delete": func(w http.ResponseWriter, r *http.Request, s string, id int) {
			probe.deleteOverride(w, r, s, id, "pg_stat_activity")
		},
		"channel upsert": func(w http.ResponseWriter, r *http.Request, s string, id int) {
			channel.upsertOverride(w, r, s, id, 1)
		},
		"channel delete": func(w http.ResponseWriter, r *http.Request, s string, id int) {
			channel.deleteOverride(w, r, s, id, 1)
		},
	}

	var cases []scopeCase
	for name, write := range writes {
		write := write
		at := func(scope string, id int) func(http.ResponseWriter, scopeCaller) {
			return func(w http.ResponseWriter, c scopeCaller) {
				write(w, newScopeRequest(c, http.MethodPut, `{}`), scope, id)
			}
		}
		cases = append(cases,
			scopeCase{
				name:    name + " server in scope",
				call:    at("server", 5),
				allowed: []scopeCaller{session, unscoped, wildcard, narrowed},
				refused: []scopeCaller{readOnly},
			},
			scopeCase{
				name:    name + " server out of scope",
				call:    at("server", 6),
				refused: []scopeCaller{narrowed, readOnly},
			},
			scopeCase{
				name:    name + " cluster",
				call:    at("cluster", 5),
				allowed: []scopeCaller{session, unscoped, wildcard},
				refused: []scopeCaller{narrowed},
			},
			scopeCase{
				name:    name + " group",
				call:    at("group", 5),
				allowed: []scopeCaller{wildcard},
				refused: []scopeCaller{narrowed},
			},
		)
	}
	runScopeCases(t, cases)
}

// TestBlackoutCreateRespectsTokenScope covers creating a blackout and a
// blackout schedule, which name their target in the request body.
func TestBlackoutCreateRespectsTokenScope(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	session, unscoped, wildcard, narrowed, readOnly := scopedCallers(t, store)
	h := NewBlackoutHandler(nil, store, auth.NewRBACChecker(store))

	blackout := func(target string) func(http.ResponseWriter, scopeCaller) {
		body := `{` + target + `,"reason":"r",` +
			`"start_time":"2026-01-01T00:00:00Z","end_time":"2026-01-02T00:00:00Z"}`
		return func(w http.ResponseWriter, c scopeCaller) {
			h.createBlackout(w, newScopeRequest(c, http.MethodPost, body))
		}
	}
	schedule := func(target string) func(http.ResponseWriter, scopeCaller) {
		body := `{` + target + `,"name":"n","cron_expression":"0 * * * *",` +
			`"duration_minutes":30,"timezone":"UTC","reason":"r"}`
		return func(w http.ResponseWriter, c scopeCaller) {
			h.createBlackoutSchedule(w, newScopeRequest(c, http.MethodPost, body))
		}
	}

	const server5 = `"scope":"server","connection_id":5`
	const server6 = `"scope":"server","connection_id":6`
	const cluster = `"scope":"cluster","cluster_id":1`
	const estate = `"scope":"estate"`

	runScopeCases(t, []scopeCase{
		{name: "blackout server in scope", call: blackout(server5),
			allowed: []scopeCaller{session, unscoped, wildcard, narrowed},
			refused: []scopeCaller{readOnly}},
		{name: "blackout server out of scope", call: blackout(server6),
			refused: []scopeCaller{narrowed}},
		{name: "blackout cluster", call: blackout(cluster),
			allowed: []scopeCaller{wildcard}, refused: []scopeCaller{narrowed}},
		{name: "blackout estate", call: blackout(estate),
			allowed: []scopeCaller{session}, refused: []scopeCaller{narrowed}},
		{name: "schedule server in scope", call: schedule(server5),
			allowed: []scopeCaller{unscoped, narrowed},
			refused: []scopeCaller{readOnly}},
		{name: "schedule server out of scope", call: schedule(server6),
			refused: []scopeCaller{narrowed}},
		{name: "schedule estate", call: schedule(estate),
			allowed: []scopeCaller{wildcard}, refused: []scopeCaller{narrowed}},
	})
}

// TestClusterWritesRespectTokenScope covers the cluster handlers: a
// change to a cluster or group definition, or to relationships, needs
// every connection in scope, and adding or removing a server needs that
// connection.
func TestClusterWritesRespectTokenScope(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	session, unscoped, wildcard, narrowed, readOnly := scopedCallers(t, store)
	h := NewClusterHandler(nil, store, auth.NewRBACChecker(store))

	put := func(fn func(http.ResponseWriter, *http.Request),
		body string) func(http.ResponseWriter, scopeCaller) {
		return func(w http.ResponseWriter, c scopeCaller) {
			fn(w, newScopeRequest(c, http.MethodPut, body))
		}
	}
	del := func(fn func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, scopeCaller) {
		return func(w http.ResponseWriter, c scopeCaller) {
			fn(w, newScopeRequest(c, http.MethodDelete, ""))
		}
	}
	everyConnection := func(name string,
		fn func(http.ResponseWriter, *http.Request), body string) scopeCase {
		return scopeCase{
			name:    name,
			call:    put(fn, body),
			allowed: []scopeCaller{session, unscoped, wildcard},
			refused: []scopeCaller{narrowed, readOnly},
		}
	}

	relationships := func(connID int) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			h.setConnectionRelationships(w, r, 1, connID)
		}
	}
	relBody := func(target string) string {
		return `{"relationships":[{"target_connection_id":` + target +
			`,"relationship_type":"streams_from"}]}`
	}

	runScopeCases(t, []scopeCase{
		everyConnection("update cluster", func(w http.ResponseWriter, r *http.Request) {
			h.updateCluster(w, r, 1)
		}, `{"name":"c"}`),
		everyConnection("delete cluster", func(w http.ResponseWriter, r *http.Request) {
			h.deleteCluster(w, r, 1)
		}, ""),
		everyConnection("update auto-detected cluster", func(w http.ResponseWriter, r *http.Request) {
			h.updateAutoDetectedCluster(w, r, "spock:1")
		}, `{"name":"c"}`),
		everyConnection("delete auto-detected cluster", func(w http.ResponseWriter, r *http.Request) {
			h.deleteAutoDetectedCluster(w, r, "spock:1")
		}, ""),
		everyConnection("update auto-detected group", func(w http.ResponseWriter, r *http.Request) {
			h.updateAutoDetectedGroup(w, r, "group-auto-1")
		}, `{"name":"g"}`),
		{
			name: "delete relationship",
			call: del(func(w http.ResponseWriter, r *http.Request) {
				h.handleDeleteRelationship(w, r, 1, 1)
			}),
			allowed: []scopeCaller{session, unscoped, wildcard},
			refused: []scopeCaller{narrowed, readOnly},
		},
		{
			name: "add server in scope",
			call: put(func(w http.ResponseWriter, r *http.Request) {
				h.addServerToCluster(w, r, 1)
			}, `{"connection_id":5}`),
			allowed: []scopeCaller{session, unscoped, wildcard, narrowed},
			refused: []scopeCaller{readOnly},
		},
		{
			name: "add server out of scope",
			call: put(func(w http.ResponseWriter, r *http.Request) {
				h.addServerToCluster(w, r, 1)
			}, `{"connection_id":6}`),
			refused: []scopeCaller{narrowed},
		},
		{
			name: "remove server in scope",
			call: del(func(w http.ResponseWriter, r *http.Request) {
				h.handleRemoveServerFromCluster(w, r, 1, 5)
			}),
			allowed: []scopeCaller{narrowed},
			refused: []scopeCaller{readOnly},
		},
		{
			name: "remove server out of scope",
			call: del(func(w http.ResponseWriter, r *http.Request) {
				h.handleRemoveServerFromCluster(w, r, 1, 6)
			}),
			refused: []scopeCaller{narrowed},
		},
		// Setting or clearing a source's relationships first deletes every
		// manual relationship from that source, whatever its target, so
		// even a source and targets the token holds are not enough.
		{
			name:    "set relationships",
			call:    put(relationships(5), relBody("7")),
			allowed: []scopeCaller{session, unscoped, wildcard},
			refused: []scopeCaller{narrowed, readOnly},
		},
		{
			name:    "set relationships to a target out of scope",
			call:    put(relationships(5), relBody("6")),
			refused: []scopeCaller{narrowed},
		},
		{
			name: "clear relationships",
			call: del(func(w http.ResponseWriter, r *http.Request) {
				h.clearConnectionRelationships(w, r, 1, 7)
			}),
			allowed: []scopeCaller{session, unscoped, wildcard},
			refused: []scopeCaller{narrowed, readOnly},
		},
	})
}
