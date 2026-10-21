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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Cluster and group override writes need every member visible (issue #716)
//
// An override at cluster or group scope applies to every member
// connection, so a restricted caller who sees only some members may not
// write one. The refusal is the handler's admin-permission refusal,
// audited, so that it says nothing about the hidden members.
// =============================================================================

func TestAllMembersVisible(t *testing.T) {
	visible := map[int]bool{1: true, 2: true}
	cases := []struct {
		name    string
		members []int
		want    bool
	}{
		{"no members", nil, true},
		{"all visible", []int{1, 2}, true},
		{"one hidden", []int{1, 3}, false},
		{"all hidden", []int{3, 4}, false},
	}
	for _, tc := range cases {
		if got := allMembersVisible(tc.members, visible); got != tc.want {
			t.Errorf("%s: allMembersVisible(%v) = %v, want %v", tc.name, tc.members, got, tc.want)
		}
	}
}

func TestCheckScopeMembersVisible(t *testing.T) {
	ds := &fakeVisibilityDatastore{
		clusters: map[int][]int{1: {10, 11}, 2: {10, 20}, 3: nil},
		groups:   map[int][]int{100: {10, 11}, 200: {10, 20}},
	}
	visible := map[int]bool{10: true, 11: true}
	refusal := adminPermissionRefusal(auth.PermManageProbes)

	cases := []struct {
		name  string
		ds    rbacVisibilityDatastore
		scope string
		id    int
		want  int // 0 means the check passes
	}{
		{"cluster all visible", ds, "cluster", 1, 0},
		{"cluster one hidden", ds, "cluster", 2, http.StatusForbidden},
		{"cluster empty", ds, "cluster", 3, 0},
		{"group all visible", ds, "group", 100, 0},
		{"group one hidden", ds, "group", 200, http.StatusForbidden},
		{"cluster lookup fails", &fakeVisibilityDatastore{err: errors.New("boom")}, "cluster", 1,
			http.StatusInternalServerError},
		{"group lookup fails", &fakeVisibilityDatastore{err: errors.New("boom")}, "group", 1,
			http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var audited []string
			record := denialRecorder(func(_ *http.Request, reason string) {
				audited = append(audited, reason)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/", nil)
			ok := checkScopeMembersVisible(rec, req, tc.ds, record, auth.PermManageProbes,
				tc.scope, tc.id, visible)

			if tc.want == 0 {
				if !ok {
					t.Fatalf("expected the check to pass; status=%d body=%s", rec.Code, rec.Body.String())
				}
				if len(audited) != 0 {
					t.Errorf("a passing check audited %v", audited)
				}
				return
			}
			if ok {
				t.Fatal("expected the check to refuse")
			}
			expectStatus(t, rec, tc.want)
			if tc.want == http.StatusForbidden {
				assertError(t, rec, http.StatusForbidden, refusal)
				if len(audited) != 1 || audited[0] != refusal {
					t.Errorf("audited %v, want [%q]", audited, refusal)
				}
			} else if len(audited) != 0 {
				t.Errorf("a failed lookup audited %v", audited)
			}
		})
	}
}

func TestRequireScopeMembersVisible_Shortcuts(t *testing.T) {
	// A server scope needs no lookup at all, so nil dependencies are
	// never touched.
	rec := httptest.NewRecorder()
	req := withUser(httptest.NewRequest(http.MethodPut, "/", nil), 1)
	if !requireScopeMembersVisible(rec, req, nil, nil, nil, auth.PermManageProbes, "server", 1) {
		t.Fatalf("server scope refused: %d %s", rec.Code, rec.Body.String())
	}

	// A superuser sees every connection and passes without a member
	// lookup, even on a datastore whose queries all fail.
	broken := newBrokenDatastore(t)
	rec = httptest.NewRecorder()
	req = withSuperuser(httptest.NewRequest(http.MethodPut, "/", nil))
	if !requireScopeMembersVisible(rec, req, newTestRBACChecker(t), broken, nil,
		auth.PermManageProbes, "cluster", 1) {
		t.Fatalf("superuser refused: %d %s", rec.Code, rec.Body.String())
	}

	// A restricted caller whose visible set cannot be resolved gets a
	// 500, not a pass.
	store := newTestAuthStore(t)
	restricted := covRestrictedUser(t, store, auth.PermManageProbes)
	rec = httptest.NewRecorder()
	req = restricted(httptest.NewRequest(http.MethodPut, "/", nil))
	if requireScopeMembersVisible(rec, req, auth.NewRBACChecker(store), broken, nil,
		auth.PermManageProbes, "group", 1) {
		t.Fatal("expected a failed visibility lookup to refuse")
	}
	expectStatus(t, rec, http.StatusInternalServerError)
}

// mixedScopes are the clusters and groups addMixedScopes seeds. "full"
// scopes have every member visible to covOwner, "mixed" ones a visible
// and a hidden member, and the split group a visible cluster and a
// hidden one, so that only the group as a whole is mixed.
type mixedScopes struct {
	fullGroup, fullCluster   int
	mixedGroup, mixedCluster int
	splitGroup               int
	splitVisible             int
	splitHidden              int
	ownedFull                int // a covOwner connection in fullCluster
}

// addMixedScopes seeds the mixed hierarchy into a covFixture. Visible
// members are owned by covOwner or shared; hidden ones belong to another
// user and are not shared.
func addMixedScopes(t *testing.T, f *covFixture) mixedScopes {
	t.Helper()
	ctx := context.Background()
	scan := func(dst *int, q string, args ...any) {
		t.Helper()
		if err := f.pool.QueryRow(ctx, q, args...).Scan(dst); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	group := func(dst *int, name string) {
		scan(dst, `INSERT INTO cluster_groups (name) VALUES ($1) RETURNING id`, name)
	}
	cluster := func(dst *int, groupID int, name string) {
		scan(dst, `INSERT INTO clusters (group_id, name) VALUES ($1, $2) RETURNING id`, groupID, name)
	}
	conn := func(name, owner string, shared bool, clusterID int) int {
		var id int
		scan(&id, `INSERT INTO connections (name, owner_username, is_shared, cluster_id)
            VALUES ($1, $2, $3, $4) RETURNING id`, name, owner, shared, clusterID)
		return id
	}

	var m mixedScopes
	group(&m.fullGroup, "full group")
	cluster(&m.fullCluster, m.fullGroup, "full cluster")
	m.ownedFull = conn("full owned", covOwner, false, m.fullCluster)
	conn("full shared", "someone_else", true, m.fullCluster)

	group(&m.mixedGroup, "mixed group")
	cluster(&m.mixedCluster, m.mixedGroup, "mixed cluster")
	conn("mixed owned", covOwner, false, m.mixedCluster)
	conn("mixed hidden", "someone_else", false, m.mixedCluster)

	group(&m.splitGroup, "split group")
	cluster(&m.splitVisible, m.splitGroup, "split visible cluster")
	cluster(&m.splitHidden, m.splitGroup, "split hidden cluster")
	conn("split owned", covOwner, false, m.splitVisible)
	conn("split hidden", "someone_else", false, m.splitHidden)
	return m
}

// overrideUnderTest is one of the three override handlers, built over a
// fixture, with what a write to it needs.
type overrideUnderTest struct {
	name       string
	prefix     string
	permission string
	okBody     string
	item       func(f *covFixture) string
	build      func(f *covFixture, store *auth.AuthStore, checker *auth.RBACChecker) (http.HandlerFunc, func(func(*http.Request, string)))
}

var overridesUnderTest = []overrideUnderTest{
	{
		name: "probe", prefix: "/api/v1/probe-overrides/", permission: auth.PermManageProbes,
		okBody: `{"is_enabled": true, "collection_interval_seconds": 30, "retention_days": 14}`,
		item:   func(f *covFixture) string { return f.probeName },
		build: func(f *covFixture, store *auth.AuthStore, checker *auth.RBACChecker) (http.HandlerFunc, func(func(*http.Request, string))) {
			h := NewProbeOverrideHandler(f.ds, store, checker)
			return h.handleProbeOverrides, h.SetDenialRecorder
		},
	},
	{
		name: "alert", prefix: "/api/v1/alert-overrides/", permission: auth.PermManageAlertRules,
		okBody: `{"operator": ">=", "threshold": 50, "severity": "critical", "enabled": true}`,
		item:   func(f *covFixture) string { return strconv.FormatInt(f.ruleID, 10) },
		build: func(f *covFixture, store *auth.AuthStore, checker *auth.RBACChecker) (http.HandlerFunc, func(func(*http.Request, string))) {
			h := NewAlertOverrideHandler(f.ds, store, checker)
			return h.handleAlertOverrides, h.SetDenialRecorder
		},
	},
	{
		name: "channel", prefix: "/api/v1/channel-overrides/", permission: auth.PermManageNotificationChannels,
		okBody: `{"enabled": false}`,
		item:   func(f *covFixture) string { return strconv.FormatInt(f.channelID, 10) },
		build: func(f *covFixture, store *auth.AuthStore, checker *auth.RBACChecker) (http.HandlerFunc, func(func(*http.Request, string))) {
			h := NewChannelOverrideHandler(f.ds, store, checker)
			return h.handleChannelOverrides, h.SetDenialRecorder
		},
	},
}

// covTokenFor mints an API token for the covOwner user, optionally with
// a connection scope, and returns a request decorator acting as it.
func covTokenFor(t *testing.T, store *auth.AuthStore, userID int64, scope []auth.ScopedConnection) func(*http.Request) *http.Request {
	t.Helper()
	_, token, err := store.AsActor(auth.SystemActor()).CreateToken(covOwner, "issue 716", nil, true)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if scope != nil {
		if err := store.AsActor(auth.SystemActor()).SetTokenConnectionScope(token.ID, scope, true); err != nil {
			t.Fatalf("SetTokenConnectionScope: %v", err)
		}
	}
	return func(r *http.Request) *http.Request {
		return withUsername(withToken(r, userID, token.ID), covOwner)
	}
}

// TestOverrideWritesNeedEveryMemberVisible drives PUT and DELETE on
// every override type at every mixed and full scope, for each kind of
// caller.
func TestOverrideWritesNeedEveryMemberVisible(t *testing.T) {
	for _, o := range overridesUnderTest {
		t.Run(o.name, func(t *testing.T) {
			f := newCovFixture(t)
			m := addMixedScopes(t, f)
			store := newTestAuthStore(t)
			checker := auth.NewRBACChecker(store)
			serve, setRecorder := o.build(f, store, checker)
			var audited []string
			setRecorder(func(_ *http.Request, reason string) { audited = append(audited, reason) })

			userID := setupUserWithPermissions(t, store, covOwner, []string{o.permission})
			session := func(r *http.Request) *http.Request {
				return withUsername(withUser(r, userID), covOwner)
			}
			unscopedToken := covTokenFor(t, store, userID, nil)
			wildcardToken := covTokenFor(t, store, userID, []auth.ScopedConnection{
				{ConnectionID: auth.ConnectionIDAll, AccessLevel: auth.AccessLevelReadWrite},
			})
			narrowedToken := covTokenFor(t, store, userID, []auth.ScopedConnection{
				{ConnectionID: m.ownedFull, AccessLevel: auth.AccessLevelReadWrite},
			})
			noPerm := func(r *http.Request) *http.Request { return withUser(r, 424242) }

			refusal := adminPermissionRefusal(o.permission)
			url := func(scope string, id int) string {
				return o.prefix + scope + "/" + strconv.Itoa(id) + "/" + o.item(f)
			}
			list := func(scope string, id int) string {
				return covDo(serve, http.MethodGet, o.prefix+scope+"/"+strconv.Itoa(id), "", withSuperuser).Body.String()
			}

			// The refusal must be byte for byte what a caller without the
			// permission gets, so that it discloses nothing more.
			noPermBody := covDo(serve, http.MethodPut, url("cluster", m.fullCluster), o.okBody, noPerm)
			covExpect(t, noPermBody, http.StatusForbidden)

			restrictedCallers := []struct {
				name string
				wrap func(*http.Request) *http.Request
			}{
				{"session", session}, {"unscoped token", unscopedToken}, {"wildcard-scope token", wildcardToken},
			}
			cases := []struct {
				scope string
				id    int
				want  int
			}{
				{"cluster", m.fullCluster, http.StatusOK},
				{"group", m.fullGroup, http.StatusOK},
				{"cluster", m.splitVisible, http.StatusOK},
				{"cluster", m.mixedCluster, http.StatusForbidden},
				{"group", m.mixedGroup, http.StatusForbidden},
				{"group", m.splitGroup, http.StatusForbidden},
				{"cluster", m.splitHidden, http.StatusNotFound},
			}
			for _, caller := range restrictedCallers {
				for _, tc := range cases {
					name := caller.name + " " + tc.scope + " " + strconv.Itoa(tc.id)
					before := list(tc.scope, tc.id)
					for _, w := range []struct{ method, body string }{
						{http.MethodPut, o.okBody},
						{http.MethodPut, "{"},
						{http.MethodDelete, ""},
					} {
						audited = nil
						rec := covDo(serve, w.method, url(tc.scope, tc.id), w.body, caller.wrap)
						want := tc.want
						if want == http.StatusOK && w.body == "{" {
							want = http.StatusBadRequest
						}
						if rec.Code != want {
							t.Fatalf("%s %s: status %d, want %d; body=%s", name, w.method, rec.Code, want, rec.Body.String())
						}
						if want != http.StatusForbidden {
							continue
						}
						// Refused before the body is read, with the
						// permission refusal, audited once.
						if rec.Body.String() != noPermBody.Body.String() {
							t.Errorf("%s %s: refusal %q, want %q", name, w.method, rec.Body.String(), noPermBody.Body.String())
						}
						if len(audited) != 1 || audited[0] != refusal {
							t.Errorf("%s %s: audited %v, want [%q]", name, w.method, audited, refusal)
						}
					}
					if tc.want != http.StatusOK {
						if after := list(tc.scope, tc.id); after != before {
							t.Errorf("%s: overrides changed from %s to %s", name, before, after)
						}
					}
				}
			}

			// A token narrowed to one member keeps the token-scope
			// refusal it already had, even where every member is visible
			// to its owner.
			rec := covDo(serve, http.MethodPut, url("cluster", m.fullCluster), o.okBody, narrowedToken)
			assertOutOfTokenScope(t, rec)
			// Its scope names no member of the mixed cluster, so that
			// cluster is hidden from it and answers as missing.
			rec = covDo(serve, http.MethodPut, url("cluster", m.mixedCluster), o.okBody, narrowedToken)
			covExpect(t, rec, http.StatusNotFound)

			// Unrestricted callers are unaffected: a superuser and a user
			// whose group grant covers every connection.
			wideID := setupUserWithPermissions(t, store, "wide_user", []string{o.permission})
			groups, err := store.GetUserGroups(wideID)
			if err != nil || len(groups) == 0 {
				t.Fatalf("GetUserGroups: %v %v", groups, err)
			}
			if err := store.GrantConnectionPrivilege(groups[0], auth.ConnectionIDAll, auth.AccessLevelReadWrite); err != nil {
				t.Fatalf("GrantConnectionPrivilege: %v", err)
			}
			wide := func(r *http.Request) *http.Request {
				return withUsername(withUser(r, wideID), "wide_user")
			}
			for _, caller := range []func(*http.Request) *http.Request{withSuperuser, wide} {
				for _, tc := range cases {
					covExpect(t, covDo(serve, http.MethodPut, url(tc.scope, tc.id), o.okBody, caller), http.StatusOK)
					covExpect(t, covDo(serve, http.MethodDelete, url(tc.scope, tc.id), "", caller), http.StatusOK)
				}
			}

			// Reads are unchanged: a mixed scope stays listable.
			covExpect(t, covDo(serve, http.MethodGet, o.prefix+"cluster/"+strconv.Itoa(m.mixedCluster), "", session),
				http.StatusOK)
		})
	}
}

// TestUpdateProbeConfigNeedsEveryMemberVisible covers PUT
// /probe-configs/{id}, which writes the same cluster and group probe
// overrides by row ID.
func TestUpdateProbeConfigNeedsEveryMemberVisible(t *testing.T) {
	f := newCovFixture(t)
	m := addMixedScopes(t, f)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	h := NewProbeConfigHandler(f.ds, store, checker)
	var audited []string
	h.SetDenialRecorder(func(_ *http.Request, reason string) { audited = append(audited, reason) })
	restricted := covRestrictedUser(t, store, auth.PermManageProbes)

	seed := func(scope, column string, id int) int64 {
		t.Helper()
		var cfgID int64
		if err := f.pool.QueryRow(context.Background(),
			`INSERT INTO probe_configs (name, description, scope, `+column+`) VALUES ($1, 'd', $2, $3) RETURNING id`,
			f.probeName, scope, id).Scan(&cfgID); err != nil {
			t.Fatalf("seed probe config: %v", err)
		}
		return cfgID
	}
	var globalID int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id FROM probe_configs WHERE scope = 'global'`).Scan(&globalID); err != nil {
		t.Fatalf("global probe config: %v", err)
	}
	refusal := adminPermissionRefusal(auth.PermManageProbes)

	cases := []struct {
		name string
		id   int64
		want int
	}{
		{"full cluster", seed("cluster", "cluster_id", m.fullCluster), http.StatusOK},
		{"full group", seed("group", "group_id", m.fullGroup), http.StatusOK},
		{"server", seed("server", "connection_id", m.ownedFull), http.StatusOK},
		{"global", globalID, http.StatusOK},
		{"mixed cluster", seed("cluster", "cluster_id", m.mixedCluster), http.StatusForbidden},
		{"mixed group", seed("group", "group_id", m.mixedGroup), http.StatusForbidden},
		{"split group", seed("group", "group_id", m.splitGroup), http.StatusForbidden},
		{"hidden cluster", seed("cluster", "cluster_id", m.splitHidden), http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			audited = nil
			rec := httptest.NewRecorder()
			h.updateProbeConfig(rec, restricted(probeConfigPut(`{"is_enabled": false}`)), tc.id)
			expectStatus(t, rec, tc.want)
			if tc.want == http.StatusForbidden {
				assertError(t, rec, http.StatusForbidden, refusal)
				if len(audited) != 1 || audited[0] != refusal {
					t.Errorf("audited %v, want [%q]", audited, refusal)
				}
				cfg, err := f.ds.GetProbeConfig(context.Background(), tc.id)
				if err != nil {
					t.Fatalf("GetProbeConfig: %v", err)
				}
				if !cfg.IsEnabled {
					t.Error("a refused update changed the probe config")
				}
			}

			// A superuser is unaffected.
			rec = httptest.NewRecorder()
			h.updateProbeConfig(rec, withSuperuser(probeConfigPut(`{"is_enabled": true}`)), tc.id)
			expectStatus(t, rec, http.StatusOK)
		})
	}
}

// TestRequireProbeConfigMembersVisible_Failures pins the gate's own
// lookups, which updateProbeConfig's earlier gates normally answer
// first.
func TestRequireProbeConfigMembersVisible_Failures(t *testing.T) {
	store := newTestAuthStore(t)
	restricted := covRestrictedUser(t, store, auth.PermManageProbes)
	checker := auth.NewRBACChecker(store)

	broken := NewProbeConfigHandler(newBrokenDatastore(t), store, checker)
	rec := httptest.NewRecorder()
	if broken.requireProbeConfigMembersVisible(rec, restricted(httptest.NewRequest(http.MethodPut, "/", nil)), 1) {
		t.Fatal("expected a failed visibility lookup to refuse")
	}
	expectStatus(t, rec, http.StatusInternalServerError)

	f := newCovFixture(t)
	h := NewProbeConfigHandler(f.ds, store, checker)
	rec = httptest.NewRecorder()
	if h.requireProbeConfigMembersVisible(rec, restricted(httptest.NewRequest(http.MethodPut, "/", nil)), 999999) {
		t.Fatal("expected a missing config to refuse")
	}
	expectStatus(t, rec, http.StatusNotFound)
}

// probeConfigPut builds an unauthenticated PUT carrying a JSON body.
func probeConfigPut(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
