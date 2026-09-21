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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"golang.org/x/crypto/bcrypt"
)

// TestListForContextHonoursMCPScope covers issue #482 on the listing
// path: ListForContext used to return every tool for a superuser,
// which was decided from the token's admin scope and so said nothing
// about tools. A superuser's token narrowed to one tool now lists that
// tool, plus the RBAC-exempt tools, which stay visible by design.
func TestListForContextHonoursMCPScope(t *testing.T) {
	tmpDir := t.TempDir()
	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := authStore.CreateUser("root", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := authStore.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	userID, err := authStore.GetUserID("root")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	_, token, err := authStore.CreateToken("root", "scoped token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, authStore, nil)
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg,
		authStore, nil, nil)

	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	if err := provider.RegisterTools(ctx); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	all := provider.List()
	if len(all) < 2 {
		t.Skipf("Need at least two tools to test filtering, got %d", len(all))
	}

	// Pick a tool that is not exempt from RBAC, so that the scope is
	// what decides whether it appears.
	wanted := ""
	for _, tool := range all {
		if !rbacExemptTools[tool.Name] {
			wanted = tool.Name
			break
		}
	}
	if wanted == "" {
		t.Skip("Every registered tool is RBAC-exempt")
	}

	if _, err := authStore.RegisterMCPPrivilege(wanted, auth.MCPPrivilegeTypeTool,
		"scoped tool", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	if err := authStore.SetTokenMCPScopeByNames(token.ID,
		[]string{wanted}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames: %v", err)
	}

	tokenCtx := context.WithValue(ctx, auth.UserIDContextKey, userID)
	tokenCtx = context.WithValue(tokenCtx, auth.IsAPITokenContextKey, true)
	tokenCtx = context.WithValue(tokenCtx, auth.TokenIDContextKey, token.ID)

	listed := provider.ListForContext(tokenCtx)
	if len(listed) >= len(all) {
		t.Fatalf("Expected the listing to be narrowed, got %d of %d tools",
			len(listed), len(all))
	}

	seen := make(map[string]bool, len(listed))
	for _, tool := range listed {
		seen[tool.Name] = true
	}
	if !seen[wanted] {
		t.Errorf("Expected the scoped tool %q to be listed, got %v", wanted, seen)
	}
	for name := range seen {
		if name != wanted && !rbacExemptTools[name] {
			t.Errorf("Tool %q is neither in scope nor RBAC-exempt", name)
		}
	}

	// A session caller still sees everything.
	if got := len(provider.ListForContext(ctx)); got != len(all) {
		t.Errorf("Expected a session to see all %d tools, got %d", len(all), got)
	}
}
