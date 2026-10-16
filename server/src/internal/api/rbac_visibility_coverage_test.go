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
	"reflect"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// fakeVisibilityDatastore is a canned rbacVisibilityDatastore.
type fakeVisibilityDatastore struct {
	clusters map[int][]int
	groups   map[int][]int
	err      error
}

func (f *fakeVisibilityDatastore) GetConnectionIDsForCluster(_ context.Context, id int) ([]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.clusters[id], nil
}

func (f *fakeVisibilityDatastore) GetConnectionIDsForGroup(_ context.Context, id int) ([]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.groups[id], nil
}

func TestVisibilityFns_WithFakeDatastore(t *testing.T) {
	ctx := context.Background()
	ds := &fakeVisibilityDatastore{
		clusters: map[int][]int{1: {10, 11}, 2: {20}},
		groups:   map[int][]int{100: {10}, 200: {20}, 300: nil},
	}
	visible := map[int]bool{10: true}

	if ok, err := clusterHasVisibleConnectionFn(ctx, ds, 1, visible); err != nil || !ok {
		t.Fatalf("cluster 1: ok=%v err=%v, want visible", ok, err)
	}
	if ok, err := clusterHasVisibleConnectionFn(ctx, ds, 2, visible); err != nil || ok {
		t.Fatalf("cluster 2: ok=%v err=%v, want hidden", ok, err)
	}
	if ok, err := groupHasVisibleConnectionFn(ctx, ds, 100, visible); err != nil || !ok {
		t.Fatalf("group 100: ok=%v err=%v, want visible", ok, err)
	}

	membership, err := clusterConnectionMembershipFn(ctx, ds, []int{1, 2, 3})
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	want := map[int][]int{1: {10, 11}, 2: {20}, 3: nil}
	if !reflect.DeepEqual(membership, want) {
		t.Fatalf("membership = %v, want %v", membership, want)
	}

	groups := []database.ClusterGroup{{ID: 100}, {ID: 200}, {ID: 300}}
	out, err := filterGroupsByVisibilityFn(ctx, ds, groups, visible)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(out) != 1 || out[0].ID != 100 {
		t.Fatalf("filter = %v, want only group 100", out)
	}
}

func TestVisibilityFns_PropagateErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	ds := &fakeVisibilityDatastore{err: boom}

	if _, err := clusterHasVisibleConnectionFn(ctx, ds, 1, nil); !errors.Is(err, boom) {
		t.Fatalf("cluster err = %v", err)
	}
	if _, err := groupHasVisibleConnectionFn(ctx, ds, 1, nil); !errors.Is(err, boom) {
		t.Fatalf("group err = %v", err)
	}
	if _, err := clusterConnectionMembershipFn(ctx, ds, []int{1}); !errors.Is(err, boom) {
		t.Fatalf("membership err = %v", err)
	}
	if _, err := filterGroupsByVisibilityFn(ctx, ds, []database.ClusterGroup{{ID: 1}}, nil); !errors.Is(err, boom) {
		t.Fatalf("filter err = %v", err)
	}
}

// covScopeCheck runs scopeVisibleToCaller for a request decorated by
// decorate and returns the verdict and the recorder.
func covScopeCheck(checker *auth.RBACChecker, ds *database.Datastore, scope string, id int,
	decorate func(*http.Request) *http.Request) (bool, *httptest.ResponseRecorder) {
	req := decorate(httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()
	ok := scopeVisibleToCaller(req.Context(), rec, checker, ds, scope, id, "not found")
	return ok, rec
}

func TestResolveVisibleConnectionSet_Restricted(t *testing.T) {
	f := newCovFixture(t)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	restricted := covRestrictedUser(t, store)
	req := restricted(httptest.NewRequest(http.MethodGet, "/", nil))

	visible, all, err := resolveVisibleConnectionSet(req.Context(), checker, f.ds)
	if err != nil || all {
		t.Fatalf("all=%v err=%v, want restricted set", all, err)
	}
	if !visible[f.connID] || visible[f.hiddenConnID] {
		t.Fatalf("visible = %v, want only %d", visible, f.connID)
	}

	if _, _, err := resolveVisibleConnectionSet(req.Context(), checker, newBrokenDatastore(t)); err == nil {
		t.Fatal("expected an error from a broken datastore")
	}
}

func TestScopeVisibleToCaller_Restricted(t *testing.T) {
	f := newCovFixture(t)
	store := newTestAuthStore(t)
	checker := auth.NewRBACChecker(store)
	restricted := covRestrictedUser(t, store)

	cases := []struct {
		scope string
		id    int
		want  bool
	}{
		{"server", f.connID, true},
		{"server", f.hiddenConnID, false},
		{"cluster", f.clusterID, true},
		{"cluster", f.hiddenCluster, false},
		{"group", f.groupID, true},
		{"group", f.hiddenGroupID, false},
	}
	for _, tc := range cases {
		ok, rec := covScopeCheck(checker, f.ds, tc.scope, tc.id, restricted)
		if ok != tc.want {
			t.Fatalf("%s %d: ok=%v, want %v", tc.scope, tc.id, ok, tc.want)
		}
		if !ok && rec.Code != http.StatusNotFound {
			t.Fatalf("%s %d: status %d, want 404", tc.scope, tc.id, rec.Code)
		}
	}

	// Superusers short-circuit before any membership lookup.
	if ok, _ := covScopeCheck(checker, f.ds, "group", f.hiddenGroupID, withSuperuser); !ok {
		t.Fatal("superuser should see every scope")
	}

	// Failing to resolve the visible set is a 500.
	if ok, rec := covScopeCheck(checker, newBrokenDatastore(t), "server", 1, restricted); ok ||
		rec.Code != http.StatusInternalServerError {
		t.Fatalf("broken resolve: ok=%v status=%d", ok, rec.Code)
	}

	// The visible set resolves but the group membership lookup fails.
	if _, err := f.pool.Exec(context.Background(), "DROP TABLE clusters CASCADE"); err != nil {
		t.Fatalf("drop clusters: %v", err)
	}
	if ok, rec := covScopeCheck(checker, f.ds, "group", f.groupID, restricted); ok ||
		rec.Code != http.StatusInternalServerError {
		t.Fatalf("broken membership: ok=%v status=%d", ok, rec.Code)
	}
}
