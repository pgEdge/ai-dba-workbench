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
    "encoding/json"
    "testing"
)

// TestNewRequest tests the NewRequest helper function
func TestNewRequest(t *testing.T) {
    tests := []struct {
        name   string
        method string
        params interface{}
    }{
        {
            name:   "Simple request",
            method: "initialize",
            params: nil,
        },
        {
            name:   "Request with ping method",
            method: "ping",
            params: nil,
        },
        {
            name:   "Request with params",
            method: "initialize",
            params: map[string]string{"version": "1.0"},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req, err := NewRequest(tt.method, tt.params)
            if err != nil {
                t.Fatalf("NewRequest failed: %v", err)
            }

            if req.JSONRPC != JSONRPCVersion {
                t.Errorf("JSONRPC version = %v, want %v", req.JSONRPC,
                    JSONRPCVersion)
            }
            if req.Method != tt.method {
                t.Errorf("Method = %v, want %v", req.Method, tt.method)
            }

            if tt.params != nil && len(req.Params) == 0 {
                t.Error("Expected params to be encoded, got empty")
            }
        })
    }
}

// TestNewResponse tests the NewResponse helper function
func TestNewResponse(t *testing.T) {
    tests := []struct {
        name   string
        id     interface{}
        result interface{}
    }{
        {
            name:   "Response with string ID and string result",
            id:     "test-1",
            result: "success",
        },
        {
            name:   "Response with numeric ID",
            id:     123,
            result: "pong",
        },
        {
            name:   "Response with nil result",
            id:     "test-2",
            result: nil,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            resp := NewResponse(tt.id, tt.result)

            if resp.JSONRPC != JSONRPCVersion {
                t.Errorf("JSONRPC version = %v, want %v", resp.JSONRPC,
                    JSONRPCVersion)
            }
            if resp.ID != tt.id {
                t.Errorf("ID = %v, want %v", resp.ID, tt.id)
            }
            if resp.Error != nil {
                t.Errorf("Error should be nil, got %v", resp.Error)
            }
            // Only compare if both are not nil, or both are nil
            if (resp.Result == nil) != (tt.result == nil) {
                t.Errorf("Result = %v, want %v", resp.Result, tt.result)
            }
        })
    }
}

// TestNewErrorResponse tests the NewErrorResponse helper function
func TestNewErrorResponse(t *testing.T) {
    tests := []struct {
        name    string
        id      interface{}
        code    int
        message string
        data    interface{}
    }{
        {
            name:    "Parse error",
            id:      nil,
            code:    ParseError,
            message: "Parse error",
            data:    nil,
        },
        {
            name:    "Invalid request",
            id:      "test-1",
            code:    InvalidRequest,
            message: "Invalid request",
            data:    "Missing required field",
        },
        {
            name:    "Method not found",
            id:      42,
            code:    MethodNotFound,
            message: "Method not found",
            data:    nil,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            resp := NewErrorResponse(tt.id, tt.code, tt.message, tt.data)

            if resp.JSONRPC != JSONRPCVersion {
                t.Errorf("JSONRPC version = %v, want %v", resp.JSONRPC,
                    JSONRPCVersion)
            }
            if resp.ID != tt.id {
                t.Errorf("ID = %v, want %v", resp.ID, tt.id)
            }
            if resp.Result != nil {
                t.Errorf("Result should be nil, got %v", resp.Result)
            }
            if resp.Error == nil {
                t.Fatal("Error should not be nil")
            }
            if resp.Error.Code != tt.code {
                t.Errorf("Error code = %v, want %v", resp.Error.Code, tt.code)
            }
            if resp.Error.Message != tt.message {
                t.Errorf("Error message = %v, want %v", resp.Error.Message,
                    tt.message)
            }
            if resp.Error.Data != tt.data {
                t.Errorf("Error data = %v, want %v", resp.Error.Data, tt.data)
            }
        })
    }
}

// TestErrorCodes verifies standard error codes are defined
func TestErrorCodes(t *testing.T) {
    codes := map[string]int{
        "ParseError":     ParseError,
        "InvalidRequest": InvalidRequest,
        "MethodNotFound": MethodNotFound,
        "InvalidParams":  InvalidParams,
        "InternalError":  InternalError,
    }

    expected := map[string]int{
        "ParseError":     -32700,
        "InvalidRequest": -32600,
        "MethodNotFound": -32601,
        "InvalidParams":  -32602,
        "InternalError":  -32603,
    }

    for name, code := range codes {
        if code != expected[name] {
            t.Errorf("%s = %d, want %d", name, code, expected[name])
        }
    }
}

// TestRequestMarshaling tests JSON marshaling of Request
func TestRequestMarshaling(t *testing.T) {
    req, err := NewRequest("initialize", nil)
    if err != nil {
        t.Fatalf("NewRequest failed: %v", err)
    }

    data, err := json.Marshal(req)
    if err != nil {
        t.Fatalf("Failed to marshal request: %v", err)
    }

    var decoded Request
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("Failed to unmarshal request: %v", err)
    }

    if decoded.JSONRPC != req.JSONRPC {
        t.Errorf("Decoded JSONRPC = %v, want %v", decoded.JSONRPC, req.JSONRPC)
    }
    if decoded.Method != req.Method {
        t.Errorf("Decoded Method = %v, want %v", decoded.Method, req.Method)
    }
}

// TestResponseMarshaling tests JSON marshaling of Response
func TestResponseMarshaling(t *testing.T) {
    resp := NewResponse("test-1", map[string]string{"status": "ok"})
    data, err := json.Marshal(resp)
    if err != nil {
        t.Fatalf("Failed to marshal response: %v", err)
    }

    var decoded Response
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("Failed to unmarshal response: %v", err)
    }

    if decoded.JSONRPC != resp.JSONRPC {
        t.Errorf("Decoded JSONRPC = %v, want %v", decoded.JSONRPC,
            resp.JSONRPC)
    }
    if decoded.ID != resp.ID {
        t.Errorf("Decoded ID = %v, want %v", decoded.ID, resp.ID)
    }
}
