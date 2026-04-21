/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package mcp

import (
	"testing"
)

func TestNewToolError(t *testing.T) {
	resp, err := NewToolError("test error message")
	if err != nil {
		t.Fatalf("NewToolError returned unexpected error: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected IsError to be true")
	}

	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(resp.Content))
	}

	if resp.Content[0].Type != "text" {
		t.Errorf("Expected content type 'text', got %q", resp.Content[0].Type)
	}

	if resp.Content[0].Text != "test error message" {
		t.Errorf("Expected text 'test error message', got %q", resp.Content[0].Text)
	}
}

func TestNewToolSuccess(t *testing.T) {
	resp, err := NewToolSuccess("success message")
	if err != nil {
		t.Fatalf("NewToolSuccess returned unexpected error: %v", err)
	}

	if resp.IsError {
		t.Error("Expected IsError to be false")
	}

	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(resp.Content))
	}

	if resp.Content[0].Type != "text" {
		t.Errorf("Expected content type 'text', got %q", resp.Content[0].Type)
	}

	if resp.Content[0].Text != "success message" {
		t.Errorf("Expected text 'success message', got %q", resp.Content[0].Text)
	}
}

func TestNewResourceError(t *testing.T) {
	content, err := NewResourceError("pg://test", "resource error")
	if err != nil {
		t.Fatalf("NewResourceError returned unexpected error: %v", err)
	}

	if content.URI != "pg://test" {
		t.Errorf("Expected URI 'pg://test', got %q", content.URI)
	}

	if len(content.Contents) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(content.Contents))
	}

	if content.Contents[0].Type != "text" {
		t.Errorf("Expected content type 'text', got %q", content.Contents[0].Type)
	}

	if content.Contents[0].Text != "resource error" {
		t.Errorf("Expected text 'resource error', got %q", content.Contents[0].Text)
	}
}

func TestNewResourceSuccess(t *testing.T) {
	content, err := NewResourceSuccess("pg://schema", "application/json", `{"table": "users"}`)
	if err != nil {
		t.Fatalf("NewResourceSuccess returned unexpected error: %v", err)
	}

	if content.URI != "pg://schema" {
		t.Errorf("Expected URI 'pg://schema', got %q", content.URI)
	}

	if content.MimeType != "application/json" {
		t.Errorf("Expected MimeType 'application/json', got %q", content.MimeType)
	}

	if len(content.Contents) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(content.Contents))
	}

	if content.Contents[0].Text != `{"table": "users"}` {
		t.Errorf("Expected JSON content, got %q", content.Contents[0].Text)
	}
}

func TestDatabaseNotReadyErrors(t *testing.T) {
	// Test that error constants are defined and non-empty
	if DatabaseNotReadyError == "" {
		t.Error("DatabaseNotReadyError should not be empty")
	}

	if DatabaseNotReadyErrorShort == "" {
		t.Error("DatabaseNotReadyErrorShort should not be empty")
	}

	// Short version should be shorter than full version
	if len(DatabaseNotReadyErrorShort) >= len(DatabaseNotReadyError) {
		t.Error("DatabaseNotReadyErrorShort should be shorter than DatabaseNotReadyError")
	}
}
