/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// logSanitizer replaces control characters that could be used for log
// injection (fake log entries, terminal escape sequences) with their
// escaped visual representation. Backspace, vertical tab, form feed,
// and DEL are included because they can forge log entries in terminal
// output or confuse less-strict log parsers.
var logSanitizer = strings.NewReplacer(
	"\r", "\\r",
	"\n", "\\n",
	"\t", "\\t",
	"\x00", "\\x00",
	"\x08", "\\x08",
	"\x0b", "\\x0b",
	"\x0c", "\\x0c",
	"\x1b", "\\x1b",
	"\x7f", "\\x7f",
)

// SanitizeForLog escapes control characters in s so that user-controlled
// values logged to a line-oriented log stream cannot forge new log
// entries or inject terminal escape sequences. Use this on any value
// derived from HTTP input, environment variables, or other untrusted
// sources before embedding it in a log message.
func SanitizeForLog(s string) string {
	return logSanitizer.Replace(s)
}

// LogLevel represents the severity of a log message
type LogLevel int32

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

var (
	// currentLevel is the minimum log level to output.
	// Default to ERROR to avoid cluttering CLI output with operational logs.
	// Uses atomic access for safe concurrent reads and writes.
	currentLevel atomic.Int32
)

func init() {
	currentLevel.Store(int32(LevelError))
}

// levelString returns the string representation of a log level
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// logEntry represents a structured log entry
type logEntry struct {
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// log writes a structured log message if the level is enabled
func log(level LogLevel, message string, keyvals ...any) {
	if int32(level) < currentLevel.Load() {
		return
	}

	entry := logEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level.String(),
		Message:   message,
		Fields:    make(map[string]any),
	}

	// Parse key-value pairs
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			key := fmt.Sprintf("%v", keyvals[i])
			entry.Fields[key] = keyvals[i+1]
		}
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to marshal log entry: %v\n", err)
		return
	}

	// Write to stderr
	fmt.Fprintln(os.Stderr, string(jsonBytes))
}

// Debug logs a debug-level message with structured fields
func Debug(message string, keyvals ...any) {
	log(LevelDebug, message, keyvals...)
}

// Info logs an info-level message with structured fields
func Info(message string, keyvals ...any) {
	log(LevelInfo, message, keyvals...)
}

// Warn logs a warning-level message with structured fields
func Warn(message string, keyvals ...any) {
	log(LevelWarn, message, keyvals...)
}

// Error logs an error-level message with structured fields
func Error(message string, keyvals ...any) {
	log(LevelError, message, keyvals...)
}

// SetLevel sets the minimum log level to output
func SetLevel(level LogLevel) {
	currentLevel.Store(int32(level))
}

// GetLevel returns the current minimum log level
func GetLevel() LogLevel {
	return LogLevel(currentLevel.Load())
}
