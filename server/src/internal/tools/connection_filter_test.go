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
	"strings"
	"testing"
)

// TestBuildConnectionFilter_AllConnections verifies that when the caller has
// unrestricted visibility (superuser or wildcard token scope) the filter
// collapses to "TRUE" with no bound parameters.
func TestBuildConnectionFilter_AllConnections(t *testing.T) {
	clause, args := buildConnectionFilter("a.connection_id", true, nil)
	if clause != "TRUE" {
		t.Errorf("Expected TRUE clause for allConnections=true, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("Expected no args, got %v", args)
	}

	// allConnections=true must override any explicit IDs; callers
	// shouldn't pass IDs in that case, but be defensive.
	clause, args = buildConnectionFilter("a.connection_id", true, []int{1, 2})
	if clause != "TRUE" {
		t.Errorf("Expected TRUE clause even with ids when allConnections=true, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("Expected no args when allConnections=true, got %v", args)
	}
}

// TestBuildConnectionFilter_EmptyIDs verifies that a caller with no visible
// connections produces a clause that matches nothing. This is the core
// regression guard for issue #35: previously a nil/empty slice was treated
// as "all connections" and leaked data to non-superusers.
func TestBuildConnectionFilter_EmptyIDs(t *testing.T) {
	clause, args := buildConnectionFilter("a.connection_id", false, nil)
	if clause != "FALSE" {
		t.Errorf("Expected FALSE clause for empty visibility, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("Expected no args, got %v", args)
	}

	clause, args = buildConnectionFilter("a.connection_id", false, []int{})
	if clause != "FALSE" {
		t.Errorf("Expected FALSE clause for empty visibility, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("Expected no args, got %v", args)
	}
}

// TestBuildConnectionFilter_ExplicitIDs verifies that an explicit allow-list
// produces a parameterised IN clause, preventing SQL injection via the
// column name or ID values.
func TestBuildConnectionFilter_ExplicitIDs(t *testing.T) {
	clause, args := buildConnectionFilter("a.connection_id", false, []int{7, 9, 42})
	if !strings.Contains(clause, "a.connection_id IN (") {
		t.Errorf("Expected IN clause with column name, got %q", clause)
	}
	if !strings.Contains(clause, "$1") || !strings.Contains(clause, "$2") || !strings.Contains(clause, "$3") {
		t.Errorf("Expected positional placeholders $1..$3, got %q", clause)
	}
	if len(args) != 3 {
		t.Fatalf("Expected 3 args, got %v", args)
	}
	want := []int{7, 9, 42}
	for i, v := range want {
		if got, ok := args[i].(int); !ok || got != v {
			t.Errorf("args[%d] = %v; want %d", i, args[i], v)
		}
	}
}
