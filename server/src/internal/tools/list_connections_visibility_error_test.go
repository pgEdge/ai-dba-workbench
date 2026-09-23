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
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// failingVisibilityLister is a ConnectionVisibilityLister whose
// enumeration always fails, so that VisibleConnectionIDs returns an
// error to its caller.
type failingVisibilityLister struct{}

func (failingVisibilityLister) GetAllConnections(context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	return nil, errors.New("visibility lookup failed")
}

// TestListConnectionsFailsClosedOnVisibilityError checks that
// list_connections refuses when the caller's visible set cannot be
// resolved. It used to log the error and skip filtering altogether, so
// a lookup failure handed the caller every connection's inventory.
func TestListConnectionsFailsClosedOnVisibilityError(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	authStore, authCleanup := newRBACTestStore(t)
	defer authCleanup()

	if err := authStore.CreateUser("carol", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("carol")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO connections (name, host, port, database_name, is_shared, owner_username)
		 VALUES ('hidden-conn', 'localhost', 5432, 'postgres', FALSE, 'alice')`); err != nil {
		t.Fatalf("failed to seed connections row: %v", err)
	}

	tool := ListConnectionsTool(pool, auth.NewRBACChecker(authStore),
		failingVisibilityLister{})

	resp, err := tool.Handler(map[string]any{
		"__context": nonSuperuserContextInt(userID, "carol"),
	})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("Expected an error response, got: %+v", resp.Content)
	}
	for _, c := range resp.Content {
		if strings.Contains(c.Text, "hidden-conn") {
			t.Errorf("Response leaked connection name 'hidden-conn': %q", c.Text)
		}
	}
}

// seedListConnections inserts two connections: one owned by alice and
// not shared, and one owned by bob, monitored, whose last collection
// error is recorded.
func seedListConnections(t *testing.T, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO connections (name, host, port, database_name, is_shared,
		     is_monitored, owner_username, connection_error)
		 VALUES ('alice-conn', 'db-a.example.com', 5432, 'postgres', FALSE,
		     FALSE, 'alice', NULL),
		        ('bob-conn', 'db-b.example.com', 5433, 'appdb', FALSE,
		     TRUE, 'bob', 'connection refused')`); err != nil {
		t.Fatalf("failed to seed connections rows: %v", err)
	}
}

// TestListConnectionsListsVisibleConnections checks the success path:
// without a checker every connection is listed with its status, and
// with one the list is confined to the caller's visible connections.
func TestListConnectionsListsVisibleConnections(t *testing.T) {
	pool, ds, cleanup := newToolsTestPool(t)
	defer cleanup()
	seedListConnections(t, pool)

	resp, err := ListConnectionsTool(pool, nil, nil).Handler(map[string]any{})
	if err != nil || resp.IsError {
		t.Fatalf("Unfiltered listing failed: %v %+v", err, resp.Content)
	}
	body := resp.Content[0].Text
	for _, want := range []string{
		"Found 2 connections (1 monitored)",
		"alice-conn", "bob-conn", "appdb", "offline", "connection refused",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Unfiltered listing: expected %q in %q", want, body)
		}
	}

	authStore, authCleanup := newRBACTestStore(t)
	defer authCleanup()
	if err := authStore.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	tool := ListConnectionsTool(pool, auth.NewRBACChecker(authStore),
		database.NewVisibilityLister(ds))
	resp, err = tool.Handler(map[string]any{
		"__context": nonSuperuserContextInt(userID, "bob"),
	})
	if err != nil || resp.IsError {
		t.Fatalf("Filtered listing failed: %v %+v", err, resp.Content)
	}
	body = resp.Content[0].Text
	if !strings.Contains(body, "Found 1 connections (1 monitored)") ||
		!strings.Contains(body, "bob-conn") {
		t.Errorf("Filtered listing: expected bob's connection only, got %q", body)
	}
	if strings.Contains(body, "alice-conn") {
		t.Errorf("Filtered listing leaked alice's connection: %q", body)
	}
}

// TestListConnectionsReportsQueryFailure checks that a failing query is
// reported as a tool error rather than an empty list.
func TestListConnectionsReportsQueryFailure(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		"DROP SCHEMA metrics CASCADE"); err != nil {
		t.Fatalf("failed to drop the metrics schema: %v", err)
	}

	resp, err := ListConnectionsTool(pool, nil, nil).Handler(map[string]any{})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content[0].Text,
		"Failed to query connections") {
		t.Errorf("Expected a query failure, got %+v", resp.Content)
	}
}
