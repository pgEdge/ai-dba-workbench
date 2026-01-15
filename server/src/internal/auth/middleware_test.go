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

// TestExtractIPAddress_Deprecated tests the deprecated function still works
func TestExtractIPAddress_Deprecated(t *testing.T) {
	t.Run("uses X-Forwarded-For first", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")

		ip := ExtractIPAddress(req)
		if ip != "203.0.113.50" {
			t.Errorf("Expected '203.0.113.50', got %q", ip)
		}
	})

	t.Run("falls back to X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Real-IP", "203.0.113.50")

		ip := ExtractIPAddress(req)
		if ip != "203.0.113.50" {
			t.Errorf("Expected '203.0.113.50', got %q", ip)
		}
	})

	t.Run("falls back to RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"

		ip := ExtractIPAddress(req)
		if ip != "10.0.0.1" {
			t.Errorf("Expected '10.0.0.1', got %q", ip)
		}
	})
}
