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
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/compactor"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
)

// HandlerDependencies holds all dependencies needed for HTTP handlers
type HandlerDependencies struct {
	AuthStore   *auth.AuthStore
	RateLimiter *auth.RateLimiter
	IPExtractor *auth.IPExtractor
	ConvStore   *conversations.Store
	Datastore   *database.Datastore
	Config      *config.Config
}

// SetupHandlers configures all HTTP handlers for the server
func SetupHandlers(deps *HandlerDependencies) func(*http.ServeMux) error {
	return func(mux *http.ServeMux) error {
		// Helper to wrap handlers with authentication
		authWrapper := createAuthWrapper(deps.AuthStore)

		// OpenAPI specification endpoint (public - used for API discovery)
		mux.HandleFunc("/api/v1/openapi.json", handleOpenAPISpec)

		// Authentication endpoint (does NOT require auth - it IS the login endpoint)
		// IPExtractor provides secure IP extraction that only trusts X-Forwarded-For
		// from configured trusted proxies, preventing rate limit bypass via IP spoofing
		// The TLS enabled flag ensures cookies are marked Secure when using HTTPS
		tlsEnabled := deps.Config != nil && deps.Config.HTTP.TLS.Enabled
		authHandler := api.NewAuthHandler(deps.AuthStore, deps.RateLimiter, deps.IPExtractor, tlsEnabled)
		authHandler.RegisterRoutes(mux)

		// Chat history compaction endpoint
		mux.HandleFunc("/api/v1/chat/compact",
			authWrapper(compactor.HandleCompact))

		// User info endpoint - returns auth status (no error if not logged in)
		mux.HandleFunc("/api/v1/user/info",
			createUserInfoHandler(deps.AuthStore))

		// LLM proxy handlers (always enabled)
		setupLLMHandlers(mux, deps.Config, authWrapper)

		// Conversation history endpoints (only if store is available)
		if deps.ConvStore != nil && deps.AuthStore != nil {
			convHandler := conversations.NewHandler(deps.ConvStore, deps.AuthStore)
			convHandler.RegisterRoutes(mux, authWrapper)
			fmt.Fprintf(os.Stderr, "Conversation history: ENABLED\n")
		}

		// Create RBAC checker for permission-based access control in REST handlers
		rbacChecker := auth.NewRBACChecker(deps.AuthStore, true)

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
		connHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Connection management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Connection management: DISABLED (datastore not configured)\n")
		}

		// Cluster hierarchy endpoints (for ClusterNavigator component)
		clusterHandler := api.NewClusterHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		clusterHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Cluster management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Cluster management: DISABLED (datastore not configured)\n")
		}

		// Alert endpoints (for StatusPanel component)
		alertHandler := api.NewAlertHandler(deps.Datastore, deps.AuthStore)
		alertHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Alert management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Alert management: DISABLED (datastore not configured)\n")
		}

		// Blackout management endpoints (for alert suppression windows)
		blackoutHandler := api.NewBlackoutHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		blackoutHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Blackout management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Blackout management: DISABLED (datastore not configured)\n")
		}

		// Probe configuration endpoints (for configurable collection intervals)
		probeConfigHandler := api.NewProbeConfigHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		probeConfigHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Probe configuration: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Probe configuration: DISABLED (datastore not configured)\n")
		}

		// Alert rule configuration endpoints (for configurable alert thresholds)
		alertRuleHandler := api.NewAlertRuleHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		alertRuleHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Alert rule configuration: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Alert rule configuration: DISABLED (datastore not configured)\n")
		}

		// Notification channel management endpoints (for alert channel configuration)
		notificationChannelHandler := api.NewNotificationChannelHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		notificationChannelHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Notification channel management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Notification channel management: DISABLED (datastore not configured)\n")
		}

		// Timeline endpoints (for EventTimeline component)
		timelineHandler := api.NewTimelineHandler(deps.Datastore, deps.AuthStore)
		timelineHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Timeline events: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Timeline events: DISABLED (datastore not configured)\n")
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

// createAuthWrapper creates a handler wrapper that enforces authentication
// Supports both Authorization header (for API tokens) and session cookies (for browser sessions)
func createAuthWrapper(authStore *auth.AuthStore) func(http.HandlerFunc) http.HandlerFunc {
	return func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var token string

			// Try Authorization header first (for API tokens and backwards compatibility)
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				// Extract Bearer token
				token = strings.TrimPrefix(authHeader, "Bearer ")
				if token == authHeader {
					http.Error(w, "Invalid Authorization header format",
						http.StatusUnauthorized)
					return
				}
			} else {
				// Try to get token from httpOnly session cookie
				cookie, err := r.Cookie("session_token")
				if err != nil || cookie.Value == "" {
					http.Error(w, "Missing authentication credentials",
						http.StatusUnauthorized)
					return
				}
				token = cookie.Value
			}

			// Token valid - add token hash to context for tracing and isolation
			tokenHash := auth.GetTokenHashByRawToken(token)
			ctx := context.WithValue(r.Context(), auth.TokenHashContextKey, tokenHash)

			// Try API token first, then session token
			// Populate RBAC context values (UserID, IsSuperuser) for permission checks
			storedToken, err := authStore.ValidateToken(token)
			if err == nil && storedToken != nil {
				ctx = context.WithValue(ctx, auth.UserIDContextKey, storedToken.OwnerID)
				// Look up user to determine superuser status
				user, userErr := authStore.GetUserByID(storedToken.OwnerID)
				if userErr == nil && user != nil {
					ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, user.IsSuperuser)
				}
			} else {
				// Try session token
				username, sessionErr := authStore.ValidateSessionToken(token)
				if sessionErr != nil {
					http.Error(w, "Invalid or expired token",
						http.StatusUnauthorized)
					return
				}
				// Get user ID and superuser status for RBAC
				user, userErr := authStore.GetUser(username)
				if userErr == nil && user != nil {
					ctx = context.WithValue(ctx, auth.UserIDContextKey, user.ID)
					ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, user.IsSuperuser)
				}
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
		var token string

		// Try Authorization header first (for API tokens and backwards compatibility)
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			// Extract Bearer token
			token = strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				api.RespondJSON(w, http.StatusOK, map[string]interface{}{
					"authenticated": false,
					"error":         "Invalid Authorization header format",
				})
				return
			}
		} else {
			// Try to get token from httpOnly session cookie
			cookie, err := r.Cookie("session_token")
			if err != nil || cookie.Value == "" {
				api.RespondJSON(w, http.StatusOK, map[string]interface{}{
					"authenticated": false,
				})
				return
			}
			token = cookie.Value
		}

		// Validate session token and get username
		username, err := authStore.ValidateSessionToken(token)
		if err != nil {
			api.RespondJSON(w, http.StatusOK, map[string]interface{}{
				"authenticated": false,
				"error":         "Invalid or expired session",
			})
			return
		}

		// Look up user to get superuser status
		isSuperuser := false
		user, userErr := authStore.GetUser(username)
		if userErr == nil && user != nil {
			isSuperuser = user.IsSuperuser
		}

		// Get admin permissions for the user
		var adminPermissions []string
		if user != nil {
			perms, permErr := authStore.GetUserAdminPermissions(user.ID)
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
		api.RespondJSON(w, http.StatusOK, map[string]interface{}{
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

// setupLLMHandlers configures LLM proxy endpoints
func setupLLMHandlers(mux *http.ServeMux, cfg *config.Config, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	// Create LLM proxy configuration
	llmConfig := &llmproxy.Config{
		Provider:        cfg.LLM.Provider,
		Model:           cfg.LLM.Model,
		AnthropicAPIKey: cfg.LLM.AnthropicAPIKey,
		OpenAIAPIKey:    cfg.LLM.OpenAIAPIKey,
		OllamaURL:       cfg.LLM.OllamaURL,
		MaxTokens:       cfg.LLM.MaxTokens,
		Temperature:     cfg.LLM.Temperature,
	}

	// Provider/model listing don't require auth (needed for login page)
	mux.HandleFunc("/api/v1/llm/providers",
		func(w http.ResponseWriter, r *http.Request) {
			llmproxy.HandleProviders(w, r, llmConfig)
		})
	mux.HandleFunc("/api/v1/llm/models",
		func(w http.ResponseWriter, r *http.Request) {
			llmproxy.HandleModels(w, r, llmConfig)
		})
	// Chat endpoint requires auth (makes actual LLM API calls)
	mux.HandleFunc("/api/v1/llm/chat",
		authWrapper(func(w http.ResponseWriter, r *http.Request) {
			llmproxy.HandleChat(w, r, llmConfig)
		}))
}
