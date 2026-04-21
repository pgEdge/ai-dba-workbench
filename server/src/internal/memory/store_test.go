/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package memory

import (
	"testing"
)

func TestNewStore(t *testing.T) {
	// NewStore should accept a nil pool without panicking
	store := NewStore(nil)
	if store == nil {
		t.Fatal("Expected non-nil store")
	}
	if store.pool != nil {
		t.Error("Expected nil pool in store")
	}
}

func TestFormatEmbedding(t *testing.T) {
	tests := []struct {
		name     string
		input    []float32
		expected string
	}{
		{
			name:     "empty embedding",
			input:    []float32{},
			expected: "[]",
		},
		{
			name:     "single value",
			input:    []float32{0.5},
			expected: "[0.5]",
		},
		{
			name:     "multiple values",
			input:    []float32{0.1, 0.2, 0.3},
			expected: "[0.1,0.2,0.3]",
		},
		{
			name:     "integer values",
			input:    []float32{1, 2, 3},
			expected: "[1,2,3]",
		},
		{
			name:     "negative values",
			input:    []float32{-0.5, 0, 0.5},
			expected: "[-0.5,0,0.5]",
		},
		{
			name:     "scientific notation",
			input:    []float32{0.0001, 1000000},
			expected: "[0.0001,1e+06]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatEmbedding(tt.input)
			if result != tt.expected {
				t.Errorf("formatEmbedding(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMemoryStruct(t *testing.T) {
	// Test that Memory struct fields are accessible
	mem := Memory{
		ID:        123,
		Username:  "testuser",
		Scope:     "user",
		Category:  "preference",
		Content:   "test content",
		Pinned:    true,
		ModelName: "test-model",
	}

	if mem.ID != 123 {
		t.Errorf("Expected ID 123, got %d", mem.ID)
	}
	if mem.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %q", mem.Username)
	}
	if mem.Scope != "user" {
		t.Errorf("Expected Scope 'user', got %q", mem.Scope)
	}
	if mem.Category != "preference" {
		t.Errorf("Expected Category 'preference', got %q", mem.Category)
	}
	if mem.Content != "test content" {
		t.Errorf("Expected Content 'test content', got %q", mem.Content)
	}
	if !mem.Pinned {
		t.Error("Expected Pinned to be true")
	}
	if mem.ModelName != "test-model" {
		t.Errorf("Expected ModelName 'test-model', got %q", mem.ModelName)
	}
}

func TestSentinelErrors(t *testing.T) {
	// Test that sentinel errors are defined
	if ErrNotFound == nil {
		t.Error("ErrNotFound should not be nil")
	}
	if ErrAccessDenied == nil {
		t.Error("ErrAccessDenied should not be nil")
	}

	// Test error messages
	if ErrNotFound.Error() != "memory not found" {
		t.Errorf("Unexpected ErrNotFound message: %q", ErrNotFound.Error())
	}
	if ErrAccessDenied.Error() != "access denied" {
		t.Errorf("Unexpected ErrAccessDenied message: %q", ErrAccessDenied.Error())
	}
}
