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
	"net/http"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// MaxRequestBodySize is the maximum size for request bodies (1MB)
const MaxRequestBodySize = 1 << 20

// SessionCookieName is the name of the httpOnly cookie used for session tokens.
// Using httpOnly cookies prevents XSS attacks from accessing the token.
const SessionCookieName = "session_token"

// AuthHandler handles authentication-related HTTP requests
type AuthHandler struct {
	authStore    *auth.AuthStore
	rateLimiter  *auth.RateLimiter
	ipExtractor  *auth.IPExtractor
	secureCookie bool // Whether to set Secure flag on cookies (true for HTTPS)
}

// NewAuthHandler creates a new authentication handler.
// The ipExtractor parameter is optional; if nil, RemoteAddr will be used directly.
// The secureCookie parameter should be true when serving over HTTPS.
func NewAuthHandler(authStore *auth.AuthStore, rateLimiter *auth.RateLimiter, ipExtractor *auth.IPExtractor) *AuthHandler {
	return &AuthHandler{
		authStore:    authStore,
		rateLimiter:  rateLimiter,
		ipExtractor:  ipExtractor,
		secureCookie: false, // Default to false for development; set via SetSecureCookie for production
	}
}

// SetSecureCookie configures whether cookies should have the Secure flag.
// This should be set to true when serving over HTTPS in production.
func (h *AuthHandler) SetSecureCookie(secure bool) {
	h.secureCookie = secure
}

// LoginRequest is the request body for the login endpoint
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the response body for successful login.
// Note: The session token is transmitted ONLY via httpOnly cookie, never in the
// response body. This prevents XSS attacks from stealing the token.
type LoginResponse struct {
	Success   bool   `json:"success"`
	ExpiresAt string `json:"expires_at"`
	Message   string `json:"message"`
}

// RegisterRoutes registers authentication routes on the mux
// Note: These routes should NOT be wrapped with auth middleware
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", h.handleLogout)
}

// handleLogin handles POST /api/v1/auth/login
func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if auth store is available
	if h.authStore == nil {
		RespondError(w, http.StatusServiceUnavailable, "User authentication is not configured")
		return
	}

	// Parse request body
	var req LoginRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate required fields
	if req.Username == "" {
		RespondError(w, http.StatusBadRequest, "Username is required")
		return
	}

	if req.Password == "" {
		RespondError(w, http.StatusBadRequest, "Password is required")
		return
	}

	// Get IP address for rate limiting using secure IP extraction
	// Uses the IPExtractor which only trusts X-Forwarded-For from configured trusted proxies
	ipAddress := h.extractIPFromRequest(r)

	// Check rate limit if rate limiter is configured
	if h.rateLimiter != nil && ipAddress != "" {
		if !h.rateLimiter.IsAllowed(ipAddress) {
			RespondError(w, http.StatusTooManyRequests,
				"Too many failed authentication attempts, please try again later")
			return
		}
	}

	// Authenticate user
	token, expiration, err := h.authStore.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		// Record failed attempt for rate limiting
		if h.rateLimiter != nil && ipAddress != "" {
			h.rateLimiter.RecordFailedAttempt(ipAddress)
		}
		RespondError(w, http.StatusUnauthorized,
			"Authentication failed: invalid username or password")
		return
	}

	// Reset rate limit on successful authentication
	if h.rateLimiter != nil && ipAddress != "" {
		h.rateLimiter.Reset(ipAddress)
	}

	// Set httpOnly cookie for secure session management.
	// This prevents XSS attacks from accessing the session token.
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiration,
		HttpOnly: true,                 // Prevents JavaScript access (XSS protection)
		Secure:   h.secureCookie,       // Only send over HTTPS in production
		SameSite: http.SameSiteLaxMode, // CSRF protection (Lax works with proxies)
	})

	// Return success response (session token is in httpOnly cookie only)
	RespondJSON(w, http.StatusOK, LoginResponse{
		Success:   true,
		ExpiresAt: expiration.Format(time.RFC3339),
		Message:   "Authentication successful",
	})
}

// handleLogout handles POST /api/v1/auth/logout
// Clears the session cookie to log the user out
func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Clear the session cookie by setting it to expire immediately
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // Expire immediately
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Logged out successfully",
	})
}

// extractIPFromRequest securely extracts the client IP from an HTTP request.
// It uses the configured IPExtractor which only trusts X-Forwarded-For headers
// from known trusted proxies, preventing IP spoofing attacks on rate limiting.
func (h *AuthHandler) extractIPFromRequest(r *http.Request) string {
	if h.ipExtractor != nil {
		return h.ipExtractor.ExtractIP(r)
	}
	// Fallback to RemoteAddr if no IPExtractor is configured
	// This is the safe default - don't trust any forwarded headers
	return r.RemoteAddr
}
