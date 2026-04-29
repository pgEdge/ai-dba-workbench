/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
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
	"golang.org/x/crypto/bcrypt"
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
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a test user
	if err := authStore.CreateUser("testuser", "Testpass1234", "Test user", "", ""); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create rate limiter
	rateLimiter := auth.NewRateLimiter(15, 5) // 15 minute window, 5 max attempts
	defer rateLimiter.Stop()

	// Create handler (tlsEnabled=false for tests)
	handler := NewAuthHandler(authStore, rateLimiter, nil, false)
	defer handler.Close()

	tests := []struct {
		name           string
		method         string
		body           any
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "successful login",
			method:         http.MethodPost,
			body:           LoginRequest{Username: "testuser", Password: "Testpass1234"},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp LoginResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if !resp.Success {
					t.Error("Expected success to be true")
				}
				if resp.ExpiresAt == "" {
					t.Error("Expected non-empty expiration time")
				}
				if resp.Message != "Authentication successful" {
					t.Errorf("Expected message 'Authentication successful', got '%s'", resp.Message)
				}
				// Verify session token is in httpOnly cookie, not response body
				cookies := w.Result().Cookies()
				var sessionCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == SessionCookieName {
						sessionCookie = c
						break
					}
				}
				if sessionCookie == nil {
					t.Error("Expected session_token cookie to be set")
				} else {
					if sessionCookie.Value == "" {
						t.Error("Expected non-empty session token in cookie")
					}
					if !sessionCookie.HttpOnly {
						t.Error("Expected session cookie to be httpOnly")
					}
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
			body:           LoginRequest{Username: "nouser", Password: "Testpass1234"},
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
			body:           LoginRequest{Username: "", Password: "Testpass1234"},
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
	// Create handler with nil auth store (tlsEnabled=false for tests)
	handler := NewAuthHandler(nil, nil, nil, false)
	defer handler.Close()

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
	handler := NewAuthHandler(nil, nil, nil, false)
	defer handler.Close()
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

func TestAuthHandler_ExtractIPFromRequest(t *testing.T) {
	// Test the secure IP extraction behavior:
	// - Without an IPExtractor, it should use RemoteAddr directly (safe default)
	// - With an IPExtractor that has trusted proxies, it should extract from headers when appropriate

	t.Run("without IPExtractor - uses RemoteAddr directly", func(t *testing.T) {
		handler := NewAuthHandler(nil, nil, nil, false)
		defer handler.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "192.168.1.1")

		// Without IPExtractor, should return raw RemoteAddr (safe default)
		ip := handler.extractIPFromRequest(req)
		if ip != "10.0.0.1:12345" {
			t.Errorf("Expected RemoteAddr '10.0.0.1:12345', got '%s'", ip)
		}
	})

	t.Run("with IPExtractor and trusted proxy", func(t *testing.T) {
		// Create IPExtractor that trusts 10.0.0.0/8 range
		extractor := auth.NewIPExtractor([]string{"10.0.0.0/8"})
		handler := NewAuthHandler(nil, nil, extractor, false)
		defer handler.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:12345" // From trusted proxy
		req.Header.Set("X-Forwarded-For", "192.168.1.1")

		// With trusted proxy, should extract client IP from X-Forwarded-For
		ip := handler.extractIPFromRequest(req)
		if ip != "192.168.1.1" {
			t.Errorf("Expected forwarded IP '192.168.1.1', got '%s'", ip)
		}
	})

	t.Run("with IPExtractor but untrusted proxy", func(t *testing.T) {
		// Create IPExtractor that trusts a different range
		extractor := auth.NewIPExtractor([]string{"172.16.0.0/12"})
		handler := NewAuthHandler(nil, nil, extractor, false)
		defer handler.Close()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:12345" // Not a trusted proxy
		req.Header.Set("X-Forwarded-For", "192.168.1.1")

		// With untrusted proxy, should return direct connection IP (not X-Forwarded-For)
		ip := handler.extractIPFromRequest(req)
		if ip != "10.0.0.1" {
			t.Errorf("Expected direct IP '10.0.0.1', got '%s'", ip)
		}
	})
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
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a test user
	if err := authStore.CreateUser("testuser", "Testpass1234", "Test user", "", ""); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create rate limiter with very low limit for testing
	rateLimiter := auth.NewRateLimiter(15, 2) // 2 max attempts
	defer rateLimiter.Stop()

	handler := NewAuthHandler(authStore, rateLimiter, nil, false)
	defer handler.Close()

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

	body, _ := json.Marshal(LoginRequest{Username: "testuser", Password: "Testpass1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()

	handler.handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("After reset: expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestAuthHandler_SecureCookieFlag(t *testing.T) {
	// Create temp directory for auth store
	tmpDir, err := os.MkdirTemp("", "auth-secure-cookie-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create auth store
	authStore, err := auth.NewAuthStore(tmpDir, 30, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a test user
	if err := authStore.CreateUser("testuser", "Testpass1234", "Test user", "", ""); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name         string
		tlsEnabled   bool
		expectSecure bool
	}{
		{
			name:         "TLS disabled - cookie should not be secure",
			tlsEnabled:   false,
			expectSecure: false,
		},
		{
			name:         "TLS enabled - cookie should be secure",
			tlsEnabled:   true,
			expectSecure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewAuthHandler(authStore, nil, nil, tt.tlsEnabled)
			defer handler.Close()

			body, _ := json.Marshal(LoginRequest{Username: "testuser", Password: "Testpass1234"})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.handleLogin(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
			}

			// Check the session cookie's Secure flag
			cookies := w.Result().Cookies()
			var sessionCookie *http.Cookie
			for _, c := range cookies {
				if c.Name == SessionCookieName {
					sessionCookie = c
					break
				}
			}

			if sessionCookie == nil {
				t.Fatal("Expected session_token cookie to be set")
			}

			if sessionCookie.Secure != tt.expectSecure {
				t.Errorf("Expected cookie Secure=%v, got %v", tt.expectSecure, sessionCookie.Secure)
			}
		})
	}
}

func TestAuthHandler_SecureCookieAutoDetect(t *testing.T) {
	// Test auto-detection of secure requests via X-Forwarded-Proto header
	// This simulates a reverse proxy that terminates TLS and forwards requests
	tmpDir, err := os.MkdirTemp("", "auth-secure-auto-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	authStore, err := auth.NewAuthStore(tmpDir, 30, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := authStore.CreateUser("testuser", "Testpass1234", "Test user", "", ""); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name           string
		tlsEnabled     bool
		useIPExtractor bool
		forwardedProto string
		expectSecure   bool
	}{
		{
			name:           "TLS disabled, no proxy, no header - not secure",
			tlsEnabled:     false,
			useIPExtractor: false,
			forwardedProto: "",
			expectSecure:   false,
		},
		{
			name:           "TLS disabled, no proxy, https header ignored - not secure",
			tlsEnabled:     false,
			useIPExtractor: false,
			forwardedProto: "https",
			expectSecure:   false, // Header ignored without IPExtractor
		},
		{
			name:           "TLS disabled, with proxy, https header - secure",
			tlsEnabled:     false,
			useIPExtractor: true,
			forwardedProto: "https",
			expectSecure:   true,
		},
		{
			name:           "TLS disabled, with proxy, http header - not secure",
			tlsEnabled:     false,
			useIPExtractor: true,
			forwardedProto: "http",
			expectSecure:   false,
		},
		{
			name:           "TLS disabled, with proxy, no header - not secure",
			tlsEnabled:     false,
			useIPExtractor: true,
			forwardedProto: "",
			expectSecure:   false,
		},
		{
			name:           "TLS enabled - always secure regardless of header",
			tlsEnabled:     true,
			useIPExtractor: false,
			forwardedProto: "",
			expectSecure:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ipExtractor *auth.IPExtractor
			if tt.useIPExtractor {
				// Create an IPExtractor - its presence indicates we trust proxy headers
				ipExtractor = auth.NewIPExtractor([]string{"10.0.0.0/8"})
			}

			handler := NewAuthHandler(authStore, nil, ipExtractor, tt.tlsEnabled)
			defer handler.Close()

			body, _ := json.Marshal(LoginRequest{Username: "testuser", Password: "Testpass1234"})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tt.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwardedProto)
			}
			w := httptest.NewRecorder()

			handler.handleLogin(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
			}

			cookies := w.Result().Cookies()
			var sessionCookie *http.Cookie
			for _, c := range cookies {
				if c.Name == SessionCookieName {
					sessionCookie = c
					break
				}
			}

			if sessionCookie == nil {
				t.Fatal("Expected session_token cookie to be set")
			}

			if sessionCookie.Secure != tt.expectSecure {
				t.Errorf("Expected cookie Secure=%v, got %v", tt.expectSecure, sessionCookie.Secure)
			}
		})
	}
}

func TestAuthHandler_SecureCookieLogout(t *testing.T) {
	// Test that logout also respects the secure cookie flag
	tests := []struct {
		name         string
		tlsEnabled   bool
		expectSecure bool
	}{
		{
			name:         "TLS disabled - logout cookie should not be secure",
			tlsEnabled:   false,
			expectSecure: false,
		},
		{
			name:         "TLS enabled - logout cookie should be secure",
			tlsEnabled:   true,
			expectSecure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewAuthHandler(nil, nil, nil, tt.tlsEnabled)
			defer handler.Close()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
			w := httptest.NewRecorder()

			handler.handleLogout(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
			}

			// Check the session cookie's Secure flag on the cleared cookie
			cookies := w.Result().Cookies()
			var sessionCookie *http.Cookie
			for _, c := range cookies {
				if c.Name == SessionCookieName {
					sessionCookie = c
					break
				}
			}

			if sessionCookie == nil {
				t.Fatal("Expected session_token cookie to be set (for clearing)")
			}

			if sessionCookie.Secure != tt.expectSecure {
				t.Errorf("Expected cookie Secure=%v, got %v", tt.expectSecure, sessionCookie.Secure)
			}
		})
	}
}
