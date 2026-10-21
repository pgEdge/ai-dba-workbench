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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/memory"
	"github.com/pgedge/ai-workbench/server/internal/resources"
)

// seedMemory stores a memory for username in scope and returns its ID.
func seedMemory(t *testing.T, store *memory.Store, username, scope string) int64 {
	t.Helper()
	mem, err := store.Store(context.Background(), username, scope, "note",
		"content", false, nil, "")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	return mem.ID
}

// memoryExists reports whether the memory is still stored.
func memoryExists(t *testing.T, store *memory.Store, id int64) bool {
	t.Helper()
	_, err := store.GetByID(context.Background(), id)
	if errors.Is(err, memory.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	return true
}

// deleteArgs returns delete_memory arguments for id, called with ctx.
func deleteArgs(ctx context.Context, id int64) map[string]any {
	return map[string]any{"id": float64(id), "__context": ctx}
}

// TestDeleteMemoryToolScopes covers delete_memory for each kind of
// memory and caller (issue #704): a system memory, even the caller's
// own, needs store_system_memory and its refusal is recorded once as
// memory.delete on memories/<id>; another user's memory and a missing
// one answer alike, are left alone and record nothing.
func TestDeleteMemoryToolScopes(t *testing.T) {
	store, _, cleanup := newMemoryEmbeddingStore(t)
	defer cleanup()

	authStore := newExecuteTestAuthStore(t)
	for _, name := range []string{"creator", "keeper"} {
		if err := authStore.CreateUser(name, "Password-123!", "", "", ""); err != nil {
			t.Fatalf("CreateUser(%s): %v", name, err)
		}
	}
	if err := authStore.SetUserSuperuser("keeper", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	creatorID, err := authStore.GetUserID("creator")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	keeperID, err := authStore.GetUserID("keeper")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	capture := &toolDenialCapture{}
	tool := DeleteMemoryTool(store, auth.NewRBACChecker(authStore),
		capture.record)
	creator := userContext("creator", creatorID)
	keeper := context.WithValue(userContext("keeper", keeperID),
		auth.IsSuperuserContextKey, true)

	system := seedMemory(t, store, "creator", "system")
	others := seedMemory(t, store, "someone_else", "user")
	own := seedMemory(t, store, "creator", "user")

	resp, err := tool.Handler(deleteArgs(creator, system))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !resp.IsError || responseText(resp) != systemMemoryDenied {
		t.Fatalf("Expected the refusal %q, got %+v", systemMemoryDenied,
			resp)
	}
	want := toolDenial{"memory.delete", fmt.Sprintf("memories/%d", system),
		systemMemoryDenied}
	if len(capture.denials) != 1 || capture.denials[0] != want {
		t.Errorf("Expected %v recorded once, got %v", want, capture.denials)
	}
	if !memoryExists(t, store, system) {
		t.Error("Expected the system memory to remain")
	}

	capture.denials = nil
	for _, id := range []int64{others, 999999} {
		resp, err = tool.Handler(deleteArgs(creator, id))
		if err != nil {
			t.Fatalf("Handler: %v", err)
		}
		wantText := fmt.Sprintf(
			"Memory with ID %d was not found or does not belong to you", id)
		if !resp.IsError || responseText(resp) != wantText {
			t.Errorf("Expected %q, got %+v", wantText, resp)
		}
	}
	if len(capture.denials) != 0 {
		t.Errorf("Expected nothing recorded, got %v", capture.denials)
	}
	if !memoryExists(t, store, others) {
		t.Error("Expected another user's memory to remain")
	}

	resp, err = tool.Handler(deleteArgs(creator, own))
	if err != nil || resp.IsError {
		t.Fatalf("Expected the caller's own memory deleted, got %+v, %v",
			resp, err)
	}
	if memoryExists(t, store, own) {
		t.Error("Expected the caller's own memory gone")
	}

	resp, err = tool.Handler(deleteArgs(keeper, system))
	if err != nil || resp.IsError {
		t.Fatalf("Expected a permitted caller to delete the system memory, "+
			"got %+v, %v", resp, err)
	}
	if memoryExists(t, store, system) {
		t.Error("Expected the system memory gone")
	}
	if len(capture.denials) != 0 {
		t.Errorf("Expected nothing recorded, got %v", capture.denials)
	}
}

// TestDeleteMemoryToolStoreFailure checks that a failed lookup is
// reported as a failure, not as a missing memory.
func TestDeleteMemoryToolStoreFailure(t *testing.T) {
	tool := DeleteMemoryTool(memory.NewStore(unreachablePool(t)), nil, nil)
	resp, err := tool.Handler(deleteArgs(userContext("creator", 1), 5))
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !resp.IsError || responseText(resp) != "Failed to delete memory" {
		t.Errorf("Expected a failure, got %+v", resp)
	}
}

// TestProviderDeleteMemoryRefusalReachesAuditLog drives delete_memory
// through the provider, so a provider that does not pass its recorder
// fails it: a creator without store_system_memory cannot delete their
// own system memory, and the refusal is one denied memory.delete row on
// memories/<id>.
func TestProviderDeleteMemoryRefusalReachesAuditLog(t *testing.T) {
	_, pool, cleanup := newMemoryEmbeddingStore(t)
	defer cleanup()
	store := memory.NewStore(pool)

	authStore := newExecuteTestAuthStore(t)
	if _, err := authStore.RegisterMCPPrivilege("delete_memory",
		auth.MCPPrivilegeTypeTool, "Delete a memory", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	if err := authStore.CreateUser("creator", "Password-123!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("creator")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	clientManager := database.NewClientManager(nil)
	t.Cleanup(func() {
		if err := clientManager.CloseAll(); err != nil {
			t.Logf("CloseAll: %v", err)
		}
	})
	cfg := &config.Config{}
	provider := NewContextAwareProvider(clientManager,
		resources.NewContextAwareRegistry(clientManager, cfg, nil, nil),
		nil, cfg, authStore, nil, database.NewTestDatastore(pool))

	system := seedMemory(t, store, "creator", "system")
	ctx := context.WithValue(userContext("creator", userID),
		auth.TokenHashContextKey, "session-token-hash")
	resp, err := provider.Execute(ctx, "delete_memory",
		map[string]any{"id": float64(system)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !resp.IsError || !strings.Contains(responseText(resp),
		systemMemoryDenied) {
		t.Fatalf("Expected the refusal %q, got %+v", systemMemoryDenied,
			resp)
	}
	if !memoryExists(t, store, system) {
		t.Error("Expected the system memory to remain")
	}

	events, _, err := authStore.ListAuditEvents(auth.AuditFilter{
		Action: "memory.delete", Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 memory.delete denial, got %d", len(events))
	}
	if events[0].ActorName != "creator" ||
		events[0].Error != systemMemoryDenied {
		t.Errorf("Unexpected denial: actor %q, reason %q",
			events[0].ActorName, events[0].Error)
	}
	var details struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if want := fmt.Sprintf("memories/%d", system); details.Target != want {
		t.Errorf("Expected target %q, got %q", want, details.Target)
	}
}
