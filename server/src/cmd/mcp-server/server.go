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
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/overview"
	"github.com/pgedge/ai-workbench/server/internal/prompts"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"github.com/pgedge/ai-workbench/server/internal/tools"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

const (
	// Token cleanup configuration
	tokenCleanupInterval = 5 * time.Minute  // How often to check for expired tokens
	tokenCleanupTimeout  = 30 * time.Second // Max time allowed for cleanup operations
)

// Server holds all server components and manages their lifecycle
type Server struct {
	cfg           *config.Config
	authStore     *auth.AuthStore
	rateLimiter   *auth.RateLimiter
	datastore     *database.Datastore
	clientManager *database.ClientManager
	convStore     *conversations.Store
	mcpServer     *mcp.Server
	overviewGen   *overview.Generator
	overviewHub   *overview.Hub
	toolProvider  *tools.ContextAwareProvider
	ctx           context.Context
	cancel        context.CancelFunc
	dataDir       string
	debug         bool
	aiEnabled     bool
}

// ServerConfig holds configuration for creating a new server
type ServerConfig struct {
	Config        *config.Config
	DataDir       string
	ExecPath      string
	DefaultSecret string
	Debug         bool
}

// NewServer creates and initializes a new Server instance
func NewServer(sc *ServerConfig) (*Server, error) {
	s := &Server{
		cfg:     sc.Config,
		dataDir: sc.DataDir,
		debug:   sc.Debug,
	}

	// Create cancellable context for graceful shutdown
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// Initialize all components
	if err := s.initTracing(); err != nil {
		return nil, err
	}

	if err := s.validateTLS(); err != nil {
		return nil, err
	}

	if err := s.initAuthStore(); err != nil {
		return nil, err
	}

	if err := s.initRateLimiter(); err != nil {
		return nil, err
	}

	serverSecret, err := s.loadServerSecret(sc.ExecPath)
	if err != nil {
		return nil, err
	}

	if err := s.initDatastore(serverSecret); err != nil {
		return nil, err
	}

	if err := s.initClientManager(); err != nil {
		return nil, err
	}

	if err := s.initMCPServer(); err != nil {
		return nil, err
	}

	if err := s.initConversationStore(); err != nil {
		// Non-fatal - just log warning
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
		fmt.Fprintf(os.Stderr, "         Conversation history will not be available\n")
	}

	s.startTokenCleanup()
	s.startOverviewGenerator()

	return s, nil
}

// initTracing initializes tracing if configured
func (s *Server) initTracing() error {
	if s.cfg.TraceFile != "" {
		if err := tracing.Initialize(s.cfg.TraceFile); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to initialize tracing: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Tracing: ENABLED (file: %s)\n", s.cfg.TraceFile)
		}
	}
	return nil
}

// validateTLS verifies TLS files exist if HTTPS is enabled
func (s *Server) validateTLS() error {
	if !s.cfg.HTTP.TLS.Enabled {
		return nil
	}

	if _, err := os.Stat(s.cfg.HTTP.TLS.CertFile); err != nil {
		return fmt.Errorf("certificate file not found: %s", s.cfg.HTTP.TLS.CertFile)
	}
	if _, err := os.Stat(s.cfg.HTTP.TLS.KeyFile); err != nil {
		return fmt.Errorf("key file not found: %s", s.cfg.HTTP.TLS.KeyFile)
	}
	if s.cfg.HTTP.TLS.ChainFile != "" {
		if _, err := os.Stat(s.cfg.HTTP.TLS.ChainFile); err != nil {
			return fmt.Errorf("chain file not found: %s", s.cfg.HTTP.TLS.ChainFile)
		}
	}
	return nil
}

// initAuthStore initializes the authentication store
func (s *Server) initAuthStore() error {
	// Create data directory if it doesn't exist
	if err := os.MkdirAll(s.dataDir, 0750); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize auth store (SQLite database)
	var err error
	s.authStore, err = auth.NewAuthStore(
		s.dataDir,
		s.cfg.HTTP.Auth.MaxUserTokenDays,
		s.cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize auth store: %w", err)
	}

	// Get counts for logging
	userCount, tokenCount := s.authStore.GetCounts()
	fmt.Fprintf(os.Stderr, "Auth store: %s/auth.db (%d user(s), %d token(s))\n",
		s.dataDir, userCount, tokenCount)

	if tokenCount == 0 && userCount == 0 {
		fmt.Fprintf(os.Stderr, "Note: No users or tokens configured. Create with:\n")
		fmt.Fprintf(os.Stderr, "  %s -add-user -username <name>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -add-token\n", os.Args[0])
	}

	// Register MCP privilege identifiers for RBAC
	registerMCPPrivileges(s.authStore)

	return nil
}

// initRateLimiter initializes the rate limiter for authentication
func (s *Server) initRateLimiter() error {
	s.rateLimiter = auth.NewRateLimiter(
		s.cfg.HTTP.Auth.RateLimitWindowMinutes,
		s.cfg.HTTP.Auth.RateLimitMaxAttempts,
	)
	fmt.Fprintf(os.Stderr, "Rate limiting enabled: %d attempts per %d minutes per IP\n",
		s.cfg.HTTP.Auth.RateLimitMaxAttempts, s.cfg.HTTP.Auth.RateLimitWindowMinutes)
	if s.cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout > 0 {
		fmt.Fprintf(os.Stderr, "Account lockout enabled: %d failed attempts before lockout\n",
			s.cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout)
	}
	return nil
}

// loadServerSecret loads the server secret for password decryption
func (s *Server) loadServerSecret(execPath string) (string, error) {
	secretPath := s.cfg.SecretFile
	if secretPath == "" {
		secretPath = config.GetDefaultSecretPath(execPath)
	}

	secretData, err := os.ReadFile(secretPath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file '%s': %w\n"+
			"       The secret file must match the collector's secret for password decryption", secretPath, err)
	}

	serverSecret := strings.TrimSpace(string(secretData))
	if serverSecret == "" {
		return "", fmt.Errorf("secret file '%s' is empty", secretPath)
	}

	fmt.Fprintf(os.Stderr, "Server secret: loaded from %s\n", secretPath)
	return serverSecret, nil
}

// initDatastore initializes the datastore connection
func (s *Server) initDatastore(serverSecret string) error {
	if s.cfg.Database == nil || s.cfg.Database.User == "" {
		return fmt.Errorf("database configuration is required")
	}

	var err error
	s.datastore, err = database.NewDatastore(s.cfg.Database, serverSecret)
	if err != nil {
		return fmt.Errorf("failed to connect to datastore: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Datastore: connected to %s@%s:%d/%s\n",
		s.cfg.Database.User, s.cfg.Database.Host, s.cfg.Database.Port, s.cfg.Database.Database)
	return nil
}

// initClientManager initializes the client manager for database connections
func (s *Server) initClientManager() error {
	s.clientManager = database.NewClientManager(s.cfg.Database)

	if s.cfg.Database != nil && s.cfg.Database.User != "" {
		fmt.Fprintf(os.Stderr, "Database configured: %s@%s:%d/%s (per-session connections)\n",
			s.cfg.Database.User, s.cfg.Database.Host, s.cfg.Database.Port, s.cfg.Database.Database)
	} else {
		fmt.Fprintf(os.Stderr, "Database: Not configured\n")
	}
	return nil
}

// initMCPServer initializes the MCP server with providers
func (s *Server) initMCPServer() error {
	// Create fallback client
	fallbackClient := database.NewClient(s.cfg.Database)

	// Context-aware resource provider
	contextAwareResourceProvider := resources.NewContextAwareRegistry(
		s.clientManager, s.cfg, s.authStore, s.datastore,
	)

	// Context-aware tool provider
	contextAwareToolProvider := tools.NewContextAwareProvider(
		s.clientManager, contextAwareResourceProvider,
		fallbackClient, s.cfg, s.authStore, s.rateLimiter, s.datastore,
	)
	if err := contextAwareToolProvider.RegisterTools(s.ctx); err != nil {
		return fmt.Errorf("failed to register tools: %w", err)
	}
	s.toolProvider = contextAwareToolProvider

	// Create MCP server with context-aware providers
	s.mcpServer = mcp.NewServer(contextAwareToolProvider)
	s.mcpServer.SetResourceProvider(contextAwareResourceProvider)

	// Register prompts (infrastructure in place for future prompts)
	promptRegistry := prompts.NewRegistry()
	s.mcpServer.SetPromptProvider(promptRegistry)

	return nil
}

// initConversationStore initializes the conversation store
func (s *Server) initConversationStore() error {
	if s.authStore == nil {
		return nil
	}

	if s.datastore == nil {
		return fmt.Errorf("datastore required for conversation storage")
	}

	s.convStore = conversations.NewStore(s.datastore.GetPool())

	fmt.Fprintf(os.Stderr, "Conversation store: PostgreSQL datastore\n")
	return nil
}

// startTokenCleanup starts the periodic token cleanup goroutine
func (s *Server) startTokenCleanup() {
	if s.authStore == nil {
		return
	}

	// Clean up expired tokens on startup
	if removed, _ := s.authStore.CleanupExpiredTokens(); removed > 0 {
		fmt.Fprintf(os.Stderr, "Removed %d expired token(s)\n", removed)
	}

	// Start periodic cleanup goroutine
	go func() {
		ticker := time.NewTicker(tokenCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				if removed, hashes := s.authStore.CleanupExpiredTokens(); removed > 0 {
					fmt.Fprintf(os.Stderr, "Removed %d expired token(s)\n", removed)
					s.cleanupExpiredConnections(hashes)
				}
			}
		}
	}()
}

// hasValidLLMConfig returns true when the configured LLM provider has the
// credentials required to make API calls.  Ollama is always considered
// valid when selected because it uses a local URL with a compiled-in
// default.  OpenAI is valid with either an API key or a custom base URL
// (for local servers like LM Studio that do not require auth).  The
// remaining providers require an explicit API key.
func (s *Server) hasValidLLMConfig() bool {
	switch s.cfg.LLM.Provider {
	case "anthropic":
		return s.cfg.LLM.AnthropicAPIKey != ""
	case "openai":
		return s.cfg.LLM.OpenAIAPIKey != "" || s.cfg.LLM.OpenAIBaseURL != ""
	case "gemini":
		return s.cfg.LLM.GeminiAPIKey != ""
	case "ollama":
		return s.cfg.LLM.OllamaURL != ""
	default:
		return false
	}
}

// startOverviewGenerator initializes and starts the AI overview generator
// if both the datastore and LLM configuration are available.
func (s *Server) startOverviewGenerator() {
	if s.datastore == nil || !s.hasValidLLMConfig() {
		fmt.Fprintf(os.Stderr, "AI Overview: DISABLED (requires datastore and LLM configuration)\n")
		return
	}

	llmConfig := &llmproxy.Config{
		Provider:               s.cfg.LLM.Provider,
		Model:                  s.cfg.LLM.Model,
		AnthropicAPIKey:        s.cfg.LLM.AnthropicAPIKey,
		AnthropicBaseURL:       s.cfg.LLM.AnthropicBaseURL,
		OpenAIAPIKey:           s.cfg.LLM.OpenAIAPIKey,
		OpenAIBaseURL:          s.cfg.LLM.OpenAIBaseURL,
		GeminiAPIKey:           s.cfg.LLM.GeminiAPIKey,
		GeminiBaseURL:          s.cfg.LLM.GeminiBaseURL,
		OllamaURL:              s.cfg.LLM.OllamaURL,
		MaxTokens:              s.cfg.LLM.MaxTokens,
		Temperature:            s.cfg.LLM.Temperature,
		UseCompactDescriptions: s.cfg.LLM.UseCompactDescriptions(),
	}

	s.overviewHub = overview.NewHub()
	s.overviewGen = overview.NewGenerator(s.datastore, llmConfig)
	s.overviewGen.SetHub(s.overviewHub)
	s.overviewGen.Start(s.ctx)
	fmt.Fprintf(os.Stderr, "AI Overview: ENABLED\n")
	s.aiEnabled = true
}

// cleanupExpiredConnections cleans up database connections for expired tokens
func (s *Server) cleanupExpiredConnections(hashes []string) {
	// Create a timeout context for cleanup operations
	cleanupCtx, cancel := context.WithTimeout(context.Background(), tokenCleanupTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.clientManager.RemoveClients(hashes)
	}()

	select {
	case err := <-done:
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to cleanup connections: %v\n", err)
		}
	case <-cleanupCtx.Done():
		fmt.Fprintf(os.Stderr, "WARNING: Connection cleanup timed out\n")
	}
}

// Run starts the HTTP server and blocks until shutdown
func (s *Server) Run(flags *Flags, configPath string) error {
	// Create HTTP server configuration
	httpConfig := &mcp.HTTPConfig{
		Addr:           s.cfg.HTTP.Address,
		TLSEnable:      s.cfg.HTTP.TLS.Enabled,
		CertFile:       s.cfg.HTTP.TLS.CertFile,
		KeyFile:        s.cfg.HTTP.TLS.KeyFile,
		ChainFile:      s.cfg.HTTP.TLS.ChainFile,
		AuthStore:      s.authStore,
		Debug:          s.debug,
		TrustedProxies: s.cfg.HTTP.TrustedProxies,
	}

	// Create secure IP extractor for rate limiting
	// This ensures X-Forwarded-For headers are only trusted from configured proxies
	ipExtractor := auth.NewIPExtractor(s.cfg.HTTP.TrustedProxies)

	// Setup HTTP handlers
	deps := &HandlerDependencies{
		AuthStore:    s.authStore,
		RateLimiter:  s.rateLimiter,
		IPExtractor:  ipExtractor,
		ConvStore:    s.convStore,
		Datastore:    s.datastore,
		Config:       s.cfg,
		OverviewGen:  s.overviewGen,
		OverviewHub:  s.overviewHub,
		ToolProvider: s.toolProvider,
		AIEnabled:    s.aiEnabled,
	}
	httpConfig.SetupHandlers = SetupHandlers(deps)

	// Log startup information
	s.logStartupInfo()

	// Setup SIGHUP handler for configuration reload
	s.setupSIGHUP(flags, configPath)

	// Run the server
	return s.mcpServer.RunHTTP(httpConfig)
}

// logStartupInfo logs server startup information
func (s *Server) logStartupInfo() {
	if s.cfg.HTTP.TLS.Enabled {
		fmt.Fprintf(os.Stderr, "Starting MCP server in HTTPS mode on %s\n", s.cfg.HTTP.Address)
		fmt.Fprintf(os.Stderr, "Certificate: %s\n", s.cfg.HTTP.TLS.CertFile)
		fmt.Fprintf(os.Stderr, "Key: %s\n", s.cfg.HTTP.TLS.KeyFile)
		if s.cfg.HTTP.TLS.ChainFile != "" {
			fmt.Fprintf(os.Stderr, "Chain: %s\n", s.cfg.HTTP.TLS.ChainFile)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Starting MCP server in HTTP mode on %s\n", s.cfg.HTTP.Address)
	}

	fmt.Fprintf(os.Stderr, "LLM Proxy: ENABLED (provider: %s, model: %s)\n",
		s.cfg.LLM.Provider, s.cfg.LLM.Model)

	if s.cfg.Knowledgebase.Enabled {
		apiKeyStatus := "not set"
		if s.cfg.Knowledgebase.EmbeddingVoyageAPIKey != "" {
			apiKeyStatus = "loaded"
		} else if s.cfg.Knowledgebase.EmbeddingOpenAIAPIKey != "" {
			apiKeyStatus = "loaded"
		}
		fmt.Fprintf(os.Stderr, "Knowledgebase: ENABLED (provider: %s, model: %s, API key: %s)\n",
			s.cfg.Knowledgebase.EmbeddingProvider, s.cfg.Knowledgebase.EmbeddingModel, apiKeyStatus)
	} else {
		fmt.Fprintf(os.Stderr, "Knowledgebase: DISABLED\n")
	}

	if s.debug {
		fmt.Fprintf(os.Stderr, "Debug logging: ENABLED\n")
	}
}

// setupSIGHUP sets up the SIGHUP handler for configuration reload
func (s *Server) setupSIGHUP(flags *Flags, configPath string) {
	cliFlags := flags.ToReloadCLIFlags()
	reloadableCfg := config.NewReloadableConfig(s.cfg, configPath, cliFlags)

	// Register callback to update client manager when database config changes
	reloadableCfg.OnReload(func(newCfg *config.Config) {
		s.clientManager.UpdateDatabaseConfig(newCfg.Database)
	})

	// Start SIGHUP listener
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			fmt.Fprintf(os.Stderr, "Received SIGHUP, reloading configuration...\n")
			if err := reloadableCfg.Reload(); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: Failed to reload config: %v\n", err)
			}
		}
	}()
}

// Close cleans up all server resources
func (s *Server) Close() {
	// Stop background goroutines
	s.cancel()

	// Close tracing
	tracing.Close()

	// Stop overview generator
	if s.overviewGen != nil {
		s.overviewGen.Stop()
	}

	// Stop rate limiter cleanup
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}

	// Close auth store
	if s.authStore != nil {
		s.authStore.Close()
	}

	// Close datastore
	if s.datastore != nil {
		s.datastore.Close()
	}

	// Close all database connections
	if s.clientManager != nil {
		if err := s.clientManager.CloseAll(); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Error closing database connections: %v\n", err)
		}
	}
}
