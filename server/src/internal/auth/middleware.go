/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// TokenHashContextKey is the context key for storing the authenticated token hash
	TokenHashContextKey contextKey = "token_hash"

	// IPAddressContextKey is the context key for storing the client IP address
	IPAddressContextKey contextKey = "ip_address"

	// UsernameContextKey is the context key for storing the session username
	UsernameContextKey contextKey = "username"

	// IsAPITokenContextKey is the context key for indicating if auth was via API token
	IsAPITokenContextKey contextKey = "is_api_token"

	// UserIDContextKey is the context key for storing the user ID (for RBAC)
	UserIDContextKey contextKey = "user_id"

	// TokenIDContextKey is the context key for storing the token ID (for scoping)
	TokenIDContextKey contextKey = "token_id"

	// IsSuperuserContextKey is the context key for storing superuser status
	IsSuperuserContextKey contextKey = "is_superuser"

	// HealthCheckPath is the path for the health check endpoint (bypasses authentication)
	HealthCheckPath = "/health"

	// UserInfoPath is the path for the user info endpoint (bypasses auth to return auth status)
	UserInfoPath = "/api/user/info"
)

// GetTokenHashFromContext retrieves the token hash from the request context
// Returns empty string if no token hash is found (e.g., unauthenticated request)
func GetTokenHashFromContext(ctx context.Context) string {
	if hash, ok := ctx.Value(TokenHashContextKey).(string); ok {
		return hash
	}
	return ""
}

// GetIPAddressFromContext retrieves the client IP address from the request context
// Returns empty string if no IP address is found
func GetIPAddressFromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(IPAddressContextKey).(string); ok {
		return ip
	}
	return ""
}

// GetUsernameFromContext retrieves the session username from the request context
// Returns empty string if no username is found (e.g., API token or unauthenticated request)
func GetUsernameFromContext(ctx context.Context) string {
	if username, ok := ctx.Value(UsernameContextKey).(string); ok {
		return username
	}
	return ""
}

// IsAPITokenFromContext checks if the request was authenticated via API token
// Returns false if not set (e.g., session token or unauthenticated request)
func IsAPITokenFromContext(ctx context.Context) bool {
	if isAPIToken, ok := ctx.Value(IsAPITokenContextKey).(bool); ok {
		return isAPIToken
	}
	return false
}

// GetUserIDFromContext retrieves the user ID from the request context
// Returns 0 if no user ID is found (e.g., API token or unauthenticated request)
func GetUserIDFromContext(ctx context.Context) int64 {
	if userID, ok := ctx.Value(UserIDContextKey).(int64); ok {
		return userID
	}
	return 0
}

// GetTokenIDFromContext retrieves the token ID from the request context
// Returns 0 if no token ID is found (e.g., session-based auth)
func GetTokenIDFromContext(ctx context.Context) int64 {
	if tokenID, ok := ctx.Value(TokenIDContextKey).(int64); ok {
		return tokenID
	}
	return 0
}

// IsSuperuserFromContext checks if the authenticated user/token has superuser privileges
// Returns false if not set (e.g., unauthenticated request or non-superuser)
func IsSuperuserFromContext(ctx context.Context) bool {
	if isSuperuser, ok := ctx.Value(IsSuperuserContextKey).(bool); ok {
		return isSuperuser
	}
	return false
}

// ExtractIPAddress extracts the client IP address from an HTTP request
// Checks X-Forwarded-For and X-Real-IP headers first (for proxies), then falls back to RemoteAddr
func ExtractIPAddress(r *http.Request) string {
	// Check X-Forwarded-For header first (used by proxies/load balancers)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs (client, proxy1, proxy2, ...)
		// Use the first one (original client IP)
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header (used by some proxies)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	// This may include the port, so strip it if present
	ip := r.RemoteAddr
	if colonIndex := strings.LastIndex(ip, ":"); colonIndex != -1 {
		ip = ip[:colonIndex]
	}

	return ip
}

// AuthMiddleware creates an HTTP middleware that validates API tokens and session tokens
// Uses the new AuthStore for all authentication
func AuthMiddleware(authStore *AuthStore, enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authentication if disabled or no auth store
			if !enabled || authStore == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Skip authentication for public endpoints (needed before login)
			switch r.URL.Path {
			case HealthCheckPath, UserInfoPath:
				next.ServeHTTP(w, r)
				return
			}

			// Check if this is an authenticate_user tool call (which should bypass auth)
			if isAuthenticateUserCall(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Get token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Parse Bearer token
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid Authorization header format. Expected: Bearer <token>", http.StatusUnauthorized)
				return
			}

			token := parts[1]

			// Try to validate as API token (service token or user token) first
			storedToken, err := authStore.ValidateToken(token)
			if err == nil && storedToken != nil {
				// Valid API token - use token hash for connection isolation
				tokenHash := GetTokenHashByRawToken(token)
				ctx := context.WithValue(r.Context(), TokenHashContextKey, tokenHash)
				ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
				ctx = context.WithValue(ctx, TokenIDContextKey, storedToken.ID)
				ctx = context.WithValue(ctx, IsSuperuserContextKey, storedToken.IsSuperuser)

				// If token is owned by a user, add the user ID for group-based privileges
				if storedToken.OwnerID != nil {
					ctx = context.WithValue(ctx, UserIDContextKey, *storedToken.OwnerID)
					// Look up user to check if they are superuser
					user, userErr := authStore.GetUserByID(*storedToken.OwnerID)
					if userErr == nil && user != nil && user.IsSuperuser {
						ctx = context.WithValue(ctx, IsSuperuserContextKey, true)
					}
				}

				r = r.WithContext(ctx)
				next.ServeHTTP(w, r)
				return
			}

			// Try to validate as session token
			username, err := authStore.ValidateSessionToken(token)
			if err == nil && username != "" {
				// Valid session token - use token hash for connection isolation
				tokenHash := GetTokenHashByRawToken(token)
				ctx := context.WithValue(r.Context(), TokenHashContextKey, tokenHash)
				ctx = context.WithValue(ctx, UsernameContextKey, username)
				ctx = context.WithValue(ctx, IsAPITokenContextKey, false)

				// Get user ID and superuser status for RBAC
				user, userErr := authStore.GetUser(username)
				if userErr == nil && user != nil {
					ctx = context.WithValue(ctx, UserIDContextKey, user.ID)
					ctx = context.WithValue(ctx, IsSuperuserContextKey, user.IsSuperuser)
				}

				r = r.WithContext(ctx)
				next.ServeHTTP(w, r)
				return
			}

			// Neither API token nor session token is valid
			http.Error(w, "Invalid or unknown token", http.StatusUnauthorized)
		})
	}
}

// isAuthenticateUserCall checks if the request is a tools/call for authenticate_user
// This function reads and restores the request body
func isAuthenticateUserCall(r *http.Request) bool {
	// Defensive nil check for request
	if r == nil {
		return false
	}

	// Defensive nil check for request body
	if r.Body == nil {
		return false
	}

	// Read the body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	defer func() {
		// Restore the body for the next handler
		r.Body = io.NopCloser(bytes.NewBuffer(body))
	}()

	// Parse as JSON-RPC request
	var req struct {
		Method string                 `json:"method"`
		Params map[string]interface{} `json:"params"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}

	// Check if it's a tools/call for authenticate_user
	if req.Method == "tools/call" {
		if name, ok := req.Params["name"].(string); ok {
			return name == "authenticate_user"
		}
	}

	return false
}
