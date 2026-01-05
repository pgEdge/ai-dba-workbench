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
