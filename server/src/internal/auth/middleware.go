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
	"errors"
	"log"
	"net"
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
	UserInfoPath = "/api/v1/user/info"

	// LLM provider endpoints (bypasses auth to allow login page to show provider options)
	LLMProvidersPath = "/api/v1/llm/providers"
	LLMModelsPath    = "/api/v1/llm/models"
)

// publicPaths are served without authentication. The OIDC endpoints join
// the list because they are how an unauthenticated browser obtains a
// session in the first place, and capabilities because the login screen
// reads it to decide which sign-in affordances to render.
var publicPaths = map[string]struct{}{
	HealthCheckPath:              {},
	UserInfoPath:                 {},
	"/api/v1/auth/login":         {},
	"/api/v1/auth/logout":        {},
	"/api/v1/auth/oidc/start":    {},
	"/api/v1/auth/oidc/callback": {},
	"/api/v1/capabilities":       {},
	LLMProvidersPath:             {},
	LLMModelsPath:                {},
}

// IsPublicPath reports whether a request path is served without credentials.
func IsPublicPath(path string) bool {
	_, ok := publicPaths[path]
	return ok
}

// GetTokenHashFromContext retrieves the token hash from the request context
// Returns empty string if no token hash is found (e.g., unauthenticated request)
func GetTokenHashFromContext(ctx context.Context) string {
	if hash, ok := ctx.Value(TokenHashContextKey).(string); ok {
		return hash
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

// IPExtractor securely extracts client IP addresses from HTTP requests.
// It only trusts X-Forwarded-For and X-Real-IP headers when the direct
// connection comes from a configured trusted proxy.
//
// Security considerations:
//   - X-Forwarded-For headers can be easily spoofed by clients
//   - Only trust these headers when the request comes from a known proxy
//   - When trusting the header, use the rightmost untrusted IP (not leftmost)
//     because attackers can prepend fake IPs to the header
type IPExtractor struct {
	// TrustedProxies contains CIDR ranges of trusted reverse proxies.
	// Only requests from these IPs will have their X-Forwarded-For headers trusted.
	TrustedProxies []*net.IPNet
}

// NewIPExtractor creates a new IPExtractor with the given trusted proxy CIDR ranges.
// Invalid CIDR strings that cannot be parsed as CIDR or IP addresses are
// logged as warnings and skipped.
func NewIPExtractor(trustedProxyCIDRs []string) *IPExtractor {
	extractor := &IPExtractor{
		TrustedProxies: make([]*net.IPNet, 0, len(trustedProxyCIDRs)),
	}

	for _, cidr := range trustedProxyCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as a single IP address (add /32 or /128 suffix)
			ip := net.ParseIP(cidr)
			if ip != nil {
				// IP address is valid (ParseIP succeeded), so adding prefix length will work
				if ip.To4() != nil {
					_, ipNet, _ = net.ParseCIDR(cidr + "/32") //nolint:errcheck // IP is already validated
				} else {
					_, ipNet, _ = net.ParseCIDR(cidr + "/128") //nolint:errcheck // IP is already validated
				}
			} else {
				log.Printf("[WARN] Ignoring invalid trusted proxy CIDR: %q", cidr)
			}
		}
		if ipNet != nil {
			extractor.TrustedProxies = append(extractor.TrustedProxies, ipNet)
		}
	}

	return extractor
}

// isTrustedProxy checks if the given IP is within any trusted proxy range.
func (e *IPExtractor) isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, trustedNet := range e.TrustedProxies {
		if trustedNet.Contains(ip) {
			return true
		}
	}
	return false
}

// TrustsRequest reports whether this request arrived from a configured
// trusted proxy, and therefore whether the forwarded headers it carries
// mean anything.
//
// It exists so that callers which read a forwarded header other than
// X-Forwarded-For (X-Forwarded-Proto, in particular, where it decides
// whether the OIDC state cookie may use the "__Host-" name prefix) make
// exactly the decision ExtractIP makes, rather than an approximation of
// it. Approximating it is a real hazard: an extractor is built on every
// deployment and is therefore never nil, so "an extractor exists" is
// not the same question as "this request came through a proxy we
// trust", and answering the former lets any client set the header and
// choose the answer.
//
// It returns false whenever no trusted proxies are configured, which is
// the same safe default ExtractIP applies.
func (e *IPExtractor) TrustsRequest(r *http.Request) bool {
	if e == nil || len(e.TrustedProxies) == 0 || r == nil {
		return false
	}

	directIP := extractIPFromRemoteAddr(r.RemoteAddr)
	if directIP == "" {
		return false
	}
	return e.isTrustedProxy(net.ParseIP(directIP))
}

// ExtractIP securely extracts the client IP address from an HTTP request.
//
// Algorithm:
//  1. Extract the direct connection IP from RemoteAddr
//  2. If no trusted proxies are configured, return the direct connection IP
//  3. If the direct connection is not from a trusted proxy, return it directly
//     (do not trust X-Forwarded-For from untrusted sources)
//  4. If behind a trusted proxy, parse X-Forwarded-For and find the rightmost
//     IP that is not a trusted proxy (this is the actual client IP)
func (e *IPExtractor) ExtractIP(r *http.Request) string {
	// Extract direct connection IP from RemoteAddr
	directIP := extractIPFromRemoteAddr(r.RemoteAddr)
	if directIP == "" {
		return r.RemoteAddr // Fallback to raw RemoteAddr if parsing fails
	}

	// If no trusted proxies configured, always use direct connection IP
	// This is the safe default - don't trust any forwarded headers
	if len(e.TrustedProxies) == 0 {
		return directIP
	}

	// Parse the direct connection IP
	parsedDirectIP := net.ParseIP(directIP)
	if parsedDirectIP == nil {
		return directIP
	}

	// Only trust X-Forwarded-For if request comes from a trusted proxy
	if !e.isTrustedProxy(parsedDirectIP) {
		return directIP
	}

	// Request is from a trusted proxy - parse X-Forwarded-For header
	// Format: X-Forwarded-For: client, proxy1, proxy2
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		// No X-Forwarded-For header, try X-Real-IP
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
		return directIP
	}

	// Split the X-Forwarded-For header and find the rightmost untrusted IP
	// Working from right to left is more secure because attackers can only
	// prepend IPs to the left side of the header
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}

		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}

		// Return the first (rightmost) IP that is not a trusted proxy
		if !e.isTrustedProxy(parsedIP) {
			return ip
		}
	}

	// All IPs in the chain are trusted proxies, return the leftmost
	// (This shouldn't normally happen in a properly configured setup)
	if len(parts) > 0 {
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}

	return directIP
}

// extractIPFromRemoteAddr extracts the IP address from a RemoteAddr string.
// RemoteAddr format is typically "IP:port" for IPv4 or "[IP]:port" for IPv6.
func extractIPFromRemoteAddr(remoteAddr string) string {
	// Try to parse as host:port
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// If SplitHostPort fails, it might be an IP without port
		// Check if it's a valid IP
		if ip := net.ParseIP(remoteAddr); ip != nil {
			return remoteAddr
		}
		// Last resort: strip port manually for simple cases
		if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
			return remoteAddr[:idx]
		}
		return remoteAddr
	}
	return host
}

// ExtractBearerToken extracts a bearer token from the request.  It checks the
// Authorization header for a "Bearer " prefix first, then falls back to the
// session_token cookie.  Returns an empty string if neither source provides a
// token.
func ExtractBearerToken(r *http.Request) string {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return authHeader[len("Bearer "):]
		}
		return ""
	}
	if cookie, err := r.Cookie("session_token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
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
			if IsPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Delegate credential validation to the single shared
			// implementation so this middleware, the REST wrapper and
			// the LLM proxy all derive an identical context.
			ctx, err := AuthenticateRequest(r, authStore)
			switch {
			case errors.Is(err, ErrMissingCredentials):
				http.Error(w, "Missing or invalid authentication credentials", http.StatusUnauthorized)
				return
			case err != nil:
				// Neither API token nor session token is valid
				http.Error(w, "Invalid or unknown token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
