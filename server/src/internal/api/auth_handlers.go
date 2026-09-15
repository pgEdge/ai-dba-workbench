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
	"log"
	"net/http"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// SessionCookieName is the name of the httpOnly cookie used for session tokens.
// Using httpOnly cookies prevents XSS attacks from accessing the token.
const SessionCookieName = "session_token"

// AuthHandler handles authentication-related HTTP requests
type AuthHandler struct {
	authStore        *auth.AuthStore
	rateLimiter      *auth.RateLimiter // Tracks failed login attempts per IP
	totalRateLimiter *auth.RateLimiter // Tracks total login requests per IP (20/min)
	ipExtractor      *auth.IPExtractor
	tlsEnabled       bool // Whether the server itself has TLS enabled
	// localEnabled is the effective value of http.auth.local.enabled.
	// When false the operator has switched username and password login
	// off, and handleLogin refuses every request regardless of whether
	// the credentials would otherwise verify. The check lives here
	// rather than in AuthStore.AuthenticateUser so that the store keeps
	// its single meaning of "does this password verify".
	localEnabled bool
}

// NewAuthHandler creates a new authentication handler.
// The ipExtractor parameter is optional; if nil, RemoteAddr will be used directly.
// The tlsEnabled parameter indicates whether the server itself has TLS enabled.
// When behind a reverse proxy that terminates TLS, set tlsEnabled to false but ensure
// the proxy passes X-Forwarded-Proto header, which will be used to auto-detect HTTPS;
// that header is honored only on a request the ipExtractor's trusted proxy list
// actually covers, which is decided per request rather than once here.
// The localEnabled parameter carries the effective value of
// http.auth.local.enabled; pass false to refuse password login outright.
func NewAuthHandler(authStore *auth.AuthStore, rateLimiter *auth.RateLimiter, ipExtractor *auth.IPExtractor, tlsEnabled, localEnabled bool) *AuthHandler {
	return &AuthHandler{
		authStore:        authStore,
		rateLimiter:      rateLimiter,
		totalRateLimiter: auth.NewRateLimiter(1, 20), // 20 total login requests per minute per IP
		ipExtractor:      ipExtractor,
		tlsEnabled:       tlsEnabled,
		localEnabled:     localEnabled,
	}
}

// Close releases background resources owned by the handler. In
// particular it stops the internally owned totalRateLimiter goroutine
// created in NewAuthHandler. The caller-supplied rateLimiter is NOT
// stopped here because the caller retains ownership and may share it
// with other handlers. Callers must invoke Close when the handler is
// torn down (notably in tests) to avoid leaking the cleanup goroutine.
func (h *AuthHandler) Close() {
	if h.totalRateLimiter != nil {
		h.totalRateLimiter.Stop()
	}
}

// isSecureRequest determines if a request came over a secure (HTTPS)
// connection, so that cookies are marked Secure when appropriate, even
// behind a reverse proxy. The rule itself lives in requestIsSecure,
// which the OIDC handler shares: the X-Forwarded-Proto trust decision
// must exist in exactly one place.
func (h *AuthHandler) isSecureRequest(r *http.Request) bool {
	return requestIsSecure(r, h.tlsEnabled, h.ipExtractor)
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

// respondLoginFailed writes the single opaque login failure response.
// Every path that declines a login - a bad password, a disabled account,
// a federated account, or local login being switched off entirely - must
// answer with exactly these bytes, so that a caller cannot tell the
// cases apart.
func respondLoginFailed(w http.ResponseWriter) {
	RespondError(w, http.StatusUnauthorized,
		"Authentication failed: invalid username or password")
}

// applyLoginRateLimits consults both login rate limiters and charges the
// total-request limiter for this attempt. It reports whether the request
// may proceed; when it returns false it has already written the
// too-many-requests response.
func (h *AuthHandler) applyLoginRateLimits(w http.ResponseWriter, ipAddress string) bool {
	// Check total request rate limit before checking failed-attempt limiter.
	// This prevents enumeration attacks that succeed on every attempt.
	if h.totalRateLimiter != nil && ipAddress != "" {
		if !h.totalRateLimiter.IsAllowed(ipAddress) {
			RespondError(w, http.StatusTooManyRequests,
				"Too many login requests, please try again later")
			return false
		}
		h.totalRateLimiter.RecordFailedAttempt(ipAddress)
	}

	// Check rate limit if rate limiter is configured
	if h.rateLimiter != nil && ipAddress != "" {
		if !h.rateLimiter.IsAllowed(ipAddress) {
			RespondError(w, http.StatusTooManyRequests,
				"Too many failed authentication attempts, please try again later")
			return false
		}
	}

	return true
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

	// Local login disabled: refuse before the body is read, so that a
	// probe learns nothing from the shape of the request it sent. The
	// rate limiters are still consulted and still charged exactly as
	// they are on the wrong-password path below, and the response is
	// byte for byte the wrong-password response, so that the switch
	// being off is indistinguishable from bad credentials. The real
	// reason goes to the server log only.
	if !h.localEnabled {
		ipAddress := h.extractIPFromRequest(r)
		if !h.applyLoginRateLimits(w, ipAddress) {
			return
		}
		if h.rateLimiter != nil && ipAddress != "" {
			h.rateLimiter.RecordFailedAttempt(ipAddress)
		}
		log.Printf("[AUTH] Login request refused: local password login is disabled (http.auth.local.enabled=false)")
		respondLoginFailed(w)
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

	if !h.applyLoginRateLimits(w, ipAddress) {
		return
	}

	// Authenticate user
	token, expiration, err := h.authStore.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		// Record failed attempt for rate limiting
		if h.rateLimiter != nil && ipAddress != "" {
			h.rateLimiter.RecordFailedAttempt(ipAddress)
		}
		respondLoginFailed(w)
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
	// #nosec G124 -- Secure is intentionally conditional on
	// isSecureRequest so local HTTP development still works;
	// in production behind TLS (direct or via a trusted proxy
	// supplying X-Forwarded-Proto) the flag evaluates to true.
	// HttpOnly and SameSite are unconditional.
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
	// #nosec G124 -- Secure is intentionally conditional on
	// isSecureRequest so local HTTP development still works;
	// in production behind TLS (direct or via a trusted proxy
	// supplying X-Forwarded-Proto) the flag evaluates to true.
	// HttpOnly and SameSite are unconditional. The clear-cookie
	// flags must mirror the set-cookie flags above so browsers
	// match and overwrite the original cookie on logout.
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
