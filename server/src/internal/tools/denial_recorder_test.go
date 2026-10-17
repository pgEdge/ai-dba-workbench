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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/resources"
)

// toolDenial is one refusal a DenialRecorder was asked to record.
type toolDenial struct {
	action, target, reason string
}

// toolDenialCapture collects the refusals passed to its record method.
type toolDenialCapture struct {
	denials []toolDenial
}

func (c *toolDenialCapture) record(_ context.Context, action, target,
	reason string) {

	c.denials = append(c.denials, toolDenial{action, target, reason})
}

// systemMemoryArgs returns store_memory arguments for a system-scoped
// memory, called with ctx.
func systemMemoryArgs(ctx context.Context) map[string]any {
	return map[string]any{
		"content":   "A system-wide memory.",
		"category":  "fact",
		"scope":     "system",
		"__context": ctx,
	}
}

// userContext returns a context naming username, as the auth wrapper
// leaves it for a session.
func userContext(username string, userID int64) context.Context {
	ctx := context.WithValue(context.Background(),
		auth.UsernameContextKey, username)
	return context.WithValue(ctx, auth.UserIDContextKey, userID)
}

// TestStoreMemorySystemScopeRefusalIsRecorded checks that a refused
// system-scoped store is passed to the recorder as memory.create on
// memories, with the reason the caller is given, and that a user-scoped
// store records nothing.
func TestStoreMemorySystemScopeRefusalIsRecorded(t *testing.T) {
	capture := &toolDenialCapture{}
	tool := StoreMemoryTool(nil, &config.Config{}, nil, capture.record)

	response, err := tool.Handler(systemMemoryArgs(userContext("plain", 0)))
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError || len(response.Content) == 0 ||
		response.Content[0].Text != systemMemoryDenied {
		t.Fatalf("Expected the refusal %q, got %+v", systemMemoryDenied,
			response)
	}
	want := []toolDenial{{"memory.create", "memories", systemMemoryDenied}}
	if len(capture.denials) != 1 || capture.denials[0] != want[0] {
		t.Errorf("Expected %v recorded, got %v", want, capture.denials)
	}

	capture.denials = nil
	args := systemMemoryArgs(userContext("plain", 0))
	args["scope"] = "user"
	if _, err := tool.Handler(args); err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if len(capture.denials) != 0 {
		t.Errorf("Expected nothing recorded for a user memory, got %v",
			capture.denials)
	}
}

// TestStoreMemoryRefusalReachesAuditLog drives the tool with a real
// RBAC checker and the auth store's recorder, and checks that the
// refusal of a user without store_system_memory is written to the audit
// log as a denied memory.create attributed to that user, while a
// superuser passes the gate without a record.
func TestStoreMemoryRefusalReachesAuditLog(t *testing.T) {
	store := newExecuteTestAuthStore(t)
	for _, name := range []string{"plain_user", "super_user"} {
		if err := store.CreateUser(name, "Password-123!", "", "", ""); err != nil {
			t.Fatalf("CreateUser(%s): %v", name, err)
		}
	}
	if err := store.SetUserSuperuser("super_user", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	plainID, err := store.GetUserID("plain_user")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	tool := StoreMemoryTool(nil, &config.Config{},
		auth.NewRBACChecker(store), NewDenialRecorder(store))

	response, err := tool.Handler(systemMemoryArgs(
		userContext("plain_user", plainID)))
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError || response.Content[0].Text != systemMemoryDenied {
		t.Fatalf("Expected the refusal %q, got %+v", systemMemoryDenied,
			response)
	}

	// The superuser passes the gate and stops at the missing memory
	// store, leaving no denial behind.
	superID, err := store.GetUserID("super_user")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	superCtx := context.WithValue(userContext("super_user", superID),
		auth.IsSuperuserContextKey, true)
	response, err = tool.Handler(systemMemoryArgs(superCtx))
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError || response.Content[0].Text == systemMemoryDenied {
		t.Fatalf("Expected the superuser past the gate, got %+v", response)
	}

	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: "memory.create", Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 memory.create denial, got %d", len(events))
	}
	if events[0].Error != systemMemoryDenied {
		t.Errorf("Expected reason %q, got %q", systemMemoryDenied,
			events[0].Error)
	}
	if events[0].ActorName != "plain_user" {
		t.Errorf("Expected the denial attributed to plain_user, got %q",
			events[0].ActorName)
	}
	var details struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if details.Target != "memories" {
		t.Errorf("Expected target memories, got %q", details.Target)
	}
}

// TestNewDenialRecorder checks that no store yields no recorder, that a
// nil recorder still refuses, and that a failure to record is logged
// rather than changing the refusal.
func TestNewDenialRecorder(t *testing.T) {
	if NewDenialRecorder(nil) != nil {
		t.Error("Expected no recorder without an auth store")
	}

	var none DenialRecorder
	response, err := none.refuse(context.Background(), "memory.create",
		"memories", systemMemoryDenied)
	if err != nil || !response.IsError ||
		response.Content[0].Text != systemMemoryDenied {
		t.Errorf("Expected a nil recorder to refuse, got %+v, %v",
			response, err)
	}

	store, err := auth.NewAuthStore(t.TempDir(), 0, 0,
		auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	record := NewDenialRecorder(store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	response, err = record.refuse(userContext("plain", 0), "memory.create",
		"memories", systemMemoryDenied)
	if err != nil || !response.IsError {
		t.Errorf("Expected a refusal despite the closed store, got %+v, %v",
			response, err)
	}
}

// TestProviderWiresStoreMemoryDenialRecorder checks that the provider
// builds store_memory with the auth store's recorder, so that a session
// user without store_system_memory who asks it for a system-scoped
// memory through Execute leaves one denied memory.create row on
// memories. The datastore has no pool, which is enough: it makes the
// provider register the memory tools, and the refusal returns before
// the memory store is used.
func TestProviderWiresStoreMemoryDenialRecorder(t *testing.T) {
	store := newExecuteTestAuthStore(t)
	// A public tool privilege lets the call past the tool-level gate,
	// so that the refusal comes from the store_system_memory check.
	if _, err := store.RegisterMCPPrivilege("store_memory",
		auth.MCPPrivilegeTypeTool, "Store a memory", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	if err := store.CreateUser("plain_user", "Password-123!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("plain_user")
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
		nil, cfg, store, nil, database.NewTestDatastore(nil))

	ctx := context.WithValue(userContext("plain_user", userID),
		auth.TokenHashContextKey, "session-token-hash")
	response, err := provider.Execute(ctx, "store_memory", map[string]any{
		"content":  "A system-wide memory.",
		"category": "fact",
		"scope":    "system",
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !response.IsError || len(response.Content) == 0 ||
		response.Content[0].Text != systemMemoryDenied {
		t.Fatalf("Expected the refusal %q, got %+v", systemMemoryDenied,
			response)
	}

	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: "memory.create", Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 memory.create denial, got %d", len(events))
	}
	if events[0].ActorName != "plain_user" {
		t.Errorf("Expected the denial attributed to plain_user, got %q",
			events[0].ActorName)
	}
	var details struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if details.Target != "memories" {
		t.Errorf("Expected target memories, got %q", details.Target)
	}
}
