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
	"sort"
	"strconv"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// probeConfigVisibilityFixture seeds a probe config at every scope on top
// of the coverage fixture: the global default, and a server, cluster and
// group override on both the visible and the hidden hierarchy.
type probeConfigVisibilityFixture struct {
	*covFixture
	store   *auth.AuthStore
	serve   http.HandlerFunc
	global  int
	visible map[string]int // scope -> probe config id, visible hierarchy
	hidden  map[string]int // scope -> probe config id, hidden hierarchy
}

func newProbeConfigVisibilityFixture(t *testing.T) *probeConfigVisibilityFixture {
	t.Helper()
	f := newCovFixture(t)
	ctx := context.Background()
	pf := &probeConfigVisibilityFixture{
		covFixture: f,
		store:      newTestAuthStore(t),
		visible:    map[string]int{},
		hidden:     map[string]int{},
	}
	if err := f.pool.QueryRow(ctx,
		`SELECT id FROM probe_configs WHERE name = $1 AND scope = 'global'`,
		f.probeName).Scan(&pf.global); err != nil {
		t.Fatalf("lookup global probe: %v", err)
	}
	seed := func(scope, column string, scopeID int) int {
		t.Helper()
		var id int
		if err := f.pool.QueryRow(ctx,
			`INSERT INTO probe_configs (name, description, scope, `+column+`)
             VALUES ($1, 'override', $2, $3) RETURNING id`,
			f.probeName, scope, scopeID).Scan(&id); err != nil {
			t.Fatalf("seed %s probe config: %v", scope, err)
		}
		return id
	}
	pf.visible["server"] = seed("server", "connection_id", f.connID)
	pf.visible["cluster"] = seed("cluster", "cluster_id", f.clusterID)
	pf.visible["group"] = seed("group", "group_id", f.groupID)
	pf.hidden["server"] = seed("server", "connection_id", f.hiddenConnID)
	pf.hidden["cluster"] = seed("cluster", "cluster_id", f.hiddenCluster)
	pf.hidden["group"] = seed("group", "group_id", f.hiddenGroupID)

	mux := http.NewServeMux()
	NewProbeConfigHandler(f.ds, pf.store, auth.NewRBACChecker(pf.store)).
		RegisterRoutes(mux, covIdentityWrapper)
	pf.serve = mux.ServeHTTP
	return pf
}

// user creates a non-superuser session caller. When grants is non-empty
// the user joins a group holding a read grant on each listed connection.
func (pf *probeConfigVisibilityFixture) user(t *testing.T, name string, grants ...int) (int64, func(*http.Request) *http.Request) {
	t.Helper()
	if err := pf.store.CreateUser(name, "Password1234", "", "", ""); err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	userID, err := pf.store.GetUserID(name)
	if err != nil {
		t.Fatalf("get user id %s: %v", name, err)
	}
	if len(grants) > 0 {
		groupID, err := pf.store.CreateGroup(name+"_group", "")
		if err != nil {
			t.Fatalf("create group: %v", err)
		}
		if err := pf.store.AddUserToGroup(groupID, userID); err != nil {
			t.Fatalf("add user to group: %v", err)
		}
		for _, connID := range grants {
			if err := pf.store.GrantConnectionPrivilege(groupID, connID,
				auth.AccessLevelRead); err != nil {
				t.Fatalf("grant connection %d: %v", connID, err)
			}
		}
	}
	return userID, func(r *http.Request) *http.Request {
		return withUsername(withUser(r, userID), name)
	}
}

// token creates an API token for owner scoped to the given connections.
func (pf *probeConfigVisibilityFixture) token(t *testing.T, owner string, scope ...int) int64 {
	t.Helper()
	_, tok, err := pf.store.AsActor(auth.SystemActor()).CreateToken(owner, "probe config scope", nil, true)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	conns := make([]auth.ScopedConnection, 0, len(scope))
	for _, id := range scope {
		conns = append(conns, auth.ScopedConnection{ConnectionID: id, AccessLevel: auth.AccessLevelRead})
	}
	if err := pf.store.AsActor(auth.SystemActor()).SetTokenConnectionScope(tok.ID, conns, true); err != nil {
		t.Fatalf("set token scope: %v", err)
	}
	return tok.ID
}

func probeConfigPath(id int) string {
	return "/api/v1/probe-configs/" + strconv.Itoa(id)
}

func probeConfigListPath(connID int) string {
	return "/api/v1/probe-configs?connection_id=" + strconv.Itoa(connID)
}

// decodeProbeConfigIDs returns the sorted IDs of a list response.
func decodeProbeConfigIDs(t *testing.T, body []byte) []int {
	t.Helper()
	var configs []database.ProbeConfig
	if err := json.Unmarshal(body, &configs); err != nil {
		t.Fatalf("decode list: %v; body=%s", err, body)
	}
	ids := make([]int, 0, len(configs))
	for i := range configs {
		ids = append(ids, configs[i].ID)
	}
	sort.Ints(ids)
	return ids
}

// TestProbeConfigVisibility_Issue647 checks that the probe-config read
// routes apply the connection visibility rule: a config tied to a
// server, cluster or group the caller cannot see answers 404, exactly as
// a missing one does, and a scoped token sees only its connections.
// Global defaults stay visible to every caller.
func TestProbeConfigVisibility_Issue647(t *testing.T) {
	pf := newProbeConfigVisibilityFixture(t)
	const missingConn = 999999
	const missingConfig = 999999

	_, noGrants := pf.user(t, "no_grants")
	_, owner := pf.user(t, covOwner)
	_, readGrant := pf.user(t, "read_grant", pf.hiddenConnID)
	scopedUserID, _ := pf.user(t, "scoped_user", pf.connID, pf.hiddenConnID)
	scopedTokenID := pf.token(t, "scoped_user", pf.connID)
	scopedToken := func(r *http.Request) *http.Request {
		return withUsername(withToken(r, scopedUserID, scopedTokenID), "scoped_user")
	}
	superTokenID := pf.token(t, "scoped_user", pf.hiddenConnID)
	superToken := func(r *http.Request) *http.Request {
		return withSuperuserToken(r, superTokenID)
	}

	type expect struct {
		visible map[string]int // scope -> status for the visible hierarchy
		hidden  map[string]int // scope -> status for the hidden hierarchy
	}
	all := func(code int) map[string]int {
		return map[string]int{"server": code, "cluster": code, "group": code}
	}
	callers := []struct {
		name     string
		decorate func(*http.Request) *http.Request
		want     expect
	}{
		{"no grants", noGrants, expect{all(http.StatusNotFound), all(http.StatusNotFound)}},
		{"owner", owner, expect{all(http.StatusOK), all(http.StatusNotFound)}},
		// The hidden cluster and group contain the granted connection.
		{"read grant", readGrant, expect{all(http.StatusNotFound), all(http.StatusOK)}},
		// The user holds grants on both connections, but the token
		// names only the visible one.
		{"scoped token", scopedToken, expect{all(http.StatusOK), all(http.StatusNotFound)}},
		{"superuser", withSuperuser, expect{all(http.StatusOK), all(http.StatusOK)}},
		{"superuser token", superToken, expect{all(http.StatusNotFound), all(http.StatusOK)}},
	}

	for _, c := range callers {
		t.Run(c.name, func(t *testing.T) {
			// Global defaults are visible to everyone, by list and by id,
			// and the unfiltered list holds nothing but the global row.
			rec := covDo(pf.serve, http.MethodGet, "/api/v1/probe-configs", "", c.decorate)
			covExpect(t, rec, http.StatusOK)
			if got := decodeProbeConfigIDs(t, rec.Body.Bytes()); len(got) != 1 || got[0] != pf.global {
				t.Errorf("unfiltered list = %v, want [%d]", got, pf.global)
			}
			covExpect(t, covDo(pf.serve, http.MethodGet, probeConfigPath(pf.global), "", c.decorate), http.StatusOK)

			for scope, code := range c.want.visible {
				covExpect(t, covDo(pf.serve, http.MethodGet, probeConfigPath(pf.visible[scope]), "", c.decorate), code)
			}
			for scope, code := range c.want.hidden {
				covExpect(t, covDo(pf.serve, http.MethodGet, probeConfigPath(pf.hidden[scope]), "", c.decorate), code)
			}

			rec = covDo(pf.serve, http.MethodGet, probeConfigListPath(pf.connID), "", c.decorate)
			covExpect(t, rec, c.want.visible["server"])
			if rec.Code == http.StatusOK {
				if got := decodeProbeConfigIDs(t, rec.Body.Bytes()); len(got) != 1 || got[0] != pf.visible["server"] {
					t.Errorf("visible list = %v, want [%d]", got, pf.visible["server"])
				}
			}
			rec = covDo(pf.serve, http.MethodGet, probeConfigListPath(pf.hiddenConnID), "", c.decorate)
			covExpect(t, rec, c.want.hidden["server"])
			if rec.Code == http.StatusOK {
				if got := decodeProbeConfigIDs(t, rec.Body.Bytes()); len(got) != 1 || got[0] != pf.hidden["server"] {
					t.Errorf("hidden list = %v, want [%d]", got, pf.hidden["server"])
				}
			}

			// A missing config is 404 for every caller.
			covExpect(t, covDo(pf.serve, http.MethodGet, probeConfigPath(missingConfig), "", c.decorate), http.StatusNotFound)
		})
	}

	// A hidden connection or config must be indistinguishable from a
	// missing one: same status and same body.
	t.Run("hidden answers as missing", func(t *testing.T) {
		pairs := []struct {
			name            string
			hidden, missing string
		}{
			{"list by connection", probeConfigListPath(pf.hiddenConnID), probeConfigListPath(missingConn)},
			{"server config", probeConfigPath(pf.hidden["server"]), probeConfigPath(missingConfig)},
			{"cluster config", probeConfigPath(pf.hidden["cluster"]), probeConfigPath(missingConfig)},
			{"group config", probeConfigPath(pf.hidden["group"]), probeConfigPath(missingConfig)},
		}
		for _, p := range pairs {
			for _, caller := range []struct {
				name     string
				decorate func(*http.Request) *http.Request
			}{{"no grants", noGrants}, {"scoped token", scopedToken}} {
				h := covDo(pf.serve, http.MethodGet, p.hidden, "", caller.decorate)
				m := covDo(pf.serve, http.MethodGet, p.missing, "", caller.decorate)
				if h.Code != http.StatusNotFound || h.Code != m.Code || h.Body.String() != m.Body.String() {
					t.Errorf("%s as %s: hidden %d %q, missing %d %q", p.name, caller.name,
						h.Code, h.Body.String(), m.Code, m.Body.String())
				}
			}
		}
	})

	// An unrestricted superuser session has nothing hidden from it, so a
	// missing connection lists as empty, as the probe override list does.
	t.Run("superuser missing connection", func(t *testing.T) {
		rec := covDo(pf.serve, http.MethodGet, probeConfigListPath(missingConn), "", withSuperuser)
		covExpect(t, rec, http.StatusOK)
		if got := decodeProbeConfigIDs(t, rec.Body.Bytes()); len(got) != 0 {
			t.Errorf("missing connection list = %v, want empty", got)
		}
	})
}

// TestProbeConfigVisibility_Issue647_Errors checks that a visibility
// lookup that fails answers 500 rather than falling through to the data.
func TestProbeConfigVisibility_Issue647_Errors(t *testing.T) {
	store := newTestAuthStore(t)
	h := NewProbeConfigHandler(newBrokenDatastore(t), store, auth.NewRBACChecker(store))
	restricted := func(r *http.Request) *http.Request { return withUsername(withUser(r, 4242), "nobody") }
	covExpect(t, covDo(h.handleProbeConfigs, http.MethodGet, probeConfigListPath(1), "", restricted),
		http.StatusInternalServerError)
}

// TestProbeConfigVisibilityTarget covers each scope the helper maps.
func TestProbeConfigVisibilityTarget(t *testing.T) {
	id := 7
	cases := []struct {
		name       string
		config     database.ProbeConfig
		wantScope  string
		wantScoped bool
	}{
		{"global", database.ProbeConfig{}, "", false},
		{"server", database.ProbeConfig{ConnectionID: &id}, "server", true},
		{"cluster", database.ProbeConfig{ClusterID: &id}, "cluster", true},
		{"group", database.ProbeConfig{GroupID: &id}, "group", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, scopeID, scoped := probeConfigVisibilityTarget(&tc.config)
			if scope != tc.wantScope || scoped != tc.wantScoped {
				t.Fatalf("got (%q, %v), want (%q, %v)", scope, scoped, tc.wantScope, tc.wantScoped)
			}
			if scoped && scopeID != id {
				t.Fatalf("scopeID = %d, want %d", scopeID, id)
			}
		})
	}
}
