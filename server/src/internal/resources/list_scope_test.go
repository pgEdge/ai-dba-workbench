/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package resources

import (
	"context"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	conf "github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// TestListForContextHonoursMCPScope covers issue #482 on the listing
// path: ListForContext used to return every resource for a superuser,
// which was decided from the token's admin scope and so said nothing
// about resources. A superuser's token narrowed to one resource now
// lists only that one, so the inventory is not disclosed and the
// listing matches what a read will actually allow.
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

	registry := NewContextAwareRegistry(clientManager, &conf.Config{}, authStore, nil)

	all := registry.List()
	if len(all) < 2 {
		t.Skipf("Need at least two resources to test filtering, got %d", len(all))
	}
	wanted := all[0].URI

	if _, err := authStore.RegisterMCPPrivilege(wanted,
		auth.MCPPrivilegeTypeResource, "scoped resource", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	if err := authStore.SetTokenMCPScopeByNames(token.ID,
		[]string{wanted}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames: %v", err)
	}

	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.TokenIDContextKey, token.ID)

	listed := registry.ListForContext(ctx)
	if len(listed) != 1 || listed[0].URI != wanted {
		t.Fatalf("Expected only %q to be listed, got %d resources: %+v", wanted,
			len(listed), listed)
	}

	// A session caller still sees everything.
	sessionCtx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)
	if got := len(registry.ListForContext(sessionCtx)); got != len(all) {
		t.Errorf("Expected a session to see all %d resources, got %d", len(all),
			got)
	}
}
