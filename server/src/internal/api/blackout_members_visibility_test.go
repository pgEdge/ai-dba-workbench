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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Cluster and group blackouts need every member visible (issue #732)
//
// A blackout or blackout schedule at cluster or group scope silences
// alerting on every member connection, so a restricted caller who sees
// only some members may not create, change or remove one. The refusal is
// the missing-permission refusal, audited, as for overrides (issue
// #716); a target the caller cannot see at all answers as missing.
// =============================================================================

// blackoutMemberTarget is one target a blackout write names, as the
// JSON fields of a request body, with the status a restricted caller
// who owns the fixture's visible connections must receive.
type blackoutMemberTarget struct {
	name      string
	scope     string
	group     *int
	cluster   *int
	conn      *int
	createWas int
}

// json renders the target as request body fields.
func (b blackoutMemberTarget) json() string {
	switch {
	case b.scope == "estate":
		return `"scope":"estate"`
	case b.conn != nil:
		return fmt.Sprintf(`"scope":"server","connection_id":%d`, *b.conn)
	case b.cluster != nil:
		return fmt.Sprintf(`"scope":"cluster","cluster_id":%d`, *b.cluster)
	default:
		return fmt.Sprintf(`"scope":"group","group_id":%d`, *b.group)
	}
}

func memberBlackoutBody(target blackoutMemberTarget) string {
	return `{` + target.json() + `,"reason":"r",` +
		`"start_time":"2026-01-01T00:00:00Z","end_time":"2036-01-01T00:00:00Z"}`
}

func memberScheduleBody(target blackoutMemberTarget) string {
	return `{` + target.json() + `,"name":"n","cron_expression":"0 * * * *",` +
		`"duration_minutes":30,"timezone":"UTC","reason":"r"}`
}

// seedBlackoutRows inserts an active blackout and a schedule on the
// target and returns their IDs.
func seedBlackoutRows(t *testing.T, f *covFixture, target blackoutMemberTarget) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	var blackoutID, scheduleID int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO blackouts
        (scope, group_id, cluster_id, connection_id, reason, start_time, end_time, created_by)
        VALUES ($1, $2, $3, $4, 'seeded', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 'admin')
        RETURNING id`, target.scope, target.group, target.cluster, target.conn).Scan(&blackoutID); err != nil {
		t.Fatalf("seed blackout: %v", err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO blackout_schedules
        (scope, group_id, cluster_id, connection_id, name, cron_expression, duration_minutes, reason, created_by)
        VALUES ($1, $2, $3, $4, 'seeded', '0 * * * *', 30, 'seeded', 'admin')
        RETURNING id`, target.scope, target.group, target.cluster, target.conn).Scan(&scheduleID); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	return blackoutID, scheduleID
}

// blackoutRowState returns a snapshot of the blackout and schedule rows,
// or "missing", so that a refused write can be shown to change nothing.
func blackoutRowState(t *testing.T, f *covFixture, blackoutID, scheduleID int64) string {
	t.Helper()
	ctx := context.Background()
	state := func(q string, id int64) string {
		var s string
		if err := f.pool.QueryRow(ctx, q, id).Scan(&s); err != nil {
			return "missing"
		}
		return s
	}
	return state(`SELECT reason || '|' || end_time::text || '|' || scope FROM blackouts WHERE id = $1`, blackoutID) +
		" / " + state(`SELECT name || '|' || reason || '|' || scope || '|' ||
            coalesce(cluster_id::text, '') || coalesce(group_id::text, '') || coalesce(connection_id::text, '')
            FROM blackout_schedules WHERE id = $1`, scheduleID)
}

// TestBlackoutWritesNeedEveryMemberVisible drives every blackout and
// schedule write at full, mixed and hidden targets, for each kind of
// caller.
func TestBlackoutWritesNeedEveryMemberVisible(t *testing.T) {
	f := newCovFixture(t)
	m := addMixedScopes(t, f)
	store := newTestAuthStore(t)
	h := NewBlackoutHandler(f.ds, store, auth.NewRBACChecker(store))
	var audited []string
	h.SetDenialRecorder(func(_ *http.Request, reason string) { audited = append(audited, reason) })

	userID := setupUserWithPermissions(t, store, covOwner, []string{auth.PermManageBlackouts})
	session := func(r *http.Request) *http.Request {
		return withUsername(withUser(r, userID), covOwner)
	}
	unscopedToken := covTokenFor(t, store, userID, nil)
	wildcardToken := covTokenFor(t, store, userID, []auth.ScopedConnection{
		{ConnectionID: auth.ConnectionIDAll, AccessLevel: auth.AccessLevelReadWrite},
	})
	noPerm := func(r *http.Request) *http.Request { return withUser(r, 424242) }

	full := blackoutMemberTarget{name: "full cluster", scope: "cluster", cluster: intPtr(m.fullCluster), createWas: http.StatusCreated}
	mixed := blackoutMemberTarget{name: "mixed cluster", scope: "cluster", cluster: intPtr(m.mixedCluster), createWas: http.StatusForbidden}
	estate := blackoutMemberTarget{name: "estate", scope: "estate", createWas: http.StatusForbidden}
	targets := []blackoutMemberTarget{
		full,
		{name: "full group", scope: "group", group: intPtr(m.fullGroup), createWas: http.StatusCreated},
		{name: "split visible cluster", scope: "cluster", cluster: intPtr(m.splitVisible), createWas: http.StatusCreated},
		{name: "visible server", scope: "server", conn: intPtr(m.ownedFull), createWas: http.StatusCreated},
		mixed,
		{name: "mixed group", scope: "group", group: intPtr(m.mixedGroup), createWas: http.StatusForbidden},
		{name: "split group", scope: "group", group: intPtr(m.splitGroup), createWas: http.StatusForbidden},
		// An estate blackout is visible to everyone but silences every
		// connection, so only a caller who sees them all may write one.
		estate,
		{name: "hidden cluster", scope: "cluster", cluster: intPtr(m.splitHidden), createWas: http.StatusNotFound},
		{name: "hidden group", scope: "group", group: intPtr(f.hiddenGroupID), createWas: http.StatusNotFound},
		{name: "hidden server", scope: "server", conn: intPtr(f.hiddenConnID), createWas: http.StatusNotFound},
	}

	// The refusal must be byte for byte what a caller without the
	// permission gets, so that it discloses nothing more.
	noPermRec := covDo(h.handleBlackouts, http.MethodPost, "/api/v1/blackouts", memberBlackoutBody(full), noPerm)
	covExpect(t, noPermRec, http.StatusForbidden)
	refusal := adminPermissionRefusal(auth.PermManageBlackouts)

	type write struct {
		name   string
		ok     int
		notID  string // the 404 message for a hidden stored target
		doCall func(caller func(*http.Request) *http.Request, target blackoutMemberTarget,
			blackoutID, scheduleID int64) *httptest.ResponseRecorder
	}
	byID := func(prefix string, id int64) string { return prefix + strconv.FormatInt(id, 10) }
	writes := []write{
		{"create blackout", http.StatusCreated, "", func(c func(*http.Request) *http.Request, tg blackoutMemberTarget, _, _ int64) *httptest.ResponseRecorder {
			return covDo(h.handleBlackouts, http.MethodPost, "/api/v1/blackouts", memberBlackoutBody(tg), c)
		}},
		{"create schedule", http.StatusCreated, "", func(c func(*http.Request) *http.Request, tg blackoutMemberTarget, _, _ int64) *httptest.ResponseRecorder {
			return covDo(h.handleBlackoutSchedules, http.MethodPost, "/api/v1/blackout-schedules", memberScheduleBody(tg), c)
		}},
		{"update blackout", http.StatusOK, "Blackout not found", func(c func(*http.Request) *http.Request, _ blackoutMemberTarget, b, _ int64) *httptest.ResponseRecorder {
			return covDo(h.handleBlackoutSubpath, http.MethodPut, byID("/api/v1/blackouts/", b), `{"reason":"changed"}`, c)
		}},
		{"update schedule", http.StatusOK, "Blackout schedule not found", func(c func(*http.Request) *http.Request, tg blackoutMemberTarget, _, s int64) *httptest.ResponseRecorder {
			return covDo(h.handleBlackoutScheduleSubpath, http.MethodPut, byID("/api/v1/blackout-schedules/", s), memberScheduleBody(tg), c)
		}},
		{"stop blackout", http.StatusOK, "Blackout not found", func(c func(*http.Request) *http.Request, _ blackoutMemberTarget, b, _ int64) *httptest.ResponseRecorder {
			return covDo(h.handleBlackoutSubpath, http.MethodPost, byID("/api/v1/blackouts/", b)+"/stop", "", c)
		}},
		{"delete blackout", http.StatusOK, "Blackout not found", func(c func(*http.Request) *http.Request, _ blackoutMemberTarget, b, _ int64) *httptest.ResponseRecorder {
			return covDo(h.handleBlackoutSubpath, http.MethodDelete, byID("/api/v1/blackouts/", b), "", c)
		}},
		{"delete schedule", http.StatusOK, "Blackout schedule not found", func(c func(*http.Request) *http.Request, _ blackoutMemberTarget, _, s int64) *httptest.ResponseRecorder {
			return covDo(h.handleBlackoutScheduleSubpath, http.MethodDelete, byID("/api/v1/blackout-schedules/", s), "", c)
		}},
	}

	restricted := []struct {
		name string
		wrap func(*http.Request) *http.Request
	}{
		{"session", session}, {"unscoped token", unscopedToken}, {"wildcard-scope token", wildcardToken},
	}
	for _, caller := range restricted {
		for _, tg := range targets {
			blackoutID, scheduleID := seedBlackoutRows(t, f, tg)
			before := blackoutRowState(t, f, blackoutID, scheduleID)
			for _, wr := range writes {
				name := caller.name + " " + wr.name + " " + tg.name
				want := wr.ok
				if tg.createWas != http.StatusCreated {
					want = tg.createWas
				}
				audited = nil
				rec := wr.doCall(caller.wrap, tg, blackoutID, scheduleID)
				if rec.Code != want {
					t.Fatalf("%s: status %d, want %d; body=%s", name, rec.Code, want, rec.Body.String())
				}
				switch want {
				case http.StatusForbidden:
					if rec.Body.String() != noPermRec.Body.String() {
						t.Errorf("%s: refusal %q, want %q", name, rec.Body.String(), noPermRec.Body.String())
					}
					if len(audited) != 1 || audited[0] != refusal {
						t.Errorf("%s: audited %v, want [%q]", name, audited, refusal)
					}
				case http.StatusNotFound:
					msg := wr.notID
					if msg == "" {
						msg = newBlackoutTarget(tg.group, tg.cluster, tg.conn).notFoundMessage()
					}
					assertError(t, rec, http.StatusNotFound, msg)
					if len(audited) != 0 {
						t.Errorf("%s: a hidden target audited %v", name, audited)
					}
				}
			}
			if tg.createWas != http.StatusCreated {
				if after := blackoutRowState(t, f, blackoutID, scheduleID); after != before {
					t.Errorf("%s %s: rows changed from %s to %s", caller.name, tg.name, before, after)
				}
			}
		}
	}

	// A schedule may be neither moved off nor onto a target with a
	// hidden member.
	moves := []struct {
		name     string
		from, to blackoutMemberTarget
	}{
		{"onto a mixed cluster", full, mixed},
		{"off a mixed cluster", mixed, full},
		{"onto the estate", full, estate},
		{"off the estate", estate, full},
	}
	for _, mv := range moves {
		_, scheduleID := seedBlackoutRows(t, f, mv.from)
		audited = nil
		rec := covDo(h.handleBlackoutScheduleSubpath, http.MethodPut,
			byID("/api/v1/blackout-schedules/", scheduleID), memberScheduleBody(mv.to), session)
		covExpect(t, rec, http.StatusForbidden)
		if rec.Body.String() != noPermRec.Body.String() || len(audited) != 1 {
			t.Errorf("move %s: refusal %q audited %v", mv.name, rec.Body.String(), audited)
		}
	}
	// A schedule moved onto a hidden target answers as missing.
	_, scheduleID := seedBlackoutRows(t, f, full)
	rec := covDo(h.handleBlackoutScheduleSubpath, http.MethodPut,
		byID("/api/v1/blackout-schedules/", scheduleID), memberScheduleBody(targets[len(targets)-1]), session)
	assertError(t, rec, http.StatusNotFound, "Connection not found")

	// A token narrowed to one member keeps the token-scope refusal it
	// already had, even where every member is visible to its owner, and
	// a cluster whose members its scope does not name answers as missing.
	narrowedToken := covTokenFor(t, store, userID, []auth.ScopedConnection{
		{ConnectionID: m.ownedFull, AccessLevel: auth.AccessLevelReadWrite},
	})
	assertOutOfTokenScope(t, covDo(h.handleBlackouts, http.MethodPost, "/api/v1/blackouts", memberBlackoutBody(full), narrowedToken))
	assertError(t, covDo(h.handleBlackouts, http.MethodPost, "/api/v1/blackouts", memberBlackoutBody(mixed), narrowedToken),
		http.StatusNotFound, "Cluster not found")

	// A token narrowed to the one member of the mixed cluster its owner
	// can see finds the cluster visible, but a cluster target needs every
	// connection in the token's scope, so it gets the token-scope refusal
	// before the member check is reached.
	var mixedOwned int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id FROM connections WHERE name = 'mixed owned'`).Scan(&mixedOwned); err != nil {
		t.Fatalf("find mixed owned: %v", err)
	}
	mixedMemberToken := covTokenFor(t, store, userID, []auth.ScopedConnection{
		{ConnectionID: mixedOwned, AccessLevel: auth.AccessLevelReadWrite},
	})
	for _, create := range []struct {
		route   string
		handler http.HandlerFunc
		body    string
	}{
		{"/api/v1/blackouts", h.handleBlackouts, memberBlackoutBody(mixed)},
		{"/api/v1/blackout-schedules", h.handleBlackoutSchedules, memberScheduleBody(mixed)},
	} {
		audited = nil
		assertOutOfTokenScope(t, covDo(create.handler, http.MethodPost, create.route, create.body, mixedMemberToken))
		if len(audited) != 1 || audited[0] != targetOutOfTokenScope {
			t.Errorf("%s: audited %v, want [%q]", create.route, audited, targetOutOfTokenScope)
		}
	}

	// Unrestricted callers are unaffected: a superuser and a user whose
	// group grant covers every connection.
	wideID := setupUserWithPermissions(t, store, "wide_user", []string{auth.PermManageBlackouts})
	groups, err := store.GetUserGroups(wideID)
	if err != nil || len(groups) == 0 {
		t.Fatalf("GetUserGroups: %v %v", groups, err)
	}
	if err := store.GrantConnectionPrivilege(groups[0], auth.ConnectionIDAll, auth.AccessLevelReadWrite); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}
	wide := func(r *http.Request) *http.Request { return withUsername(withUser(r, wideID), "wide_user") }
	for _, caller := range []func(*http.Request) *http.Request{withSuperuser, wide} {
		for _, tg := range targets {
			blackoutID, scheduleID := seedBlackoutRows(t, f, tg)
			for _, wr := range writes {
				rec := wr.doCall(caller, tg, blackoutID, scheduleID)
				if rec.Code != wr.ok {
					t.Fatalf("unrestricted %s %s: status %d, want %d; body=%s",
						wr.name, tg.name, rec.Code, wr.ok, rec.Body.String())
				}
			}
		}
	}

	// A target that does not exist answers as missing for an unrestricted
	// caller too, rather than failing on the foreign key.
	missing := 987654321
	_, fullScheduleID := seedBlackoutRows(t, f, full)
	for _, tg := range []blackoutMemberTarget{
		{name: "missing server", scope: "server", conn: &missing},
		{name: "missing cluster", scope: "cluster", cluster: &missing},
		{name: "missing group", scope: "group", group: &missing},
	} {
		msg := newBlackoutTarget(tg.group, tg.cluster, tg.conn).notFoundMessage()
		assertError(t, covDo(h.handleBlackouts, http.MethodPost, "/api/v1/blackouts", memberBlackoutBody(tg), withSuperuser),
			http.StatusNotFound, msg)
		assertError(t, covDo(h.handleBlackoutSchedules, http.MethodPost, "/api/v1/blackout-schedules", memberScheduleBody(tg), withSuperuser),
			http.StatusNotFound, msg)
		assertError(t, covDo(h.handleBlackoutScheduleSubpath, http.MethodPut,
			byID("/api/v1/blackout-schedules/", fullScheduleID), memberScheduleBody(tg), withSuperuser),
			http.StatusNotFound, msg)
	}

	// A caller who sees every connection always holds a token scope that
	// covers every connection too, since a narrowed scope narrows the
	// visible set, so the fast path that skips loading the stored record
	// never bypasses the token-scope check.
	wildcardReadToken := covTokenFor(t, store, userID, []auth.ScopedConnection{
		{ConnectionID: auth.ConnectionIDAll, AccessLevel: auth.AccessLevelRead},
	})
	for name, caller := range map[string]func(*http.Request) *http.Request{
		"superuser": withSuperuser, "wide grant": wide, "session": session,
		"wildcard token": wildcardToken, "wildcard read token": wildcardReadToken,
		"narrowed token": narrowedToken, "mixed member token": mixedMemberToken,
	} {
		r := caller(httptest.NewRequest(http.MethodGet, "/", nil))
		a, ok := h.resolveBlackoutWriteAccess(httptest.NewRecorder(), r)
		if !ok {
			t.Fatalf("%s: access did not resolve", name)
		}
		if a.all && !h.unrestrictedBlackoutWriter(r, a) {
			t.Errorf("%s: sees every connection but its token scope is narrowed", name)
		}
	}

	// Reads are unchanged: a blackout on a mixed cluster stays readable.
	blackoutID, _ := seedBlackoutRows(t, f, mixed)
	covExpect(t, covDo(h.handleBlackoutSubpath, http.MethodGet, byID("/api/v1/blackouts/", blackoutID), "", session),
		http.StatusOK)
}

// TestBlackoutWriteAccessFailures covers the 500s a blackout write gives
// when the caller's view of the target cannot be worked out.
func TestBlackoutWriteAccessFailures(t *testing.T) {
	createCluster := `{"scope":"cluster","cluster_id":1,"reason":"r",` +
		`"start_time":"2026-01-01T00:00:00Z","end_time":"2036-01-01T00:00:00Z"}`
	clusterSchedule := `{"scope":"cluster","cluster_id":1,"name":"n",` +
		`"cron_expression":"0 * * * *","duration_minutes":30,"timezone":"UTC","reason":"r"}`
	calls := map[string]func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller){
		"create blackout": func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller) {
			h.createBlackout(w, newScopeRequest(c, http.MethodPost, createCluster))
		},
		"create schedule": func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller) {
			h.createBlackoutSchedule(w, newScopeRequest(c, http.MethodPost, clusterSchedule))
		},
		"stop blackout": func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller) {
			h.stopBlackout(w, newScopeRequest(c, http.MethodPost, ""), 3)
		},
		"update blackout": func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller) {
			h.updateBlackout(w, newScopeRequest(c, http.MethodPut, `{"reason":"x"}`), 3)
		},
		"delete blackout": func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller) {
			h.deleteBlackout(w, newScopeRequest(c, http.MethodDelete, ""), 3)
		},
		"update schedule": func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller) {
			h.updateBlackoutSchedule(w, newScopeRequest(c, http.MethodPut, clusterSchedule), 3)
		},
		"delete schedule": func(h *BlackoutHandler, w http.ResponseWriter, c scopeCaller) {
			h.deleteBlackoutSchedule(w, newScopeRequest(c, http.MethodDelete, ""), 3)
		},
	}

	for name, call := range calls {
		// A restricted user's visible connections are listed from the
		// connections table, which is gone, whilst the blackout tables
		// still answer.
		t.Run(name+"/visible set", func(t *testing.T) {
			h, _, _ := newBlackoutCoverageHandler(t, blackoutDropConnections)
			restricted := scopeCaller{name: "restricted",
				wrap: covRestrictedUser(t, h.authStore, auth.PermManageBlackouts)}
			rec := httptest.NewRecorder()
			call(h, rec, restricted)
			assertError(t, rec, http.StatusInternalServerError, "Failed to check access")
		})
		// The visible set resolves from the token alone, but the
		// cluster's members cannot be read.
		t.Run(name+"/members", func(t *testing.T) {
			h, _, callers := newBlackoutCoverageHandler(t, blackoutDropConnections)
			rec := httptest.NewRecorder()
			call(h, rec, callers["narrowed"])
			assertError(t, rec, http.StatusInternalServerError, "Failed to check access")
		})
	}
}

func TestBlackoutTargetReach(t *testing.T) {
	ds := &fakeVisibilityDatastore{
		clusters: map[int][]int{1: {10, 11}, 2: {20}, 3: nil},
		groups:   map[int][]int{100: {10, 20}},
	}
	visible := map[int]bool{10: true, 11: true}
	cases := []struct {
		name    string
		target  blackoutTarget
		members []int
		seen    bool
		message string
	}{
		{"visible server", newBlackoutTarget(nil, nil, intPtr(10)), []int{10}, true, "Connection not found"},
		{"hidden server", newBlackoutTarget(nil, nil, intPtr(20)), []int{20}, false, "Connection not found"},
		{"visible cluster", newBlackoutTarget(nil, intPtr(1), nil), []int{10, 11}, true, "Cluster not found"},
		{"hidden cluster", newBlackoutTarget(nil, intPtr(2), nil), []int{20}, false, "Cluster not found"},
		{"empty cluster", newBlackoutTarget(nil, intPtr(3), nil), nil, false, "Cluster not found"},
		{"mixed group", newBlackoutTarget(intPtr(100), nil, nil), []int{10, 20}, true, "Cluster group not found"},
		// An estate target is visible to everyone; the member check, not
		// visibility, refuses a caller who cannot see every connection.
		{"estate", newBlackoutTarget(nil, nil, nil), nil, true, "Cluster group not found"},
		// A stored row keeps only the ID its scope names, and a server row
		// without a connection silences everything, as the alerter reads it.
		{"server row without a connection", targetOfBlackout(&database.Blackout{Scope: "server", ClusterID: intPtr(2)}), nil, true, "Cluster group not found"},
		{"server row with a stray cluster", targetOfBlackout(&database.Blackout{Scope: "server", ClusterID: intPtr(2), ConnectionID: intPtr(10)}), []int{10}, true, "Connection not found"},
		{"cluster schedule with a stray group", targetOfSchedule(&database.BlackoutSchedule{Scope: "cluster", GroupID: intPtr(100), ClusterID: intPtr(2)}), []int{20}, false, "Cluster not found"},
		{"group row", targetOfBlackout(&database.Blackout{Scope: "group", GroupID: intPtr(100), ConnectionID: intPtr(20)}), []int{10, 20}, true, "Cluster group not found"},
		{"estate row with stray IDs", targetOfSchedule(&database.BlackoutSchedule{Scope: "estate", ConnectionID: intPtr(20)}), nil, true, "Cluster group not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			members, seen, err := blackoutTargetReach(context.Background(), ds, tc.target, visible)
			if err != nil {
				t.Fatalf("blackoutTargetReach: %v", err)
			}
			if seen != tc.seen || fmt.Sprint(members) != fmt.Sprint(tc.members) {
				t.Errorf("got %v %v, want %v %v", members, seen, tc.members, tc.seen)
			}
			if got := tc.target.notFoundMessage(); got != tc.message {
				t.Errorf("notFoundMessage = %q, want %q", got, tc.message)
			}
			// Only an estate target is seen with no members.
			if got, want := tc.target.estate(), tc.members == nil && tc.seen; got != want {
				t.Errorf("estate() = %v, want %v", got, want)
			}
		})
	}

	failing := &fakeVisibilityDatastore{err: errors.New("boom")}
	for _, tg := range []blackoutTarget{newBlackoutTarget(nil, intPtr(1), nil), newBlackoutTarget(intPtr(1), nil, nil)} {
		if _, _, err := blackoutTargetReach(context.Background(), failing, tg, visible); err == nil {
			t.Errorf("expected a lookup error for %+v", tg)
		}
	}

	// A missing record is never visible.
	h := &BlackoutHandler{}
	if seen, err := h.blackoutVisible(context.Background(), nil, visible); seen || err != nil {
		t.Errorf("blackoutVisible(nil) = %v, %v", seen, err)
	}
	if seen, err := h.blackoutScheduleVisible(context.Background(), nil, visible); seen || err != nil {
		t.Errorf("blackoutScheduleVisible(nil) = %v, %v", seen, err)
	}
}
