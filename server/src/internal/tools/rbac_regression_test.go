/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// stubVisibilityLister is a static implementation of
// auth.ConnectionVisibilityLister used to drive RBAC-denial scenarios
// without a live datastore. The sharing and ownership metadata provided
// here is consulted by auth.RBACChecker.VisibleConnectionIDs exactly once.
type stubVisibilityLister struct {
	connections []auth.ConnectionVisibilityInfo
	err         error
}

func (s *stubVisibilityLister) GetAllConnections(_ context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.connections, nil
}

// newRBACRegressionTestStore spins up a throwaway SQLite auth store.
// Tests can use it to build non-superuser contexts. The caller receives a
// cleanup closure that closes the store and removes the temp directory.
func newRBACRegressionTestStore(t *testing.T) (*auth.AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "tools-rbac-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewAuthStore: %v", err)
	}
	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

// dummyPool returns a pgxpool.Pool pointed at an unreachable address.
// The pool is non-nil (so the tool's "Datastore not configured" guard
// does not fire) but any attempt to execute SQL against it will fail.
// This lets RBAC-denial tests prove that the tool short-circuits before
// issuing any query.
func dummyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// A syntactically valid DSN is enough; pgxpool.New is lazy and does
	// not dial the target until the first query is issued.
	pool, err := pgxpool.New(context.Background(), "postgres://nobody:nopass@127.0.0.1:1/nope")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	return pool
}

// nonSuperuserContext builds a context that matches how the auth
// middleware would populate it for a logged-in, non-superuser session.
func nonSuperuserContext(userID int64, username string) context.Context {
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, username)
	return ctx
}

// TestGetAlertHistoryTool_RBAC_NoAccessDeniesEmptyResult is the regression
// guard for issue #35 on the MCP tool side.
//
// A non-superuser user with zero group-granted connections, who is not the
// owner of the target connection and for whom the target is not shared,
// must NOT see any alerts from that connection when invoking
// get_alert_history without an explicit connection_id. Previously a
// helper collapsed the "full access" and "no grants" cases into a single
// nil return value, and the tool treated that nil as "all connections",
// producing a SQL WHERE clause of TRUE that leaked every alert in the
// datastore.
func TestGetAlertHistoryTool_RBAC_NoAccessDeniesEmptyResult(t *testing.T) {
	store, cleanup := newRBACRegressionTestStore(t)
	defer cleanup()

	// bob has no group membership and no grants.
	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	// Lister advertises an unshared connection owned by alice, NOT bob.
	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: 42, IsShared: false, OwnerUsername: "alice"},
		},
	}

	pool := dummyPool(t)
	defer pool.Close()

	rbac := auth.NewRBACChecker(store)
	tool := GetAlertHistoryTool(pool, rbac, lister)

	ctx := nonSuperuserContext(userID, "bob")
	resp, err := tool.Handler(map[string]any{"__context": ctx})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("Handler returned error response: %+v", resp.Content)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "No alerts found") ||
		!strings.Contains(body, "You do not have access to any connections") {
		t.Errorf("Expected RBAC denial message, got: %q", body)
	}
	// Must NOT leak alice's connection id or name anywhere in the body.
	if strings.Contains(body, "42") {
		t.Errorf("Response leaked connection ID 42: %q", body)
	}
}

// TestGetMetricBaselinesTool_RBAC_NoAccessDeniesEmptyResult mirrors
// TestGetAlertHistoryTool_RBAC_NoAccessDeniesEmptyResult for
// get_metric_baselines. The same leak existed in that tool.
func TestGetMetricBaselinesTool_RBAC_NoAccessDeniesEmptyResult(t *testing.T) {
	store, cleanup := newRBACRegressionTestStore(t)
	defer cleanup()

	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: 42, IsShared: false, OwnerUsername: "alice"},
		},
	}

	pool := dummyPool(t)
	defer pool.Close()

	rbac := auth.NewRBACChecker(store)
	tool := GetMetricBaselinesTool(pool, rbac, lister)

	ctx := nonSuperuserContext(userID, "bob")
	resp, err := tool.Handler(map[string]any{"__context": ctx})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("Handler returned error response: %+v", resp.Content)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "No metric baselines found") ||
		!strings.Contains(body, "You do not have access to any connections") {
		t.Errorf("Expected RBAC denial message, got: %q", body)
	}
	if strings.Contains(body, "42") {
		t.Errorf("Response leaked connection ID 42: %q", body)
	}
}

// TestQueryMetricsTool_RBAC_DeniesUnsharedConnection is the regression
// guard for the query_metrics leak flagged in the #35 follow-up audit.
//
// A non-owner non-superuser calling query_metrics with an explicit
// connection_id for an unshared connection must receive a generic
// "connection not found or not accessible" error. Previously the tool
// executed the existence probe and, on miss, echoed up to 20 valid
// connection IDs/names in the error body. Both behaviors leaked
// visibility information.
func TestQueryMetricsTool_RBAC_DeniesUnsharedConnection(t *testing.T) {
	store, cleanup := newRBACRegressionTestStore(t)
	defer cleanup()

	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	pool := dummyPool(t)
	defer pool.Close()

	// Sharing lookup returns a connection owned by alice that is not
	// shared; bob must therefore be denied.
	rbac := auth.NewRBACChecker(store)
	rbac.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		if connectionID == 42 {
			return false, "alice", nil
		}
		return false, "", nil
	})
	tool := QueryMetricsTool(pool, rbac)

	ctx := nonSuperuserContext(userID, "bob")
	resp, err := tool.Handler(map[string]any{
		"__context":     ctx,
		"probe_name":    "pg_stat_database",
		"connection_id": float64(42),
	})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("Expected error response, got success: %+v", resp.Content)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "connection not found or not accessible") {
		t.Errorf("Expected generic RBAC denial message, got: %q", body)
	}
	// Must not leak connection IDs/names or any other enumeration hint.
	if strings.Contains(body, "alice") || strings.Contains(body, "Valid connection IDs") {
		t.Errorf("Response leaked enumeration data: %q", body)
	}
}

// TestGetAlertRulesTool_RBAC_DeniesUnsharedConnection is the regression
// guard for the get_alert_rules leak flagged in the #35 follow-up audit.
// A non-owner non-superuser supplying connection_id for an unshared
// connection must receive the generic denial message without any
// threshold data being returned.
func TestGetAlertRulesTool_RBAC_DeniesUnsharedConnection(t *testing.T) {
	store, cleanup := newRBACRegressionTestStore(t)
	defer cleanup()

	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	pool := dummyPool(t)
	defer pool.Close()

	rbac := auth.NewRBACChecker(store)
	rbac.SetConnectionSharingLookup(func(_ context.Context, connectionID int) (bool, string, error) {
		if connectionID == 42 {
			return false, "alice", nil
		}
		return false, "", nil
	})
	tool := GetAlertRulesTool(pool, rbac)

	ctx := nonSuperuserContext(userID, "bob")
	resp, err := tool.Handler(map[string]any{
		"__context":     ctx,
		"connection_id": float64(42),
	})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("Expected error response, got success: %+v", resp.Content)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "connection not found or not accessible") {
		t.Errorf("Expected generic RBAC denial message, got: %q", body)
	}
	// Must not leak enumeration hints: the unshared connection's owner
	// name, its ID, any adjacent connection identifiers the tool
	// previously echoed back, or the "Valid connection IDs" list header.
	leakers := []string{
		"alice",                // unshared connection's owner
		"42",                   // unshared connection's ID
		"Valid connection IDs", // legacy enumeration header
	}
	for _, s := range leakers {
		if strings.Contains(body, s) {
			t.Errorf("Response leaked enumeration data %q: %q", s, body)
		}
	}
}

// TestGetBlackoutsTool_RBAC_NoAccessDeniesEmptyResult mirrors the above
// tests for get_blackouts. The tool is not currently registered in
// cmd/mcp-server/privileges.go, but the code path must still honor RBAC
// so enabling the tool in a later change does not reintroduce the leak.
func TestGetBlackoutsTool_RBAC_NoAccessDeniesEmptyResult(t *testing.T) {
	store, cleanup := newRBACRegressionTestStore(t)
	defer cleanup()

	if err := store.CreateUser("bob", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: 42, IsShared: false, OwnerUsername: "alice"},
		},
	}

	pool := dummyPool(t)
	defer pool.Close()

	rbac := auth.NewRBACChecker(store)
	tool := GetBlackoutsTool(pool, rbac, lister)

	ctx := nonSuperuserContext(userID, "bob")
	resp, err := tool.Handler(map[string]any{"__context": ctx})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("Handler returned error response: %+v", resp.Content)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}
	body := resp.Content[0].Text
	if !strings.Contains(body, "No blackouts found") ||
		!strings.Contains(body, "You do not have access to any connections") {
		t.Errorf("Expected RBAC denial message, got: %q", body)
	}
	if strings.Contains(body, "42") {
		t.Errorf("Response leaked connection ID 42: %q", body)
	}
}
