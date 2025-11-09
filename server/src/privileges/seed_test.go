/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package privileges

import (
    "context"
    "testing"
)

func TestGetDefaultMCPPrivileges(t *testing.T) {
    privileges := GetDefaultMCPPrivileges()

    // Should have 32 MCP tools registered
    expectedCount := 32
    if len(privileges) != expectedCount {
        t.Errorf("Expected %d privileges, got %d", expectedCount, len(privileges))
    }

    // Verify all required fields are populated
    for i, priv := range privileges {
        if priv.Identifier == "" {
            t.Errorf("Privilege %d has empty identifier", i)
        }
        if priv.ItemType == "" {
            t.Errorf("Privilege %d (%s) has empty item_type", i, priv.Identifier)
        }
        if priv.ItemType != "tool" && priv.ItemType != "resource" && priv.ItemType != "prompt" {
            t.Errorf("Privilege %d (%s) has invalid item_type: %s", i, priv.Identifier, priv.ItemType)
        }
        if priv.Description == "" {
            t.Errorf("Privilege %d (%s) has empty description", i, priv.Identifier)
        }
    }

    // Verify some known tools are present
    expectedTools := []string{
        "authenticate_user",
        "create_user",
        "create_user_group",
        "grant_connection_privilege",
        "set_token_connection_scope",
    }

    found := make(map[string]bool)
    for _, priv := range privileges {
        found[priv.Identifier] = true
    }

    for _, tool := range expectedTools {
        if !found[tool] {
            t.Errorf("Expected tool %s not found in privileges", tool)
        }
    }
}

func TestSeedMCPPrivileges_Idempotent(t *testing.T) {
    pool := skipIfNoDatabase(t)

    ctx := context.Background()

    // Seed privileges first time
    err := SeedMCPPrivileges(ctx, pool)
    if err != nil {
        t.Fatalf("First seed failed: %v", err)
    }

    // Seed again - should be idempotent
    err = SeedMCPPrivileges(ctx, pool)
    if err != nil {
        t.Fatalf("Second seed failed (not idempotent): %v", err)
    }

    // Verify the count
    var count int
    err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_privilege_identifiers").Scan(&count)
    if err != nil {
        t.Fatalf("Failed to query privilege count: %v", err)
    }

    expectedCount := 32
    if count != expectedCount {
        t.Errorf("Expected %d privileges in database, got %d", expectedCount, count)
    }
}
