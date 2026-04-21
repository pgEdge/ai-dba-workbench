/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package logger

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestSetVerboseAndIsVerbose tests the verbose flag setters and getters
func TestSetVerboseAndIsVerbose(t *testing.T) {
	// Test default value
	if IsVerbose() {
		t.Error("Expected verbose to be false by default")
	}

	// Test setting to true
	SetVerbose(true)
	if !IsVerbose() {
		t.Error("Expected verbose to be true after SetVerbose(true)")
	}

	// Test setting to false
	SetVerbose(false)
	if IsVerbose() {
		t.Error("Expected verbose to be false after SetVerbose(false)")
	}
}

// TestInfoVerboseModes tests that Info/Infof respect verbose mode
func TestInfoVerboseModes(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0) // Remove timestamps for easier testing

	// Test Info in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Info("test info message")
	if buf.String() != "" {
		t.Errorf("Expected no output in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Info in verbose mode
	SetVerbose(true)
	buf.Reset()
	Info("test info message")
	if !strings.Contains(buf.String(), "test info message") {
		t.Errorf("Expected info message in verbose mode, got: %s",
			buf.String())
	}

	// Test Infof in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Infof("formatted %s %d", "test", 123)
	if buf.String() != "" {
		t.Errorf("Expected no output in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Infof in verbose mode
	SetVerbose(true)
	buf.Reset()
	Infof("formatted %s %d", "test", 123)
	if !strings.Contains(buf.String(), "formatted test 123") {
		t.Errorf("Expected formatted message in verbose mode, got: %s",
			buf.String())
	}
}

// TestErrorAlwaysShown tests that Error/Errorf always output regardless of
// verbose mode
func TestErrorAlwaysShown(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	// Test Error in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Error("error message")
	if !strings.Contains(buf.String(), "error message") {
		t.Errorf("Expected error message in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Error in verbose mode
	SetVerbose(true)
	buf.Reset()
	Error("error message")
	if !strings.Contains(buf.String(), "error message") {
		t.Errorf("Expected error message in verbose mode, got: %s",
			buf.String())
	}

	// Test Errorf in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Errorf("formatted error %d", 404)
	if !strings.Contains(buf.String(), "formatted error 404") {
		t.Errorf("Expected formatted error in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Errorf in verbose mode
	SetVerbose(true)
	buf.Reset()
	Errorf("formatted error %d", 404)
	if !strings.Contains(buf.String(), "formatted error 404") {
		t.Errorf("Expected formatted error in verbose mode, got: %s",
			buf.String())
	}
}

// TestStartupAlwaysShown tests that Startup/Startupf always output
// regardless of verbose mode
func TestStartupAlwaysShown(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	// Test Startup in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Startup("startup message")
	if !strings.Contains(buf.String(), "startup message") {
		t.Errorf("Expected startup message in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Startup in verbose mode
	SetVerbose(true)
	buf.Reset()
	Startup("startup message")
	if !strings.Contains(buf.String(), "startup message") {
		t.Errorf("Expected startup message in verbose mode, got: %s",
			buf.String())
	}

	// Test Startupf in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Startupf("starting on port %d", 8080)
	if !strings.Contains(buf.String(), "starting on port 8080") {
		t.Errorf("Expected formatted startup in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Startupf in verbose mode
	SetVerbose(true)
	buf.Reset()
	Startupf("starting on port %d", 8080)
	if !strings.Contains(buf.String(), "starting on port 8080") {
		t.Errorf("Expected formatted startup in verbose mode, got: %s",
			buf.String())
	}
}

// TestInit tests that Init sets up the logger correctly
func TestInit(t *testing.T) {
	// Just verify Init doesn't panic
	Init()

	// Verify that logging works after Init
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	Error("test after init")
	if !strings.Contains(buf.String(), "test after init") {
		t.Errorf("Expected logging to work after Init, got: %s",
			buf.String())
	}
}

// TestDebugVerboseModes tests that Debug/Debugf respect verbose mode
func TestDebugVerboseModes(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	// Test Debug in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Debug("debug message")
	if buf.String() != "" {
		t.Errorf("Expected no output in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Debug in verbose mode
	SetVerbose(true)
	buf.Reset()
	Debug("debug message")
	if !strings.Contains(buf.String(), "debug message") {
		t.Errorf("Expected debug message in verbose mode, got: %s",
			buf.String())
	}

	// Test Debugf in non-verbose mode
	SetVerbose(false)
	buf.Reset()
	Debugf("formatted debug %s %d", "test", 456)
	if buf.String() != "" {
		t.Errorf("Expected no output in non-verbose mode, got: %s",
			buf.String())
	}

	// Test Debugf in verbose mode
	SetVerbose(true)
	buf.Reset()
	Debugf("formatted debug %s %d", "test", 456)
	if !strings.Contains(buf.String(), "formatted debug test 456") {
		t.Errorf("Expected formatted debug in verbose mode, got: %s",
			buf.String())
	}

	// Reset verbose mode
	SetVerbose(false)
}

// TestMultipleLogCalls tests multiple log calls in sequence
func TestMultipleLogCalls(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	SetVerbose(true)
	buf.Reset()

	Info("first message")
	Info("second message")
	Debug("third message")

	output := buf.String()
	if !strings.Contains(output, "first message") {
		t.Error("Expected first message in output")
	}
	if !strings.Contains(output, "second message") {
		t.Error("Expected second message in output")
	}
	if !strings.Contains(output, "third message") {
		t.Error("Expected third message in output")
	}

	SetVerbose(false)
}

// TestLogWithVariousTypes tests logging with various argument types
func TestLogWithVariousTypes(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	SetVerbose(true)
	buf.Reset()

	Info("string", 123, true, 3.14)
	output := buf.String()
	if !strings.Contains(output, "string") {
		t.Error("Expected string in output")
	}
	if !strings.Contains(output, "123") {
		t.Error("Expected integer in output")
	}
	if !strings.Contains(output, "true") {
		t.Error("Expected boolean in output")
	}
	if !strings.Contains(output, "3.14") {
		t.Error("Expected float in output")
	}

	SetVerbose(false)
}

// TestErrorfFormatting tests that Errorf properly formats strings
func TestErrorfFormatting(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	buf.Reset()
	Errorf("Error: %s (code: %d, retry: %v)", "connection failed", 503, true)

	output := buf.String()
	expected := "Error: connection failed (code: 503, retry: true)"
	if !strings.Contains(output, expected) {
		t.Errorf("Expected %q in output, got: %s", expected, output)
	}
}

// TestStartupfFormatting tests that Startupf properly formats strings
func TestStartupfFormatting(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	buf.Reset()
	Startupf("Server starting on %s:%d", "0.0.0.0", 8080)

	output := buf.String()
	expected := "Server starting on 0.0.0.0:8080"
	if !strings.Contains(output, expected) {
		t.Errorf("Expected %q in output, got: %s", expected, output)
	}
}

// TestVerboseToggle tests rapidly toggling verbose mode
func TestVerboseToggle(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	for i := 0; i < 10; i++ {
		SetVerbose(true)
		if !IsVerbose() {
			t.Errorf("Iteration %d: Expected verbose to be true", i)
		}
		SetVerbose(false)
		if IsVerbose() {
			t.Errorf("Iteration %d: Expected verbose to be false", i)
		}
	}
}
