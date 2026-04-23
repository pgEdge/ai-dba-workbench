/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
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

	"golang.org/x/crypto/bcrypt"
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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

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
	if !strings.Contains(body, "Missing or invalid authentication credentials") {
		t.Errorf("Expected 'Missing or invalid authentication credentials' in response, got %q", body)
	}
}

// TestAuthMiddleware_MalformedAuthHeader tests rejection of malformed Authorization headers
func TestAuthMiddleware_MalformedAuthHeader(t *testing.T) {
	testCases := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "sometoken123"},
		{"wrong prefix", "Basic sometoken123"},
		{"missing token", "Bearer"},
		{"empty token", "Bearer "},
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
			// ExtractBearerToken returns "" for all malformed headers,
			// so the middleware responds with the missing credentials message.
			if !strings.Contains(body, "Missing or invalid authentication credentials") {
				t.Errorf("Expected 'Missing or invalid authentication credentials' in response, got %q", body)
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

	// Create a user and token
	store.CreateUser("tokenuser", "Password1", "", "", "")
	rawToken, _, err := store.CreateToken("tokenuser", "Test token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a user and a token that expires immediately
	store.CreateUser("tokenuser", "Password1", "", "", "")
	expiryTime := time.Now().Add(1 * time.Millisecond)
	rawToken, _, err := store.CreateToken("tokenuser", "Test expired token", &expiryTime)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
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
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a user
	err = store.CreateUser("testuser", "Testpass123", "Test user", "", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Authenticate to get session token
	sessionToken, _, err := store.AuthenticateUser("testuser", "Testpass123")
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

// TestNewIPExtractor tests creating an IPExtractor with various CIDR inputs
func TestNewIPExtractor(t *testing.T) {
	t.Run("valid CIDRs", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8", "192.168.0.0/16"})
		if len(extractor.TrustedProxies) != 2 {
			t.Errorf("Expected 2 trusted proxies, got %d", len(extractor.TrustedProxies))
		}
	})

	t.Run("valid single IPs", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.1", "192.168.1.1"})
		if len(extractor.TrustedProxies) != 2 {
			t.Errorf("Expected 2 trusted proxies from single IPs, got %d", len(extractor.TrustedProxies))
		}
	})

	t.Run("invalid CIDRs ignored", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8", "invalid", "192.168.0.0/16"})
		if len(extractor.TrustedProxies) != 2 {
			t.Errorf("Expected 2 trusted proxies (invalid ignored), got %d", len(extractor.TrustedProxies))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		extractor := NewIPExtractor([]string{})
		if len(extractor.TrustedProxies) != 0 {
			t.Errorf("Expected 0 trusted proxies, got %d", len(extractor.TrustedProxies))
		}
	})

	t.Run("nil input", func(t *testing.T) {
		extractor := NewIPExtractor(nil)
		if len(extractor.TrustedProxies) != 0 {
			t.Errorf("Expected 0 trusted proxies, got %d", len(extractor.TrustedProxies))
		}
	})
}

// TestIPExtractor_ExtractIP tests secure IP extraction with various scenarios
func TestIPExtractor_ExtractIP(t *testing.T) {
	// Test case: no trusted proxies - always use RemoteAddr
	t.Run("no trusted proxies uses RemoteAddr", func(t *testing.T) {
		extractor := NewIPExtractor(nil)
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.50:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")

		ip := extractor.ExtractIP(req)
		if ip != "203.0.113.50" {
			t.Errorf("Expected '203.0.113.50' (RemoteAddr), got %q", ip)
		}
	})

	// Test case: request not from trusted proxy - ignore X-Forwarded-For
	t.Run("untrusted source ignores XFF", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.50:12345" // Not in trusted range
		req.Header.Set("X-Forwarded-For", "192.168.1.100")

		ip := extractor.ExtractIP(req)
		if ip != "203.0.113.50" {
			t.Errorf("Expected '203.0.113.50' (ignore spoofed XFF), got %q", ip)
		}
	})

	// Test case: request from trusted proxy - use rightmost untrusted IP
	t.Run("trusted proxy uses rightmost untrusted IP", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345" // Trusted proxy
		// Attacker could prepend fake IPs on left, but we use rightmost untrusted
		req.Header.Set("X-Forwarded-For", "1.1.1.1, 192.168.1.100, 10.0.0.2")

		ip := extractor.ExtractIP(req)
		// Should get 192.168.1.100 (rightmost non-trusted IP)
		if ip != "192.168.1.100" {
			t.Errorf("Expected '192.168.1.100' (rightmost untrusted), got %q", ip)
		}
	})

	// Test case: single IP in X-Forwarded-For from trusted proxy
	t.Run("trusted proxy single IP in XFF", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.50")

		ip := extractor.ExtractIP(req)
		if ip != "203.0.113.50" {
			t.Errorf("Expected '203.0.113.50', got %q", ip)
		}
	})

	// Test case: no X-Forwarded-For, check X-Real-IP from trusted proxy
	t.Run("trusted proxy uses X-Real-IP", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Real-IP", "203.0.113.50")

		ip := extractor.ExtractIP(req)
		if ip != "203.0.113.50" {
			t.Errorf("Expected '203.0.113.50' from X-Real-IP, got %q", ip)
		}
	})

	// Test case: trusted proxy with no forwarding headers
	t.Run("trusted proxy no headers", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"

		ip := extractor.ExtractIP(req)
		if ip != "10.0.0.1" {
			t.Errorf("Expected '10.0.0.1', got %q", ip)
		}
	})

	// Test case: IPv6 addresses
	t.Run("IPv6 addresses", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"::1/128"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "[::1]:12345"
		req.Header.Set("X-Forwarded-For", "2001:db8::1")

		ip := extractor.ExtractIP(req)
		if ip != "2001:db8::1" {
			t.Errorf("Expected '2001:db8::1', got %q", ip)
		}
	})

	// Test case: all IPs in chain are trusted (edge case)
	t.Run("all IPs trusted returns leftmost", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8", "192.168.0.0/16"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.5, 192.168.1.1, 10.0.0.2")

		ip := extractor.ExtractIP(req)
		// All are trusted, should return leftmost
		if ip != "10.0.0.5" {
			t.Errorf("Expected '10.0.0.5' (leftmost when all trusted), got %q", ip)
		}
	})

	// Test case: malformed X-Forwarded-For entries
	t.Run("malformed XFF entries skipped", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "not-an-ip, , 203.0.113.50")

		ip := extractor.ExtractIP(req)
		if ip != "203.0.113.50" {
			t.Errorf("Expected '203.0.113.50', got %q", ip)
		}
	})
}

// TestIPExtractor_SecurityScenarios tests specific attack prevention scenarios
func TestIPExtractor_SecurityScenarios(t *testing.T) {
	// Scenario: Attacker tries to spoof IP to bypass rate limiting
	t.Run("spoofing attempt blocked when not from proxy", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"}) // Only internal IPs trusted
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.100:12345"         // Attacker's real IP (not trusted)
		req.Header.Set("X-Forwarded-For", "127.0.0.1") // Attacker tries to spoof localhost

		ip := extractor.ExtractIP(req)
		if ip != "203.0.113.100" {
			t.Errorf("Expected attacker's real IP '203.0.113.100', got %q", ip)
		}
	})

	// Scenario: Attacker prepends fake IPs to X-Forwarded-For
	t.Run("prepended fake IPs ignored", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345" // Request through trusted proxy
		// Attacker's request had: X-Forwarded-For: 8.8.8.8
		// Proxy appends their real IP: X-Forwarded-For: 8.8.8.8, 203.0.113.100
		// Proxy forwards to another proxy: X-Forwarded-For: 8.8.8.8, 203.0.113.100, 10.0.0.2
		req.Header.Set("X-Forwarded-For", "8.8.8.8, 203.0.113.100, 10.0.0.2")

		ip := extractor.ExtractIP(req)
		// Should get 203.0.113.100 (rightmost non-trusted), not 8.8.8.8 (spoofed by attacker)
		if ip != "203.0.113.100" {
			t.Errorf("Expected real client IP '203.0.113.100', got %q", ip)
		}
	})
}

// TestExtractIPFromRemoteAddr tests the helper function for parsing RemoteAddr
func TestExtractIPFromRemoteAddr(t *testing.T) {
	testCases := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"IPv4 with port", "192.168.1.1:8080", "192.168.1.1"},
		{"IPv6 with port", "[::1]:8080", "::1"},
		{"IPv4 without port", "192.168.1.1", "192.168.1.1"},
		{"IPv6 without port brackets", "::1", "::1"},
		{"empty string", "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractIPFromRemoteAddr(tc.remoteAddr)
			if result != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, result)
			}
		})
	}
}

// =============================================================================
// Additional Context Helper Tests
// =============================================================================

func TestGetIPAddressFromContext(t *testing.T) {
	t.Run("with IP address", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IPAddressContextKey, "192.168.1.100")
		ip := GetIPAddressFromContext(ctx)
		if ip != "192.168.1.100" {
			t.Errorf("Expected '192.168.1.100', got %q", ip)
		}
	})

	t.Run("without IP address", func(t *testing.T) {
		ctx := context.Background()
		ip := GetIPAddressFromContext(ctx)
		if ip != "" {
			t.Errorf("Expected empty string for missing IP, got %q", ip)
		}
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IPAddressContextKey, 12345)
		ip := GetIPAddressFromContext(ctx)
		if ip != "" {
			t.Errorf("Expected empty string for wrong type, got %q", ip)
		}
	})
}

func TestGetUsernameFromContext(t *testing.T) {
	t.Run("with username", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), UsernameContextKey, "testuser")
		username := GetUsernameFromContext(ctx)
		if username != "testuser" {
			t.Errorf("Expected 'testuser', got %q", username)
		}
	})

	t.Run("without username", func(t *testing.T) {
		ctx := context.Background()
		username := GetUsernameFromContext(ctx)
		if username != "" {
			t.Errorf("Expected empty string for missing username, got %q", username)
		}
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), UsernameContextKey, 12345)
		username := GetUsernameFromContext(ctx)
		if username != "" {
			t.Errorf("Expected empty string for wrong type, got %q", username)
		}
	})
}

func TestIsAPITokenFromContext(t *testing.T) {
	t.Run("true value", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IsAPITokenContextKey, true)
		if !IsAPITokenFromContext(ctx) {
			t.Error("Expected true")
		}
	})

	t.Run("false value", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IsAPITokenContextKey, false)
		if IsAPITokenFromContext(ctx) {
			t.Error("Expected false")
		}
	})

	t.Run("not set", func(t *testing.T) {
		ctx := context.Background()
		if IsAPITokenFromContext(ctx) {
			t.Error("Expected false when not set")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IsAPITokenContextKey, "true")
		if IsAPITokenFromContext(ctx) {
			t.Error("Expected false for wrong type")
		}
	})
}

func TestGetUserIDFromContext(t *testing.T) {
	t.Run("with user ID", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), UserIDContextKey, int64(42))
		userID := GetUserIDFromContext(ctx)
		if userID != 42 {
			t.Errorf("Expected 42, got %d", userID)
		}
	})

	t.Run("without user ID", func(t *testing.T) {
		ctx := context.Background()
		userID := GetUserIDFromContext(ctx)
		if userID != 0 {
			t.Errorf("Expected 0 for missing user ID, got %d", userID)
		}
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), UserIDContextKey, 42) // int, not int64
		userID := GetUserIDFromContext(ctx)
		if userID != 0 {
			t.Errorf("Expected 0 for wrong type, got %d", userID)
		}
	})
}

func TestGetTokenIDFromContext(t *testing.T) {
	t.Run("with token ID", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), TokenIDContextKey, int64(123))
		tokenID := GetTokenIDFromContext(ctx)
		if tokenID != 123 {
			t.Errorf("Expected 123, got %d", tokenID)
		}
	})

	t.Run("without token ID", func(t *testing.T) {
		ctx := context.Background()
		tokenID := GetTokenIDFromContext(ctx)
		if tokenID != 0 {
			t.Errorf("Expected 0 for missing token ID, got %d", tokenID)
		}
	})

	t.Run("with wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), TokenIDContextKey, "123")
		tokenID := GetTokenIDFromContext(ctx)
		if tokenID != 0 {
			t.Errorf("Expected 0 for wrong type, got %d", tokenID)
		}
	})
}

func TestIsSuperuserFromContext(t *testing.T) {
	t.Run("true value", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IsSuperuserContextKey, true)
		if !IsSuperuserFromContext(ctx) {
			t.Error("Expected true")
		}
	})

	t.Run("false value", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IsSuperuserContextKey, false)
		if IsSuperuserFromContext(ctx) {
			t.Error("Expected false")
		}
	})

	t.Run("not set", func(t *testing.T) {
		ctx := context.Background()
		if IsSuperuserFromContext(ctx) {
			t.Error("Expected false when not set")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), IsSuperuserContextKey, 1)
		if IsSuperuserFromContext(ctx) {
			t.Error("Expected false for wrong type")
		}
	})
}

// =============================================================================
// Additional Middleware Tests
// =============================================================================

func TestAuthMiddleware_NilAuthStore(t *testing.T) {
	middleware := AuthMiddleware(nil, true)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should pass through when auth store is nil
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK when auth store is nil, got %d", rr.Code)
	}
}

func TestAuthMiddleware_PublicEndpoints(t *testing.T) {
	store, cleanup := createTestAuthStore(t)
	defer cleanup()

	middleware := AuthMiddleware(store, true)

	publicPaths := []string{
		HealthCheckPath,
		UserInfoPath,
		"/api/v1/auth/login",
		"/api/v1/auth/logout",
		LLMProvidersPath,
		LLMModelsPath,
	}

	for _, path := range publicPaths {
		t.Run(path, func(t *testing.T) {
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected status OK for public endpoint %s, got %d", path, rr.Code)
			}
		})
	}
}

func TestAuthMiddleware_SessionCookie(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "auth-cookie-test")
	os.MkdirAll(tmpDir, 0750)
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a user and get session token
	store.CreateUser("testuser", "Testpass123", "Test user", "", "")
	sessionToken, _, err := store.AuthenticateUser("testuser", "Testpass123")
	if err != nil {
		t.Fatalf("Failed to authenticate user: %v", err)
	}

	middleware := AuthMiddleware(store, true)

	var capturedContext context.Context
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContext = r.Context()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_token",
		Value: sessionToken,
	})
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK for valid session cookie, got %d", rr.Code)
	}

	// Verify context values
	username := GetUsernameFromContext(capturedContext)
	if username != "testuser" {
		t.Errorf("Expected username 'testuser', got %q", username)
	}

	isAPIToken := IsAPITokenFromContext(capturedContext)
	if isAPIToken {
		t.Error("Expected IsAPIToken to be false for session token")
	}
}

func TestAuthMiddleware_EmptySessionCookie(t *testing.T) {
	store, cleanup := createTestAuthStore(t)
	defer cleanup()

	middleware := AuthMiddleware(store, true)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for empty session cookie")
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_token",
		Value: "",
	})
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized for empty cookie, got %d", rr.Code)
	}
}

func TestAuthMiddleware_APITokenWithOwner(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "auth-owner-test")
	os.MkdirAll(tmpDir, 0750)
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a user and a token
	store.CreateUser("testuser", "Testpass123", "Test user", "", "")
	rawToken, _, err := store.CreateToken("testuser", "User API token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	middleware := AuthMiddleware(store, true)

	var capturedContext context.Context
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContext = r.Context()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK for valid user token, got %d", rr.Code)
	}

	// Verify context values
	isAPIToken := IsAPITokenFromContext(capturedContext)
	if !isAPIToken {
		t.Error("Expected IsAPIToken to be true for user token")
	}

	userID := GetUserIDFromContext(capturedContext)
	if userID == 0 {
		t.Error("Expected non-zero user ID for user-owned token")
	}
}

func TestAuthMiddleware_SuperuserToken(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "auth-superuser-test")
	os.MkdirAll(tmpDir, 0750)
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a superuser and a token for them
	store.CreateUser("superuser-svc", "Testpass123", "Superuser service", "", "")
	store.SetUserSuperuser("superuser-svc", true)
	rawToken, _, err := store.CreateToken("superuser-svc", "Superuser token", nil)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	middleware := AuthMiddleware(store, true)

	var capturedContext context.Context
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContext = r.Context()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK for valid superuser token, got %d", rr.Code)
	}

	// Verify superuser context
	isSuperuser := IsSuperuserFromContext(capturedContext)
	if !isSuperuser {
		t.Error("Expected IsSuperuser to be true for superuser token")
	}
}

func TestAuthMiddleware_SuperuserSessionUser(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "auth-superuser-session-test")
	os.MkdirAll(tmpDir, 0750)
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer store.Close()
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	// Create a superuser
	store.CreateUser("superuser", "Testpass123", "Superuser", "", "")
	store.SetUserSuperuser("superuser", true)

	sessionToken, _, err := store.AuthenticateUser("superuser", "Testpass123")
	if err != nil {
		t.Fatalf("Failed to authenticate user: %v", err)
	}

	middleware := AuthMiddleware(store, true)

	var capturedContext context.Context
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContext = r.Context()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/mcp/v1", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK for superuser session, got %d", rr.Code)
	}

	// Verify superuser context
	isSuperuser := IsSuperuserFromContext(capturedContext)
	if !isSuperuser {
		t.Error("Expected IsSuperuser to be true for superuser session")
	}
}

// =============================================================================
// IPv6 IPExtractor Tests
// =============================================================================

func TestNewIPExtractor_IPv6(t *testing.T) {
	t.Run("valid IPv6 CIDR", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"2001:db8::/32"})
		if len(extractor.TrustedProxies) != 1 {
			t.Errorf("Expected 1 trusted proxy, got %d", len(extractor.TrustedProxies))
		}
	})

	t.Run("valid IPv6 single address", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"2001:db8::1"})
		if len(extractor.TrustedProxies) != 1 {
			t.Errorf("Expected 1 trusted proxy from IPv6 address, got %d", len(extractor.TrustedProxies))
		}
	})
}

func TestIPExtractor_ExtractIP_EdgeCases(t *testing.T) {
	t.Run("empty X-Forwarded-For header", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "")

		ip := extractor.ExtractIP(req)
		if ip != "10.0.0.1" {
			t.Errorf("Expected '10.0.0.1', got %q", ip)
		}
	})

	t.Run("invalid IP in RemoteAddr", func(t *testing.T) {
		extractor := NewIPExtractor(nil)
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "invalid-address"

		ip := extractor.ExtractIP(req)
		// Should return the raw RemoteAddr when it cannot be parsed
		if ip != "invalid-address" {
			t.Errorf("Expected 'invalid-address', got %q", ip)
		}
	})

	t.Run("nil IP check in isTrustedProxy", func(t *testing.T) {
		extractor := NewIPExtractor([]string{"10.0.0.0/8"})
		if extractor.isTrustedProxy(nil) {
			t.Error("Expected false for nil IP")
		}
	})
}
