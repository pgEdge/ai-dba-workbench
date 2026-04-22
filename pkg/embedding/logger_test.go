/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package embedding

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLogLevel_None(t *testing.T) {
	// Create a logger with LogLevelNone
	var buf bytes.Buffer
	testLogger := &Logger{
		level:  LogLevelNone,
		logger: log.New(&buf, "[LLM] ", log.LstdFlags),
	}

	// Try logging at all levels - nothing should be logged
	testLogger.Info("This is an info message")
	testLogger.Debug("This is a debug message")
	testLogger.Trace("This is a trace message")

	// Buffer should be empty
	if buf.Len() > 0 {
		t.Errorf("Expected no output with LogLevelNone, got: %s", buf.String())
	}
}

func TestLogLevel_Info(t *testing.T) {
	// Create a logger with LogLevelInfo
	var buf bytes.Buffer
	testLogger := &Logger{
		level:  LogLevelInfo,
		logger: log.New(&buf, "[LLM] ", 0), // 0 flags for predictable output
	}

	// Info should be logged
	testLogger.Info("This is an info message")
	if !strings.Contains(buf.String(), "[INFO] This is an info message") {
		t.Errorf("Expected info message to be logged, got: %s", buf.String())
	}

	// Debug and Trace should not be logged
	buf.Reset()
	testLogger.Debug("This is a debug message")
	testLogger.Trace("This is a trace message")
	if buf.Len() > 0 {
		t.Errorf("Expected no debug/trace output with LogLevelInfo, got: %s", buf.String())
	}
}

func TestLogLevel_Debug(t *testing.T) {
	// Create a logger with LogLevelDebug
	var buf bytes.Buffer
	testLogger := &Logger{
		level:  LogLevelDebug,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	// Info and Debug should be logged
	testLogger.Info("This is an info message")
	testLogger.Debug("This is a debug message")
	output := buf.String()
	if !strings.Contains(output, "[INFO] This is an info message") {
		t.Errorf("Expected info message to be logged, got: %s", output)
	}
	if !strings.Contains(output, "[DEBUG] This is a debug message") {
		t.Errorf("Expected debug message to be logged, got: %s", output)
	}

	// Trace should not be logged
	buf.Reset()
	testLogger.Trace("This is a trace message")
	if buf.Len() > 0 {
		t.Errorf("Expected no trace output with LogLevelDebug, got: %s", buf.String())
	}
}

func TestLogLevel_Trace(t *testing.T) {
	// Create a logger with LogLevelTrace
	var buf bytes.Buffer
	testLogger := &Logger{
		level:  LogLevelTrace,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	// All levels should be logged
	testLogger.Info("This is an info message")
	testLogger.Debug("This is a debug message")
	testLogger.Trace("This is a trace message")
	output := buf.String()

	if !strings.Contains(output, "[INFO] This is an info message") {
		t.Errorf("Expected info message to be logged, got: %s", output)
	}
	if !strings.Contains(output, "[DEBUG] This is a debug message") {
		t.Errorf("Expected debug message to be logged, got: %s", output)
	}
	if !strings.Contains(output, "[TRACE] This is a trace message") {
		t.Errorf("Expected trace message to be logged, got: %s", output)
	}
}

func TestSetLogLevel(t *testing.T) {
	// Save original logger
	original := globalLogger

	// Create test logger
	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelNone,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	// Set to Info level
	SetLogLevel(LogLevelInfo)
	if globalLogger.level != LogLevelInfo {
		t.Errorf("Expected LogLevelInfo, got %v", globalLogger.level)
	}

	// Verify Info is logged
	globalLogger.Info("Test message")
	if !strings.Contains(buf.String(), "[INFO] Test message") {
		t.Errorf("Expected info message after SetLogLevel, got: %s", buf.String())
	}

	// Restore original logger
	globalLogger = original
}

func TestLogAPICall(t *testing.T) {
	// Save original logger
	original := globalLogger

	// Create test logger with Info level
	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelInfo,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	// Test successful API call
	duration := 100 * time.Millisecond
	LogAPICall("openai", "text-embedding-3-small", 50, duration, 1536, nil)
	output := buf.String()

	if !strings.Contains(output, "API call succeeded") {
		t.Errorf("Expected success message, got: %s", output)
	}
	if !strings.Contains(output, "provider=openai") {
		t.Errorf("Expected provider info, got: %s", output)
	}

	// Test failed API call
	buf.Reset()
	LogAPICall("openai", "text-embedding-3-small", 50, duration, 0, os.ErrInvalid)
	output = buf.String()

	if !strings.Contains(output, "API call failed") {
		t.Errorf("Expected failure message, got: %s", output)
	}

	// Restore original logger
	globalLogger = original
}

func TestLogLLMCall(t *testing.T) {
	// Save original logger
	original := globalLogger

	// Create test logger with Info level
	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelInfo,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	// Test successful LLM call
	duration := 200 * time.Millisecond
	LogLLMCall("anthropic", "claude-sonnet-4", "chat", 100, 50, duration, nil)
	output := buf.String()

	if !strings.Contains(output, "LLM call succeeded") {
		t.Errorf("Expected success message, got: %s", output)
	}
	if !strings.Contains(output, "input_tokens=100") {
		t.Errorf("Expected input tokens, got: %s", output)
	}
	if !strings.Contains(output, "output_tokens=50") {
		t.Errorf("Expected output tokens, got: %s", output)
	}
	if !strings.Contains(output, "total_tokens=150") {
		t.Errorf("Expected total tokens, got: %s", output)
	}

	// Test failed LLM call
	buf.Reset()
	LogLLMCall("anthropic", "claude-sonnet-4", "chat", 0, 0, duration, os.ErrInvalid)
	output = buf.String()

	if !strings.Contains(output, "LLM call failed") {
		t.Errorf("Expected failure message, got: %s", output)
	}

	// Restore original logger
	globalLogger = original
}

func TestLogLLMCallDetails_OnlyAtDebugLevel(t *testing.T) {
	// Save original logger
	original := globalLogger

	// Test at Info level - should not log
	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelInfo,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogLLMCallDetails("anthropic", "claude-sonnet-4", "chat", "https://api.anthropic.com", 3)
	if buf.Len() > 0 {
		t.Errorf("Expected no output at Info level, got: %s", buf.String())
	}

	// Test at Debug level - should log
	buf.Reset()
	globalLogger.level = LogLevelDebug
	LogLLMCallDetails("anthropic", "claude-sonnet-4", "chat", "https://api.anthropic.com", 3)
	if !strings.Contains(buf.String(), "Starting LLM call") {
		t.Errorf("Expected debug message at Debug level, got: %s", buf.String())
	}

	// Restore original logger
	globalLogger = original
}

func TestLogLLMResponseTrace_OnlyAtTraceLevel(t *testing.T) {
	// Save original logger
	original := globalLogger

	// Test at Debug level - should not log
	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelDebug,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogLLMResponseTrace("anthropic", "claude-sonnet-4", "chat", 200, "end_turn")
	if buf.Len() > 0 {
		t.Errorf("Expected no output at Debug level, got: %s", buf.String())
	}

	// Test at Trace level - should log
	buf.Reset()
	globalLogger.level = LogLevelTrace
	LogLLMResponseTrace("anthropic", "claude-sonnet-4", "chat", 200, "end_turn")
	if !strings.Contains(buf.String(), "LLM response details") {
		t.Errorf("Expected trace message at Trace level, got: %s", buf.String())
	}

	// Restore original logger
	globalLogger = original
}

func TestGetLogLevel(t *testing.T) {
	// Save original logger
	original := globalLogger

	// Test GetLogLevel
	globalLogger = &Logger{
		level:  LogLevelDebug,
		logger: log.New(os.Stderr, "[LLM] ", 0),
	}

	if GetLogLevel() != LogLevelDebug {
		t.Errorf("Expected LogLevelDebug, got %v", GetLogLevel())
	}

	SetLogLevel(LogLevelTrace)
	if GetLogLevel() != LogLevelTrace {
		t.Errorf("Expected LogLevelTrace, got %v", GetLogLevel())
	}

	// Restore original logger
	globalLogger = original
}

func TestLogAPICallDetails(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelDebug,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogAPICallDetails("voyage", "voyage-3-lite", "https://api.voyageai.com/v1/embeddings", 100)
	output := buf.String()

	if !strings.Contains(output, "Starting API call") {
		t.Errorf("Expected starting API call message, got: %s", output)
	}
	if !strings.Contains(output, "provider=voyage") {
		t.Errorf("Expected provider info, got: %s", output)
	}
	if !strings.Contains(output, "text_length=100") {
		t.Errorf("Expected text length, got: %s", output)
	}

	globalLogger = original
}

func TestLogRequestTrace(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelTrace,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogRequestTrace("openai", "text-embedding-3-small", "This is a test text for embedding")
	output := buf.String()

	if !strings.Contains(output, "Request details") {
		t.Errorf("Expected request details message, got: %s", output)
	}
	if !strings.Contains(output, "text_preview=") {
		t.Errorf("Expected text preview, got: %s", output)
	}

	globalLogger = original
}

func TestLogResponseTrace(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelTrace,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogResponseTrace("gemini", "text-embedding-004", 200, 768)
	output := buf.String()

	if !strings.Contains(output, "Response details") {
		t.Errorf("Expected response details message, got: %s", output)
	}
	if !strings.Contains(output, "status_code=200") {
		t.Errorf("Expected status code, got: %s", output)
	}
	if !strings.Contains(output, "dimensions=768") {
		t.Errorf("Expected dimensions, got: %s", output)
	}

	globalLogger = original
}

func TestLogRateLimitError(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelInfo,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogRateLimitError("openai", "text-embedding-3-small", 429, `{"error": "rate limit exceeded"}`)
	output := buf.String()

	if !strings.Contains(output, "RATE LIMIT ERROR") {
		t.Errorf("Expected rate limit error message, got: %s", output)
	}
	if !strings.Contains(output, "status_code=429") {
		t.Errorf("Expected status code, got: %s", output)
	}

	globalLogger = original
}

func TestLogConnectionError(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelInfo,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogConnectionError("ollama", "http://localhost:11434", os.ErrNotExist)
	output := buf.String()

	if !strings.Contains(output, "Connection failed") {
		t.Errorf("Expected connection failed message, got: %s", output)
	}
	if !strings.Contains(output, "provider=ollama") {
		t.Errorf("Expected provider info, got: %s", output)
	}

	globalLogger = original
}

func TestLogProviderInit(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelDebug,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	config := map[string]string{
		"base_url": "https://api.openai.com/v1",
		"api_key":  "sk-12345678",
	}
	LogProviderInit("openai", "text-embedding-3-small", config)
	output := buf.String()

	if !strings.Contains(output, "Provider initialized") {
		t.Errorf("Expected provider initialized message, got: %s", output)
	}
	// API key should be redacted
	if !strings.Contains(output, "***REDACTED***") {
		t.Errorf("Expected API key to be redacted, got: %s", output)
	}
	if strings.Contains(output, "sk-12345678") {
		t.Errorf("API key should not appear in output, got: %s", output)
	}

	globalLogger = original
}

func TestLogProviderInit_NoLogAtInfo(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelInfo,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	config := map[string]string{
		"base_url": "https://api.openai.com/v1",
	}
	LogProviderInit("openai", "text-embedding-3-small", config)

	// Should not log at Info level
	if buf.Len() > 0 {
		t.Errorf("Expected no output at Info level, got: %s", buf.String())
	}

	globalLogger = original
}

func TestLogLLMRequestTrace(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelTrace,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	LogLLMRequestTrace("anthropic", "claude-sonnet-4", "sql_generation", "Generate SQL for: SELECT * FROM users")
	output := buf.String()

	if !strings.Contains(output, "LLM request details") {
		t.Errorf("Expected LLM request details message, got: %s", output)
	}
	if !strings.Contains(output, "operation=sql_generation") {
		t.Errorf("Expected operation, got: %s", output)
	}

	globalLogger = original
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is a ..."},
		{"", 5, ""},
		{"abc", 0, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestLogLevelConstants(t *testing.T) {
	// Verify log level constants are in increasing order
	if LogLevelNone >= LogLevelInfo {
		t.Error("LogLevelNone should be less than LogLevelInfo")
	}
	if LogLevelInfo >= LogLevelDebug {
		t.Error("LogLevelInfo should be less than LogLevelDebug")
	}
	if LogLevelDebug >= LogLevelTrace {
		t.Error("LogLevelDebug should be less than LogLevelTrace")
	}
}

func TestLoggerWithFormat(t *testing.T) {
	original := globalLogger

	var buf bytes.Buffer
	globalLogger = &Logger{
		level:  LogLevelTrace,
		logger: log.New(&buf, "[LLM] ", 0),
	}

	// Test Info with format arguments
	globalLogger.Info("value=%d, name=%s", 42, "test")
	if !strings.Contains(buf.String(), "value=42, name=test") {
		t.Errorf("Expected formatted output, got: %s", buf.String())
	}

	// Test Debug with format arguments
	buf.Reset()
	globalLogger.Debug("config=%v", map[string]int{"a": 1})
	if !strings.Contains(buf.String(), "config=") {
		t.Errorf("Expected formatted output, got: %s", buf.String())
	}

	// Test Trace with format arguments
	buf.Reset()
	globalLogger.Trace("data=%+v", struct{ X int }{X: 5})
	if !strings.Contains(buf.String(), "X:5") {
		t.Errorf("Expected formatted output, got: %s", buf.String())
	}

	globalLogger = original
}
