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
	authStore         *auth.AuthStore
	rateLimiter       *auth.RateLimiter // Tracks failed login attempts per IP
	totalRateLimiter  *auth.RateLimiter // Tracks total login requests per IP (20/min)
	ipExtractor       *auth.IPExtractor
	tlsEnabled        bool // Whether the server itself has TLS enabled
	trustProxyHeaders bool // Whether to trust X-Forwarded-Proto for secure detection
}

// NewAuthHandler creates a new authentication handler.
// The ipExtractor parameter is optional; if nil, RemoteAddr will be used directly.
// The tlsEnabled parameter indicates whether the server itself has TLS enabled.
// When behind a reverse proxy that terminates TLS, set tlsEnabled to false but ensure
// the proxy passes X-Forwarded-Proto header, which will be used to auto-detect HTTPS.
func NewAuthHandler(authStore *auth.AuthStore, rateLimiter *auth.RateLimiter, ipExtractor *auth.IPExtractor, tlsEnabled bool) *AuthHandler {
	// If an IP extractor is configured, it means we're behind a trusted proxy,
	// so we should also trust X-Forwarded-Proto for secure cookie detection.
	trustProxyHeaders := ipExtractor != nil

	return &AuthHandler{
		authStore:         authStore,
		rateLimiter:       rateLimiter,
		totalRateLimiter:  auth.NewRateLimiter(1, 20), // 20 total login requests per minute per IP
		ipExtractor:       ipExtractor,
		tlsEnabled:        tlsEnabled,
		trustProxyHeaders: trustProxyHeaders,
	}
}

// SetTrustProxyHeaders configures whether to trust X-Forwarded-Proto header
// for determining if the connection is secure. This should be true when
// behind a trusted reverse proxy that terminates TLS.
func (h *AuthHandler) SetTrustProxyHeaders(trust bool) {
	h.trustProxyHeaders = trust
}

// isSecureRequest determines if a request came over a secure (HTTPS) connection.
// It checks multiple indicators:
// 1. If the server has TLS enabled directly
// 2. If the request URL scheme is HTTPS
// 3. If trusted proxy headers indicate HTTPS (X-Forwarded-Proto)
// This ensures cookies are marked Secure when appropriate, even behind reverse proxies.
func (h *AuthHandler) isSecureRequest(r *http.Request) bool {
	// Server has TLS enabled directly
	if h.tlsEnabled {
		return true
	}

	// Check the request TLS state (set by Go's http server when TLS is used)
	if r.TLS != nil {
		return true
	}

	// Check X-Forwarded-Proto header if we trust proxy headers
	if h.trustProxyHeaders {
		proto := r.Header.Get("X-Forwarded-Proto")
		if proto == "https" {
			return true
		}
	}

	return false
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

	// Check total request rate limit before checking failed-attempt limiter.
	// This prevents enumeration attacks that succeed on every attempt.
	if h.totalRateLimiter != nil && ipAddress != "" {
		if !h.totalRateLimiter.IsAllowed(ipAddress) {
			RespondError(w, http.StatusTooManyRequests,
				"Too many login requests, please try again later")
			return
		}
		h.totalRateLimiter.RecordFailedAttempt(ipAddress)
	}

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
	// Auto-detect if this is a secure request (HTTPS or behind TLS-terminating proxy)
	secureCookie := h.isSecureRequest(r)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiration,
		HttpOnly: true,         // Prevents JavaScript access (XSS protection)
		Secure:   secureCookie, // Auto-detected: only send over HTTPS
		// NOTE: SameSite=Lax is appropriate for same-origin and proxied
		// setups. For cross-origin deployments, use a reverse proxy to
		// serve frontend and backend from the same origin.
		SameSite: http.SameSiteLaxMode,
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

	// Invalidate the server-side session before clearing the cookie
	cookie, err := r.Cookie(SessionCookieName)
	if err == nil && cookie.Value != "" {
		h.authStore.InvalidateSession(cookie.Value)
	}

	// Clear the session cookie by setting it to expire immediately
	// Auto-detect if this is a secure request (HTTPS or behind TLS-terminating proxy)
	secureCookie := h.isSecureRequest(r)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // Expire immediately
		HttpOnly: true,
		Secure:   secureCookie,
		// NOTE: SameSite=Lax is appropriate for same-origin and proxied
		// setups. For cross-origin deployments, use a reverse proxy to
		// serve frontend and backend from the same origin.
		SameSite: http.SameSiteLaxMode,
	})

	RespondJSON(w, http.StatusOK, map[string]any{
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
