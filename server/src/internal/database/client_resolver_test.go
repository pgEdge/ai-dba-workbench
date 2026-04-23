/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mockSessionProvider implements SessionProvider for testing
type mockSessionProvider struct {
	session     *ConnectionSession
	getError    error
	clearError  error
	clearCalled bool
	clearedHash string
}

func (m *mockSessionProvider) GetConnectionSession(tokenHash string) (*ConnectionSession, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	return m.session, nil
}

func (m *mockSessionProvider) ClearConnectionSession(tokenHash string) error {
	m.clearCalled = true
	m.clearedHash = tokenHash
	return m.clearError
}

// mockAccessChecker implements AccessChecker for testing
type mockAccessChecker struct {
	canAccess   bool
	accessLevel string
}

func (m *mockAccessChecker) CanAccessConnection(ctx context.Context, connectionID int) (bool, string) {
	return m.canAccess, m.accessLevel
}

// mockConnInfoProvider implements ConnectionInfoProvider for testing
type mockConnInfoProvider struct {
	conn                     *MonitoredConnection
	password                 string
	getError                 error
	builtConnStr             string
	receivedDatabaseOverride string
}

func (m *mockConnInfoProvider) GetConnectionWithPassword(ctx context.Context, connectionID int) (*MonitoredConnection, string, error) {
	if m.getError != nil {
		return nil, "", m.getError
	}
	return m.conn, m.password, nil
}

func (m *mockConnInfoProvider) BuildConnectionString(conn *MonitoredConnection, password string, databaseOverride string) string {
	m.receivedDatabaseOverride = databaseOverride
	return m.builtConnStr
}

func TestClientResolver_ResolveClient_EmptyToken(t *testing.T) {
	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "" },
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if !strings.Contains(err.Error(), "no authentication token") {
		t.Errorf("expected 'no authentication token' error, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_NoSessionProvider_NoClientManager(t *testing.T) {
	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       nil,
		ConnInfo:       nil,
		ClientManager:  nil,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error when no client manager configured")
	}
	if !strings.Contains(err.Error(), "no client manager configured") {
		t.Errorf("expected 'no client manager configured' error, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_SessionLookupError_LogsWarning(t *testing.T) {
	sessions := &mockSessionProvider{
		getError: errors.New("session lookup failed"),
	}
	connInfo := &mockConnInfoProvider{}

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		ConnInfo:       connInfo,
	}

	// With session lookup error and no fallback session, should get "no connection selected"
	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no database connection selected") {
		t.Errorf("expected 'no database connection selected' error, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_NoSession_ReturnsNoConnectionError(t *testing.T) {
	sessions := &mockSessionProvider{
		session: nil, // No session
	}
	connInfo := &mockConnInfoProvider{}

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		ConnInfo:       connInfo,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error when no session")
	}
	if !strings.Contains(err.Error(), "no database connection selected") {
		t.Errorf("expected 'no database connection selected' error, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_SessionAccessDenied_ClearsSession(t *testing.T) {
	connID := 42
	sessions := &mockSessionProvider{
		session: &ConnectionSession{
			ConnectionID: connID,
			DatabaseName: nil,
		},
	}
	access := &mockAccessChecker{
		canAccess:   false,
		accessLevel: "",
	}
	connInfo := &mockConnInfoProvider{}

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		Access:         access,
		ConnInfo:       connInfo,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error when access denied")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}

	// Verify session was cleared
	if !sessions.clearCalled {
		t.Error("expected ClearConnectionSession to be called")
	}
	if sessions.clearedHash != "test-token-hash" {
		t.Errorf("expected token hash 'test-token-hash', got: %s", sessions.clearedHash)
	}
}

func TestClientResolver_ResolveClient_GetConnectionError(t *testing.T) {
	connID := 42
	sessions := &mockSessionProvider{
		session: &ConnectionSession{
			ConnectionID: connID,
			DatabaseName: nil,
		},
	}
	access := &mockAccessChecker{
		canAccess:   true,
		accessLevel: "read_write",
	}
	connInfo := &mockConnInfoProvider{
		getError: errors.New("connection not found"),
	}

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		Access:         access,
		ConnInfo:       connInfo,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error when connection info fetch fails")
	}
	if !strings.Contains(err.Error(), "failed to get connection info") {
		t.Errorf("expected 'failed to get connection info' error, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_NoAccessChecker_SkipsRBAC(t *testing.T) {
	connID := 42
	sessions := &mockSessionProvider{
		session: &ConnectionSession{
			ConnectionID: connID,
			DatabaseName: nil,
		},
	}
	connInfo := &mockConnInfoProvider{
		conn: &MonitoredConnection{
			ID:           connID,
			Host:         "localhost",
			Port:         5432,
			DatabaseName: "testdb",
			Username:     "testuser",
		},
		password:     "testpass",
		builtConnStr: "postgres://testuser:testpass@localhost:5432/testdb",
	}
	clientManager := NewClientManager(nil)
	defer clientManager.CloseAll()

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		Access:         nil, // No RBAC checker
		ConnInfo:       connInfo,
		ClientManager:  clientManager,
	}

	// Without a real database, GetClientForSession will fail, but we're testing
	// that the RBAC check is skipped (no access denied error)
	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		// If it succeeds, that's fine (shouldn't happen without real DB)
		return
	}
	// The error should be about database connection, not access denied
	if strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected database connection error, not access denied, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_WithDatabaseOverride(t *testing.T) {
	dbName := "custom_db"
	connID := 42
	sessions := &mockSessionProvider{
		session: &ConnectionSession{
			ConnectionID: connID,
			DatabaseName: &dbName,
		},
	}
	access := &mockAccessChecker{
		canAccess:   true,
		accessLevel: "read_write",
	}
	connInfo := &mockConnInfoProvider{
		conn: &MonitoredConnection{
			ID:           connID,
			Host:         "localhost",
			Port:         5432,
			DatabaseName: "default_db",
			Username:     "testuser",
		},
		password:     "testpass",
		builtConnStr: "postgres://testuser:testpass@localhost:5432/custom_db",
	}
	clientManager := NewClientManager(nil)
	defer clientManager.CloseAll()

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		Access:         access,
		ConnInfo:       connInfo,
		ClientManager:  clientManager,
	}

	// Without a real database, this will fail, but we're testing the flow
	_, err := resolver.ResolveClient(context.Background())
	// Error is expected since we don't have a real database
	if err == nil || strings.Contains(err.Error(), "access denied") {
		if err != nil {
			t.Errorf("unexpected access denied error: %v", err)
		}
	}

	// Verify databaseOverride was passed correctly to BuildConnectionString
	if connInfo.receivedDatabaseOverride != dbName {
		t.Errorf("expected databaseOverride %q, got %q", dbName, connInfo.receivedDatabaseOverride)
	}
}

func TestClientResolver_ResolveClient_FallbackToClientManager(t *testing.T) {
	// Test fallback when Sessions is nil but ConnInfo is also nil
	clientManager := NewClientManager(nil)
	defer clientManager.CloseAll()

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       nil,
		ConnInfo:       nil,
		ClientManager:  clientManager,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error since no database is configured")
	}
	// Should fall through to ClientManager.GetOrCreateClient which fails
	// with "no database configured" since ClientManager has nil dbConfig
	if !strings.Contains(err.Error(), "no database connection configured") {
		t.Errorf("expected 'no database connection configured' error, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_OnlySessionsNil(t *testing.T) {
	// When Sessions is nil but ConnInfo is set, should still fall back
	connInfo := &mockConnInfoProvider{}
	clientManager := NewClientManager(nil)
	defer clientManager.CloseAll()

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       nil,
		ConnInfo:       connInfo,
		ClientManager:  clientManager,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error since no database is configured")
	}
	// Should fall through to ClientManager.GetOrCreateClient
	if !strings.Contains(err.Error(), "no database connection configured") {
		t.Errorf("expected 'no database connection configured' error, got: %v", err)
	}
}

func TestClientResolver_ResolveClient_OnlyConnInfoNil(t *testing.T) {
	// When ConnInfo is nil but Sessions is set, should still fall back
	sessions := &mockSessionProvider{
		session: &ConnectionSession{ConnectionID: 1},
	}
	clientManager := NewClientManager(nil)
	defer clientManager.CloseAll()

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		ConnInfo:       nil,
		ClientManager:  clientManager,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error since no database is configured")
	}
	// Should fall through to ClientManager.GetOrCreateClient since both
	// Sessions AND ConnInfo must be non-nil for session-based resolution
	if !strings.Contains(err.Error(), "no database connection configured") {
		t.Errorf("expected 'no database connection configured' error, got: %v", err)
	}
}

func TestConnectionSession_Fields(t *testing.T) {
	// Test that ConnectionSession fields are accessible
	dbName := "mydb"
	session := &ConnectionSession{
		ConnectionID: 123,
		DatabaseName: &dbName,
	}

	if session.ConnectionID != 123 {
		t.Errorf("expected ConnectionID 123, got %d", session.ConnectionID)
	}
	if session.DatabaseName == nil {
		t.Fatal("expected DatabaseName to be non-nil")
	}
	if *session.DatabaseName != "mydb" {
		t.Errorf("expected DatabaseName 'mydb', got '%s'", *session.DatabaseName)
	}

	// Test with nil DatabaseName
	session2 := &ConnectionSession{
		ConnectionID: 456,
		DatabaseName: nil,
	}
	if session2.ConnectionID != 456 {
		t.Errorf("expected ConnectionID 456, got %d", session2.ConnectionID)
	}
	if session2.DatabaseName != nil {
		t.Error("expected DatabaseName to be nil")
	}
}

func TestClientResolver_TokenExtractorCalled(t *testing.T) {
	called := false
	expectedToken := "expected-token-hash"

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string {
			called = true
			return expectedToken
		},
		Sessions: &mockSessionProvider{session: nil},
		ConnInfo: &mockConnInfoProvider{},
	}

	resolver.ResolveClient(context.Background())

	if !called {
		t.Error("expected TokenExtractor to be called")
	}
}

func TestClientResolver_ClearSessionError_StillReturnsAccessDenied(t *testing.T) {
	// Test that ClearConnectionSession error doesn't prevent access denied error
	connID := 42
	sessions := &mockSessionProvider{
		session: &ConnectionSession{
			ConnectionID: connID,
			DatabaseName: nil,
		},
		clearError: errors.New("clear failed"),
	}
	access := &mockAccessChecker{
		canAccess:   false,
		accessLevel: "",
	}
	connInfo := &mockConnInfoProvider{}

	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions:       sessions,
		Access:         access,
		ConnInfo:       connInfo,
	}

	_, err := resolver.ResolveClient(context.Background())
	if err == nil {
		t.Fatal("expected error when access denied")
	}
	// Even though clear failed, we should still get access denied
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}

	// Clear should still have been called
	if !sessions.clearCalled {
		t.Error("expected ClearConnectionSession to be called despite error")
	}
}
