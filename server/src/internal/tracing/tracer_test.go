/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package tracing

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestGenerateRequestID(t *testing.T) {
    id1 := GenerateRequestID()
    id2 := GenerateRequestID()

    if id1 == "" {
        t.Error("GenerateRequestID should return non-empty string")
    }
    if id1 == id2 {
        t.Error("GenerateRequestID should return unique IDs")
    }
}

func TestGenerateSessionID(t *testing.T) {
    id1 := GenerateSessionID()
    time.Sleep(time.Nanosecond) // Ensure time advances
    id2 := GenerateSessionID()

    if id1 == "" {
        t.Error("GenerateSessionID should return non-empty string")
    }
    if !strings.HasPrefix(id1, "sess_") {
        t.Error("GenerateSessionID should start with 'sess_'")
    }
    // Note: IDs may be the same if generated at the same nanosecond
    // We just verify the format is correct
    _ = id2
}

func TestTruncateHash(t *testing.T) {
    tests := []struct {
        input    string
        expected string
    }{
        {"", ""},
        {"short", "short"},
        {"12345678", "12345678"},
        {"123456789", "12345678"},
        {"abcdefghijklmnop", "abcdefgh"},
    }

    for _, tt := range tests {
        result := truncateHash(tt.input)
        if result != tt.expected {
            t.Errorf("truncateHash(%q) = %q, want %q", tt.input, result, tt.expected)
        }
    }
}

func TestIsEnabled_NotInitialized(t *testing.T) {
    // Reset global tracer for this test
    originalTracer := globalTracer
    globalTracer = nil
    defer func() { globalTracer = originalTracer }()

    if IsEnabled() {
        t.Error("IsEnabled should return false when tracer is not initialized")
    }
}

func TestGetFilePath_NotInitialized(t *testing.T) {
    // Reset global tracer for this test
    originalTracer := globalTracer
    globalTracer = nil
    defer func() { globalTracer = originalTracer }()

    if GetFilePath() != "" {
        t.Error("GetFilePath should return empty string when tracer is not initialized")
    }
}

func TestTraceEntryMarshalJSON(t *testing.T) {
    duration := 100 * time.Millisecond
    entry := TraceEntry{
        Timestamp: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
        SessionID: "sess_123",
        Type:      EntryTypeToolCall,
        Name:      "test_tool",
        Parameters: map[string]interface{}{
            "param1": "value1",
        },
        Duration:  &duration,
        TokenHash: "abcd1234",
        RequestID: "req_123",
    }

    data, err := json.Marshal(entry)
    if err != nil {
        t.Fatalf("Failed to marshal TraceEntry: %v", err)
    }

    // Verify the JSON contains expected fields
    jsonStr := string(data)
    if !strings.Contains(jsonStr, `"type":"tool_call"`) {
        t.Error("JSON should contain type field")
    }
    if !strings.Contains(jsonStr, `"name":"test_tool"`) {
        t.Error("JSON should contain name field")
    }
    if !strings.Contains(jsonStr, `"session_id":"sess_123"`) {
        t.Error("JSON should contain session_id field")
    }
    if !strings.Contains(jsonStr, `"duration_ms":100`) {
        t.Error("JSON should contain duration_ms as integer milliseconds")
    }
}

func TestTraceEntryMarshalJSON_NoDuration(t *testing.T) {
    entry := TraceEntry{
        Timestamp: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
        SessionID: "sess_123",
        Type:      EntryTypeToolCall,
        Name:      "test_tool",
    }

    data, err := json.Marshal(entry)
    if err != nil {
        t.Fatalf("Failed to marshal TraceEntry: %v", err)
    }

    // Verify the JSON does not contain duration_ms when nil
    jsonStr := string(data)
    if strings.Contains(jsonStr, "duration_ms") {
        t.Error("JSON should not contain duration_ms when Duration is nil")
    }
}

func TestInitializeAndLog(t *testing.T) {
    // Create a temporary file for testing
    tmpDir := t.TempDir()
    traceFile := filepath.Join(tmpDir, "test-trace.jsonl")

    // Reset the once so we can initialize again
    // Note: This is a bit hacky but necessary for testing
    originalTracer := globalTracer
    defer func() { globalTracer = originalTracer }()
    globalTracer = nil

    // Create a new tracer directly (bypassing once for testing)
    file, err := os.OpenFile(traceFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
    if err != nil {
        t.Fatalf("Failed to create trace file: %v", err)
    }

    globalTracer = &Tracer{
        file:     file,
        encoder:  json.NewEncoder(file),
        enabled:  true,
        filePath: traceFile,
    }

    // Verify tracing is enabled
    if !IsEnabled() {
        t.Error("IsEnabled should return true after initialization")
    }

    if GetFilePath() != traceFile {
        t.Errorf("GetFilePath() = %q, want %q", GetFilePath(), traceFile)
    }

    // Log some entries
    LogToolCall("sess_123", "token_abc", "req_001", "query_database", map[string]interface{}{
        "query": "SELECT 1",
    })

    duration := 50 * time.Millisecond
    LogToolResult("sess_123", "token_abc", "req_001", "query_database", "success", nil, duration)

    LogResourceRead("sess_123", "token_abc", "req_002", "pg://system_info")

    LogResourceResult("sess_123", "token_abc", "req_002", "pg://system_info", map[string]string{
        "version": "PostgreSQL 16.0",
    }, nil, duration)

    // Close to flush
    if err := Close(); err != nil {
        t.Errorf("Close() failed: %v", err)
    }

    // Read and verify the trace file
    data, err := os.ReadFile(traceFile)
    if err != nil {
        t.Fatalf("Failed to read trace file: %v", err)
    }

    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    if len(lines) != 4 {
        t.Errorf("Expected 4 trace entries, got %d", len(lines))
    }

    // Verify first entry is a tool_call
    var entry1 TraceEntry
    if err := json.Unmarshal([]byte(lines[0]), &entry1); err != nil {
        t.Errorf("Failed to parse first entry: %v", err)
    }
    if entry1.Type != EntryTypeToolCall {
        t.Errorf("First entry type = %q, want %q", entry1.Type, EntryTypeToolCall)
    }
    if entry1.Name != "query_database" {
        t.Errorf("First entry name = %q, want %q", entry1.Name, "query_database")
    }
}

func TestLogWithDisabledTracer(t *testing.T) {
    // Reset global tracer
    originalTracer := globalTracer
    defer func() { globalTracer = originalTracer }()

    globalTracer = &Tracer{enabled: false}

    // These should not panic even when tracing is disabled
    LogToolCall("sess", "token", "req", "tool", nil)
    LogToolResult("sess", "token", "req", "tool", nil, nil, time.Second)
    LogResourceRead("sess", "token", "req", "uri")
    LogResourceResult("sess", "token", "req", "uri", nil, nil, time.Second)
    LogHTTPRequest("sess", "token", "req", "POST", "/path", nil)
    LogHTTPResponse("sess", "token", "req", "POST", "/path", 200, nil, time.Second)
    LogUserPrompt("sess", "token", "req", "prompt")
    LogLLMResponse("sess", "token", "req", "response", time.Second)
    LogSessionStart("sess", "token", nil)
    LogSessionEnd("sess", "token", nil)
    LogError("sess", "token", "req", "context", nil)
}

func TestEntryTypes(t *testing.T) {
    // Verify entry type constants are as expected
    tests := []struct {
        entryType EntryType
        expected  string
    }{
        {EntryTypeUserPrompt, "user_prompt"},
        {EntryTypeLLMResponse, "llm_response"},
        {EntryTypeToolCall, "tool_call"},
        {EntryTypeToolResult, "tool_result"},
        {EntryTypeResourceRead, "resource_read"},
        {EntryTypeResourceResult, "resource_result"},
        {EntryTypeHTTPRequest, "http_request"},
        {EntryTypeHTTPResponse, "http_response"},
        {EntryTypeSessionStart, "session_start"},
        {EntryTypeSessionEnd, "session_end"},
        {EntryTypeError, "error"},
    }

    for _, tt := range tests {
        if string(tt.entryType) != tt.expected {
            t.Errorf("EntryType %v = %q, want %q", tt.entryType, string(tt.entryType), tt.expected)
        }
    }
}
