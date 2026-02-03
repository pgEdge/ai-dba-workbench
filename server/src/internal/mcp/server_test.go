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
	"context"
	"errors"
	"testing"
)

// Mock implementations for testing

type mockToolProvider struct {
	tools       []Tool
	executeFunc func(ctx context.Context, name string, args map[string]interface{}) (ToolResponse, error)
}

func (m *mockToolProvider) List() []Tool {
	return m.tools
}

func (m *mockToolProvider) Execute(ctx context.Context, name string, args map[string]interface{}) (ToolResponse, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, name, args)
	}
	return NewToolSuccess("executed")
}

type mockResourceProvider struct {
	resources []Resource
	readFunc  func(ctx context.Context, uri string) (ResourceContent, error)
}

func (m *mockResourceProvider) List() []Resource {
	return m.resources
}

func (m *mockResourceProvider) Read(ctx context.Context, uri string) (ResourceContent, error) {
	if m.readFunc != nil {
		return m.readFunc(ctx, uri)
	}
	return NewResourceSuccess(uri, "text/plain", "content")
}

type mockPromptProvider struct {
	prompts     []Prompt
	executeFunc func(name string, args map[string]string) (PromptResult, error)
}

func (m *mockPromptProvider) List() []Prompt {
	return m.prompts
}

func (m *mockPromptProvider) Execute(name string, args map[string]string) (PromptResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(name, args)
	}
	return PromptResult{
		Messages: []PromptMessage{
			{Role: "user", Content: ContentItem{Type: "text", Text: "test"}},
		},
	}, nil
}

func TestNewServer(t *testing.T) {
	tools := &mockToolProvider{
		tools: []Tool{{Name: "test", Description: "Test tool"}},
	}

	server := NewServer(tools)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.tools == nil {
		t.Error("expected tools provider to be set")
	}
}

func TestServerSetProviders(t *testing.T) {
	tools := &mockToolProvider{}
	server := NewServer(tools)

	// Test SetResourceProvider
	resources := &mockResourceProvider{
		resources: []Resource{{URI: "pg://test", Name: "test"}},
	}
	server.SetResourceProvider(resources)
	if server.resources == nil {
		t.Error("expected resource provider to be set")
	}

	// Test SetPromptProvider
	prompts := &mockPromptProvider{
		prompts: []Prompt{{Name: "test", Description: "Test prompt"}},
	}
	server.SetPromptProvider(prompts)
	if server.prompts == nil {
		t.Error("expected prompt provider to be set")
	}
}

func TestServerConstants(t *testing.T) {
	// Verify server constants are set correctly
	if ProtocolVersion == "" {
		t.Error("ProtocolVersion should not be empty")
	}
	if ServerName == "" {
		t.Error("ServerName should not be empty")
	}
	if ServerVersion == "" {
		t.Error("ServerVersion should not be empty")
	}

	// Verify expected values
	if ServerName != "pgedge-postgres-mcp" {
		t.Errorf("expected ServerName 'pgedge-postgres-mcp', got %q", ServerName)
	}
}

func TestScannerConstants(t *testing.T) {
	// Verify buffer size constants are reasonable
	if ScannerInitialBufferSize <= 0 {
		t.Error("ScannerInitialBufferSize should be positive")
	}
	if ScannerMaxBufferSize <= ScannerInitialBufferSize {
		t.Error("ScannerMaxBufferSize should be greater than initial size")
	}

	// Verify expected values
	if ScannerInitialBufferSize != 64*1024 {
		t.Errorf("expected initial buffer size 64KB, got %d", ScannerInitialBufferSize)
	}
	if ScannerMaxBufferSize != 1024*1024 {
		t.Errorf("expected max buffer size 1MB, got %d", ScannerMaxBufferSize)
	}
}

// Test mock providers work correctly
func TestMockToolProvider(t *testing.T) {
	provider := &mockToolProvider{
		tools: []Tool{
			{Name: "tool1", Description: "First tool"},
			{Name: "tool2", Description: "Second tool"},
		},
	}

	tools := provider.List()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	// Test execute with default behavior
	resp, err := provider.Execute(context.Background(), "test", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Error("expected success response")
	}

	// Test execute with custom function
	provider.executeFunc = func(ctx context.Context, name string, args map[string]interface{}) (ToolResponse, error) {
		if name == "fail" {
			return NewToolError("failed")
		}
		return NewToolSuccess("custom: " + name)
	}

	resp, err = provider.Execute(context.Background(), "test", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.Content[0].Text != "custom: test" {
		t.Errorf("expected custom response, got %q", resp.Content[0].Text)
	}
}

func TestMockResourceProvider(t *testing.T) {
	provider := &mockResourceProvider{
		resources: []Resource{
			{URI: "pg://schema", Name: "schema"},
		},
	}

	resources := provider.List()
	if len(resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(resources))
	}

	// Test read with default behavior
	content, err := provider.Read(context.Background(), "pg://test")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if content.URI != "pg://test" {
		t.Errorf("expected URI 'pg://test', got %q", content.URI)
	}

	// Test read with custom function
	provider.readFunc = func(ctx context.Context, uri string) (ResourceContent, error) {
		if uri == "pg://error" {
			return ResourceContent{}, errors.New("not found")
		}
		return NewResourceSuccess(uri, "application/json", `{"key": "value"}`)
	}

	content, err = provider.Read(context.Background(), "pg://custom")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if content.MimeType != "application/json" {
		t.Errorf("expected application/json, got %q", content.MimeType)
	}
}

func TestMockPromptProvider(t *testing.T) {
	provider := &mockPromptProvider{
		prompts: []Prompt{
			{Name: "prompt1", Description: "First prompt"},
		},
	}

	prompts := provider.List()
	if len(prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(prompts))
	}

	// Test execute with default behavior
	result, err := provider.Execute("test", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(result.Messages))
	}

	// Test execute with custom function
	provider.executeFunc = func(name string, args map[string]string) (PromptResult, error) {
		if name == "fail" {
			return PromptResult{}, errors.New("prompt not found")
		}
		return PromptResult{
			Description: "Custom prompt",
			Messages: []PromptMessage{
				{Role: "user", Content: ContentItem{Type: "text", Text: args["query"]}},
			},
		}, nil
	}

	result, err = provider.Execute("custom", map[string]string{"query": "test query"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Description != "Custom prompt" {
		t.Errorf("expected description 'Custom prompt', got %q", result.Description)
	}
}
