/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package resources

import (
	"context"
	"fmt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// ContextAwareHandler is a function that reads a resource with context and database client
type ContextAwareHandler func(ctx context.Context, dbClient *database.Client) (mcp.ResourceContent, error)

// ContextAwareRegistry wraps a resource registry and provides per-token database clients
// This ensures connection isolation in HTTP/HTTPS mode with authentication
type ContextAwareRegistry struct {
	clientManager   *database.ClientManager
	authEnabled     bool
	customResources map[string]customResource
	cfg             *config.Config
	authStore       *auth.AuthStore     // Auth store for connection sessions
	datastore       *database.Datastore // Datastore for monitored connection info
}

// customResource represents a user-defined resource
type customResource struct {
	definition mcp.Resource
	handler    ContextAwareHandler
}

// NewContextAwareRegistry creates a new context-aware resource registry
func NewContextAwareRegistry(clientManager *database.ClientManager, authEnabled bool, cfg *config.Config, authStore *auth.AuthStore, datastore *database.Datastore) *ContextAwareRegistry {
	return &ContextAwareRegistry{
		clientManager:   clientManager,
		authEnabled:     authEnabled,
		customResources: make(map[string]customResource),
		cfg:             cfg,
		authStore:       authStore,
		datastore:       datastore,
	}
}

// List returns all available resource definitions
func (r *ContextAwareRegistry) List() []mcp.Resource {
	// Start with static built-in resources (only include enabled ones)
	resources := []mcp.Resource{}

	if r.cfg.Builtins.Resources.IsResourceEnabled(URISystemInfo) {
		resources = append(resources, mcp.Resource{
			URI:         URISystemInfo,
			Name:        "PostgreSQL System Information",
			Description: "Returns PostgreSQL version, operating system, and build architecture information. Provides a quick way to check server version and platform details.",
			MimeType:    "application/json",
		})
	}

	// Add custom resources
	for _, customRes := range r.customResources {
		resources = append(resources, customRes.definition)
	}

	return resources
}

// Read retrieves a resource by URI with the appropriate database client
func (r *ContextAwareRegistry) Read(ctx context.Context, uri string) (mcp.ResourceContent, error) {
	// Check if this is a custom resource first
	if customRes, exists := r.customResources[uri]; exists {
		// Get database client for custom resource
		dbClient, err := r.getClient(ctx)
		if err != nil {
			return mcp.ResourceContent{
				URI: uri,
				Contents: []mcp.ContentItem{
					{
						Type: "text",
						Text: fmt.Sprintf("Error: %v", err),
					},
				},
			}, nil
		}
		return customRes.handler(ctx, dbClient)
	}

	// Check if URI is a known resource before trying to get client
	// This ensures unknown URIs return "Resource not found" instead of connection errors
	switch uri {
	case URISystemInfo:
		// Valid URI, continue to process
	default:
		return mcp.ResourceContent{
			URI: uri,
			Contents: []mcp.ContentItem{
				{
					Type: "text",
					Text: "Resource not found: " + uri,
				},
			},
		}, nil
	}

	// Check if the built-in resource is enabled
	if uri == URISystemInfo && !r.cfg.Builtins.Resources.IsResourceEnabled(uri) {
		return mcp.ResourceContent{
			URI: uri,
			Contents: []mcp.ContentItem{
				{
					Type: "text",
					Text: fmt.Sprintf("Resource '%s' is not available", uri),
				},
			},
		}, nil
	}

	// Get the appropriate database client for built-in resources
	dbClient, err := r.getClient(ctx)
	if err != nil {
		return mcp.ResourceContent{
			URI: uri,
			Contents: []mcp.ContentItem{
				{
					Type: "text",
					Text: fmt.Sprintf("Error: %v", err),
				},
			},
		}, nil
	}

	// Create resource handler with the correct client
	// Note: At this point URI has already been validated as a known resource
	resource := PGSystemInfoResource(dbClient)
	return resource.Handler()
}

// getClient returns the appropriate database client based on authentication state
func (r *ContextAwareRegistry) getClient(ctx context.Context) (*database.Client, error) {
	if !r.authEnabled {
		// Authentication disabled - use "default" key in ClientManager
		client, err := r.clientManager.GetOrCreateClient("default", true)
		if err != nil {
			return nil, fmt.Errorf("no database connection configured: %w", err)
		}
		return client, nil
	}

	// Authentication enabled - get per-token client
	tokenHash := auth.GetTokenHashFromContext(ctx)
	if tokenHash == "" {
		return nil, fmt.Errorf("no authentication token found in request context")
	}

	// Check if there's a selected connection in the session
	if r.authStore != nil && r.datastore != nil {
		session, err := r.authStore.GetConnectionSession(tokenHash)
		if err != nil {
			// Log warning but continue to fallback
			fmt.Printf("WARNING: Failed to get connection session: %v\n", err)
		}

		if session != nil {
			// Get connection info from datastore
			conn, password, err := r.datastore.GetConnectionWithPassword(ctx, session.ConnectionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get connection info: %w", err)
			}

			// Build connection string with optional database override
			var databaseOverride string
			if session.DatabaseName != nil {
				databaseOverride = *session.DatabaseName
			}
			connStr := r.datastore.BuildConnectionString(conn, password, databaseOverride)

			// Get or create client using the connection string
			// Use a unique key combining token hash and connection ID
			clientKey := fmt.Sprintf("%s:conn:%d", tokenHash, session.ConnectionID)
			if session.DatabaseName != nil {
				clientKey = fmt.Sprintf("%s:db:%s", clientKey, *session.DatabaseName)
			}

			client, err := r.clientManager.GetOrCreateClientWithConnString(clientKey, connStr)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to selected database: %w", err)
			}

			return client, nil
		}

		// No connection selected - return helpful error
		return nil, fmt.Errorf("no database connection selected. Please select a database connection using your client interface (CLI or web client)")
	}

	// Fallback: Get or create client for this token using default config
	client, err := r.clientManager.GetOrCreateClient(tokenHash, true)
	if err != nil {
		return nil, fmt.Errorf("no database connection configured for this token: %w", err)
	}

	return client, nil
}
