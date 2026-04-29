/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// ContextAwareHandler is a function that reads a resource with context and database client
type ContextAwareHandler func(ctx context.Context, dbClient *database.Client) (mcp.ResourceContent, error)

// ContextAwareRegistry wraps a resource registry and provides per-token database clients
// This ensures connection isolation in HTTP/HTTPS mode with authentication
type ContextAwareRegistry struct {
	clientManager   *database.ClientManager
	customResources map[string]customResource
	cfg             *config.Config
	authStore       *auth.AuthStore          // Auth store for connection sessions
	datastore       *database.Datastore      // Datastore for monitored connection info
	rbacChecker     *auth.RBACChecker        // RBAC checker for privilege-based access control
	clientResolver  *database.ClientResolver // Resolver for per-token database clients
}

// customResource represents a user-defined resource
type customResource struct {
	definition mcp.Resource
	handler    ContextAwareHandler
}

// authSessionAdapter adapts auth.AuthStore to database.SessionProvider
// This allows the database package to retrieve session data without importing auth
type authSessionAdapter struct {
	store *auth.AuthStore
}

func (a *authSessionAdapter) GetConnectionSession(tokenHash string) (*database.ConnectionSession, error) {
	s, err := a.store.GetConnectionSession(tokenHash)
	if err != nil || s == nil {
		return nil, err
	}
	return &database.ConnectionSession{
		ConnectionID: s.ConnectionID,
		DatabaseName: s.DatabaseName,
	}, nil
}

func (a *authSessionAdapter) ClearConnectionSession(tokenHash string) error {
	return a.store.ClearConnectionSession(tokenHash)
}

// NewContextAwareRegistry creates a new context-aware resource registry
func NewContextAwareRegistry(clientManager *database.ClientManager, cfg *config.Config, authStore *auth.AuthStore, datastore *database.Datastore) *ContextAwareRegistry {
	rbacChecker := auth.NewRBACCheckerForDatastore(authStore, datastore)

	// Build the ClientResolver for per-token database client resolution
	var clientResolver *database.ClientResolver
	if clientManager != nil {
		clientResolver = &database.ClientResolver{
			TokenExtractor: auth.GetTokenHashFromContext,
			ClientManager:  clientManager,
		}
		// Add session-based resolution if authStore and datastore are available
		if authStore != nil && datastore != nil {
			clientResolver.Sessions = &authSessionAdapter{store: authStore}
			clientResolver.Access = rbacChecker
			clientResolver.ConnInfo = datastore
		}
	}

	return &ContextAwareRegistry{
		clientManager:   clientManager,
		customResources: make(map[string]customResource),
		cfg:             cfg,
		authStore:       authStore,
		datastore:       datastore,
		rbacChecker:     rbacChecker,
		clientResolver:  clientResolver,
	}
}

// List returns all available resource definitions
// Note: This returns ALL resources without RBAC filtering. Use ListForContext for filtered results.
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

	if r.cfg.Builtins.Resources.IsResourceEnabled(URIConnectionInfo) {
		resources = append(resources, ConnectionInfoResourceDefinition())
	}

	// Add custom resources
	for _, customRes := range r.customResources {
		resources = append(resources, customRes.definition)
	}

	return resources
}

// ListForContext returns resource definitions filtered by the user's RBAC privileges
// This is the RBAC-aware version of List() that should be used in authenticated contexts
func (r *ContextAwareRegistry) ListForContext(ctx context.Context) []mcp.Resource {
	allResources := r.List()

	// If auth is disabled or user is superuser, return all resources
	if r.rbacChecker.IsSuperuser(ctx) {
		return allResources
	}

	// Filter resources based on user's privileges
	var filtered []mcp.Resource
	for _, resource := range allResources {
		if r.rbacChecker.CanAccessMCPItem(ctx, resource.URI) {
			filtered = append(filtered, resource)
		}
	}

	return filtered
}

// Read retrieves a resource by URI with the appropriate database client
func (r *ContextAwareRegistry) Read(ctx context.Context, uri string) (mcp.ResourceContent, error) {
	startTime := time.Now()
	tokenHash := auth.GetTokenHashFromContext(ctx)
	requestID := mcp.GetRequestIDFromContext(ctx)
	sessionID := tokenHash // Use token hash as session ID

	// Log resource read if tracing is enabled
	if tracing.IsEnabled() {
		tracing.LogResourceRead(sessionID, tokenHash, requestID, uri)
	}

	// Helper to log result and return
	logAndReturn := func(content mcp.ResourceContent, err error) (mcp.ResourceContent, error) {
		if tracing.IsEnabled() {
			duration := time.Since(startTime)
			var result any
			if len(content.Contents) > 0 {
				// Extract text content for logging
				texts := make([]string, 0, len(content.Contents))
				for _, c := range content.Contents {
					if c.Text != "" {
						texts = append(texts, c.Text)
					}
				}
				if len(texts) == 1 {
					result = texts[0]
				} else if len(texts) > 1 {
					result = texts
				}
			}
			tracing.LogResourceResult(sessionID, tokenHash, requestID, uri, result, err, duration)
		}
		return content, err
	}

	// RBAC check: verify user has access to this resource
	if !r.rbacChecker.CanAccessMCPItem(ctx, uri) {
		return logAndReturn(mcp.ResourceContent{
			URI: uri,
			Contents: []mcp.ContentItem{
				{
					Type: "text",
					Text: fmt.Sprintf("Access denied: you do not have permission to read resource '%s'", uri),
				},
			},
		}, nil)
	}

	// Check if this is a custom resource first
	if customRes, exists := r.customResources[uri]; exists {
		// Get database client for custom resource
		dbClient, err := r.getClient(ctx)
		if err != nil {
			return logAndReturn(mcp.ResourceContent{
				URI: uri,
				Contents: []mcp.ContentItem{
					{
						Type: "text",
						Text: fmt.Sprintf("Error: %v", err),
					},
				},
			}, nil)
		}
		content, err := customRes.handler(ctx, dbClient)
		return logAndReturn(content, err)
	}

	// Check if URI is a known resource before trying to get client
	// This ensures unknown URIs return "Resource not found" instead of connection errors
	switch uri {
	case URISystemInfo, URIConnectionInfo:
		// Valid URI, continue to process
	default:
		return logAndReturn(mcp.ResourceContent{
			URI: uri,
			Contents: []mcp.ContentItem{
				{
					Type: "text",
					Text: "Resource not found: " + uri,
				},
			},
		}, nil)
	}

	// Check if the built-in resource is enabled
	if !r.cfg.Builtins.Resources.IsResourceEnabled(uri) {
		return logAndReturn(mcp.ResourceContent{
			URI: uri,
			Contents: []mcp.ContentItem{
				{
					Type: "text",
					Text: fmt.Sprintf("Resource '%s' is not available", uri),
				},
			},
		}, nil)
	}

	// Handle connection_info resource specially - it doesn't query a database
	if uri == URIConnectionInfo {
		content, err := r.readConnectionInfo(ctx)
		return logAndReturn(content, err)
	}

	// Get the appropriate database client for built-in resources that need it
	dbClient, err := r.getClient(ctx)
	if err != nil {
		return logAndReturn(mcp.ResourceContent{
			URI: uri,
			Contents: []mcp.ContentItem{
				{
					Type: "text",
					Text: fmt.Sprintf("Error: %v", err),
				},
			},
		}, nil)
	}

	// Create resource handler with the correct client
	// Note: At this point URI has already been validated as a known resource
	resource := PGSystemInfoResource(dbClient)
	content, err := resource.Handler()
	return logAndReturn(content, err)
}

// readConnectionInfo returns the current connection context without querying a database
func (r *ContextAwareRegistry) readConnectionInfo(ctx context.Context) (mcp.ResourceContent, error) {
	// Get token hash from context
	tokenHash := auth.GetTokenHashFromContext(ctx)
	if tokenHash == "" {
		info := NewNoConnectionInfo()
		info.Message = "No authentication token found in request context"
		return BuildConnectionInfoResponse(info)
	}

	// Check if authStore and datastore are available
	if r.authStore == nil || r.datastore == nil {
		info := &ConnectionInfo{
			Connected: false,
			Message:   "Connection management not available (datastore not configured)",
		}
		return BuildConnectionInfoResponse(info)
	}

	// Get the connection session for this token
	session, err := r.authStore.GetConnectionSession(tokenHash)
	if err != nil {
		info := &ConnectionInfo{
			Connected: false,
			Message:   fmt.Sprintf("Error retrieving connection session: %v", err),
		}
		return BuildConnectionInfoResponse(info)
	}

	if session == nil {
		// No connection selected
		return BuildConnectionInfoResponse(NewNoConnectionInfo())
	}

	// Get connection details from datastore
	conn, _, err := r.datastore.GetConnectionWithPassword(ctx, session.ConnectionID)
	if err != nil {
		info := &ConnectionInfo{
			Connected: false,
			Message:   fmt.Sprintf("Error retrieving connection details: %v", err),
		}
		return BuildConnectionInfoResponse(info)
	}

	// Determine the actual database name (session override or connection default)
	databaseName := conn.DatabaseName
	if session.DatabaseName != nil {
		databaseName = *session.DatabaseName
	}

	// Build the connection info
	info := NewConnectionInfo(
		conn.ID,
		conn.Name,
		conn.Host,
		conn.Port,
		databaseName,
		conn.Username,
		conn.IsMonitored,
	)

	return BuildConnectionInfoResponse(info)
}

// getClient returns the appropriate database client based on authentication state
func (r *ContextAwareRegistry) getClient(ctx context.Context) (*database.Client, error) {
	return r.clientResolver.ResolveOrError(ctx)
}
