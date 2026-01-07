/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package tools

import (
    "strings"
    "testing"
)

func TestListConnectionsTool_Definition(t *testing.T) {
    tool := ListConnectionsTool(nil)

    if tool.Definition.Name != "list_connections" {
        t.Errorf("Expected tool name 'list_connections', got '%s'", tool.Definition.Name)
    }

    if !strings.Contains(tool.Definition.Description, "database connections") {
        t.Error("Tool description should mention database connections")
    }

    if !strings.Contains(tool.Definition.Description, "DATASTORE") {
        t.Error("Tool description should mention DATASTORE context")
    }

    // Should have no required parameters
    if len(tool.Definition.InputSchema.Required) != 0 {
        t.Errorf("Expected 0 required parameters, got %d", len(tool.Definition.InputSchema.Required))
    }
}

func TestListConnectionsTool_NoDatastore(t *testing.T) {
    tool := ListConnectionsTool(nil)

    // Execute with no datastore - should return error
    response, err := tool.Handler(map[string]interface{}{})

    if err != nil {
        t.Errorf("Handler should not return error, got: %v", err)
    }

    if !response.IsError {
        t.Error("Response should be an error when datastore is not configured")
    }

    if len(response.Content) == 0 || !strings.Contains(response.Content[0].Text, "Datastore not configured") {
        t.Errorf("Expected 'Datastore not configured' error, got: %v", response.Content)
    }
}
