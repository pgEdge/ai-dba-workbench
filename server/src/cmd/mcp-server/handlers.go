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
	"errors"
	"fmt"
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
		authWrapper := createAuthWrapper(deps.AuthStore)

		// NOTE: These endpoints are intentionally unauthenticated to allow
		// API tooling (e.g. RESTish) to discover the API schema without
		// requiring credentials.
		mux.HandleFunc("/api/v1/openapi.json", handleOpenAPISpec)

		// Capabilities endpoint (public - used by client to detect available features)
		maxIterations := 50
		if deps.Config != nil && deps.Config.LLM.MaxIterations > 0 {
			maxIterations = deps.Config.LLM.MaxIterations
		}
		mux.HandleFunc("/api/v1/capabilities", handleCapabilities(deps.AIEnabled, maxIterations))

		// Authentication endpoint (does NOT require auth - it IS the login endpoint)
		// IPExtractor provides secure IP extraction that only trusts X-Forwarded-For
		// from configured trusted proxies, preventing rate limit bypass via IP spoofing
		// The TLS enabled flag ensures cookies are marked Secure when using HTTPS
		tlsEnabled := deps.Config != nil && deps.Config.HTTP.TLS.Enabled
		authHandler := api.NewAuthHandler(deps.AuthStore, deps.RateLimiter, deps.IPExtractor, tlsEnabled)

		// NewAuthHandler starts a cleanup goroutine for its internal
		// login rate limiter, which only Close stops. Hand that back
		// to the server so it is stopped on shutdown rather than
		// running for the remaining life of the process.
		if deps.RegisterCloser != nil {
			deps.RegisterCloser(authHandler.Close)
		}
		authHandler.RegisterRoutes(mux)

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

// extractToken extracts a bearer or session token from the request.
// It delegates to auth.ExtractBearerToken and returns the raw token
// string, a boolean indicating whether extraction succeeded, and an
// error message suitable for the client when it fails.
func extractToken(r *http.Request) (string, bool, string) {
	token := auth.ExtractBearerToken(r)
	if token == "" {
		return "", false, "Missing or invalid authentication credentials"
	}
	return token, true, ""
}

// createAuthWrapper creates a handler wrapper that enforces authentication
// Supports both Authorization header (for API tokens) and session cookies (for browser sessions).
// The actual token/session validation is delegated to auth.AuthenticateRequest,
// the single shared implementation used by both this middleware and the
// LLM proxy's Authorize hook.
func createAuthWrapper(authStore *auth.AuthStore) func(http.HandlerFunc) http.HandlerFunc {
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

			r = r.WithContext(ctx)

			// Proceed with handler
			handler(w, r)
		}
	}
}

// createUserInfoHandler creates a handler for the user info endpoint
func createUserInfoHandler(authStore *auth.AuthStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok, errMsg := extractToken(r)
		if !ok {
			// For auth header format errors, report the issue;
			// for missing credentials just report unauthenticated.
			resp := map[string]any{"authenticated": false}
			if errMsg == "Invalid Authorization header format" {
				resp["error"] = errMsg
			}
			api.RespondJSON(w, http.StatusOK, resp)
			return
		}

		// Try API token first, then fall back to session token
		var username string
		var isSuperuser bool
		var userID int64

		storedToken, tokenErr := authStore.ValidateToken(token)
		if tokenErr == nil && storedToken != nil {
			// Valid API token - look up the owner user
			owner, ownerErr := authStore.GetUserByID(storedToken.OwnerID)
			if ownerErr != nil || owner == nil {
				api.RespondJSON(w, http.StatusOK, map[string]any{
					"authenticated": false,
					"error":         "Invalid or expired token",
				})
				return
			}
			username = owner.Username
			isSuperuser = owner.IsSuperuser
			userID = owner.ID
		} else {
			// Try session token
			sessionUsername, sessionErr := authStore.ValidateSessionToken(token)
			if sessionErr != nil {
				api.RespondJSON(w, http.StatusOK, map[string]any{
					"authenticated": false,
					"error":         "Invalid or expired session",
				})
				return
			}
			username = sessionUsername
			user, userErr := authStore.GetUser(username)
			if userErr == nil && user != nil {
				isSuperuser = user.IsSuperuser
				userID = user.ID
			}
		}

		// Get admin permissions for the user
		var adminPermissions []string
		if userID > 0 {
			perms, permErr := authStore.GetUserAdminPermissions(userID)
			if permErr == nil {
				for perm := range perms {
					adminPermissions = append(adminPermissions, perm)
				}
			}
		}
		if adminPermissions == nil {
			adminPermissions = []string{}
		}

		// Return user info as JSON
		api.RespondJSON(w, http.StatusOK, map[string]any{
			"authenticated":     true,
			"username":          username,
			"is_superuser":      isSuperuser,
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

// handleCapabilities returns server capability flags for the client
func handleCapabilities(aiEnabled bool, maxIterations int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		api.RespondJSON(w, http.StatusOK, map[string]any{
			"ai_enabled":     aiEnabled,
			"max_iterations": maxIterations,
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
