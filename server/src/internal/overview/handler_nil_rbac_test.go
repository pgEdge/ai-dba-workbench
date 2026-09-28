/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package overview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// Tests for issue #561: an overview handler with no RBAC checker must
// show nothing, not the whole estate. Every request carries a superuser
// context, which a real checker would admit, to prove that the nil
// checker rather than the caller is what denies.

// newNilRBACHandler returns a handler with a cached estate overview and
// no RBAC checker.
func newNilRBACHandler(ds *database.Datastore) (*Handler, *Hub) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	g.current = newTestOverview("Full estate overview - must NOT be served.")
	hub := NewHub()
	return NewHandlerWithRBAC(g, hub, nil, ds), hub
}

// assertEmptyStatus checks that rr is a 200 carrying the "empty" status.
func assertEmptyStatus(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp generatingResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "empty" {
		t.Errorf("expected status %q, got %q", "empty", resp.Status)
	}
}

func TestHandleOverview_NilRBAC_EstateWideIsEmpty(t *testing.T) {
	h, _ := newNilRBACHandler(nil)
	assertEmptyStatus(t, doRequest(t, h, http.MethodGet, "/api/v1/overview"))
}

func TestHandleOverview_NilRBAC_ConnectionIDsFiltered(t *testing.T) {
	h, _ := newNilRBACHandler(nil)
	assertEmptyStatus(t, doRequest(t, h, http.MethodGet,
		"/api/v1/overview?connection_ids=1,2"))
}

func TestHandleOverview_NilRBAC_ScopeNotFound(t *testing.T) {
	for _, scope := range []string{"server", "cluster", "group"} {
		t.Run(scope, func(t *testing.T) {
			h, _ := newNilRBACHandler(nil)
			rr := doRequest(t, h, http.MethodGet,
				"/api/v1/overview?scope_type="+scope+"&scope_id=1")
			if rr.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestHandleOverview_NilRBAC_ScopeMemberLookupFailure covers the
// membership lookup when a datastore is present: the lookup fails
// against an unreachable pool, and the handler answers 500 rather than
// falling back to showing the scope.
func TestHandleOverview_NilRBAC_ScopeMemberLookupFailure(t *testing.T) {
	pool, err := pgxpool.New(context.Background(),
		"postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()
	ds := database.NewTestDatastore(pool)

	for _, scope := range []string{"cluster", "group"} {
		t.Run(scope, func(t *testing.T) {
			h, _ := newNilRBACHandler(ds)
			rr := doRequest(t, h, http.MethodGet,
				"/api/v1/overview?scope_type="+scope+"&scope_id=1")
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("expected status 500, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestHandleSSE_NilRBAC_NotSubscribedToEstate checks that an estate-wide
// stream request from a caller who can see nothing is not subscribed to
// the estate feed, and so is not sent the cached estate overview.
func TestHandleSSE_NilRBAC_NotSubscribedToEstate(t *testing.T) {
	h, hub := newNilRBACHandler(nil)
	assertSSEScopeKey(t, hub, asSuperuser(h.handleSSE), "connections:")
}

// TestHandleSSE_RestrictedNoVisible_NotSubscribedToEstate is the same
// check for a real checker and a signed-in user with no grants: before
// issue #561 an empty visible set left the key empty, which is the
// estate-wide feed.
func TestHandleSSE_RestrictedNoVisible_NotSubscribedToEstate(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	g.current = newTestOverview("Full estate overview - must NOT be served.")
	hub := NewHub()
	h := newTestHandlerFor(t, g, hub)

	restricted := func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), auth.UserIDContextKey, int64(4242))
		ctx = context.WithValue(ctx, auth.UsernameContextKey, "restricted")
		h.handleSSE(w, r.WithContext(ctx))
	}
	assertSSEScopeKey(t, hub, restricted, "connections:")
}

// TestHandleSSE_Superuser_SubscribedToEstate checks that the estate-wide
// key is still used for a caller who can see everything.
func TestHandleSSE_Superuser_SubscribedToEstate(t *testing.T) {
	h := newTestHandler(t)
	assertSSEScopeKey(t, h.hub, asSuperuser(h.handleSSE), "")
}

// assertSSEScopeKey opens an estate-wide stream through serve and checks
// the scope key the single resulting subscriber was registered under.
func assertSSEScopeKey(t *testing.T, hub *Hub, serve http.HandlerFunc, want string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/overview/stream", serve)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/api/v1/overview/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// The response headers are flushed only after the scope is resolved
	// and immediately before Subscribe, so poll briefly for it.
	var keys []string
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		hub.mu.RLock()
		keys = keys[:0]
		for sub := range hub.subscribers {
			keys = append(keys, sub.scopeKey)
		}
		hub.mu.RUnlock()
		if len(keys) == 1 {
			break
		}
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", len(keys))
	}
	if keys[0] != want {
		t.Errorf("expected scope key %q, got %q", want, keys[0])
	}
}

// grantedUserContext creates a user whose group is granted connID and
// returns a request context for them.
func grantedUserContext(t *testing.T, store *auth.AuthStore, connID int) context.Context {
	t.Helper()
	if err := store.CreateUser("granted", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("granted")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	groupID, err := store.CreateGroup("granted_group", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, connID, auth.AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}
	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, userID)
	return context.WithValue(ctx, auth.UsernameContextKey, "granted")
}

// unreachableDatastore returns a datastore whose every query fails fast.
func unreachableDatastore(t *testing.T) *database.Datastore {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return database.NewTestDatastore(pool)
}

// TestHandleOverview_EstateWide_RestrictedServesVisibleSummary checks
// that an estate-wide request from a user granted one connection gets
// the summary for that connection alone, not the estate overview.
func TestHandleOverview_EstateWide_RestrictedServesVisibleSummary(t *testing.T) {
	store, cleanup := newRBACTestStore(t)
	defer cleanup()
	ctx := grantedUserContext(t, store, 7)

	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	g.current = newTestOverview("Full estate overview - must NOT be served.")
	g.scopedCache["connections:7"] = &scopedEntry{
		overview:   newTestOverview("Connection 7 overview."),
		lastAccess: time.Now().UTC(),
	}
	h := NewHandlerWithRBAC(g, NewHub(), auth.NewRBACChecker(store), nil)

	rr := doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp Overview
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Summary != "Connection 7 overview." {
		t.Errorf("expected the connection 7 summary, got %q", resp.Summary)
	}
}

// TestHandleOverview_EstateWide_RestrictedSummaryFailure checks that a
// failure to generate the restricted summary is a 500, not a fallback to
// the estate overview.
func TestHandleOverview_EstateWide_RestrictedSummaryFailure(t *testing.T) {
	store, cleanup := newRBACTestStore(t)
	defer cleanup()
	ctx := grantedUserContext(t, store, 7)

	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
		ctx:         context.Background(),
		datastore:   unreachableDatastore(t),
	}
	g.current = newTestOverview("Full estate overview - must NOT be served.")
	h := NewHandlerWithRBAC(g, NewHub(), auth.NewRBACChecker(store), nil)

	rr := doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview")
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestHandleOverview_EstateWide_VisibilityFailure checks that a failure
// to resolve the caller's visible connections is a 500.
func TestHandleOverview_EstateWide_VisibilityFailure(t *testing.T) {
	store, cleanup := newRBACTestStore(t)
	defer cleanup()
	ctx := grantedUserContext(t, store, 7)

	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	g.current = newTestOverview("Full estate overview - must NOT be served.")
	h := NewHandlerWithRBAC(g, NewHub(), auth.NewRBACChecker(store), unreachableDatastore(t))

	rr := doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview")
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", rr.Code, rr.Body.String())
	}
}
