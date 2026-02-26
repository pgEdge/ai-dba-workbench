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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// ---------------------------------------------------------------------------
// store_memory tool definition tests
// ---------------------------------------------------------------------------

func TestStoreMemoryToolDefinition(t *testing.T) {
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	if tool.Definition.Name != "store_memory" {
		t.Errorf("expected tool name 'store_memory', got %q",
			tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}

	// Verify required parameters
	required := tool.Definition.InputSchema.Required
	if len(required) != 2 {
		t.Fatalf("expected 2 required parameters, got %d", len(required))
	}

	wantRequired := map[string]bool{"content": true, "category": true}
	for _, r := range required {
		if !wantRequired[r] {
			t.Errorf("unexpected required parameter %q", r)
		}
	}

	// Verify all expected properties exist
	for _, prop := range []string{"content", "category", "scope", "pinned"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// recall_memories tool definition tests
// ---------------------------------------------------------------------------

func TestRecallMemoriesToolDefinition(t *testing.T) {
	tool := RecallMemoriesTool(nil, &config.Config{})

	if tool.Definition.Name != "recall_memories" {
		t.Errorf("expected tool name 'recall_memories', got %q",
			tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}

	// Verify required parameters
	required := tool.Definition.InputSchema.Required
	if len(required) != 1 {
		t.Fatalf("expected 1 required parameter, got %d", len(required))
	}
	if required[0] != "query" {
		t.Errorf("expected required parameter 'query', got %q", required[0])
	}

	// Verify all expected properties exist
	for _, prop := range []string{"query", "category", "scope", "limit"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// delete_memory tool definition tests
// ---------------------------------------------------------------------------

func TestDeleteMemoryToolDefinition(t *testing.T) {
	tool := DeleteMemoryTool(nil)

	if tool.Definition.Name != "delete_memory" {
		t.Errorf("expected tool name 'delete_memory', got %q",
			tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}

	// Verify required parameters
	required := tool.Definition.InputSchema.Required
	if len(required) != 1 {
		t.Fatalf("expected 1 required parameter, got %d", len(required))
	}
	if required[0] != "id" {
		t.Errorf("expected required parameter 'id', got %q", required[0])
	}

	// Verify property exists
	if _, ok := tool.Definition.InputSchema.Properties["id"]; !ok {
		t.Error("expected property 'id' in input schema")
	}
}

// ---------------------------------------------------------------------------
// store_memory parameter validation tests
// ---------------------------------------------------------------------------

func TestStoreMemoryMissingContent(t *testing.T) {
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "content key absent",
			args: map[string]interface{}{
				"category": "preference",
			},
		},
		{
			name: "content is empty string",
			args: map[string]interface{}{
				"content":  "",
				"category": "preference",
			},
		},
		{
			name: "content is wrong type",
			args: map[string]interface{}{
				"content":  123,
				"category": "preference",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected an error response for missing/invalid content")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "content") {
				t.Errorf("error message should mention 'content', got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestStoreMemoryWhitespaceOnlyContent(t *testing.T) {
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	args := map[string]interface{}{
		"content":  "   \t\n   ",
		"category": "preference",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response for whitespace-only content")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "empty or whitespace-only") {
		t.Errorf("expected 'empty or whitespace-only' message, got: %s",
			response.Content[0].Text)
	}
}

func TestStoreMemoryMissingCategory(t *testing.T) {
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "category key absent",
			args: map[string]interface{}{
				"content": "Remember this fact.",
			},
		},
		{
			name: "category is empty string",
			args: map[string]interface{}{
				"content":  "Remember this fact.",
				"category": "",
			},
		},
		{
			name: "category is wrong type",
			args: map[string]interface{}{
				"content":  "Remember this fact.",
				"category": 42,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected an error response for missing/invalid category")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "category") {
				t.Errorf("error message should mention 'category', got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestStoreMemoryWhitespaceOnlyCategory(t *testing.T) {
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	args := map[string]interface{}{
		"content":  "Remember this fact.",
		"category": "   \t   ",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response for whitespace-only category")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "empty or whitespace-only") {
		t.Errorf("expected 'empty or whitespace-only' message, got: %s",
			response.Content[0].Text)
	}
}

func TestStoreMemoryInvalidScope(t *testing.T) {
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	args := map[string]interface{}{
		"content":  "Remember this fact.",
		"category": "preference",
		"scope":    "invalid",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected an error response for invalid scope")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "scope") {
		t.Errorf("error message should mention 'scope', got: %s",
			response.Content[0].Text)
	}
}

func TestStoreMemoryValidScopeValues(t *testing.T) {
	// Both "user" and "system" should pass scope validation.
	// They will fail later because there is no authenticated user in
	// the context, but they must not fail on the scope check itself.
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	for _, scope := range []string{"user", "system"} {
		t.Run("scope="+scope, func(t *testing.T) {
			args := map[string]interface{}{
				"content":  "Remember this fact.",
				"category": "preference",
				"scope":    scope,
			}

			response, err := tool.Handler(args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			// The response may be an error (no user context), but the
			// error must not be about the scope value.
			if response.IsError &&
				strings.Contains(response.Content[0].Text, "scope") {
				t.Errorf("valid scope %q should not produce a scope error, got: %s",
					scope, response.Content[0].Text)
			}
		})
	}
}

func TestStoreMemorySystemScopeRequiresPermission(t *testing.T) {
	// When rbacChecker is nil, storing with scope "system" must be denied.
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	ctx := context.WithValue(context.Background(),
		auth.UsernameContextKey, "testuser")
	args := map[string]interface{}{
		"content":   "A system-wide memory.",
		"category":  "fact",
		"scope":     "system",
		"__context": ctx,
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when rbacChecker is nil and scope is system")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "Permission denied") {
		t.Errorf("expected 'Permission denied' error, got: %s",
			response.Content[0].Text)
	}
}

func TestStoreMemoryNoAuthContext(t *testing.T) {
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	// Provide valid params but no __context with a username
	args := map[string]interface{}{
		"content":  "Remember this fact.",
		"category": "preference",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when no user in context")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "current user") {
		t.Errorf("expected 'current user' error, got: %s",
			response.Content[0].Text)
	}
}

func TestStoreMemoryWithAuthContextButNilStore(t *testing.T) {
	// Verify that passing valid params and a valid user context but a
	// nil memory store returns a tool error instead of panicking.
	tool := StoreMemoryTool(nil, &config.Config{}, nil)

	ctx := context.WithValue(context.Background(),
		auth.UsernameContextKey, "testuser")
	args := map[string]interface{}{
		"content":   "Remember this fact.",
		"category":  "preference",
		"__context": ctx,
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when memory store is nil")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "not configured") {
		t.Errorf("expected 'not configured' error, got: %s",
			response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// recall_memories parameter validation tests
// ---------------------------------------------------------------------------

func TestRecallMemoriesMissingQuery(t *testing.T) {
	tool := RecallMemoriesTool(nil, &config.Config{})

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "query key absent",
			args: map[string]interface{}{},
		},
		{
			name: "query is empty string",
			args: map[string]interface{}{
				"query": "",
			},
		},
		{
			name: "query is wrong type",
			args: map[string]interface{}{
				"query": 42,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected an error response for missing/invalid query")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "query") {
				t.Errorf("error message should mention 'query', got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestRecallMemoriesWhitespaceOnlyQuery(t *testing.T) {
	tool := RecallMemoriesTool(nil, &config.Config{})

	args := map[string]interface{}{
		"query": "   \t\n   ",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response for whitespace-only query")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "empty or whitespace-only") {
		t.Errorf("expected 'empty or whitespace-only' message, got: %s",
			response.Content[0].Text)
	}
}

func TestRecallMemoriesNoAuthContext(t *testing.T) {
	tool := RecallMemoriesTool(nil, &config.Config{})

	args := map[string]interface{}{
		"query": "database preferences",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when no user in context")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "current user") {
		t.Errorf("expected 'current user' error, got: %s",
			response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// delete_memory parameter validation tests
// ---------------------------------------------------------------------------

func TestDeleteMemoryMissingID(t *testing.T) {
	tool := DeleteMemoryTool(nil)

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "id key absent",
			args: map[string]interface{}{},
		},
		{
			name: "id is wrong type (string)",
			args: map[string]interface{}{
				"id": "abc",
			},
		},
		{
			name: "id is wrong type (bool)",
			args: map[string]interface{}{
				"id": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected an error response for missing/invalid id")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "id") {
				t.Errorf("error message should mention 'id', got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestDeleteMemoryNoAuthContext(t *testing.T) {
	tool := DeleteMemoryTool(nil)

	// Provide a valid numeric id but no authenticated user in context
	args := map[string]interface{}{
		"id": float64(42),
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when no user in context")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "current user") {
		t.Errorf("expected 'current user' error, got: %s",
			response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// Config integration: IsToolEnabled for memory tools
// ---------------------------------------------------------------------------

func TestIsToolEnabledForMemoryTools(t *testing.T) {
	falseVal := false
	trueVal := true

	tests := []struct {
		name     string
		config   config.ToolsConfig
		toolName string
		expected bool
	}{
		{
			name:     "store_memory nil defaults to true",
			config:   config.ToolsConfig{},
			toolName: "store_memory",
			expected: true,
		},
		{
			name:     "store_memory explicit true",
			config:   config.ToolsConfig{StoreMemory: &trueVal},
			toolName: "store_memory",
			expected: true,
		},
		{
			name:     "store_memory explicit false",
			config:   config.ToolsConfig{StoreMemory: &falseVal},
			toolName: "store_memory",
			expected: false,
		},
		{
			name:     "recall_memories nil defaults to true",
			config:   config.ToolsConfig{},
			toolName: "recall_memories",
			expected: true,
		},
		{
			name:     "recall_memories explicit true",
			config:   config.ToolsConfig{RecallMemories: &trueVal},
			toolName: "recall_memories",
			expected: true,
		},
		{
			name:     "recall_memories explicit false",
			config:   config.ToolsConfig{RecallMemories: &falseVal},
			toolName: "recall_memories",
			expected: false,
		},
		{
			name:     "delete_memory nil defaults to true",
			config:   config.ToolsConfig{},
			toolName: "delete_memory",
			expected: true,
		},
		{
			name:     "delete_memory explicit true",
			config:   config.ToolsConfig{DeleteMemory: &trueVal},
			toolName: "delete_memory",
			expected: true,
		},
		{
			name:     "delete_memory explicit false",
			config:   config.ToolsConfig{DeleteMemory: &falseVal},
			toolName: "delete_memory",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsToolEnabled(tt.toolName)
			if result != tt.expected {
				t.Errorf("IsToolEnabled(%q): expected %v, got %v",
					tt.toolName, tt.expected, result)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// float64sToFloat32s helper test
// ---------------------------------------------------------------------------

func TestFloat64sToFloat32s(t *testing.T) {
	tests := []struct {
		name string
		src  []float64
		want []float32
	}{
		{
			name: "empty slice",
			src:  []float64{},
			want: []float32{},
		},
		{
			name: "single element",
			src:  []float64{1.5},
			want: []float32{1.5},
		},
		{
			name: "multiple elements",
			src:  []float64{0.1, 0.2, 0.3},
			want: []float32{0.1, 0.2, 0.3},
		},
		{
			name: "negative values",
			src:  []float64{-1.0, 0.0, 1.0},
			want: []float32{-1.0, 0.0, 1.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := float64sToFloat32s(tt.src)
			if len(got) != len(tt.want) {
				t.Fatalf("length mismatch: got %d, want %d",
					len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("element [%d]: got %f, want %f",
						i, got[i], tt.want[i])
				}
			}
		})
	}
}
