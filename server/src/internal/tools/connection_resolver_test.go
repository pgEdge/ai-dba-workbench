/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

func TestParseConnectionArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		wantConnID  int
		wantHasConn bool
		wantDBName  string
	}{
		{
			name:        "empty args",
			args:        map[string]interface{}{},
			wantConnID:  0,
			wantHasConn: false,
			wantDBName:  "",
		},
		{
			name: "connection_id as float64",
			args: map[string]interface{}{
				"connection_id": float64(42),
			},
			wantConnID:  42,
			wantHasConn: true,
			wantDBName:  "",
		},
		{
			name: "connection_id as int",
			args: map[string]interface{}{
				"connection_id": int(7),
			},
			wantConnID:  7,
			wantHasConn: true,
			wantDBName:  "",
		},
		{
			name: "connection_id with database_name",
			args: map[string]interface{}{
				"connection_id": float64(10),
				"database_name": "mydb",
			},
			wantConnID:  10,
			wantHasConn: true,
			wantDBName:  "mydb",
		},
		{
			name: "database_name only",
			args: map[string]interface{}{
				"database_name": "otherdb",
			},
			wantConnID:  0,
			wantHasConn: false,
			wantDBName:  "otherdb",
		},
		{
			name: "connection_id as string is not parsed",
			args: map[string]interface{}{
				"connection_id": "42",
			},
			wantConnID:  0,
			wantHasConn: false,
			wantDBName:  "",
		},
		{
			name: "empty database_name is ignored",
			args: map[string]interface{}{
				"database_name": "",
			},
			wantConnID:  0,
			wantHasConn: false,
			wantDBName:  "",
		},
		{
			name: "connection_id as float64 zero",
			args: map[string]interface{}{
				"connection_id": float64(0),
			},
			wantConnID:  0,
			wantHasConn: true,
			wantDBName:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseConnectionArgs(tt.args)

			if got.ConnectionID != tt.wantConnID {
				t.Errorf("ConnectionID = %d, want %d", got.ConnectionID, tt.wantConnID)
			}
			if got.HasConnID != tt.wantHasConn {
				t.Errorf("HasConnID = %v, want %v", got.HasConnID, tt.wantHasConn)
			}
			if got.DatabaseName != tt.wantDBName {
				t.Errorf("DatabaseName = %q, want %q", got.DatabaseName, tt.wantDBName)
			}
		})
	}
}

func TestResolve_NilResolver_NoConnectionID(t *testing.T) {
	// A nil resolver with no connection_id should fall back to the
	// fallback client. When the fallback client has no default
	// connection, Resolve returns an error response.
	var resolver *ConnectionResolver
	fallbackClient := database.NewClient(nil)

	resolved, errResp := resolver.Resolve(context.Background(), map[string]interface{}{}, fallbackClient)

	if resolved != nil {
		t.Fatal("Expected nil resolved connection")
	}
	if errResp == nil {
		t.Fatal("Expected error response")
	}
	if !errResp.IsError {
		t.Error("Expected IsError to be true")
	}
	if !strings.Contains(errResp.Content[0].Text, "No default connection") {
		t.Errorf("Expected 'No default connection' error, got: %s", errResp.Content[0].Text)
	}
}

func TestResolve_NilResolver_WithConnectionID(t *testing.T) {
	// A nil resolver with connection_id present should return an
	// error about connection resolution not being available.
	var resolver *ConnectionResolver
	args := map[string]interface{}{
		"connection_id": float64(1),
	}

	resolved, errResp := resolver.Resolve(context.Background(), args, nil)

	if resolved != nil {
		t.Fatal("Expected nil resolved connection")
	}
	if errResp == nil {
		t.Fatal("Expected error response")
	}
	if !errResp.IsError {
		t.Error("Expected IsError to be true")
	}
	if !strings.Contains(errResp.Content[0].Text, "connection resolution is not available") {
		t.Errorf("Expected 'connection resolution is not available' error, got: %s", errResp.Content[0].Text)
	}
}

func TestResolveFallback_NilClient(t *testing.T) {
	resolved, errResp := resolveFallback(nil)

	if resolved != nil {
		t.Fatal("Expected nil resolved connection")
	}
	if errResp == nil {
		t.Fatal("Expected error response")
	}
	if !errResp.IsError {
		t.Error("Expected IsError to be true")
	}
	if !strings.Contains(errResp.Content[0].Text, "No database connection available") {
		t.Errorf("Expected 'No database connection available' error, got: %s", errResp.Content[0].Text)
	}
}

func TestResolveFallback_EmptyDefaultConnection(t *testing.T) {
	// NewClient(nil) creates a client with no default connection string.
	client := database.NewClient(nil)

	resolved, errResp := resolveFallback(client)

	if resolved != nil {
		t.Fatal("Expected nil resolved connection")
	}
	if errResp == nil {
		t.Fatal("Expected error response")
	}
	if !errResp.IsError {
		t.Error("Expected IsError to be true")
	}
	if !strings.Contains(errResp.Content[0].Text, "No default connection configured") {
		t.Errorf("Expected 'No default connection configured' error, got: %s", errResp.Content[0].Text)
	}
}

func TestNewConnectionResolver(t *testing.T) {
	resolver := NewConnectionResolver(nil, nil, nil)

	if resolver == nil {
		t.Fatal("Expected non-nil resolver")
	}
	if resolver.clientManager != nil {
		t.Error("Expected nil clientManager")
	}
	if resolver.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if resolver.rbacChecker != nil {
		t.Error("Expected nil rbacChecker")
	}
}
