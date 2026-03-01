/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tracing

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// EntryType identifies the type of trace entry
type EntryType string

const (
	EntryTypeUserPrompt     EntryType = "user_prompt"
	EntryTypeLLMResponse    EntryType = "llm_response"
	EntryTypeToolCall       EntryType = "tool_call"
	EntryTypeToolResult     EntryType = "tool_result"
	EntryTypeResourceRead   EntryType = "resource_read"
	EntryTypeResourceResult EntryType = "resource_result"
	EntryTypeHTTPRequest    EntryType = "http_request"
	EntryTypeHTTPResponse   EntryType = "http_response"
	EntryTypeSessionStart   EntryType = "session_start"
	EntryTypeSessionEnd     EntryType = "session_end"
	EntryTypeError          EntryType = "error"
)

// TraceEntry represents a single trace log entry
type TraceEntry struct {
	Timestamp  time.Time      `json:"timestamp"`
	SessionID  string         `json:"session_id"`
	Type       EntryType      `json:"type"`
	Name       string         `json:"name,omitempty"`        // Tool name, resource URI, endpoint
	Parameters map[string]any `json:"parameters,omitempty"`  // Input parameters
	Result     any            `json:"result,omitempty"`      // Output/response data
	Error      string         `json:"error,omitempty"`       // Error message if any
	Duration   *time.Duration `json:"duration_ms,omitempty"` // Duration in milliseconds
	TokenHash  string         `json:"token_hash,omitempty"`  // Truncated token hash for identification
	RequestID  string         `json:"request_id,omitempty"`  // Unique request identifier
	Metadata   map[string]any `json:"metadata,omitempty"`    // Additional context
}

// MarshalJSON customizes JSON output for TraceEntry
func (e TraceEntry) MarshalJSON() ([]byte, error) {
	type Alias TraceEntry
	aux := struct {
		Alias
		DurationMS *int64 `json:"duration_ms,omitempty"`
	}{
		Alias: Alias(e),
	}
	if e.Duration != nil {
		ms := e.Duration.Milliseconds()
		aux.DurationMS = &ms
	}
	return json.Marshal(aux)
}

// Tracer manages trace logging to a file
type Tracer struct {
	mu       sync.Mutex
	file     *os.File
	encoder  *json.Encoder
	enabled  bool
	filePath string
}

// Global tracer instance
var globalTracer *Tracer
var tracerOnce sync.Once

// Initialize creates the global tracer. Call this once at startup.
func Initialize(filePath string) error {
	var initErr error
	tracerOnce.Do(func() {
		if filePath == "" {
			globalTracer = &Tracer{enabled: false}
			return
		}

		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			initErr = fmt.Errorf("failed to open trace file %s: %w", filePath, err)
			globalTracer = &Tracer{enabled: false}
			return
		}

		encoder := json.NewEncoder(file)
		// Disable HTML escaping so <, >, and & are written literally
		// This makes the trace file more human-readable
		encoder.SetEscapeHTML(false)

		globalTracer = &Tracer{
			file:     file,
			encoder:  encoder,
			enabled:  true,
			filePath: filePath,
		}
	})
	return initErr
}

// IsEnabled returns true if tracing is enabled
func IsEnabled() bool {
	if globalTracer == nil {
		return false
	}
	return globalTracer.enabled
}

// GetFilePath returns the trace file path
func GetFilePath() string {
	if globalTracer == nil {
		return ""
	}
	return globalTracer.filePath
}

// Close closes the trace file
func Close() error {
	if globalTracer == nil || !globalTracer.enabled || globalTracer.file == nil {
		return nil
	}
	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()
	return globalTracer.file.Close()
}

// Log writes a trace entry to the file
func Log(entry TraceEntry) {
	if globalTracer == nil || !globalTracer.enabled {
		return
	}

	// Set timestamp if not provided
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	globalTracer.mu.Lock()
	defer globalTracer.mu.Unlock()

	// Write as JSONL (one JSON object per line)
	if err := globalTracer.encoder.Encode(entry); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to write trace entry: %v\n", err)
	}
}

// LogToolCall logs a tool invocation
func LogToolCall(sessionID, tokenHash, requestID, toolName string, params map[string]any) {
	Log(TraceEntry{
		SessionID:  sessionID,
		Type:       EntryTypeToolCall,
		Name:       toolName,
		Parameters: params,
		TokenHash:  truncateHash(tokenHash),
		RequestID:  requestID,
	})
}

// LogToolResult logs a tool result
func LogToolResult(sessionID, tokenHash, requestID, toolName string, result any, err error, duration time.Duration) {
	entry := TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeToolResult,
		Name:      toolName,
		Result:    result,
		Duration:  &duration,
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	Log(entry)
}

// LogResourceRead logs a resource read request
func LogResourceRead(sessionID, tokenHash, requestID, resourceURI string) {
	Log(TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeResourceRead,
		Name:      resourceURI,
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	})
}

// LogResourceResult logs a resource read result
func LogResourceResult(sessionID, tokenHash, requestID, resourceURI string, result any, err error, duration time.Duration) {
	entry := TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeResourceResult,
		Name:      resourceURI,
		Result:    result,
		Duration:  &duration,
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	Log(entry)
}

// LogHTTPRequest logs an incoming HTTP request
func LogHTTPRequest(sessionID, tokenHash, requestID, method, path string, body any) {
	Log(TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeHTTPRequest,
		Name:      method + " " + path,
		Parameters: map[string]any{
			"method": method,
			"path":   path,
			"body":   body,
		},
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	})
}

// LogHTTPResponse logs an outgoing HTTP response
func LogHTTPResponse(sessionID, tokenHash, requestID, method, path string, statusCode int, body any, duration time.Duration) {
	Log(TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeHTTPResponse,
		Name:      method + " " + path,
		Result: map[string]any{
			"status_code": statusCode,
			"body":        body,
		},
		Duration:  &duration,
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	})
}

// LogUserPrompt logs a user's input prompt from chat
func LogUserPrompt(sessionID, tokenHash, requestID string, prompt any) {
	Log(TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeUserPrompt,
		Result:    prompt,
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	})
}

// LogLLMResponse logs an LLM's response
func LogLLMResponse(sessionID, tokenHash, requestID string, response any, duration time.Duration) {
	Log(TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeLLMResponse,
		Result:    response,
		Duration:  &duration,
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	})
}

// LogSessionStart logs the start of a new session
func LogSessionStart(sessionID, tokenHash string, metadata map[string]any) {
	Log(TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeSessionStart,
		TokenHash: truncateHash(tokenHash),
		Metadata:  metadata,
	})
}

// LogSessionEnd logs the end of a session
func LogSessionEnd(sessionID, tokenHash string, metadata map[string]any) {
	Log(TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeSessionEnd,
		TokenHash: truncateHash(tokenHash),
		Metadata:  metadata,
	})
}

// LogError logs an error that occurred
func LogError(sessionID, tokenHash, requestID, context string, err error) {
	entry := TraceEntry{
		SessionID: sessionID,
		Type:      EntryTypeError,
		Name:      context,
		TokenHash: truncateHash(tokenHash),
		RequestID: requestID,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	Log(entry)
}

// truncateHash returns a shortened version of the token hash for identification
func truncateHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

// GenerateRequestID creates a unique request ID
func GenerateRequestID() string {
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(), time.Now().UnixNano()%0xFFFF)
}

// GenerateSessionID creates a unique session ID
func GenerateSessionID() string {
	return fmt.Sprintf("sess_%d", time.Now().UnixNano())
}
