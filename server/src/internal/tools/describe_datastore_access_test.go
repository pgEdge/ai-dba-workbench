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
	"slices"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/resources"
)

// newDatastoreAccessProvider returns a provider with its tools
// registered, backed by store, and with the two datastore tools
// registered as non-public privileges, as the server registers them.
func newDatastoreAccessProvider(t *testing.T, store *auth.AuthStore) *ContextAwareProvider {
	t.Helper()
	for _, name := range []string{"query_datastore", "describe_datastore"} {
		if _, err := store.RegisterMCPPrivilege(name, auth.MCPPrivilegeTypeTool, name, false); err != nil {
			t.Fatalf("RegisterMCPPrivilege(%s): %v", name, err)
		}
	}
	cfg := &config.Config{}
	clientManager := database.NewClientManager(nil)
	t.Cleanup(func() { _ = clientManager.CloseAll() })
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, store, nil)
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, store, nil, nil)
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	if err := provider.RegisterTools(ctx); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	return provider
}

// listedNames returns the names of the tools ctx may list.
func listedNames(p *ContextAwareProvider, ctx context.Context) []string {
	var names []string
	for _, tool := range p.ListForContext(ctx) {
		names = append(names, tool.Name)
	}
	return names
}

// refused reports whether resp is the provider's tool-gate refusal.
func refused(resp mcp.ToolResponse) bool {
	return resp.IsError && len(resp.Content) > 0 &&
		strings.Contains(resp.Content[0].Text, "do not have permission to use tool")
}

// TestDescribeDatastoreFollowsGroupGrant covers a group granted
// query_datastore by name before describe_datastore existed: its members
// may list and run describe_datastore too, and a group with neither
// grant may do neither.
func TestDescribeDatastoreFollowsGroupGrant(t *testing.T) {
	store := newExecuteTestAuthStore(t)
	provider := newDatastoreAccessProvider(t, store)
	actor := store.AsActor(auth.SystemActor())

	userCtx := func(username string, grant []string) context.Context {
		t.Helper()
		if err := store.CreateUser(username, "Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		userID, err := store.GetUserID(username)
		if err != nil {
			t.Fatalf("GetUserID: %v", err)
		}
		groupID, err := actor.CreateGroup(username+"_group", "test group")
		if err != nil {
			t.Fatalf("CreateGroup: %v", err)
		}
		if err := actor.AddUserToGroup(groupID, userID); err != nil {
			t.Fatalf("AddUserToGroup: %v", err)
		}
		for _, name := range grant {
			if err := actor.GrantMCPPrivilegeByName(groupID, name); err != nil {
				t.Fatalf("GrantMCPPrivilegeByName(%s): %v", name, err)
			}
		}
		return context.WithValue(tokenContext(), auth.UserIDContextKey, userID)
	}

	granted := userCtx("granted", []string{"query_datastore"})
	names := listedNames(provider, granted)
	if !slices.Contains(names, "query_datastore") || !slices.Contains(names, "describe_datastore") {
		t.Errorf("a query_datastore grant should list both datastore tools, got %v", names)
	}
	resp, err := provider.Execute(granted, "describe_datastore", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if refused(resp) {
		t.Errorf("a query_datastore grant should admit describe_datastore, got %+v", resp.Content)
	}

	ungranted := userCtx("ungranted", nil)
	names = listedNames(provider, ungranted)
	if slices.Contains(names, "describe_datastore") || slices.Contains(names, "query_datastore") {
		t.Errorf("no grant should list neither datastore tool, got %v", names)
	}
	resp, err = provider.Execute(ungranted, "describe_datastore", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !refused(resp) {
		t.Errorf("no grant should refuse describe_datastore, got %+v", resp.Content)
	}

	// The companion rule runs one way only: describe_datastore alone
	// does not admit query_datastore.
	describeOnly := userCtx("describe_only", []string{"describe_datastore"})
	resp, err = provider.Execute(describeOnly, "query_datastore", map[string]any{"source": "alerts"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !refused(resp) {
		t.Errorf("a describe_datastore grant should not admit query_datastore, got %+v", resp.Content)
	}
}

// TestDescribeDatastoreFollowsTokenScope covers a token whose MCP scope
// names only query_datastore: it may list and run describe_datastore.
func TestDescribeDatastoreFollowsTokenScope(t *testing.T) {
	store := newExecuteTestAuthStore(t)
	provider := newDatastoreAccessProvider(t, store)
	actor := store.AsActor(auth.SystemActor())

	if err := store.CreateUser("root", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	userID, err := store.GetUserID("root")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := actor.CreateToken("root", "scoped token", nil, true)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if err := actor.SetTokenMCPScopeByNames(token.ID, []string{"query_datastore"}, true); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames: %v", err)
	}

	ctx := context.WithValue(tokenContext(), auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.TokenIDContextKey, token.ID)

	names := listedNames(provider, ctx)
	if !slices.Contains(names, "describe_datastore") {
		t.Errorf("a query_datastore scope should list describe_datastore, got %v", names)
	}
	if slices.Contains(names, "list_probes") {
		t.Errorf("the scope should still narrow the listing, got %v", names)
	}
	resp, err := provider.Execute(ctx, "describe_datastore", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if refused(resp) {
		t.Errorf("a query_datastore scope should admit describe_datastore, got %+v", resp.Content)
	}
	resp, err = provider.Execute(ctx, "list_probes", map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !refused(resp) {
		t.Errorf("a tool outside the scope should still be refused, got %+v", resp.Content)
	}
}
