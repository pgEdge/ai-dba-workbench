/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
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
		authHandler := api.NewAuthHandler(deps.AuthStore, deps.RateLimiter)
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

		// Connection management endpoints (for selecting monitored database connections)
		connHandler := api.NewConnectionHandler(deps.Datastore, deps.AuthStore)
		connHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Connection management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Connection management: DISABLED (datastore not configured)\n")
		}

		// Cluster hierarchy endpoints (for ClusterNavigator component)
		clusterHandler := api.NewClusterHandler(deps.Datastore, deps.AuthStore)
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

		// Timeline endpoints (for EventTimeline component)
		timelineHandler := api.NewTimelineHandler(deps.Datastore, deps.AuthStore)
		timelineHandler.RegisterRoutes(mux, authWrapper)
		if deps.Datastore != nil {
			fmt.Fprintf(os.Stderr, "Timeline events: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Timeline events: DISABLED (datastore not configured)\n")
		}

		return nil
	}
}

// createAuthWrapper creates a handler wrapper that enforces authentication
func createAuthWrapper(authStore *auth.AuthStore) func(http.HandlerFunc) http.HandlerFunc {
	return func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization header",
					http.StatusUnauthorized)
				return
			}

			// Extract Bearer token
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				http.Error(w, "Invalid Authorization header format",
					http.StatusUnauthorized)
				return
			}

			// Try API/service token first, then session token
			if _, err := authStore.ValidateToken(token); err != nil {
				// Try session token
				if _, err := authStore.ValidateSessionToken(token); err != nil {
					http.Error(w, "Invalid or expired token",
						http.StatusUnauthorized)
					return
				}
			}

			// Token valid - add token hash to context for tracing and isolation
			tokenHash := auth.GetTokenHashByRawToken(token)
			ctx := context.WithValue(r.Context(), auth.TokenHashContextKey, tokenHash)
			r = r.WithContext(ctx)

			// Proceed with handler
			handler(w, r)
		}
	}
}

// createUserInfoHandler creates a handler for the user info endpoint
func createUserInfoHandler(authStore *auth.AuthStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract session token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			api.RespondJSON(w, http.StatusOK, map[string]interface{}{
				"authenticated": false,
			})
			return
		}

		// Extract Bearer token
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			api.RespondJSON(w, http.StatusOK, map[string]interface{}{
				"authenticated": false,
				"error":         "Invalid Authorization header format",
			})
			return
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

		// Return user info as JSON
		api.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": true,
			"username":      username,
			"is_superuser":  isSuperuser,
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
