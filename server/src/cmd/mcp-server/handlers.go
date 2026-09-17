/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/compactor"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
	"github.com/pgedge/ai-workbench/server/internal/memory"
	"github.com/pgedge/ai-workbench/server/internal/oidc"
	"github.com/pgedge/ai-workbench/server/internal/overview"
)

// routeRegistrar is implemented by any handler that can register routes on a mux.
type routeRegistrar interface {
	RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc)
}

// registerDatastoreHandler registers a handler that depends on the datastore and
// logs its enabled/disabled status.  This eliminates the repeated if/else pattern.
func registerDatastoreHandler(mux *http.ServeMux, handler routeRegistrar, authWrapper func(http.HandlerFunc) http.HandlerFunc, name string, datastore any) {
	handler.RegisterRoutes(mux, authWrapper)
	if datastore != nil {
		fmt.Fprintf(os.Stderr, "%s: ENABLED\n", name)
	} else {
		fmt.Fprintf(os.Stderr, "%s: DISABLED (datastore not configured)\n", name)
	}
}

// HandlerDependencies holds all dependencies needed for HTTP handlers
type HandlerDependencies struct {
	AuthStore    *auth.AuthStore
	RateLimiter  *auth.RateLimiter
	IPExtractor  *auth.IPExtractor
	ConvStore    *conversations.Store
	Datastore    *database.Datastore
	Config       *config.Config
	OverviewGen  *overview.Generator
	OverviewHub  *overview.Hub
	ToolProvider api.ContextAwareToolProvider
	AIEnabled    bool

	// OIDCProvider is the discovered identity provider, or nil when
	// federated login is switched off. OIDCStateKey is the 32-byte key
	// the login state cookie is sealed with, and is meaningful only
	// alongside a non-nil provider.
	OIDCProvider *oidc.Provider
	OIDCStateKey []byte

	// RegisterCloser records a cleanup function to be run when the
	// server shuts down. Handlers that own background goroutines use
	// it to hand that ownership back to the server. It may be nil in
	// tests that only exercise route registration.
	RegisterCloser func(func())
}

// SetupHandlers configures all HTTP handlers for the server
func SetupHandlers(deps *HandlerDependencies) func(*http.ServeMux) error {
	return func(mux *http.ServeMux) error {
		// Helper to wrap handlers with authentication
		authWrapper := createAuthWrapper(deps.AuthStore, deps.IPExtractor)

		// NOTE: These endpoints are intentionally unauthenticated to allow
		// API tooling (e.g. RESTish) to discover the API schema without
		// requiring credentials.
		mux.HandleFunc("/api/v1/openapi.json", handleOpenAPISpec)

		// Capabilities endpoint (public - used by client to detect available features)
		maxIterations := 50
		if deps.Config != nil && deps.Config.LLM.MaxIterations > 0 {
			maxIterations = deps.Config.LLM.MaxIterations
		}
		// The login page state is derived once here and shared with the
		// login handler, so that what the capabilities endpoint reports
		// and what the login endpoint enforces cannot drift apart.
		authInfo := authCapabilities(deps)
		mux.HandleFunc("/api/v1/capabilities",
			handleCapabilities(deps.AIEnabled, maxIterations, authInfo))

		// Authentication endpoint (does NOT require auth - it IS the login endpoint)
		// IPExtractor provides secure IP extraction that only trusts X-Forwarded-For
		// from configured trusted proxies, preventing rate limit bypass via IP spoofing
		// The TLS enabled flag ensures cookies are marked Secure when using HTTPS
		tlsEnabled := deps.Config != nil && deps.Config.HTTP.TLS.Enabled
		authHandler := api.NewAuthHandler(deps.AuthStore, deps.RateLimiter, deps.IPExtractor,
			tlsEnabled, authInfo.LocalEnabled)

		// NewAuthHandler starts a cleanup goroutine for its internal
		// login rate limiter, which only Close stops. Hand that back
		// to the server so it is stopped on shutdown rather than
		// running for the remaining life of the process.
		if deps.RegisterCloser != nil {
			deps.RegisterCloser(authHandler.Close)
		}
		authHandler.RegisterRoutes(mux)

		// Federated login endpoints, registered alongside the local ones
		// and equally unauthenticated: a user starting a login has no
		// session, and the callback is how they get one.
		//
		// The start endpoint is registered either way. With no provider
		// there is no login to start, but the login screen still offers
		// the button whenever it cannot reach the capabilities endpoint,
		// and following it is a full-page navigation: a 404 from the mux
		// would leave the user on a browser error page with the login
		// screen gone, so the disabled route redirects them back to it
		// instead. The callback is registered only with a provider,
		// since nothing sends a user there by hand.
		if deps.OIDCProvider != nil && deps.Config != nil {
			oidcHandler := api.NewOIDCHandler(deps.AuthStore, deps.OIDCProvider,
				deps.Config.HTTP.Auth.OIDC, deps.OIDCStateKey, tlsEnabled, deps.IPExtractor)
			// NewOIDCHandler owns a rate limiter whose cleanup goroutine
			// only Close stops; hand that back to the server.
			if deps.RegisterCloser != nil {
				deps.RegisterCloser(oidcHandler.Close)
			}
			oidcHandler.RegisterRoutes(mux)
			fmt.Fprintf(os.Stderr, "OIDC login endpoints: ENABLED\n")
		} else {
			api.RegisterDisabledStartRoute(mux)
		}

		// Chat history compaction endpoint
		mux.HandleFunc("/api/v1/chat/compact",
			authWrapper(compactor.HandleCompact))

		// User info endpoint - returns auth status (no error if not logged in)
		mux.HandleFunc("/api/v1/user/info",
			createUserInfoHandler(deps.AuthStore))

		// LLM proxy handlers (always enabled)
		// Create memory store for pinned memory injection into system prompt
		var memoryStore *memory.Store
		if deps.Datastore != nil && deps.Config != nil && deps.Config.Memory.IsEnabled() {
			memoryStore = memory.NewStore(deps.Datastore.GetPool())
		}
		if err := setupLLMHandlers(mux, deps.Config, deps.ToolProvider, memoryStore, deps.AuthStore); err != nil {
			return err
		}

		// MCP tool REST bridge (exposes tools/list and tools/call over REST)
		if deps.ToolProvider != nil {
			mcpToolHandler := api.NewMCPToolHandler(deps.ToolProvider)
			mcpToolHandler.RegisterRoutes(mux, authWrapper)
			fmt.Fprintf(os.Stderr, "MCP tool REST bridge: ENABLED\n")
		}

		// Conversation history endpoints (only if store is available)
		if deps.ConvStore != nil && deps.AuthStore != nil {
			convHandler := conversations.NewHandler(deps.ConvStore, deps.AuthStore)
			convHandler.RegisterRoutes(mux, authWrapper)
			fmt.Fprintf(os.Stderr, "Conversation history: ENABLED\n")
		}

		// Create RBAC checker for permission-based access control in REST handlers
		rbacChecker := auth.NewRBACCheckerForDatastore(deps.AuthStore, deps.Datastore)

		// Connection management endpoints (for selecting monitored database connections)
		// Uses security configuration to prevent SSRF attacks
		connHandler := api.NewConnectionHandlerWithSecurity(
			deps.Datastore,
			deps.AuthStore,
			rbacChecker,
			deps.Config.ConnectionSecurity.AllowInternalNetworks,
			deps.Config.ConnectionSecurity.AllowedHosts,
			deps.Config.ConnectionSecurity.BlockedHosts,
		)
		registerDatastoreHandler(mux, connHandler, authWrapper, "Connection management", deps.Datastore)

		// Cluster hierarchy endpoints (for ClusterNavigator component)
		clusterHandler := api.NewClusterHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, clusterHandler, authWrapper, "Cluster management", deps.Datastore)

		// Alert endpoints (for StatusPanel component)
		alertHandler := api.NewAlertHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, alertHandler, authWrapper, "Alert management", deps.Datastore)

		// Blackout management endpoints (for alert suppression windows)
		blackoutHandler := api.NewBlackoutHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, blackoutHandler, authWrapper, "Blackout management", deps.Datastore)

		// Probe configuration endpoints (for configurable collection intervals)
		probeConfigHandler := api.NewProbeConfigHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, probeConfigHandler, authWrapper, "Probe configuration", deps.Datastore)

		// Alert rule configuration endpoints (for configurable alert thresholds)
		alertRuleHandler := api.NewAlertRuleHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, alertRuleHandler, authWrapper, "Alert rule configuration", deps.Datastore)

		// Alert override endpoints (for hierarchical threshold configuration)
		alertOverrideHandler := api.NewAlertOverrideHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, alertOverrideHandler, authWrapper, "Alert override configuration", deps.Datastore)

		// Probe override endpoints (for hierarchical probe configuration)
		probeOverrideHandler := api.NewProbeOverrideHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, probeOverrideHandler, authWrapper, "Probe override configuration", deps.Datastore)

		// Notification channel management endpoints (for alert channel configuration)
		notificationChannelHandler := api.NewNotificationChannelHandlerWithSecurity(
			deps.Datastore,
			deps.AuthStore,
			rbacChecker,
			deps.Config.ConnectionSecurity.AllowInternalNetworks,
			deps.Config.ConnectionSecurity.AllowedHosts,
			deps.Config.ConnectionSecurity.BlockedHosts,
		)
		registerDatastoreHandler(mux, notificationChannelHandler, authWrapper, "Notification channel management", deps.Datastore)

		// Channel override endpoints (for hierarchical notification channel configuration)
		channelOverrideHandler := api.NewChannelOverrideHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, channelOverrideHandler, authWrapper, "Channel override configuration", deps.Datastore)

		// Server info endpoint (for Server Info Dialog)
		serverInfoLLMConfig := &llmproxy.Config{
			Provider:               deps.Config.LLM.Provider,
			Model:                  deps.Config.LLM.Model,
			AnthropicAPIKey:        deps.Config.LLM.AnthropicAPIKey,
			AnthropicBaseURL:       deps.Config.LLM.AnthropicBaseURL,
			OpenAIAPIKey:           deps.Config.LLM.OpenAIAPIKey,
			OpenAIBaseURL:          deps.Config.LLM.OpenAIBaseURL,
			GeminiAPIKey:           deps.Config.LLM.GeminiAPIKey,
			GeminiBaseURL:          deps.Config.LLM.GeminiBaseURL,
			OllamaURL:              deps.Config.LLM.OllamaURL,
			MaxTokens:              deps.Config.LLM.MaxTokens,
			Temperature:            deps.Config.LLM.Temperature,
			UseCompactDescriptions: deps.Config.LLM.UseCompactDescriptions(),
			LLMConfig:              &deps.Config.LLM,
		}
		serverInfoHandler := api.NewServerInfoHandler(deps.Datastore, deps.AuthStore, rbacChecker, serverInfoLLMConfig)
		registerDatastoreHandler(mux, serverInfoHandler, authWrapper, "Server info", deps.Datastore)

		// Timeline endpoints (for EventTimeline component)
		timelineHandler := api.NewTimelineHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, timelineHandler, authWrapper, "Timeline events", deps.Datastore)

		// Performance summary endpoint (for performance dashboard)
		perfHandler := api.NewPerfSummaryHandler(deps.Datastore, deps.AuthStore)
		registerDatastoreHandler(mux, perfHandler, authWrapper, "Performance summary", deps.Datastore)

		// Metrics query endpoints (for monitoring dashboards)
		metricsHandler := api.NewMetricsHandler(deps.Datastore, deps.AuthStore)
		registerDatastoreHandler(mux, metricsHandler, authWrapper, "Metrics query", deps.Datastore)

		// Latest snapshot endpoint (for table/index leaderboards)
		latestHandler := api.NewLatestSnapshotHandler(deps.Datastore, deps.AuthStore)
		registerDatastoreHandler(mux, latestHandler, authWrapper, "Latest snapshot", deps.Datastore)

		// AI Overview endpoint (for estate overview summary)
		if deps.OverviewGen != nil {
			overviewHandler := overview.NewHandlerWithRBAC(deps.OverviewGen, deps.OverviewHub, rbacChecker, deps.Datastore)
			overviewHandler.RegisterRoutes(mux, authWrapper)
			deps.OverviewGen.OnRestart(func() {
				serverInfoHandler.InvalidateCache()
			})
			fmt.Fprintf(os.Stderr, "AI Overview API: ENABLED\n")
		}

		// Memory management endpoints
		memoryHandler := api.NewMemoryHandler(memoryStore, deps.AuthStore, rbacChecker)
		memoryHandler.RegisterRoutes(mux, authWrapper)
		if memoryStore != nil {
			fmt.Fprintf(os.Stderr, "Memory management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Memory management: DISABLED (memory not configured)\n")
		}

		// RBAC management endpoints
		if deps.AuthStore != nil {
			rbacHandler := api.NewRBACHandler(deps.AuthStore, rbacChecker)
			rbacHandler.RegisterRoutes(mux, authWrapper)
			fmt.Fprintf(os.Stderr, "RBAC management: ENABLED\n")
		}

		return nil
	}
}

// clientIP returns the address to attribute a request to. It prefers
// the trusted-proxy aware extractor, which only trusts forwarding
// headers from configured proxies, and falls back to the host part of
// RemoteAddr when no extractor is configured.
func clientIP(r *http.Request, ipExtractor *auth.IPExtractor) string {
	if ipExtractor != nil {
		return ipExtractor.ExtractIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// createAuthWrapper creates a handler wrapper that enforces authentication
// Supports both Authorization header (for API tokens) and session cookies (for browser sessions).
// The actual token/session validation is delegated to auth.AuthenticateRequest,
// the single shared implementation used by both this middleware and the
// LLM proxy's Authorize hook.
//
// On success the client IP is added to the context under
// auth.IPAddressContextKey, so that auth.ActorFromContext can attribute
// audited changes to the address the request came from.
func createAuthWrapper(authStore *auth.AuthStore, ipExtractor *auth.IPExtractor) func(http.HandlerFunc) http.HandlerFunc {
	return func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, err := auth.AuthenticateRequest(r, authStore)
			if err != nil {
				// Preserve the historical, capitalised 401 response
				// bodies that existing clients may assert on.
				msg := "Missing or invalid authentication credentials"
				if errors.Is(err, auth.ErrInvalidToken) {
					msg = "Invalid or expired token"
				}
				http.Error(w, msg, http.StatusUnauthorized)
				return
			}

			ctx = context.WithValue(ctx, auth.IPAddressContextKey,
				clientIP(r, ipExtractor))
			r = r.WithContext(ctx)

			// Proceed with handler
			handler(w, r)
		}
	}
}

// createUserInfoHandler creates a handler for the user info endpoint.
//
// The endpoint never answers 401: the web client calls it before it knows
// whether it holds a session, and uses the "authenticated" flag to decide
// whether to show the login screen. Credential validation is delegated to
// auth.AuthenticateRequest so that a caller the rest of the server would
// accept is a caller this endpoint reports as authenticated.
func createUserInfoHandler(authStore *auth.AuthStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := auth.AuthenticateRequest(r, authStore)
		if err != nil {
			resp := map[string]any{"authenticated": false}
			if !errors.Is(err, auth.ErrMissingCredentials) {
				resp["error"] = "Invalid or expired token"
			}
			api.RespondJSON(w, http.StatusOK, resp)
			return
		}

		// AuthenticateRequest guarantees a non-empty username on success:
		// a credential whose identity does not resolve is rejected as
		// ErrInvalidToken and handled above.
		username := auth.GetUsernameFromContext(ctx)

		// Get admin permissions for the user
		adminPermissions := []string{}
		if userID := auth.GetUserIDFromContext(ctx); userID > 0 {
			perms, permErr := authStore.GetUserAdminPermissions(userID)
			if permErr == nil {
				for perm := range perms {
					adminPermissions = append(adminPermissions, perm)
				}
			}
		}

		// Return user info as JSON
		api.RespondJSON(w, http.StatusOK, map[string]any{
			"authenticated":     true,
			"username":          username,
			"is_superuser":      auth.IsSuperuserFromContext(ctx),
			"admin_permissions": adminPermissions,
		})
	}
}

// handleOpenAPISpec serves the OpenAPI specification
func handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	spec := api.BuildOpenAPISpec()
	api.RespondJSON(w, http.StatusOK, spec)
}

// authCapabilitiesInfo is what the login page needs in order to render
// itself: whether to show the username and password form, whether to
// show the federated login button, and what to write on it.
//
// It holds these three values and nothing else. The OIDC configuration
// also carries the client secret, the issuer and the claim mapping, none
// of which an unauthenticated caller has any business seeing, so this
// endpoint reports a hand-built struct rather than serializing the
// configuration.
type authCapabilitiesInfo struct {
	LocalEnabled bool   `json:"local_enabled"`
	OIDCEnabled  bool   `json:"oidc_enabled"`
	OIDCLabel    string `json:"oidc_label"`
}

// defaultOIDCButtonLabel is what the federated login button says when
// the operator did not name their identity provider.
const defaultOIDCButtonLabel = "Sign in with SSO"

// authCapabilities derives the login page state from the dependencies,
// so that the handler itself never reaches into the configuration.
//
// OIDC counts as enabled only when a provider was actually discovered at
// start-up as well as switched on in the configuration, since a button
// pointing at an endpoint that answers 404 is worse than no button.
func authCapabilities(deps *HandlerDependencies) authCapabilitiesInfo {
	info := authCapabilitiesInfo{LocalEnabled: true}
	if deps == nil || deps.Config == nil {
		return info
	}

	info.LocalEnabled = deps.Config.HTTP.Auth.LocalEnabled()
	info.OIDCEnabled = deps.Config.HTTP.Auth.OIDC.IsEnabled() && deps.OIDCProvider != nil
	if info.OIDCEnabled {
		info.OIDCLabel = deps.Config.HTTP.Auth.OIDC.ButtonLabel
		if info.OIDCLabel == "" {
			info.OIDCLabel = defaultOIDCButtonLabel
		}
	}
	return info
}

// handleCapabilities returns server capability flags for the client
func handleCapabilities(aiEnabled bool, maxIterations int,
	authInfo authCapabilitiesInfo) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		api.RespondJSON(w, http.StatusOK, map[string]any{
			"ai_enabled":     aiEnabled,
			"max_iterations": maxIterations,
			"auth":           authInfo,
		})
	}
}

// setupLLMHandlers configures LLM proxy endpoints by mounting the
// library LLM proxy handler under /api/v1/llm. Returns an error if the
// proxy handler cannot be constructed.
func setupLLMHandlers(mux *http.ServeMux, cfg *config.Config, toolProvider api.ContextAwareToolProvider, memoryStore *memory.Store, authStore *auth.AuthStore) error {
	// Build a compact description lookup map from registered tools.
	// The web client sends tools without CompactDescription populated,
	// so the proxy's TransformRequest hook looks them up server-side and
	// applies them before dispatch.
	compactDescs := make(map[string]string)
	if toolProvider != nil {
		for _, t := range toolProvider.List() {
			if t.CompactDescription != "" {
				compactDescs[t.Name] = t.CompactDescription
			}
		}
	}

	// Create LLM proxy configuration
	llmConfig := &llmproxy.Config{
		Provider:               cfg.LLM.Provider,
		Model:                  cfg.LLM.Model,
		AnthropicAPIKey:        cfg.LLM.AnthropicAPIKey,
		AnthropicBaseURL:       cfg.LLM.AnthropicBaseURL,
		OpenAIAPIKey:           cfg.LLM.OpenAIAPIKey,
		OpenAIBaseURL:          cfg.LLM.OpenAIBaseURL,
		GeminiAPIKey:           cfg.LLM.GeminiAPIKey,
		GeminiBaseURL:          cfg.LLM.GeminiBaseURL,
		OllamaURL:              cfg.LLM.OllamaURL,
		MaxTokens:              cfg.LLM.MaxTokens,
		Temperature:            cfg.LLM.Temperature,
		UseCompactDescriptions: cfg.LLM.UseCompactDescriptions(),
		CompactDescriptions:    compactDescs,
		MemoryStore:            memoryStore,
		AuthStore:              authStore,
		LLMConfig:              &cfg.LLM,
	}

	// Mount the library LLM proxy under the /api/v1/llm prefix. The proxy
	// registers providers/models/health (public) and chat/chat/stream/
	// embed/rerank (authed) under this prefix; its Authorize hook enforces
	// the public/auth split and its TransformRequest hook injects the
	// Workbench system prompt, pinned memory, and user context.
	h, err := llmproxy.NewHandler(llmConfig)
	if err != nil {
		return fmt.Errorf("failed to create LLM proxy handler: %w", err)
	}
	mux.Handle("/api/v1/llm/", h)
	return nil
}
