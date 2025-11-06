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
    "testing"
)

// TestNewHandler tests handler creation
func TestNewHandler(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0")

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
    handler := NewHandler("TestServer", "1.0.0")

    reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "initialize",
        "params": {}
    }`)

    resp, err := handler.HandleRequest(reqData)
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
    handler := NewHandler("TestServer", "1.0.0")

    reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": 42,
        "method": "ping"
    }`)

    resp, err := handler.HandleRequest(reqData)
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
    handler := NewHandler("TestServer", "1.0.0")

    reqData := []byte(`{invalid json}`)

    resp, err := handler.HandleRequest(reqData)
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
    handler := NewHandler("TestServer", "1.0.0")

    reqData := []byte(`{
        "jsonrpc": "1.0",
        "id": "test-1",
        "method": "ping"
    }`)

    resp, err := handler.HandleRequest(reqData)
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
    handler := NewHandler("TestServer", "1.0.0")

    reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "unknownMethod"
    }`)

    resp, err := handler.HandleRequest(reqData)
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
    handler := NewHandler("TestServer", "1.0.0")

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

    resp, err := handler.HandleRequest(initReq)
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

    resp, err = handler.HandleRequest(pingReq)
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
    resp, err = handler.HandleRequest(initReq)
    if err != nil {
        t.Fatalf("Second initialize failed: %v", err)
    }
    if resp.Error != nil {
        t.Fatalf("Second initialize returned error: %v", resp.Error)
    }
}
