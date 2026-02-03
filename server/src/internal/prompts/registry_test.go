/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package prompts

import (
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// createTestPrompt creates a simple test prompt for testing the registry
func createTestPrompt(name, description string) Prompt {
	return Prompt{
		Definition: mcp.Prompt{
			Name:        name,
			Description: description,
			Arguments:   []mcp.PromptArgument{},
		},
		Handler: func(args map[string]string) mcp.PromptResult {
			return mcp.PromptResult{
				Description: "Test result for " + name,
				Messages: []mcp.PromptMessage{
					{
						Role: "user",
						Content: mcp.ContentItem{
							Type: "text",
							Text: "Test message for " + name,
						},
					},
				},
			}
		},
	}
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("Expected registry to be created, got nil")
	}

	if registry.prompts == nil {
		t.Error("Expected prompts map to be initialized")
	}
}

func TestRegisterAndGet(t *testing.T) {
	registry := NewRegistry()

	// Create a test prompt
	testPrompt := createTestPrompt("test-prompt", "A test prompt")

	// Register it
	registry.Register("test-prompt", testPrompt)

	// Retrieve it
	prompt, found := registry.Get("test-prompt")

	if !found {
		t.Fatal("Expected to find registered prompt")
	}

	if prompt.Definition.Name != "test-prompt" {
		t.Errorf("Expected prompt name 'test-prompt', got %q", prompt.Definition.Name)
	}
}

func TestGetNonExistent(t *testing.T) {
	registry := NewRegistry()

	_, found := registry.Get("non-existent")

	if found {
		t.Error("Expected not to find non-existent prompt")
	}
}

func TestList(t *testing.T) {
	registry := NewRegistry()

	// Register multiple prompts
	registry.Register("prompt-1", createTestPrompt("prompt-1", "First prompt"))
	registry.Register("prompt-2", createTestPrompt("prompt-2", "Second prompt"))
	registry.Register("prompt-3", createTestPrompt("prompt-3", "Third prompt"))

	// List all prompts
	prompts := registry.List()

	if len(prompts) != 3 {
		t.Errorf("Expected 3 prompts, got %d", len(prompts))
	}

	// Verify all prompts have required fields
	for _, prompt := range prompts {
		if prompt.Name == "" {
			t.Error("Prompt is missing name")
		}
		if prompt.Description == "" {
			t.Errorf("Prompt %q is missing description", prompt.Name)
		}
	}
}

func TestExecute(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test-prompt", createTestPrompt("test-prompt", "A test prompt"))

	args := map[string]string{}

	result, err := registry.Execute("test-prompt", args)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if result.Description == "" {
		t.Error("Result description should not be empty")
	}

	if len(result.Messages) == 0 {
		t.Error("Result should have at least one message")
	}
}

func TestExecuteNonExistent(t *testing.T) {
	registry := NewRegistry()

	// Register a prompt so we can verify it appears in the error message
	registry.Register("test-prompt", createTestPrompt("test-prompt", "A test prompt"))

	args := map[string]string{}
	_, err := registry.Execute("non-existent", args)

	if err == nil {
		t.Error("Expected error when executing non-existent prompt")
	}

	// Verify error message contains the prompt name and lists available prompts
	errMsg := err.Error()
	if !strings.Contains(errMsg, "non-existent") {
		t.Errorf("Error should contain the requested prompt name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "Available prompts") {
		t.Errorf("Error should list available prompts, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "test-prompt") {
		t.Errorf("Error should include the registered prompt name, got: %s", errMsg)
	}
}

func TestEmptyRegistry(t *testing.T) {
	registry := NewRegistry()

	// List should return empty slice
	prompts := registry.List()
	if len(prompts) != 0 {
		t.Errorf("Expected 0 prompts in empty registry, got %d", len(prompts))
	}

	// Execute should return error
	_, err := registry.Execute("any-prompt", map[string]string{})
	if err == nil {
		t.Error("Expected error when executing prompt on empty registry")
	}
}

func TestPromptWithArguments(t *testing.T) {
	registry := NewRegistry()

	// Create a prompt with arguments
	promptWithArgs := Prompt{
		Definition: mcp.Prompt{
			Name:        "prompt-with-args",
			Description: "A prompt that accepts arguments",
			Arguments: []mcp.PromptArgument{
				{
					Name:        "query",
					Description: "The query text",
					Required:    true,
				},
				{
					Name:        "optional_param",
					Description: "An optional parameter",
					Required:    false,
				},
			},
		},
		Handler: func(args map[string]string) mcp.PromptResult {
			query := args["query"]
			if query == "" {
				query = "default query"
			}
			return mcp.PromptResult{
				Description: "Result with query: " + query,
				Messages: []mcp.PromptMessage{
					{
						Role: "user",
						Content: mcp.ContentItem{
							Type: "text",
							Text: "Processing query: " + query,
						},
					},
				},
			}
		},
	}

	registry.Register("prompt-with-args", promptWithArgs)

	// Test with arguments
	args := map[string]string{
		"query": "test query",
	}
	result, err := registry.Execute("prompt-with-args", args)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !strings.Contains(result.Description, "test query") {
		t.Errorf("Expected result to contain 'test query', got: %s", result.Description)
	}

	if len(result.Messages) == 0 {
		t.Error("Result should have at least one message")
	}

	if result.Messages[0].Role != "user" {
		t.Errorf("Expected first message role 'user', got %q", result.Messages[0].Role)
	}
}
