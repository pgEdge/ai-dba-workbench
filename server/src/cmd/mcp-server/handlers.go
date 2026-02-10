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
	"github.com/pgedge/ai-workbench/server/internal/overview"
)

// routeRegistrar is implemented by any handler that can register routes on a mux.
type routeRegistrar interface {
	RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc)
}

// registerDatastoreHandler registers a handler that depends on the datastore and
// logs its enabled/disabled status.  This eliminates the repeated if/else pattern.
func registerDatastoreHandler(mux *http.ServeMux, handler routeRegistrar, authWrapper func(http.HandlerFunc) http.HandlerFunc, name string, datastore interface{}) {
	handler.RegisterRoutes(mux, authWrapper)
	if datastore != nil {
		fmt.Fprintf(os.Stderr, "%s: ENABLED\n", name)
	} else {
		fmt.Fprintf(os.Stderr, "%s: DISABLED (datastore not configured)\n", name)
	}
}

// HandlerDependencies holds all dependencies needed for HTTP handlers
type HandlerDependencies struct {
	AuthStore   *auth.AuthStore
	RateLimiter *auth.RateLimiter
	IPExtractor *auth.IPExtractor
	ConvStore   *conversations.Store
	Datastore   *database.Datastore
	Config      *config.Config
	OverviewGen *overview.Generator
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
		registerDatastoreHandler(mux, connHandler, authWrapper, "Connection management", deps.Datastore)

		// Cluster hierarchy endpoints (for ClusterNavigator component)
		clusterHandler := api.NewClusterHandler(deps.Datastore, deps.AuthStore, rbacChecker)
		registerDatastoreHandler(mux, clusterHandler, authWrapper, "Cluster management", deps.Datastore)

		// Alert endpoints (for StatusPanel component)
		alertHandler := api.NewAlertHandler(deps.Datastore, deps.AuthStore)
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

		// Timeline endpoints (for EventTimeline component)
		timelineHandler := api.NewTimelineHandler(deps.Datastore, deps.AuthStore)
		registerDatastoreHandler(mux, timelineHandler, authWrapper, "Timeline events", deps.Datastore)

		// AI Overview endpoint (for estate overview summary)
		if deps.OverviewGen != nil {
			overviewHandler := overview.NewHandler(deps.OverviewGen)
			overviewHandler.RegisterRoutes(mux, authWrapper)
			fmt.Fprintf(os.Stderr, "AI Overview API: ENABLED\n")
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
// It checks the Authorization header first (Bearer token), then falls
// back to the session_token cookie.  Returns the raw token string and
// a boolean indicating whether extraction succeeded plus an error
// message suitable for the client when it fails.
func extractToken(r *http.Request) (string, bool, string) {
	// Try Authorization header first (for API tokens and backwards compatibility)
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return "", false, "Invalid Authorization header format"
		}
		return token, true, ""
	}

	// Try to get token from httpOnly session cookie
	cookie, err := r.Cookie("session_token")
	if err != nil || cookie.Value == "" {
		return "", false, "Missing authentication credentials"
	}
	return cookie.Value, true, ""
}

// createAuthWrapper creates a handler wrapper that enforces authentication
// Supports both Authorization header (for API tokens) and session cookies (for browser sessions)
func createAuthWrapper(authStore *auth.AuthStore) func(http.HandlerFunc) http.HandlerFunc {
	return func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token, ok, errMsg := extractToken(r)
			if !ok {
				http.Error(w, errMsg, http.StatusUnauthorized)
				return
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
		token, ok, errMsg := extractToken(r)
		if !ok {
			// For auth header format errors, report the issue;
			// for missing credentials just report unauthenticated.
			resp := map[string]interface{}{"authenticated": false}
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
				api.RespondJSON(w, http.StatusOK, map[string]interface{}{
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
				api.RespondJSON(w, http.StatusOK, map[string]interface{}{
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
