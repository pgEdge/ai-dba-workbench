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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TestQueryDatabase_RecordsWriteRefusal runs query_database against the
// test database as a caller with a read grant on the connection, and
// checks that a write is refused with the unchanged message and recorded
// as connection.query.write on the connection with the fixed write
// reason, whilst a read records nothing (issue #706).
func TestQueryDatabase_RecordsWriteRefusal(t *testing.T) {
	env := newResolverTestEnv(t)

	store, closeStore := newRBACTestStore(t)
	defer closeStore()
	if err := store.CreateUser("reader", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	readerID, err := store.GetUserID("reader")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	groupID, err := store.CreateGroup("readers", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, readerID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, env.connID,
		auth.AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}

	tool := QueryDatabaseTool(nil, NewConnectionResolver(env.clients,
		database.NewTestDatastoreWithSecret(env.pool, resolverTestSecret),
		auth.NewRBACChecker(store), NewDenialRecorder(store)))
	ctx := nonSuperuserContextInt(readerID, "reader")

	resp, err := tool.Handler(map[string]any{
		"__context":     ctx,
		"connection_id": float64(env.connID),
		"query":         "SELECT 1",
	})
	if err != nil || resp.IsError {
		t.Fatalf("plain SELECT refused: %v %s", err, responseText(resp))
	}
	if events := deniedRows(t, store, deniedActionConnectionQueryWrite); len(events) != 0 {
		t.Fatalf("Expected no denial for a read, got %d", len(events))
	}

	resp, err = tool.Handler(map[string]any{
		"__context":     ctx,
		"connection_id": float64(env.connID),
		"query":         "DELETE FROM ai_wb_resolver_fixture.connections",
	})
	if err != nil {
		t.Fatalf("Handler returned a Go error: %v", err)
	}
	if !resp.IsError || responseText(resp) !=
		"Write operations require read_write access to this connection" {
		t.Fatalf("Expected the read_write refusal, got %+v", resp)
	}

	events := deniedRows(t, store, deniedActionConnectionQueryWrite)
	if len(events) != 1 {
		t.Fatalf("Expected 1 connection.query.write denial, got %d",
			len(events))
	}
	if events[0].ActorName != "reader" ||
		events[0].Error != connectionWriteDenied {
		t.Errorf("Unexpected denial %+v", events[0])
	}
	if got := denialTarget(t, events[0]); got != connectionDenialTarget(env.connID) {
		t.Errorf("Expected target %q, got %q",
			connectionDenialTarget(env.connID), got)
	}
}
