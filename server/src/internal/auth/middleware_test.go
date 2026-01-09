/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createTestAuthStore creates a temporary auth store for testing
func createTestAuthStore(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// TestAuthMiddleware_Disabled tests that middleware is bypassed when disabled
func TestAuthMiddleware_Disabled(t *testing.T) {
	store, cleanup := createTestAuthStore(t)
	defer cleanup()

	middleware := AuthMiddleware(store, false)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", rr.Code)
	}

	if body := rr.Body.String(); body != "success" {
		t.Errorf("Expected 'success', got %q", body)
	}
}

// TestAuthMiddleware_HealthCheck tests that health check endpoint bypasses auth
func TestAuthMiddleware_HealthCheck(t *testing.T) {
	store, cleanup := createTestAuthStore(t)
	defer cleanup()

	middleware := AuthMiddleware(store, true)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("healthy"))
	}))

	req := httptest.NewRequest("GET", HealthCheckPath, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK for health check, got %d", rr.Code)
	}

	if body := rr.Body.String(); body != "healthy" {
		t.Errorf("Expected 'healthy', got %q", body)
	}
}

// TestAuthMiddleware_MissingAuthHeader tests rejection of requests without Authorization header
func TestAuthMiddleware_MissingAuthHeader(t *testing.T) {
	store, cleanup := createTestAuthStore(t)
	defer cleanup()

	middleware := AuthMiddleware(store, true)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for missing auth header")
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", rr.Code)
	}

	body := strings.TrimSpace(rr.Body.String())
	if !strings.Contains(body, "Missing Authorization header") {
		t.Errorf("Expected 'Missing Authorization header' in response, got %q", body)
	}
}

// TestAuthMiddleware_MalformedAuthHeader tests rejection of malformed Authorization headers
func TestAuthMiddleware_MalformedAuthHeader(t *testing.T) {
	testCases := []struct {
		name          string
		header        string
		expectFormat  bool // true if expecting "Invalid Authorization header format"
		expectInvalid bool // true if expecting "Invalid or unknown token"
	}{
		{"no bearer prefix", "sometoken123", true, false},
		{"wrong prefix", "Basic sometoken123", true, false},
		{"missing token", "Bearer", true, false},
		{"empty token", "Bearer ", false, true}, // Empty token passes format check but fails validation
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStore(t)
			defer cleanup()

			middleware := AuthMiddleware(store, true)

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("Handler should not be called for malformed auth header")
			}))

			req := httptest.NewRequest("POST", "/mcp/v1", nil)
			req.Header.Set("Authorization", tc.header)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("Expected status Unauthorized, got %d", rr.Code)
			}

			body := strings.TrimSpace(rr.Body.String())
			if tc.expectFormat && !strings.Contains(body, "Invalid Authorization header format") {
				t.Errorf("Expected 'Invalid Authorization header format' in response, got %q", body)
			}
			if tc.expectInvalid && !strings.Contains(body, "Invalid or unknown token") {
				t.Errorf("Expected 'Invalid or unknown token' in response, got %q", body)
			}
		})
	}
}

// TestAuthMiddleware_InvalidToken tests rejection of invalid tokens
func TestAuthMiddleware_InvalidToken(t *testing.T) {
	store, cleanup := createTestAuthStore(t)
	defer cleanup()

	middleware := AuthMiddleware(store, true)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for invalid token")
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-12345")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", rr.Code)
	}

	body := strings.TrimSpace(rr.Body.String())
	if !strings.Contains(body, "Invalid or unknown token") {
		t.Errorf("Expected 'Invalid or unknown token' in response, got %q", body)
	}
}

// TestAuthMiddleware_ValidToken tests successful authentication with valid token
func TestAuthMiddleware_ValidToken(t *testing.T) {
	store, cleanup := createTestAuthStore(t)
	defer cleanup()

	// Create a service token
	rawToken, _, err := store.CreateServiceToken("Test token", nil, "", false)
	if err != nil {
		t.Fatalf("Failed to create service token: %v", err)
	}

	middleware := AuthMiddleware(store, true)

	var capturedContext context.Context
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContext = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("authenticated"))
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK for valid token, got %d", rr.Code)
	}

	if body := rr.Body.String(); body != "authenticated" {
		t.Errorf("Expected 'authenticated', got %q", body)
	}

	// Verify token hash was stored in context
	ctxHash := GetTokenHashFromContext(capturedContext)
	if ctxHash == "" {
		t.Error("Expected token hash in context, got empty string")
	}
}

// TestAuthMiddleware_ExpiredToken tests rejection of expired tokens
func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	// Create temp directory for this test
	tmpDir, err := os.MkdirTemp("", "auth-expired-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()

	// Create a token that expires immediately
	expiryTime := time.Now().Add(1 * time.Millisecond)
	rawToken, _, err := store.CreateServiceToken("Test expired token", &expiryTime, "", false)
	if err != nil {
		t.Fatalf("Failed to create service token: %v", err)
	}

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	middleware := AuthMiddleware(store, true)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for expired token")
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized for expired token, got %d", rr.Code)
	}

	body := strings.TrimSpace(rr.Body.String())
	// Should get generic "Invalid or unknown token" message, not internal error details
	if !strings.Contains(body, "Invalid or unknown token") {
		t.Errorf("Expected 'Invalid or unknown token' in response, got %q", body)
	}
}

// TestGetTokenHashFromContext tests retrieving token hash from context
func TestGetTokenHashFromContext(t *testing.T) {
	t.Run("with token hash", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), TokenHashContextKey, "test-hash-123")
		hash := GetTokenHashFromContext(ctx)
		if hash != "test-hash-123" {
			t.Errorf("Expected 'test-hash-123', got %q", hash)
		}
	})

	t.Run("without token hash", func(t *testing.T) {
		ctx := context.Background()
		hash := GetTokenHashFromContext(ctx)
		if hash != "" {
			t.Errorf("Expected empty string for missing token hash, got %q", hash)
		}
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), TokenHashContextKey, 12345)
		hash := GetTokenHashFromContext(ctx)
		if hash != "" {
			t.Errorf("Expected empty string for wrong type, got %q", hash)
		}
	})
}

// TestAuthMiddleware_ValidSessionToken tests successful authentication with session token
func TestAuthMiddleware_ValidSessionToken(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "auth-session-test")
	os.MkdirAll(tmpDir, 0750)
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()

	// Create a user
	err = store.CreateUser("testuser", "testpass123", "Test user")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Authenticate to get session token
	sessionToken, _, err := store.AuthenticateUser("testuser", "testpass123")
	if err != nil {
		t.Fatalf("Failed to authenticate user: %v", err)
	}

	middleware := AuthMiddleware(store, true)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("session authenticated"))
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK for valid session token, got %d", rr.Code)
	}

	if body := rr.Body.String(); body != "session authenticated" {
		t.Errorf("Expected 'session authenticated', got %q", body)
	}
}
