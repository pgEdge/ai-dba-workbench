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

// assertSingleTextContent verifies that contents contains exactly one
// "text" item whose payload equals expectedText. It reports failures
// against the calling test via t so assertion errors surface at the
// correct line.
func assertSingleTextContent(t *testing.T, contents []ContentItem, expectedText string) {
	t.Helper()

	if len(contents) != 1 {
		t.Fatalf("Expected 1 content item, got %d", len(contents))
	}

	if contents[0].Type != "text" {
		t.Errorf("Expected content type 'text', got %q", contents[0].Type)
	}

	if contents[0].Text != expectedText {
		t.Errorf("Expected text %q, got %q", expectedText, contents[0].Text)
	}
}

func TestNewToolError(t *testing.T) {
	resp, err := NewToolError("test error message")
	if err != nil {
		t.Fatalf("NewToolError returned unexpected error: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected IsError to be true")
	}

	assertSingleTextContent(t, resp.Content, "test error message")
}

func TestNewToolSuccess(t *testing.T) {
	resp, err := NewToolSuccess("success message")
	if err != nil {
		t.Fatalf("NewToolSuccess returned unexpected error: %v", err)
	}

	if resp.IsError {
		t.Error("Expected IsError to be false")
	}

	assertSingleTextContent(t, resp.Content, "success message")
}

func TestNewResourceError(t *testing.T) {
	content, err := NewResourceError("pg://test", "resource error")
	if err != nil {
		t.Fatalf("NewResourceError returned unexpected error: %v", err)
	}

	if content.URI != "pg://test" {
		t.Errorf("Expected URI 'pg://test', got %q", content.URI)
	}

	assertSingleTextContent(t, content.Contents, "resource error")
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

	assertSingleTextContent(t, content.Contents, `{"table": "users"}`)
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
