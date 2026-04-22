/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// MaxRequestBodySize is the maximum allowed size for HTTP request bodies (1MB).
// This prevents denial-of-service attacks via memory exhaustion from large payloads.
const MaxRequestBodySize = 1 << 20 // 1MB

// contextKey is a type for context keys to avoid collisions
type contextKey string

// requestIDContextKey is used to store the request ID in context for tracing
const requestIDContextKey contextKey = "request_id"

// GetRequestIDFromContext extracts the request ID from context
func GetRequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDContextKey).(string); ok {
		return id
	}
	return ""
}

// HTTPConfig holds configuration for HTTP/HTTPS server mode
type HTTPConfig struct {
	Addr           string                         // Server address (e.g., ":8080")
	TLSEnable      bool                           // Enable HTTPS
	CertFile       string                         // Path to TLS certificate file
	KeyFile        string                         // Path to TLS key file
	ChainFile      string                         // Optional path to certificate chain file
	AuthStore      *auth.AuthStore                // Auth store for all authentication (users and tokens)
	SetupHandlers  func(mux *http.ServeMux) error // Optional callback to add custom handlers before auth middleware
	Debug          bool                           // Enable debug logging
	TrustedProxies []string                       // CIDR ranges of trusted reverse proxies for secure IP extraction
	CORSOrigin     string                         // Allowed CORS origin; empty disables CORS headers (same-origin deployment)
	HSTSEnabled    bool                           // Enable Strict-Transport-Security header
}

// RunHTTP starts the MCP server in HTTP/HTTPS mode
func (s *Server) RunHTTP(config *HTTPConfig) error {
	if config == nil {
		return fmt.Errorf("HTTP config is required")
	}

	// Store debug flag for use in handlers
	s.debug = config.Debug

	// Create secure IP extractor with configured trusted proxies
	s.ipExtractor = auth.NewIPExtractor(config.TrustedProxies)

	// Create HTTP handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/v1", s.handleHTTPRequest)
	mux.HandleFunc("/health", s.handleHealthCheck)

	// Call custom handler setup if provided (allows main.go to add LLM proxy endpoints)
	if config.SetupHandlers != nil {
		if err := config.SetupHandlers(mux); err != nil {
			return fmt.Errorf("failed to setup custom handlers: %w", err)
		}
	}

	// Validate CORS configuration before binding any listener so
	// misconfigurations fail fast at startup rather than producing
	// browsers that silently reject responses at runtime.
	// Auth is enabled whenever AuthStore is non-nil: AuthMiddleware below
	// is always constructed with enabled=true, so it authenticates every
	// request (issuing Set-Cookie / reading Authorization headers)
	// precisely when AuthStore != nil.
	if err := validateCORSOrigin(config.CORSOrigin, config.AuthStore != nil); err != nil {
		return err
	}

	// Wrap with auth middleware
	var handler http.Handler = mux
	handler = auth.AuthMiddleware(config.AuthStore, true)(handler)

	// Apply request body size limit middleware to prevent memory exhaustion attacks
	handler = MaxBytesMiddleware(MaxRequestBodySize)(handler)

	// Apply CORS middleware if an origin is configured (skip for same-origin deployments)
	if config.CORSOrigin != "" {
		handler = CORSMiddleware(config.CORSOrigin)(handler)
	}

	// Apply security headers middleware (outermost to ensure headers on all responses)
	handler = SecurityHeadersMiddleware(config.HSTSEnabled)(handler)

	// Configure server with timeouts to prevent slowloris DoS attacks
	httpServer := &http.Server{
		Addr:         config.Addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,  // Prevents slow request body attacks
		WriteTimeout: 300 * time.Second, // Must accommodate long LLM agentic loops (e.g. server analysis)
		IdleTimeout:  120 * time.Second, // Limits keep-alive connection duration
	}

	// Start server with or without TLS
	if config.TLSEnable {
		// Load TLS configuration
		tlsConfig, err := s.loadTLSConfig(config)
		if err != nil {
			return fmt.Errorf("failed to load TLS config: %w", err)
		}
		httpServer.TLSConfig = tlsConfig

		return httpServer.ListenAndServeTLS(config.CertFile, config.KeyFile)
	}

	return httpServer.ListenAndServe()
}

// loadTLSConfig loads TLS certificates and creates a TLS configuration
func (s *Server) loadTLSConfig(config *HTTPConfig) (*tls.Config, error) {
	// Load certificate and key
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate and key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Load certificate chain if provided
	if config.ChainFile != "" {
		chainData, err := os.ReadFile(config.ChainFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read certificate chain: %w", err)
		}

		// Parse PEM-encoded chain and append DER blocks to certificate
		for {
			block, rest := pem.Decode(chainData)
			if block == nil {
				break
			}
			cert.Certificate = append(cert.Certificate, block.Bytes)
			chainData = rest
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// handleHTTPRequest handles HTTP requests and translates them to JSON-RPC
func (s *Server) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract IP address securely and add to context
	// Uses IPExtractor which only trusts X-Forwarded-For from configured trusted proxies
	var ipAddress string
	if s.ipExtractor != nil {
		ipAddress = s.ipExtractor.ExtractIP(r)
	} else {
		// Fallback to direct connection IP if extractor not configured
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ipAddress = r.RemoteAddr
		} else {
			ipAddress = host
		}
	}
	ctx := context.WithValue(r.Context(), auth.IPAddressContextKey, ipAddress)

	// Generate request ID and session ID for tracing
	requestID := tracing.GenerateRequestID()
	tokenHash := auth.GetTokenHashFromContext(ctx)
	sessionID := tokenHash // Use token hash as session ID for correlation

	// Read request body (size already limited by MaxBytesMiddleware)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// Check if this is a "request body too large" error from MaxBytesMiddleware
		if err.Error() == "http: request body too large" {
			http.Error(w, "Request body too large (max 1MB)", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to close request body: %v\n", err)
		}
	}()

	// Parse JSON-RPC request
	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		sendHTTPError(w, nil, -32700, "Parse error", err.Error())
		return
	}

	// Trace incoming request
	if tracing.IsEnabled() {
		var params any
		if req.Params != nil {
			params = req.Params
		}
		tracing.LogHTTPRequest(sessionID, tokenHash, requestID, r.Method, "/mcp/v1 "+req.Method, params)
	}

	// Debug logging: log incoming request
	if s.debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] Incoming request: method=%s id=%v ip=%s\n", req.Method, req.ID, ipAddress)
		if req.Params != nil {
			if paramsJSON, err := json.Marshal(req.Params); err == nil {
				fmt.Fprintf(os.Stderr, "[DEBUG] Request params: %s\n", string(paramsJSON))
			}
		}
	}

	// Store request ID in context for use by tool/resource handlers
	ctx = context.WithValue(ctx, requestIDContextKey, requestID)

	// Handle the request and capture the response (pass context with IP address)
	response := s.handleRequestHTTP(ctx, req)

	// Debug logging: log outgoing response
	if s.debug {
		if responseJSON, err := json.Marshal(response); err == nil {
			fmt.Fprintf(os.Stderr, "[DEBUG] Outgoing response: %s\n", string(responseJSON))
		}
	}

	// Trace outgoing response
	if tracing.IsEnabled() {
		duration := time.Since(startTime)
		var result any
		if response.Error != nil {
			result = map[string]any{
				"error": response.Error,
			}
		} else {
			result = response.Result
		}
		tracing.LogHTTPResponse(sessionID, tokenHash, requestID, r.Method, "/mcp/v1 "+req.Method, http.StatusOK, result, duration)
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to encode response: %v\n", err)
	}
}

// handleRequestHTTP handles a JSON-RPC request and returns the response
func (s *Server) handleRequestHTTP(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitializeHTTP(req)
	case "notifications/initialized":
		// Client notification - return empty response
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{}`),
		}
	case "tools/list":
		return s.handleToolsListHTTP(req)
	case "tools/call":
		return s.handleToolCallHTTP(ctx, req)
	case "resources/list":
		return s.handleResourcesListHTTP(req)
	case "resources/read":
		return s.handleResourceReadHTTP(ctx, req)
	case "prompts/list":
		return s.handlePromptsListHTTP(req)
	case "prompts/get":
		return s.handlePromptGetHTTP(req)
	default:
		return createErrorResponse(req.ID, -32601, "Method not found", nil)
	}
}

// HTTP-specific handlers that return responses instead of sending them

func (s *Server) handleInitializeHTTP(req JSONRPCRequest) JSONRPCResponse {
	capabilities := map[string]any{
		"tools": map[string]any{},
	}

	// Add resources capability if resource provider is set
	if s.resources != nil {
		capabilities["resources"] = map[string]any{}
	}

	// Add prompts capability if prompt provider is set
	if s.prompts != nil {
		capabilities["prompts"] = map[string]any{}
	}

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    capabilities,
		ServerInfo: Implementation{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleToolsListHTTP(req JSONRPCRequest) JSONRPCResponse {
	tools := s.tools.List()
	result := ToolsListResult{Tools: tools}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleToolCallHTTP(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	var params ToolCallParams

	// Convert any to JSON bytes first
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return createErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return createErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	// Pass context for per-token connection isolation
	response, err := s.tools.Execute(ctx, params.Name, params.Arguments)
	if err != nil {
		return createErrorResponse(req.ID, -32603, "Internal error", err.Error())
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  response,
	}
}

func (s *Server) handleResourcesListHTTP(req JSONRPCRequest) JSONRPCResponse {
	if s.resources == nil {
		return createErrorResponse(req.ID, -32603, "Resources not available", nil)
	}

	resources := s.resources.List()
	result := ResourcesListResult{Resources: resources}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleResourceReadHTTP(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	if s.resources == nil {
		return createErrorResponse(req.ID, -32603, "Resources not available", nil)
	}

	var params ResourceReadParams

	// Convert any to JSON bytes first
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return createErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return createErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	content, err := s.resources.Read(ctx, params.URI)
	if err != nil {
		return createErrorResponse(req.ID, -32603, "Failed to read resource", err.Error())
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  content,
	}
}

func (s *Server) handlePromptsListHTTP(req JSONRPCRequest) JSONRPCResponse {
	if s.prompts == nil {
		return createErrorResponse(req.ID, -32601, "Prompts not supported", nil)
	}

	prompts := s.prompts.List()
	result := PromptsListResult{Prompts: prompts}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handlePromptGetHTTP(req JSONRPCRequest) JSONRPCResponse {
	if s.prompts == nil {
		return createErrorResponse(req.ID, -32601, "Prompts not supported", nil)
	}

	var params PromptGetParams

	// Convert any to JSON bytes first
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return createErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return createErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	result, err := s.prompts.Execute(params.Name, params.Arguments)
	if err != nil {
		return createErrorResponse(req.ID, -32603, "Prompt execution error", err.Error())
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleHealthCheck provides a simple health check endpoint
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, `{"status":"ok","server":"%s","version":"%s"}`, ServerName, ServerVersion); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to write health check response: %v\n", err)
	}
}

// Helper functions

func sendHTTPError(w http.ResponseWriter, id any, code int, message string, data any) {
	response := createErrorResponse(id, code, message, data)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors are still HTTP 200
	if err := json.NewEncoder(w).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to encode error response: %v\n", err)
	}
}

func createErrorResponse(id any, code int, message string, data any) JSONRPCResponse {
	errResp := RPCError{
		Code:    code,
		Message: message,
	}
	if data != nil {
		errResp.Data = data
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &errResp,
	}
}

// MaxBytesMiddleware limits request body size to prevent memory exhaustion attacks.
// This middleware wraps all incoming request bodies with http.MaxBytesReader,
// ensuring consistent protection across all endpoints.
func MaxBytesMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only limit body size for requests that have a body
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// validateCORSOrigin validates a configured CORS origin. An empty origin
// is allowed (same-origin deployments skip CORS entirely). A wildcard
// origin ("*") is rejected when auth is enabled because the Fetch spec
// forbids combining Access-Control-Allow-Origin: * with
// Access-Control-Allow-Credentials: true, causing browsers to silently
// drop responses. Any non-wildcard origin must parse as a valid URI.
func validateCORSOrigin(origin string, authEnabled bool) error {
	if origin == "" {
		return nil
	}
	if origin == "*" {
		if authEnabled {
			return fmt.Errorf(
				"cors_origin %q cannot be used when authentication is "+
					"enabled: browsers reject Access-Control-Allow-Origin: * "+
					"with Access-Control-Allow-Credentials: true (per the "+
					"Fetch spec). Set cors_origin to an explicit origin "+
					"(e.g., https://dba.example.com) or leave it empty for "+
					"same-origin deployments",
				origin)
		}
		return nil
	}
	if _, err := url.ParseRequestURI(origin); err != nil {
		return fmt.Errorf("invalid CORS origin %q: %w", origin, err)
	}
	return nil
}

// CORSMiddleware adds Cross-Origin Resource Sharing headers for the specified origin.
// It handles OPTIONS preflight requests by returning 204 No Content.
// When origin is empty, the middleware should not be applied (same-origin deployment).
func CORSMiddleware(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware returns a middleware that adds security headers to
// protect against common web attacks.  When hstsEnabled is true, the
// Strict-Transport-Security header is included; otherwise it is omitted so
// that plain-HTTP deployments do not advertise HSTS.
func SecurityHeadersMiddleware(hstsEnabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// NOTE: HSTS is intentionally disabled by default. Users should
			// only enable it after confirming their TLS configuration is
			// correct, as HSTS is difficult to undo once browsers cache
			// the policy.
			if hstsEnabled {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			// X-Content-Type-Options: Prevent MIME type sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// X-Frame-Options: Prevent clickjacking by denying framing
			w.Header().Set("X-Frame-Options", "DENY")

			// X-XSS-Protection: Enable XSS filter in legacy browsers
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Content-Security-Policy: Restrict resource loading; allow inline
			// styles and data: fonts required by MUI (Material UI)
			w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self' data:")

			// Referrer-Policy: Control referrer information sent with requests
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			next.ServeHTTP(w, r)
		})
	}
}
