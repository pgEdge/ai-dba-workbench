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
	"encoding/json"
	"net/http"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// AuthHandler handles authentication-related HTTP requests
type AuthHandler struct {
	authStore   *auth.AuthStore
	rateLimiter *auth.RateLimiter
}

// NewAuthHandler creates a new authentication handler
func NewAuthHandler(authStore *auth.AuthStore, rateLimiter *auth.RateLimiter) *AuthHandler {
	return &AuthHandler{
		authStore:   authStore,
		rateLimiter: rateLimiter,
	}
}

// LoginRequest is the request body for the login endpoint
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the response body for successful login
type LoginResponse struct {
	Success      bool   `json:"success"`
	SessionToken string `json:"session_token"`
	ExpiresAt    string `json:"expires_at"`
	Message      string `json:"message"`
}

// RegisterRoutes registers authentication routes on the mux
// Note: These routes should NOT be wrapped with auth middleware
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/login", h.handleLogin)
}

// handleLogin handles POST /api/auth/login
func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if auth store is available
	if h.authStore == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "User authentication is not configured",
		})
		return
	}

	// Parse request body
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid request body",
		})
		return
	}

	// Validate required fields
	if req.Username == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Username is required",
		})
		return
	}

	if req.Password == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Password is required",
		})
		return
	}

	// Get IP address for rate limiting
	ipAddress := extractIPFromRequest(r)

	// Check rate limit if rate limiter is configured
	if h.rateLimiter != nil && ipAddress != "" {
		if !h.rateLimiter.IsAllowed(ipAddress) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			//nolint:errcheck // Encoding simple error response
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "Too many failed authentication attempts, please try again later",
			})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Authentication failed: invalid username or password",
		})
		return
	}

	// Reset rate limit on successful authentication
	if h.rateLimiter != nil && ipAddress != "" {
		h.rateLimiter.Reset(ipAddress)
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding login response
	json.NewEncoder(w).Encode(LoginResponse{
		Success:      true,
		SessionToken: token,
		ExpiresAt:    expiration.Format(time.RFC3339),
		Message:      "Authentication successful",
	})
}

// extractIPFromRequest extracts the client IP from an HTTP request
func extractIPFromRequest(r *http.Request) string {
	// Try X-Forwarded-For first (common proxy header)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Try X-Real-IP (nginx proxy header)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	return r.RemoteAddr
}
