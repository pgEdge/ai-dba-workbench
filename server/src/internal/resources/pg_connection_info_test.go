/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package resources

import (
	"encoding/json"
	"testing"
)

func TestConnectionInfoResourceDefinition(t *testing.T) {
	def := ConnectionInfoResourceDefinition()

	if def.URI != URIConnectionInfo {
		t.Errorf("expected URI %q, got %q", URIConnectionInfo, def.URI)
	}

	if def.Name == "" {
		t.Error("expected non-empty Name")
	}

	if def.Description == "" {
		t.Error("expected non-empty Description")
	}

	if def.MimeType != "application/json" {
		t.Errorf("expected MimeType application/json, got %q", def.MimeType)
	}
}

func TestNewNoConnectionInfo(t *testing.T) {
	info := NewNoConnectionInfo()

	if info.Connected {
		t.Error("expected Connected to be false")
	}

	if info.ConnectionID != nil {
		t.Error("expected ConnectionID to be nil")
	}

	if info.Message == "" {
		t.Error("expected non-empty Message")
	}
}

func TestNewConnectionInfo(t *testing.T) {
	info := NewConnectionInfo(
		42,
		"Test Connection",
		"localhost",
		5432,
		"testdb",
		"testuser",
		true,
	)

	if !info.Connected {
		t.Error("expected Connected to be true")
	}

	if info.ConnectionID == nil || *info.ConnectionID != 42 {
		t.Errorf("expected ConnectionID 42, got %v", info.ConnectionID)
	}

	if info.ConnectionName == nil || *info.ConnectionName != "Test Connection" {
		t.Errorf("expected ConnectionName 'Test Connection', got %v", info.ConnectionName)
	}

	if info.Host == nil || *info.Host != "localhost" {
		t.Errorf("expected Host 'localhost', got %v", info.Host)
	}

	if info.Port == nil || *info.Port != 5432 {
		t.Errorf("expected Port 5432, got %v", info.Port)
	}

	if info.DatabaseName == nil || *info.DatabaseName != "testdb" {
		t.Errorf("expected DatabaseName 'testdb', got %v", info.DatabaseName)
	}

	if info.Username == nil || *info.Username != "testuser" {
		t.Errorf("expected Username 'testuser', got %v", info.Username)
	}

	if info.IsMonitored == nil || !*info.IsMonitored {
		t.Error("expected IsMonitored to be true")
	}

	if info.Message == "" {
		t.Error("expected non-empty Message")
	}
}

func TestBuildConnectionInfoResponse(t *testing.T) {
	info := NewConnectionInfo(
		1,
		"My Server",
		"db.example.com",
		5432,
		"production",
		"admin",
		false,
	)

	response, err := BuildConnectionInfoResponse(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response.URI != URIConnectionInfo {
		t.Errorf("expected URI %q, got %q", URIConnectionInfo, response.URI)
	}

	if len(response.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(response.Contents))
	}

	if response.Contents[0].Type != "text" {
		t.Errorf("expected content type 'text', got %q", response.Contents[0].Type)
	}

	// Verify JSON can be parsed back
	var parsed ConnectionInfo
	if err := json.Unmarshal([]byte(response.Contents[0].Text), &parsed); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if !parsed.Connected {
		t.Error("expected parsed Connected to be true")
	}

	if parsed.ConnectionID == nil || *parsed.ConnectionID != 1 {
		t.Errorf("expected parsed ConnectionID 1, got %v", parsed.ConnectionID)
	}
}

func TestBuildConnectionInfoResponse_NoConnection(t *testing.T) {
	info := NewNoConnectionInfo()

	response, err := BuildConnectionInfoResponse(info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify JSON can be parsed back
	var parsed ConnectionInfo
	if err := json.Unmarshal([]byte(response.Contents[0].Text), &parsed); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if parsed.Connected {
		t.Error("expected parsed Connected to be false")
	}

	// These should be nil/omitted when not connected
	if parsed.ConnectionID != nil {
		t.Error("expected ConnectionID to be nil")
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{5432, "5432"},
		{-1, "-1"},
		{-42, "-42"},
	}

	for _, tt := range tests {
		result := itoa(tt.input)
		if result != tt.expected {
			t.Errorf("itoa(%d) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}
