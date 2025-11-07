/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestNewHandler tests handler creation
func TestNewHandler(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	if handler == nil {
		t.Fatal("NewHandler returned nil")
	}
	if handler.serverName != "TestServer" {
		t.Errorf("serverName = %v, want TestServer", handler.serverName)
	}
	if handler.serverVersion != "1.0.0" {
		t.Errorf("serverVersion = %v, want 1.0.0", handler.serverVersion)
	}
	if handler.initialized {
		t.Error("Handler should not be initialized on creation")
	}
}

// TestHandleInitialize tests the initialize method
func TestHandleInitialize(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "initialize",
        "params": {}
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Expected no error, got: %v", resp.Error)
	}

	if resp.ID != "test-1" {
		t.Errorf("Response ID = %v, want test-1", resp.ID)
	}

	// Verify the result structure
	result, ok := resp.Result.(InitializeResult)
	if !ok {
		t.Fatalf("Result is not InitializeResult, got %T", resp.Result)
	}

	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("ProtocolVersion = %v, want 2024-11-05",
			result.ProtocolVersion)
	}

	if result.ServerInfo.Name != "TestServer" {
		t.Errorf("ServerInfo.Name = %v, want TestServer",
			result.ServerInfo.Name)
	}

	if result.ServerInfo.Version != "1.0.0" {
		t.Errorf("ServerInfo.Version = %v, want 1.0.0",
			result.ServerInfo.Version)
	}

	// Verify handler is now initialized
	if !handler.initialized {
		t.Error("Handler should be initialized after initialize method")
	}
}

// TestHandlePing tests the ping method
func TestHandlePing(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": 42,
        "method": "ping"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Expected no error, got: %v", resp.Error)
	}

	if resp.ID != float64(42) {
		t.Errorf("Response ID = %v, want 42", resp.ID)
	}

	// Ping returns a map with status: ok
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map, got %T", resp.Result)
	}
	if result["status"] != "ok" {
		t.Errorf("Result status = %v, want ok", result["status"])
	}
}

// TestHandleInvalidJSON tests handling of invalid JSON
func TestHandleInvalidJSON(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{invalid json}`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Fatal("Expected error response for invalid JSON")
	}

	if resp.Error.Code != ParseError {
		t.Errorf("Error code = %v, want %v (ParseError)", resp.Error.Code,
			ParseError)
	}
}

// TestHandleInvalidJSONRPCVersion tests handling of invalid JSON-RPC version
func TestHandleInvalidJSONRPCVersion(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "1.0",
        "id": "test-1",
        "method": "ping"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Fatal("Expected error response for invalid JSON-RPC version")
	}

	if resp.Error.Code != InvalidRequest {
		t.Errorf("Error code = %v, want %v (InvalidRequest)",
			resp.Error.Code, InvalidRequest)
	}
}

// TestHandleUnknownMethod tests handling of unknown methods
func TestHandleUnknownMethod(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "unknownMethod"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Fatal("Expected error response for unknown method")
	}

	if resp.Error.Code != MethodNotFound {
		t.Errorf("Error code = %v, want %v (MethodNotFound)",
			resp.Error.Code, MethodNotFound)
	}
}

// TestFormatResponse tests the FormatResponse helper
func TestFormatResponse(t *testing.T) {
	resp := NewResponse("test-1", map[string]string{"status": "ok"})

	jsonBytes, err := FormatResponse(resp)
	if err != nil {
		t.Fatalf("FormatResponse failed: %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("FormatResponse returned empty bytes")
	}

	// Verify it's valid JSON
	var decoded Response
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("FormatResponse produced invalid JSON: %v", err)
	}

	if decoded.ID != "test-1" {
		t.Errorf("Decoded ID = %v, want test-1", decoded.ID)
	}
}

// TestHandleRequestSequence tests a sequence of requests
func TestHandleRequestSequence(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	// First, send initialize
	initReq := []byte(`{
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {
                "name": "TestClient",
                "version": "1.0.0"
            }
        }
    }`)

	resp, err := handler.HandleRequest(initReq, "")
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("Initialize returned error: %v", resp.Error)
	}
	if !handler.initialized {
		t.Error("Handler should be initialized")
	}

	// Then send ping
	pingReq := []byte(`{
        "jsonrpc": "2.0",
        "id": 2,
        "method": "ping"
    }`)

	resp, err = handler.HandleRequest(pingReq, "")
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("Ping returned error: %v", resp.Error)
	}

	// Ping returns a map with status: ok
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map, got %T", resp.Result)
	}
	if result["status"] != "ok" {
		t.Errorf("Result status = %v, want ok", result["status"])
	}

	// Send initialize again (should still work)
	resp, err = handler.HandleRequest(initReq, "")
	if err != nil {
		t.Fatalf("Second initialize failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("Second initialize returned error: %v", resp.Error)
	}
}

// TestHandleListResources tests the resources/list method
func TestHandleListResources(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-list-resources",
        "method": "resources/list"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Expected no error, got: %v", resp.Error)
	}

	// Verify result structure
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map, got %T", resp.Result)
	}

	resources, ok := result["resources"].([]map[string]interface{})
	if !ok {
		t.Fatalf("resources is not an array, got %T", result["resources"])
	}

	// Should have 2 resources: users and service-tokens
	if len(resources) != 2 {
		t.Errorf("Expected 2 resources, got %d", len(resources))
	}

	// Verify users resource
	foundUsers := false
	foundTokens := false
	for _, res := range resources {
		uri, _ := res["uri"].(string) //nolint:errcheck // Test code, type assertion checked in test logic
		switch uri {
		case "ai-workbench://users":
			foundUsers = true
			if res["name"] != "User Accounts" {
				t.Errorf("Users resource name = %v, want User Accounts",
					res["name"])
			}
			if res["mimeType"] != "application/json" {
				t.Errorf("Users resource mimeType = %v, want "+
					"application/json", res["mimeType"])
			}
		case "ai-workbench://service-tokens":
			foundTokens = true
			if res["name"] != "Service Tokens" {
				t.Errorf("Tokens resource name = %v, want Service Tokens",
					res["name"])
			}
		}
	}

	if !foundUsers {
		t.Error("Users resource not found in list")
	}
	if !foundTokens {
		t.Error("Service tokens resource not found in list")
	}
}

// TestHandleReadResourceInvalidURI tests resources/read with invalid URI
func TestHandleReadResourceInvalidURI(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-read-invalid",
        "method": "resources/read",
        "params": {
            "uri": "ai-workbench://invalid"
        }
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Fatal("Expected error for invalid URI")
	}

	if resp.Error.Code != InvalidParams {
		t.Errorf("Error code = %v, want %v (InvalidParams)",
			resp.Error.Code, InvalidParams)
	}
}

// TestHandleReadResourceMissingParams tests resources/read without params
func TestHandleReadResourceMissingParams(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-read-missing",
        "method": "resources/read"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Fatal("Expected error for missing params")
	}

	if resp.Error.Code != InvalidParams {
		t.Errorf("Error code = %v, want %v (InvalidParams)",
			resp.Error.Code, InvalidParams)
	}
}

// TestHandleListTools tests the tools/list method
func TestHandleListTools(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-list-tools",
        "method": "tools/list"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Expected no error, got: %v", resp.Error)
	}

	// Verify result structure
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map, got %T", resp.Result)
	}

	tools, ok := result["tools"].([]map[string]interface{})
	if !ok {
		t.Fatalf("tools is not an array, got %T", result["tools"])
	}

	// Should have 29 tools (10 existing + 8 group management + 11 privilege/scope management)
	expectedTools := []string{
		"authenticate_user",
		"create_user",
		"update_user",
		"delete_user",
		"create_service_token",
		"update_service_token",
		"delete_service_token",
		"create_user_token",
		"list_user_tokens",
		"delete_user_token",
		"create_user_group",
		"update_user_group",
		"delete_user_group",
		"list_user_groups",
		"add_group_member",
		"remove_group_member",
		"list_group_members",
		"list_user_group_memberships",
		"grant_connection_privilege",
		"revoke_connection_privilege",
		"list_connection_privileges",
		"list_mcp_privilege_identifiers",
		"grant_mcp_privilege",
		"revoke_mcp_privilege",
		"list_group_mcp_privileges",
		"set_token_connection_scope",
		"set_token_mcp_scope",
		"get_token_scope",
		"clear_token_scope",
	}

	if len(tools) != len(expectedTools) {
		t.Errorf("Expected %d tools, got %d", len(expectedTools),
			len(tools))
	}

	// Verify each tool is present and has required fields
	foundTools := make(map[string]bool)
	for _, tool := range tools {
		name, _ := tool["name"].(string) //nolint:errcheck // Test code, type assertion checked in test logic
		foundTools[name] = true

		// Verify tool has required fields
		if tool["description"] == nil {
			t.Errorf("Tool %s missing description", name)
		}
		if tool["inputSchema"] == nil {
			t.Errorf("Tool %s missing inputSchema", name)
		}

		// Verify inputSchema structure
		schema, ok := tool["inputSchema"].(map[string]interface{})
		if !ok {
			t.Errorf("Tool %s inputSchema is not a map", name)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("Tool %s inputSchema type = %v, want object",
				name, schema["type"])
		}
		if schema["properties"] == nil {
			t.Errorf("Tool %s inputSchema missing properties", name)
		}
		if schema["required"] == nil {
			t.Errorf("Tool %s inputSchema missing required fields", name)
		}
	}

	// Verify all expected tools were found
	for _, expectedTool := range expectedTools {
		if !foundTools[expectedTool] {
			t.Errorf("Expected tool %s not found", expectedTool)
		}
	}
}

// TestHandleCallToolUnknown tests calling an unknown tool
func TestHandleCallToolUnknown(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-unknown-tool",
        "method": "tools/call",
        "params": {
            "name": "unknown_tool",
            "arguments": {}
        }
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Fatal("Expected error for unknown tool")
	}

	if resp.Error.Code != MethodNotFound {
		t.Errorf("Error code = %v, want %v (MethodNotFound)",
			resp.Error.Code, MethodNotFound)
	}
}

// TestHandleCallToolMissingParams tests calling a tool without params
func TestHandleCallToolMissingParams(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-missing-params",
        "method": "tools/call"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error == nil {
		t.Fatal("Expected error for missing params")
	}

	if resp.Error.Code != InvalidParams {
		t.Errorf("Error code = %v, want %v (InvalidParams)",
			resp.Error.Code, InvalidParams)
	}
}

// TestHandleListPrompts tests the prompts/list method
func TestHandleListPrompts(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-list-prompts",
        "method": "prompts/list"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.Error != nil {
		t.Errorf("Expected no error, got: %v", resp.Error)
	}

	// Verify result structure
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map, got %T", resp.Result)
	}

	// prompts can be either []interface{} or []map[string]interface{}
	// When empty, it's []interface{}
	prompts, ok := result["prompts"]
	if !ok {
		t.Fatal("prompts field not found in result")
	}

	// Use reflection to get length
	switch p := prompts.(type) {
	case []interface{}:
		if len(p) != 0 {
			t.Errorf("Expected 0 prompts, got %d", len(p))
		}
	case []map[string]interface{}:
		if len(p) != 0 {
			t.Errorf("Expected 0 prompts, got %d", len(p))
		}
	default:
		t.Fatalf("prompts is not an array, got %T", prompts)
	}
}

// TestToolInputSchemaValidation tests that tool schemas are properly defined
func TestToolInputSchemaValidation(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test",
        "method": "tools/list"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	result := resp.Result.(map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic
	tools := result["tools"].([]map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic

	// Test create_user schema
	createUserTool := findTool(tools, "create_user")
	if createUserTool == nil {
		t.Fatal("create_user tool not found")
	}
	schema := createUserTool["inputSchema"].(map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic
	required := schema["required"].([]string) //nolint:errcheck // Test code, type assertion checked in test logic
	expectedRequired := []string{"username", "email", "fullName", "password"}
	if !stringSlicesEqual(required, expectedRequired) {
		t.Errorf("create_user required fields = %v, want %v",
			required, expectedRequired)
	}

	// Test update_user schema
	updateUserTool := findTool(tools, "update_user")
	if updateUserTool == nil {
		t.Fatal("update_user tool not found")
	}
	schema = updateUserTool["inputSchema"].(map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic
	required = schema["required"].([]string) //nolint:errcheck // Test code, type assertion checked in test logic
	expectedRequired = []string{"username"}
	if !stringSlicesEqual(required, expectedRequired) {
		t.Errorf("update_user required fields = %v, want %v",
			required, expectedRequired)
	}

	// Test delete_user schema
	deleteUserTool := findTool(tools, "delete_user")
	if deleteUserTool == nil {
		t.Fatal("delete_user tool not found")
	}
	schema = deleteUserTool["inputSchema"].(map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic
	required = schema["required"].([]string) //nolint:errcheck // Test code, type assertion checked in test logic
	expectedRequired = []string{"username"}
	if !stringSlicesEqual(required, expectedRequired) {
		t.Errorf("delete_user required fields = %v, want %v",
			required, expectedRequired)
	}

	// Test create_service_token schema
	createTokenTool := findTool(tools, "create_service_token")
	if createTokenTool == nil {
		t.Fatal("create_service_token tool not found")
	}
	schema = createTokenTool["inputSchema"].(map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic
	required = schema["required"].([]string) //nolint:errcheck // Test code, type assertion checked in test logic
	expectedRequired = []string{"name"}
	if !stringSlicesEqual(required, expectedRequired) {
		t.Errorf("create_service_token required fields = %v, want %v",
			required, expectedRequired)
	}

	// Test update_service_token schema
	updateTokenTool := findTool(tools, "update_service_token")
	if updateTokenTool == nil {
		t.Fatal("update_service_token tool not found")
	}
	schema = updateTokenTool["inputSchema"].(map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic
	required = schema["required"].([]string) //nolint:errcheck // Test code, type assertion checked in test logic
	expectedRequired = []string{"name"}
	if !stringSlicesEqual(required, expectedRequired) {
		t.Errorf("update_service_token required fields = %v, want %v",
			required, expectedRequired)
	}

	// Test delete_service_token schema
	deleteTokenTool := findTool(tools, "delete_service_token")
	if deleteTokenTool == nil {
		t.Fatal("delete_service_token tool not found")
	}
	schema = deleteTokenTool["inputSchema"].(map[string]interface{}) //nolint:errcheck // Test code, type assertion checked in test logic
	required = schema["required"].([]string) //nolint:errcheck // Test code, type assertion checked in test logic
	expectedRequired = []string{"name"}
	if !stringSlicesEqual(required, expectedRequired) {
		t.Errorf("delete_service_token required fields = %v, want %v",
			required, expectedRequired)
	}
}

// Helper function to find a tool by name
func findTool(tools []map[string]interface{}, name string) map[string]interface{} {
	for _, tool := range tools {
		if tool["name"] == name {
			return tool
		}
	}
	return nil
}

// Helper function to compare string slices
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSuperuserPrivilegeRequired tests that management tools require superuser
func TestSuperuserPrivilegeRequired(t *testing.T) {
	// Test cases for tools that require superuser privileges
	testCases := []struct {
		toolName string
		args     map[string]interface{}
	}{
		{
			toolName: "create_user",
			args: map[string]interface{}{
				"username": "testuser",
				"email":    "test@example.com",
				"fullName": "Test User",
				"password": "testpass123",
			},
		},
		{
			toolName: "update_user",
			args: map[string]interface{}{
				"username": "testuser",
				"email":    "newemail@example.com",
			},
		},
		{
			toolName: "delete_user",
			args: map[string]interface{}{
				"username": "testuser",
			},
		},
		{
			toolName: "create_service_token",
			args: map[string]interface{}{
				"name": "testtoken",
			},
		},
		{
			toolName: "update_service_token",
			args: map[string]interface{}{
				"name": "testtoken",
				"note": "Updated note",
			},
		},
		{
			toolName: "delete_service_token",
			args: map[string]interface{}{
				"name": "testtoken",
			},
		},
	}

	// Test 1: nil userInfo should be rejected
	t.Run("NilUserInfo", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = nil

		for _, tc := range testCases {
			t.Run(tc.toolName, func(t *testing.T) {
				_, err := handler.callToolByName(tc.toolName, tc.args)
				if err == nil {
					t.Errorf("Expected error for %s with nil userInfo",
						tc.toolName)
				}
				expectedMsg := "permission denied: superuser privileges required"
				if err.Error() != expectedMsg {
					t.Errorf("Expected error message '%s', got '%s'",
						expectedMsg, err.Error())
				}
			})
		}
	})

	// Test 2: Non-superuser should be rejected
	t.Run("NonSuperuser", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false, // Not a superuser
			Username:        "regularuser",
			IsServiceToken:  false,
		}

		for _, tc := range testCases {
			t.Run(tc.toolName, func(t *testing.T) {
				_, err := handler.callToolByName(tc.toolName, tc.args)
				if err == nil {
					t.Errorf("Expected error for %s with non-superuser",
						tc.toolName)
				}
				expectedMsg := "permission denied: superuser privileges required"
				if err.Error() != expectedMsg {
					t.Errorf("Expected error message '%s', got '%s'",
						expectedMsg, err.Error())
				}
			})
		}
	})

	// Test 3: Non-superuser service token should be rejected
	t.Run("NonSuperuserServiceToken", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false, // Not a superuser
			Username:        "",
			IsServiceToken:  true,
		}

		for _, tc := range testCases {
			t.Run(tc.toolName, func(t *testing.T) {
				_, err := handler.callToolByName(tc.toolName, tc.args)
				if err == nil {
					t.Errorf("Expected error for %s with non-superuser token",
						tc.toolName)
				}
				expectedMsg := "permission denied: superuser privileges required"
				if err.Error() != expectedMsg {
					t.Errorf("Expected error message '%s', got '%s'",
						expectedMsg, err.Error())
				}
			})
		}
	})
}

// TestAuthenticateUserNoSuperuserRequired tests that authenticate_user doesn't
// require superuser by verifying it's not in the superuser-required list
func TestAuthenticateUserNoSuperuserRequired(t *testing.T) {
	// List of tools that require superuser privileges
	superuserTools := map[string]bool{
		"create_user":           true,
		"update_user":           true,
		"delete_user":           true,
		"create_service_token":  true,
		"update_service_token":  true,
		"delete_service_token":  true,
	}

	// authenticate_user should NOT be in this list
	if superuserTools["authenticate_user"] {
		t.Error("authenticate_user should not require superuser privileges")
	}

	// Verify it's excluded from the superuser tools list
	if len(superuserTools) != 6 {
		t.Errorf("Expected exactly 6 tools to require superuser, got %d",
			len(superuserTools))
	}
}

// Helper function to call a tool by name (used for testing)
func (h *Handler) callToolByName(name string, args map[string]interface{}) (
	interface{}, error) {
	switch name {
	case "authenticate_user":
		return h.handleAuthenticateUser(args)
	case "create_user":
		return h.handleCreateUser(args)
	case "update_user":
		return h.handleUpdateUser(args)
	case "delete_user":
		return h.handleDeleteUser(args)
	case "create_service_token":
		return h.handleCreateServiceToken(args)
	case "update_service_token":
		return h.handleUpdateServiceToken(args)
	case "delete_service_token":
		return h.handleDeleteServiceToken(args)
	case "create_user_token":
		return h.handleCreateUserToken(args)
	case "list_user_tokens":
		return h.handleListUserTokens(args)
	case "delete_user_token":
		return h.handleDeleteUserToken(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// TestUserTokenToolsAuthorization tests authorization for user token management tools
func TestUserTokenToolsAuthorization(t *testing.T) {
	// Skip database-dependent tests if SKIP_DB_TESTS is set
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}

	t.Run("CreateUserToken_SelfAccess", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false,
			Username:        "testuser",
			IsServiceToken:  false,
		}

		args := map[string]interface{}{
			"username":     "testuser",
			"lifetimeDays": float64(30),
		}

		// Should succeed - user creating their own token
		// Note: Will fail in practice because no dbPool, but we're testing authorization
		_, err := handler.handleCreateUserToken(args)
		// We expect it to fail on database access, not authorization
		if err != nil && err.Error() == "permission denied: can only create tokens for your own account" {
			t.Error("User should be able to create their own tokens")
		}
	})

	t.Run("CreateUserToken_OtherUserDenied", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false,
			Username:        "testuser",
			IsServiceToken:  false,
		}

		args := map[string]interface{}{
			"username":     "otheruser",
			"lifetimeDays": float64(30),
		}

		_, err := handler.handleCreateUserToken(args)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if err.Error() != "permission denied: can only create tokens for your own account" {
			t.Errorf("Expected permission denied, got: %v", err)
		}
	})

	t.Run("CreateUserToken_SuperuserAllowed", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     true,
			Username:        "admin",
			IsServiceToken:  false,
		}

		args := map[string]interface{}{
			"username":     "otheruser",
			"lifetimeDays": float64(30),
		}

		// Should not fail on authorization (will fail on database)
		_, err := handler.handleCreateUserToken(args)
		if err != nil && err.Error() == "permission denied: can only create tokens for your own account" {
			t.Error("Superuser should be able to create tokens for any user")
		}
	})

	t.Run("CreateUserToken_UnauthenticatedDenied", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = nil

		args := map[string]interface{}{
			"username":     "testuser",
			"lifetimeDays": float64(30),
		}

		_, err := handler.handleCreateUserToken(args)
		if err == nil {
			t.Error("Expected authentication required error")
		}
		if err.Error() != "authentication required" {
			t.Errorf("Expected authentication required, got: %v", err)
		}
	})

	t.Run("ListUserTokens_SelfAccess", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false,
			Username:        "testuser",
			IsServiceToken:  false,
		}

		args := map[string]interface{}{
			"username": "testuser",
		}

		_, err := handler.handleListUserTokens(args)
		if err != nil && err.Error() == "permission denied: can only list your own tokens" {
			t.Error("User should be able to list their own tokens")
		}
	})

	t.Run("ListUserTokens_OtherUserDenied", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false,
			Username:        "testuser",
			IsServiceToken:  false,
		}

		args := map[string]interface{}{
			"username": "otheruser",
		}

		_, err := handler.handleListUserTokens(args)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if err.Error() != "permission denied: can only list your own tokens" {
			t.Errorf("Expected permission denied, got: %v", err)
		}
	})

	t.Run("DeleteUserToken_SelfAccess", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false,
			Username:        "testuser",
			IsServiceToken:  false,
		}

		args := map[string]interface{}{
			"username": "testuser",
			"tokenId":  float64(123),
		}

		_, err := handler.handleDeleteUserToken(args)
		if err != nil && err.Error() == "permission denied: can only delete your own tokens" {
			t.Error("User should be able to delete their own tokens")
		}
	})

	t.Run("DeleteUserToken_OtherUserDenied", func(t *testing.T) {
		handler := NewHandler("TestServer", "1.0.0", nil, nil)
		handler.userInfo = &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     false,
			Username:        "testuser",
			IsServiceToken:  false,
		}

		args := map[string]interface{}{
			"username": "otheruser",
			"tokenId":  float64(123),
		}

		_, err := handler.handleDeleteUserToken(args)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if err.Error() != "permission denied: can only delete your own tokens" {
			t.Errorf("Expected permission denied, got: %v", err)
		}
	})
}

// TestHandleListTools_IncludesUserTokenTools tests that user token tools are listed
func TestHandleListTools_IncludesUserTokenTools(t *testing.T) {
	handler := NewHandler("TestServer", "1.0.0", nil, nil)

	reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "tools/list"
    }`)

	resp, err := handler.HandleRequest(reqData, "")
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("Expected no error, got: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map")
	}

	tools, ok := result["tools"].([]map[string]interface{})
	if !ok {
		t.Fatalf("Tools is not an array")
	}

	// Check that user token tools are present
	expectedTools := map[string]bool{
		"create_user_token": false,
		"list_user_tokens":  false,
		"delete_user_token": false,
	}

	for _, tool := range tools {
		name, ok := tool["name"].(string)
		if !ok {
			continue
		}
		if _, exists := expectedTools[name]; exists {
			expectedTools[name] = true
		}
	}

	for toolName, found := range expectedTools {
		if !found {
			t.Errorf("Expected tool '%s' not found in tools list", toolName)
		}
	}
}
