/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

func TestAuthHandler_HandleLogin(t *testing.T) {
	// Create temp directory for auth store
	tmpDir, err := os.MkdirTemp("", "auth-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create auth store
	authStore, err := auth.NewAuthStore(tmpDir, 30, 5)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer authStore.Close()

	// Create a test user
	if err := authStore.CreateUser("testuser", "testpass123", "Test user"); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create rate limiter
	rateLimiter := auth.NewRateLimiter(15, 5) // 15 minute window, 5 max attempts
	defer rateLimiter.Stop()

	// Create handler
	handler := NewAuthHandler(authStore, rateLimiter)

	tests := []struct {
		name           string
		method         string
		body           interface{}
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "successful login",
			method:         http.MethodPost,
			body:           LoginRequest{Username: "testuser", Password: "testpass123"},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp LoginResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if !resp.Success {
					t.Error("Expected success to be true")
				}
				if resp.SessionToken == "" {
					t.Error("Expected non-empty session token")
				}
				if resp.ExpiresAt == "" {
					t.Error("Expected non-empty expiration time")
				}
				if resp.Message != "Authentication successful" {
					t.Errorf("Expected message 'Authentication successful', got '%s'", resp.Message)
				}
			},
		},
		{
			name:           "wrong password",
			method:         http.MethodPost,
			body:           LoginRequest{Username: "testuser", Password: "wrongpass"},
			expectedStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.Error == "" {
					t.Error("Expected non-empty error message")
				}
			},
		},
		{
			name:           "nonexistent user",
			method:         http.MethodPost,
			body:           LoginRequest{Username: "nouser", Password: "testpass123"},
			expectedStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.Error == "" {
					t.Error("Expected non-empty error message")
				}
			},
		},
		{
			name:           "missing username",
			method:         http.MethodPost,
			body:           LoginRequest{Username: "", Password: "testpass123"},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.Error != "Username is required" {
					t.Errorf("Expected error 'Username is required', got '%s'", resp.Error)
				}
			},
		},
		{
			name:           "missing password",
			method:         http.MethodPost,
			body:           LoginRequest{Username: "testuser", Password: ""},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.Error != "Password is required" {
					t.Errorf("Expected error 'Password is required', got '%s'", resp.Error)
				}
			},
		},
		{
			name:           "method not allowed",
			method:         http.MethodGet,
			body:           nil,
			expectedStatus: http.StatusMethodNotAllowed,
			checkResponse:  nil,
		},
		{
			name:           "invalid json",
			method:         http.MethodPost,
			body:           "invalid json",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.Error != "Invalid request body" {
					t.Errorf("Expected error 'Invalid request body', got '%s'", resp.Error)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.body != nil {
				switch v := tt.body.(type) {
				case string:
					body = []byte(v)
				default:
					body, _ = json.Marshal(tt.body)
				}
			}

			req := httptest.NewRequest(tt.method, "/api/v1/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.handleLogin(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestAuthHandler_NilAuthStore(t *testing.T) {
	// Create handler with nil auth store
	handler := NewAuthHandler(nil, nil)

	body, _ := json.Marshal(LoginRequest{Username: "test", Password: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.handleLogin(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp.Error != "User authentication is not configured" {
		t.Errorf("Expected error about unconfigured auth, got '%s'", resp.Error)
	}
}

func TestAuthHandler_RegisterRoutes(t *testing.T) {
	handler := NewAuthHandler(nil, nil)
	mux := http.NewServeMux()

	handler.RegisterRoutes(mux)

	// Test that the route is registered
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	// Should get a response (even if it's an error, not a 404)
	if w.Code == http.StatusNotFound {
		t.Error("Expected /api/v1/auth/login to be registered")
	}
}

func TestExtractIPFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "X-Forwarded-For header",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.1"},
			remoteAddr: "10.0.0.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Real-IP header",
			headers:    map[string]string{"X-Real-IP": "192.168.2.2"},
			remoteAddr: "10.0.0.1:12345",
			expected:   "192.168.2.2",
		},
		{
			name:       "RemoteAddr fallback",
			headers:    map[string]string{},
			remoteAddr: "10.0.0.1:12345",
			expected:   "10.0.0.1:12345",
		},
		{
			name:       "X-Forwarded-For takes precedence",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.1", "X-Real-IP": "192.168.2.2"},
			remoteAddr: "10.0.0.1:12345",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := extractIPFromRequest(req)
			if ip != tt.expected {
				t.Errorf("Expected IP '%s', got '%s'", tt.expected, ip)
			}
		})
	}
}

func TestAuthHandler_RateLimiting(t *testing.T) {
	// Create temp directory for auth store
	tmpDir, err := os.MkdirTemp("", "auth-ratelimit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create auth store
	authStore, err := auth.NewAuthStore(filepath.Join(tmpDir, "auth"), 30, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer authStore.Close()

	// Create a test user
	if err := authStore.CreateUser("testuser", "testpass123", "Test user"); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create rate limiter with very low limit for testing
	rateLimiter := auth.NewRateLimiter(15, 2) // 2 max attempts
	defer rateLimiter.Stop()

	handler := NewAuthHandler(authStore, rateLimiter)

	// Make failed attempts to trigger rate limit
	for i := 0; i < 3; i++ {
		body, _ := json.Marshal(LoginRequest{Username: "testuser", Password: "wrongpass"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()

		handler.handleLogin(w, req)

		if i < 2 {
			// First two attempts should be Unauthorized
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Attempt %d: expected status %d, got %d", i+1, http.StatusUnauthorized, w.Code)
			}
		} else {
			// Third attempt should be rate limited
			if w.Code != http.StatusTooManyRequests {
				t.Errorf("Attempt %d: expected rate limit status %d, got %d", i+1, http.StatusTooManyRequests, w.Code)
			}
		}
	}

	// Reset rate limit and verify successful login works
	rateLimiter.Reset("192.168.1.100:12345")

	body, _ := json.Marshal(LoginRequest{Username: "testuser", Password: "testpass123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()

	handler.handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("After reset: expected status %d, got %d", http.StatusOK, w.Code)
	}
}
