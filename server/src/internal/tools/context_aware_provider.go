/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/memory"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// ContextAwareProvider wraps a tool registry and provides per-token database clients
// This ensures connection isolation in HTTP/HTTPS mode with authentication
type ContextAwareProvider struct {
	baseRegistry   *Registry // Registry for tool definitions (List operation)
	clientManager  *database.ClientManager
	resourceReg    *resources.ContextAwareRegistry
	fallbackClient *database.Client    // Fallback client when no session connection is selected
	cfg            *config.Config      // Server configuration (for embedding settings)
	authStore      *auth.AuthStore     // Auth store for users and tokens
	rateLimiter    *auth.RateLimiter   // Rate limiter for authentication attempts
	datastore      *database.Datastore // Datastore for monitored connection info
	rbacChecker    *auth.RBACChecker   // RBAC checker for privilege-based access control
	memoryStore    *memory.Store       // Memory store for chat memory persistence
	resolver       *ConnectionResolver // Resolver for explicit connection_id in tool args

	// Cache of registries per client to avoid re-creating tools on every Execute()
	mu               sync.RWMutex
	clientRegistries map[*database.Client]*Registry

	// Hidden tools registry (not advertised to LLM but available for execution)
	hiddenRegistry *Registry
}

// registerStatelessTools registers all stateless tools (those that don't require a database client)
func (p *ContextAwareProvider) registerStatelessTools(registry *Registry) {
	// Note: read_resource tool provides backward compatibility for resource access
	// Resources are also accessible via the native MCP resources/read endpoint
	// This tool is always enabled as it's used to list resources
	registry.Register("read_resource", ReadResourceTool(p.createResourceAdapter()))

	// Embedding generation tool (stateless, only requires config)
	if p.cfg.Builtins.Tools.IsToolEnabled("generate_embedding") {
		registry.Register("generate_embedding", GenerateEmbeddingTool(p.cfg))
	}

	// Knowledgebase search tool (if enabled, path configured, file exists, and tool enabled)
	if p.cfg.Knowledgebase.Enabled && p.cfg.Knowledgebase.DatabasePath != "" &&
		p.cfg.Builtins.Tools.IsToolEnabled("search_knowledgebase") {
		if _, err := os.Stat(p.cfg.Knowledgebase.DatabasePath); err == nil {
			registry.Register("search_knowledgebase", SearchKnowledgebaseTool(p.cfg.Knowledgebase.DatabasePath, p.cfg))
		}
	}
}

// registerDatastoreTools registers tools that query the datastore (metrics database)
func (p *ContextAwareProvider) registerDatastoreTools(registry *Registry) {
	// Datastore tools use the shared datastore pool, not per-token connections
	if p.datastore != nil {
		// Register metrics tools if datastore is configured
		datastorePool := p.datastore.GetPool()
		if p.cfg.Builtins.Tools.IsToolEnabled("list_probes") {
			registry.Register("list_probes", ListProbesTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("describe_probe") {
			registry.Register("describe_probe", DescribeProbeTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("query_metrics") {
			registry.Register("query_metrics", QueryMetricsTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("list_connections") {
			registry.Register("list_connections", ListConnectionsTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_alert_history") {
			registry.Register("get_alert_history", GetAlertHistoryTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_alert_rules") {
			registry.Register("get_alert_rules", GetAlertRulesTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_metric_baselines") {
			registry.Register("get_metric_baselines", GetMetricBaselinesTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("query_datastore") {
			registry.Register("query_datastore", QueryDatastoreTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_blackouts") {
			registry.Register("get_blackouts", GetBlackoutsTool(datastorePool))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("store_memory") && p.memoryStore != nil {
			registry.Register("store_memory", StoreMemoryTool(p.memoryStore, p.cfg))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("recall_memories") && p.memoryStore != nil {
			registry.Register("recall_memories", RecallMemoriesTool(p.memoryStore, p.cfg))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("delete_memory") && p.memoryStore != nil {
			registry.Register("delete_memory", DeleteMemoryTool(p.memoryStore))
		}
	} else {
		// Register tools with nil pool - they'll return helpful errors
		if p.cfg.Builtins.Tools.IsToolEnabled("list_probes") {
			registry.Register("list_probes", ListProbesTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("describe_probe") {
			registry.Register("describe_probe", DescribeProbeTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("query_metrics") {
			registry.Register("query_metrics", QueryMetricsTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("list_connections") {
			registry.Register("list_connections", ListConnectionsTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_alert_history") {
			registry.Register("get_alert_history", GetAlertHistoryTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_alert_rules") {
			registry.Register("get_alert_rules", GetAlertRulesTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_metric_baselines") {
			registry.Register("get_metric_baselines", GetMetricBaselinesTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("query_datastore") {
			registry.Register("query_datastore", QueryDatastoreTool(nil))
		}
		if p.cfg.Builtins.Tools.IsToolEnabled("get_blackouts") {
			registry.Register("get_blackouts", GetBlackoutsTool(nil))
		}
	}
}

// registerDatabaseTools registers all database-dependent tools
func (p *ContextAwareProvider) registerDatabaseTools(registry *Registry, client *database.Client) {
	if p.cfg.Builtins.Tools.IsToolEnabled("query_database") {
		registry.Register("query_database", QueryDatabaseTool(client, p.resolver))
	}
	if p.cfg.Builtins.Tools.IsToolEnabled("get_schema_info") {
		registry.Register("get_schema_info", GetSchemaInfoTool(client, p.resolver))
	}
	if p.cfg.Builtins.Tools.IsToolEnabled("similarity_search") {
		registry.Register("similarity_search", SimilaritySearchTool(client, p.resolver, p.cfg))
	}
	if p.cfg.Builtins.Tools.IsToolEnabled("execute_explain") {
		registry.Register("execute_explain", ExecuteExplainTool(client, p.resolver))
	}
	if p.cfg.Builtins.Tools.IsToolEnabled("count_rows") {
		registry.Register("count_rows", CountRowsTool(client, p.resolver))
	}
	if p.cfg.Builtins.Tools.IsToolEnabled("test_query") {
		registry.Register("test_query", TestQueryTool(client, p.resolver))
	}
}

// NewContextAwareProvider creates a new context-aware tool provider
func NewContextAwareProvider(clientManager *database.ClientManager, resourceReg *resources.ContextAwareRegistry, fallbackClient *database.Client, cfg *config.Config, authStore *auth.AuthStore, rateLimiter *auth.RateLimiter, datastore *database.Datastore) *ContextAwareProvider {
	rbacChecker := auth.NewRBACChecker(authStore)
	var resolver *ConnectionResolver
	if clientManager != nil && datastore != nil {
		resolver = NewConnectionResolver(clientManager, datastore, rbacChecker)
	}

	provider := &ContextAwareProvider{
		baseRegistry:     NewRegistry(),
		clientManager:    clientManager,
		resourceReg:      resourceReg,
		fallbackClient:   fallbackClient,
		cfg:              cfg,
		authStore:        authStore,
		rateLimiter:      rateLimiter,
		datastore:        datastore,
		rbacChecker:      rbacChecker,
		resolver:         resolver,
		clientRegistries: make(map[*database.Client]*Registry),
		hiddenRegistry:   NewRegistry(),
	}

	// Initialize memory store from the datastore pool (only when memory is enabled)
	if datastore != nil && cfg.Memory.IsEnabled() {
		provider.memoryStore = memory.NewStore(datastore.GetPool())
	}

	// Register ALL tools in base registry so they're always visible in tools/list
	// Database-dependent tools will fail gracefully in Execute() if no connection exists
	// This provides better UX - users can discover all tools even before connecting
	provider.registerStatelessTools(provider.baseRegistry)
	provider.registerDatastoreTools(provider.baseRegistry)
	provider.registerDatabaseTools(provider.baseRegistry, nil) // nil client for base registry

	return provider
}

// resourceReaderAdapter adapts ContextAwareRegistry to the ResourceReader interface
// This provides backward compatibility for the read_resource tool
type resourceReaderAdapter struct {
	registry *resources.ContextAwareRegistry
}

func (a *resourceReaderAdapter) List() []mcp.Resource {
	return a.registry.List()
}

func (a *resourceReaderAdapter) Read(ctx context.Context, uri string) (mcp.ResourceContent, error) {
	// Pass the context through to the ContextAwareRegistry
	// This ensures the authentication token is available for per-token connection isolation
	return a.registry.Read(ctx, uri)
}

// createResourceAdapter creates an adapter for the resource registry
func (p *ContextAwareProvider) createResourceAdapter() ResourceReader {
	return &resourceReaderAdapter{
		registry: p.resourceReg,
	}
}

// GetBaseRegistry returns the base registry for adding additional tools
func (p *ContextAwareProvider) GetBaseRegistry() *Registry {
	return p.baseRegistry
}

// GetMemoryStore returns the memory store instance
func (p *ContextAwareProvider) GetMemoryStore() *memory.Store {
	return p.memoryStore
}

// RegisterTools initializes tool registrations
// This is called at startup to ensure the base registry is populated for List() operations
func (p *ContextAwareProvider) RegisterTools(ctx context.Context) error {
	return nil
}

// List returns all registered tool definitions
// Note: This returns ALL tools without RBAC filtering. Use ListForContext for filtered results.
func (p *ContextAwareProvider) List() []mcp.Tool {
	return p.baseRegistry.List()
}

// rbacExemptTools lists tools that bypass MCP tool RBAC filtering.
// These tools are always visible and executable regardless of privilege
// settings. Connection-level RBAC still applies within the tool handler.
var rbacExemptTools = map[string]bool{
	"test_query": true, // Query validation never returns data; gating it would prevent tokens from validating their own queries
}

// ListForContext returns tool definitions filtered by the user's RBAC privileges
// This is the RBAC-aware version of List() that should be used in authenticated contexts
func (p *ContextAwareProvider) ListForContext(ctx context.Context) []mcp.Tool {
	allTools := p.baseRegistry.List()

	// If auth is disabled or user is superuser, return all tools
	if p.rbacChecker.IsSuperuser(ctx) {
		return allTools
	}

	// Filter tools based on user's privileges
	var filtered []mcp.Tool
	for _, tool := range allTools {
		if rbacExemptTools[tool.Name] || p.rbacChecker.CanAccessMCPItem(ctx, tool.Name) {
			filtered = append(filtered, tool)
		}
	}

	return filtered
}

// getOrCreateRegistryForClient returns a cached registry for the given client
// or creates a new one if it doesn't exist
func (p *ContextAwareProvider) getOrCreateRegistryForClient(client *database.Client) *Registry {
	if client == nil {
		// No client available - return base registry only
		return p.baseRegistry
	}

	// Fast path: check if registry already exists (read lock)
	p.mu.RLock()
	if registry, exists := p.clientRegistries[client]; exists {
		p.mu.RUnlock()
		return registry
	}
	p.mu.RUnlock()

	// Slow path: create new registry (write lock)
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if registry, exists := p.clientRegistries[client]; exists {
		return registry
	}

	// Create new registry with all tools for this client
	registry := NewRegistry()

	// Register all tools using helper methods to avoid duplication
	p.registerStatelessTools(registry)
	p.registerDatastoreTools(registry)
	p.registerDatabaseTools(registry, client)

	// Cache for future use
	p.clientRegistries[client] = registry

	return registry
}

// Execute runs a tool by name with the given arguments and context
// Uses cached per-client registries to avoid re-creating tools on every request
func (p *ContextAwareProvider) Execute(ctx context.Context, name string, args map[string]interface{}) (mcp.ToolResponse, error) {
	startTime := time.Now()
	tokenHash := auth.GetTokenHashFromContext(ctx)
	requestID := mcp.GetRequestIDFromContext(ctx)
	sessionID := tokenHash // Use token hash as session ID

	// Log tool call if tracing is enabled
	if tracing.IsEnabled() {
		tracing.LogToolCall(sessionID, tokenHash, requestID, name, args)
	}

	// Helper to log result and return
	logAndReturn := func(response mcp.ToolResponse, err error) (mcp.ToolResponse, error) {
		if tracing.IsEnabled() {
			duration := time.Since(startTime)
			var result interface{}
			if len(response.Content) > 0 {
				// Extract text content for logging
				texts := make([]string, 0, len(response.Content))
				for _, c := range response.Content {
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
			tracing.LogToolResult(sessionID, tokenHash, requestID, name, result, err, duration)
		}
		return response, err
	}

	// Check if this is a hidden tool
	// Hidden tools don't require authentication and are not advertised to LLM
	if p.hiddenRegistry != nil {
		if _, exists := p.hiddenRegistry.Get(name); exists {
			// Tool found in hidden registry - execute it without auth validation
			// Note: AuthStore uses SQLite which persists automatically
			response, err := p.hiddenRegistry.Execute(ctx, name, args)
			return logAndReturn(response, err)
		}
	}

	// Check if this tool is enabled in the builtins configuration
	// read_resource is always enabled as it's used to list resources
	if name != "read_resource" && !p.cfg.Builtins.Tools.IsToolEnabled(name) {
		return logAndReturn(mcp.ToolResponse{
			Content: []mcp.ContentItem{
				{
					Type: "text",
					Text: fmt.Sprintf("Tool '%s' is not available", name),
				},
			},
			IsError: true,
		}, nil)
	}

	// Validate token for ALL non-hidden tools
	if tokenHash == "" {
		return logAndReturn(mcp.ToolResponse{}, fmt.Errorf("no authentication token found in request context"))
	}

	// RBAC check: verify user has access to this tool
	// This check applies even for tools that are enabled in config
	// RBAC-exempt tools bypass this check (connection RBAC still applies inside the handler)
	if !rbacExemptTools[name] && !p.rbacChecker.CanAccessMCPItem(ctx, name) {
		return logAndReturn(mcp.ToolResponse{
			Content: []mcp.ContentItem{
				{
					Type: "text",
					Text: fmt.Sprintf("Access denied: you do not have permission to use tool '%s'", name),
				},
			},
			IsError: true,
		}, nil)
	}

	// Check if this is a stateless tool that doesn't require a per-token database client
	statelessTools := map[string]bool{
		"read_resource":        true,                 // Resource access tool
		"generate_embedding":   true,                 // Embedding generation doesn't need database
		"search_knowledgebase": true,                 // Knowledgebase search - uses independent SQLite DB
		"list_probes":          true,                 // Datastore tool - uses shared datastore pool
		"describe_probe":       true,                 // Datastore tool - uses shared datastore pool
		"query_metrics":        true,                 // Datastore tool - uses shared datastore pool
		"list_connections":     true,                 // Datastore tool - uses shared datastore pool
		"get_alert_history":    true,                 // Datastore tool - uses shared datastore pool
		"get_alert_rules":      true,                 // Datastore tool - uses shared datastore pool
		"get_metric_baselines": true,                 // Datastore tool - uses shared datastore pool
		"query_datastore":      true,                 // Datastore tool - uses shared datastore pool
		"get_blackouts":        true,                 // Datastore tool - uses shared datastore pool
		"store_memory":         p.memoryStore != nil, // Memory tool - requires memory store
		"recall_memories":      p.memoryStore != nil, // Memory tool - requires memory store
		"delete_memory":        p.memoryStore != nil, // Memory tool - requires memory store
	}

	if statelessTools[name] {
		// For datastore tools that use connection_id, inject the default from session if not provided
		connectionIDTools := map[string]bool{
			"query_metrics":        true,
			"get_alert_history":    true,
			"get_metric_baselines": true,
			"get_blackouts":        true,
		}
		if connectionIDTools[name] {
			if _, hasConnID := args["connection_id"]; !hasConnID && p.authStore != nil {
				if tokenHash != "" {
					session, err := p.authStore.GetConnectionSession(tokenHash)
					if err == nil && session != nil {
						// Inject the session's connection ID as the default
						args["connection_id"] = float64(session.ConnectionID)
					}
				}
			}
		}
		// Execute from base registry (no per-token database client needed)
		response, err := p.baseRegistry.Execute(ctx, name, args)
		return logAndReturn(response, err)
	}

	// Get the appropriate database client for this request
	dbClient, err := p.getClient(ctx)
	if err != nil {
		// When connection_id is present in args, the ConnectionResolver inside
		// each tool handler resolves the connection independently - it does not
		// need the per-session fallback client. Fall through to baseRegistry so
		// the resolver gets a chance to run.
		ca := parseConnectionArgs(args)
		if !ca.HasConnID {
			// Extract the root cause error for cleaner display
			rootErr := err
			for {
				if unwrapped := errors.Unwrap(rootErr); unwrapped != nil {
					rootErr = unwrapped
				} else {
					break
				}
			}

			// Log the full error chain for debugging
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get database client for tool '%s': %v\n", name, err)

			// Show the root cause to the client for actionable feedback
			return logAndReturn(mcp.ToolResponse{
				Content: []mcp.ContentItem{
					{
						Type: "text",
						Text: fmt.Sprintf("Database connection error: %v", rootErr),
					},
				},
				IsError: true,
			}, nil) // Don't return error, just error response
		}

		// connection_id is present - use baseRegistry which has database tools
		// registered with nil fallback client; the resolver handles everything
		response, err := p.baseRegistry.Execute(ctx, name, args)
		return logAndReturn(response, err)
	}

	// Get the cached registry for this client (or create if first use)
	// This avoids re-creating all tools on every request
	registry := p.getOrCreateRegistryForClient(dbClient)

	// Execute the tool using the client-specific registry
	response, err := registry.Execute(ctx, name, args)
	return logAndReturn(response, err)
}

// getClient returns the appropriate database client based on authentication state
func (p *ContextAwareProvider) getClient(ctx context.Context) (*database.Client, error) {
	// Get per-token client
	tokenHash := auth.GetTokenHashFromContext(ctx)
	if tokenHash == "" {
		return nil, fmt.Errorf("no authentication token found in request context")
	}

	// Check if there's a selected connection in the session
	if p.authStore != nil && p.datastore != nil {
		session, err := p.authStore.GetConnectionSession(tokenHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to get connection session: %v\n", err)
		}

		if session != nil {
			// Get connection info from datastore
			conn, password, err := p.datastore.GetConnectionWithPassword(ctx, session.ConnectionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get connection info: %w", err)
			}

			// Build connection string with optional database override
			var databaseOverride string
			if session.DatabaseName != nil {
				databaseOverride = *session.DatabaseName
			}
			connStr := p.datastore.BuildConnectionString(conn, password, databaseOverride)

			// Get or create client using the session helper
			sessionInfo := &database.SessionInfo{
				TokenHash:    tokenHash,
				ConnectionID: session.ConnectionID,
				DatabaseName: session.DatabaseName,
			}
			client, err := p.clientManager.GetClientForSession(sessionInfo, connStr)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to selected database: %w", err)
			}

			return client, nil
		}

		// No connection selected - return helpful error
		return nil, fmt.Errorf("no database connection selected. Please select a database connection using your client interface (CLI or web client)")
	}

	// Fallback: Get or create client for this token using default config
	client, err := p.clientManager.GetOrCreateClient(tokenHash, true)
	if err != nil {
		return nil, fmt.Errorf("no database connection configured for this token: %w", err)
	}

	return client, nil
}
