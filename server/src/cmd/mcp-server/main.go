/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/compactor"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/prompts"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"github.com/pgedge/ai-workbench/server/internal/tools"
)

const (
	// Token cleanup configuration
	tokenCleanupInterval = 5 * time.Minute  // How often to check for expired tokens
	tokenCleanupTimeout  = 30 * time.Second // Max time allowed for cleanup operations
)

func main() {
	// Get executable path for default config location
	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to get executable path: %v\n", err)
		os.Exit(1)
	}
	defaultConfigPath := config.GetDefaultConfigPath(execPath)

	// Command line flags
	configFile := flag.String("config", defaultConfigPath, "Path to configuration file")
	httpAddr := flag.String("addr", "", "HTTP server address")
	tlsMode := flag.Bool("tls", false, "Enable TLS/HTTPS")
	certFile := flag.String("cert", "", "Path to TLS certificate file")
	keyFile := flag.String("key", "", "Path to TLS key file")
	chainFile := flag.String("chain", "", "Path to TLS certificate chain file (optional)")
	debug := flag.Bool("debug", false, "Enable debug logging (logs HTTP requests/responses)")
	dataDir := flag.String("data-dir", "", "Data directory for auth database and conversations")

	// Database connection flags
	dbHost := flag.String("db-host", "", "Database host")
	dbPort := flag.Int("db-port", 0, "Database port")
	dbName := flag.String("db-name", "", "Database name")
	dbUser := flag.String("db-user", "", "Database user")
	dbPassword := flag.String("db-password", "", "Database password")
	dbSSLMode := flag.String("db-sslmode", "", "Database SSL mode (disable, require, verify-ca, verify-full)")

	// Token management commands
	addTokenCmd := flag.Bool("add-token", false, "Add a new service token")
	removeTokenCmd := flag.String("remove-token", "", "Remove a service token by ID or hash prefix")
	listTokensCmd := flag.Bool("list-tokens", false, "List all service tokens")
	tokenNote := flag.String("token-note", "", "Annotation for the new token (used with -add-token)")
	tokenExpiry := flag.String("token-expiry", "", "Token expiry duration: '30d', '1y', '2w', '12h', 'never' (used with -add-token)")

	// User management commands
	addUserCmd := flag.Bool("add-user", false, "Add a new user")
	updateUserCmd := flag.Bool("update-user", false, "Update an existing user")
	deleteUserCmd := flag.Bool("delete-user", false, "Delete a user")
	listUsersCmd := flag.Bool("list-users", false, "List all users")
	enableUserCmd := flag.Bool("enable-user", false, "Enable a user account")
	disableUserCmd := flag.Bool("disable-user", false, "Disable a user account")
	username := flag.String("username", "", "Username for user management commands")
	userPassword := flag.String("password", "", "Password for user management commands (prompted if not provided)")
	userNote := flag.String("user-note", "", "Annotation for the new user (used with -add-user)")

	flag.Parse()

	// Determine data directory for auth database
	resolvedDataDir := *dataDir
	if resolvedDataDir == "" {
		resolvedDataDir = filepath.Join(filepath.Dir(execPath), "data")
	}

	// Handle token management commands
	if *addTokenCmd || *removeTokenCmd != "" || *listTokensCmd {
		if *addTokenCmd {
			var expiry time.Duration
			switch {
			case *tokenExpiry != "" && *tokenExpiry != "never":
				var err error
				expiry, err = parseDuration(*tokenExpiry)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: Invalid expiry duration: %v\n", err)
					os.Exit(1)
				}
			case *tokenExpiry == "":
				expiry = 0 // Will prompt user
			default:
				expiry = -1 // Never expires
			}

			if err := addTokenCommand(resolvedDataDir, *tokenNote, expiry); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if *removeTokenCmd != "" {
			if err := removeTokenCommand(resolvedDataDir, *removeTokenCmd); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if *listTokensCmd {
			if err := listTokensCommand(resolvedDataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Handle user management commands
	if *addUserCmd || *updateUserCmd || *deleteUserCmd || *listUsersCmd || *enableUserCmd || *disableUserCmd {
		if *addUserCmd {
			if err := addUserCommand(resolvedDataDir, *username, *userPassword, *userNote); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if *updateUserCmd {
			if err := updateUserCommand(resolvedDataDir, *username, *userPassword, *userNote); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if *deleteUserCmd {
			if err := deleteUserCommand(resolvedDataDir, *username); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if *listUsersCmd {
			if err := listUsersCommand(resolvedDataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if *enableUserCmd {
			if err := enableUserCommand(resolvedDataDir, *username); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if *disableUserCmd {
			if err := disableUserCommand(resolvedDataDir, *username); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Track which flags were explicitly set
	// Auth is always required
	cliFlags := config.CLIFlags{
		AuthEnabledSet: true,
		AuthEnabled:    true,
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config":
			cliFlags.ConfigFileSet = true
			cliFlags.ConfigFile = *configFile
		case "addr":
			cliFlags.HTTPAddrSet = true
			cliFlags.HTTPAddr = *httpAddr
		case "tls":
			cliFlags.TLSEnabledSet = true
			cliFlags.TLSEnabled = *tlsMode
		case "cert":
			cliFlags.TLSCertSet = true
			cliFlags.TLSCertFile = *certFile
		case "key":
			cliFlags.TLSKeySet = true
			cliFlags.TLSKeyFile = *keyFile
		case "chain":
			cliFlags.TLSChainSet = true
			cliFlags.TLSChainFile = *chainFile
		case "db-host":
			cliFlags.DBHostSet = true
			cliFlags.DBHost = *dbHost
		case "db-port":
			cliFlags.DBPortSet = true
			cliFlags.DBPort = *dbPort
		case "db-name":
			cliFlags.DBNameSet = true
			cliFlags.DBName = *dbName
		case "db-user":
			cliFlags.DBUserSet = true
			cliFlags.DBUser = *dbUser
		case "db-password":
			cliFlags.DBPassSet = true
			cliFlags.DBPassword = *dbPassword
		case "db-sslmode":
			cliFlags.DBSSLSet = true
			cliFlags.DBSSLMode = *dbSSLMode
		}
	})

	// Determine which config file to load and save to
	configPath := *configFile
	if !cliFlags.ConfigFileSet {
		// Use default config path (will be created if needed for saving connections)
		configPath = defaultConfigPath
	}

	// For loading, only attempt to load if file exists
	configPathForLoad := ""
	if config.ConfigFileExists(configPath) {
		configPathForLoad = configPath
	}

	// Load configuration (empty path means no config file, will use env vars and defaults)
	cfg, err := config.LoadConfig(configPathForLoad, cliFlags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Verify TLS files exist if HTTPS is enabled
	if cfg.HTTP.TLS.Enabled {
		if _, err := os.Stat(cfg.HTTP.TLS.CertFile); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Certificate file not found: %s\n", cfg.HTTP.TLS.CertFile)
			os.Exit(1)
		}
		if _, err := os.Stat(cfg.HTTP.TLS.KeyFile); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Key file not found: %s\n", cfg.HTTP.TLS.KeyFile)
			os.Exit(1)
		}
		if cfg.HTTP.TLS.ChainFile != "" {
			if _, err := os.Stat(cfg.HTTP.TLS.ChainFile); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: Chain file not found: %s\n", cfg.HTTP.TLS.ChainFile)
				os.Exit(1)
			}
		}
	}

	// Initialize auth store if auth is enabled
	var authStore *auth.AuthStore
	if cfg.HTTP.Auth.Enabled {
		// Create data directory if it doesn't exist
		if err := os.MkdirAll(resolvedDataDir, 0750); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to create data directory: %v\n", err)
			os.Exit(1)
		}

		// Initialize auth store (SQLite database)
		authStore, err = auth.NewAuthStore(
			resolvedDataDir,
			cfg.HTTP.Auth.MaxUserTokenDays,
			cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to initialize auth store: %v\n", err)
			os.Exit(1)
		}
		defer authStore.Close()

		// Get counts for logging
		userCount, tokenCount := authStore.GetCounts()
		fmt.Fprintf(os.Stderr, "Auth store: %s/auth.db (%d user(s), %d token(s))\n",
			resolvedDataDir, userCount, tokenCount)

		if tokenCount == 0 && userCount == 0 {
			fmt.Fprintf(os.Stderr, "Note: No users or tokens configured. Create with:\n")
			fmt.Fprintf(os.Stderr, "  %s -add-user -username <name>\n", os.Args[0])
			fmt.Fprintf(os.Stderr, "  %s -add-token\n", os.Args[0])
		}
	}

	// Create rate limiter for authentication if auth is enabled
	var rateLimiter *auth.RateLimiter
	if cfg.HTTP.Auth.Enabled {
		rateLimiter = auth.NewRateLimiter(cfg.HTTP.Auth.RateLimitWindowMinutes, cfg.HTTP.Auth.RateLimitMaxAttempts)
		fmt.Fprintf(os.Stderr, "Rate limiting enabled: %d attempts per %d minutes per IP\n",
			cfg.HTTP.Auth.RateLimitMaxAttempts, cfg.HTTP.Auth.RateLimitWindowMinutes)
		if cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout > 0 {
			fmt.Fprintf(os.Stderr, "Account lockout enabled: %d failed attempts before lockout\n",
				cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout)
		}
	}

	// Create a cancellable context for graceful shutdown of background goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure background goroutines are stopped on exit

	// Ensure rate limiter cleanup goroutine is stopped on exit
	if rateLimiter != nil {
		defer rateLimiter.Stop()
	}

	// Load server secret for password decryption (required for monitored connections)
	var serverSecret string
	secretPath := cfg.SecretFile
	if secretPath == "" {
		secretPath = config.GetDefaultSecretPath(execPath)
	}
	secretData, err := os.ReadFile(secretPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to read secret file '%s': %v\n", secretPath, err)
		fmt.Fprintf(os.Stderr, "       The secret file must match the collector's secret for password decryption\n")
		os.Exit(1)
	}
	serverSecret = strings.TrimSpace(string(secretData))
	if serverSecret == "" {
		fmt.Fprintf(os.Stderr, "ERROR: Secret file '%s' is empty\n", secretPath)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Server secret: loaded from %s\n", secretPath)

	// Initialize datastore connection for accessing monitored database info
	var datastore *database.Datastore
	if cfg.Database != nil && cfg.Database.User != "" {
		var err error
		datastore, err = database.NewDatastore(cfg.Database, serverSecret)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to connect to datastore: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Datastore: connected to %s@%s:%d/%s\n",
			cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database)
		defer datastore.Close()
	} else {
		fmt.Fprintf(os.Stderr, "ERROR: Database configuration is required\n")
		os.Exit(1)
	}

	// Initialize client manager for database connections with single database configuration
	clientManager := database.NewClientManager(cfg.Database)

	// Authentication is always enabled in HTTP mode
	authEnabled := true

	// Create fallback database client - connections are created per-session on-demand
	var fallbackClient *database.Client
	if cfg.Database != nil && cfg.Database.User != "" {
		// Create a template client that won't be connected
		fallbackClient = database.NewClient(cfg.Database)
		fmt.Fprintf(os.Stderr, "Database configured: %s@%s:%d/%s (per-session connections)\n",
			cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database)
	} else {
		// No database configured
		fallbackClient = database.NewClient(nil)
		fmt.Fprintf(os.Stderr, "Database: Not configured\n")
	}

	// Context-aware resource provider
	contextAwareResourceProvider := resources.NewContextAwareRegistry(clientManager, authEnabled, cfg, authStore, datastore)

	// Context-aware tool provider
	contextAwareToolProvider := tools.NewContextAwareProvider(clientManager, contextAwareResourceProvider, authEnabled, fallbackClient, cfg, authStore, rateLimiter, datastore)
	if err := contextAwareToolProvider.RegisterTools(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to register tools: %v\n", err)
		os.Exit(1)
	}

	// Create MCP server with context-aware providers
	server := mcp.NewServer(contextAwareToolProvider)
	server.SetResourceProvider(contextAwareResourceProvider)

	// Register prompts (only enabled ones)
	promptRegistry := prompts.NewRegistry()
	if cfg.Builtins.Prompts.IsPromptEnabled("explore-database") {
		promptRegistry.Register("explore-database", prompts.ExploreDatabase())
	}
	if cfg.Builtins.Prompts.IsPromptEnabled("setup-semantic-search") {
		promptRegistry.Register("setup-semantic-search", prompts.SetupSemanticSearch())
	}
	if cfg.Builtins.Prompts.IsPromptEnabled("diagnose-query-issue") {
		promptRegistry.Register("diagnose-query-issue", prompts.DiagnoseQueryIssue())
	}
	if cfg.Builtins.Prompts.IsPromptEnabled("design-schema") {
		promptRegistry.Register("design-schema", prompts.DesignSchema())
	}
	server.SetPromptProvider(promptRegistry)

	// Start periodic cleanup of expired tokens if auth is enabled
	if cfg.HTTP.Auth.Enabled && authStore != nil {
		// Clean up expired tokens on startup (no connections exist yet)
		if removed, _ := authStore.CleanupExpiredTokens(); removed > 0 {
			fmt.Fprintf(os.Stderr, "Removed %d expired token(s)\n", removed)
		}

		// Start periodic cleanup goroutine
		go func() {
			ticker := time.NewTicker(tokenCleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if removed, hashes := authStore.CleanupExpiredTokens(); removed > 0 {
						fmt.Fprintf(os.Stderr, "Removed %d expired token(s)\n", removed)

						// Create a timeout context for cleanup operations to prevent indefinite blocking
						cleanupCtx, cancel := context.WithTimeout(context.Background(), tokenCleanupTimeout)

						// Clean up database connections for expired tokens
						done := make(chan error, 1)
						go func() {
							done <- clientManager.RemoveClients(hashes)
						}()

						select {
						case err := <-done:
							if err != nil {
								fmt.Fprintf(os.Stderr, "WARNING: Failed to cleanup connections: %v\n", err)
							}
						case <-cleanupCtx.Done():
							fmt.Fprintf(os.Stderr, "WARNING: Connection cleanup timed out\n")
						}

						// Cancel context after cleanup is done
						cancel()
					}
				}
			}
		}()
	}

	// Initialize conversation store
	var convStore *conversations.Store
	if authStore != nil {
		var err error
		convStore, err = conversations.NewStore(resolvedDataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to initialize conversation store: %v\n", err)
			fmt.Fprintf(os.Stderr, "         Conversation history will not be available\n")
		} else {
			fmt.Fprintf(os.Stderr, "Conversation store: %s/conversations.db\n", resolvedDataDir)
			defer convStore.Close()
		}
	}

	// Create HTTP server configuration
	httpConfig := &mcp.HTTPConfig{
		Addr:        cfg.HTTP.Address,
		TLSEnable:   cfg.HTTP.TLS.Enabled,
		CertFile:    cfg.HTTP.TLS.CertFile,
		KeyFile:     cfg.HTTP.TLS.KeyFile,
		ChainFile:   cfg.HTTP.TLS.ChainFile,
		AuthEnabled: cfg.HTTP.Auth.Enabled,
		AuthStore:   authStore,
		Debug:       *debug,
	}

	// Setup additional HTTP handlers
	httpConfig.SetupHandlers = func(mux *http.ServeMux) error {
		// Helper to wrap handlers with authentication
		authWrapper := func(handler http.HandlerFunc) http.HandlerFunc {
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

				// Token valid, proceed with handler
				handler(w, r)
			}
		}

		// Chat history compaction endpoint
		mux.HandleFunc("/api/chat/compact",
			authWrapper(compactor.HandleCompact))

		// User info endpoint - returns auth status (no error if not logged in)
		mux.HandleFunc("/api/user/info", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			// Extract session token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				//nolint:errcheck // Encoding a simple map should never fail
				json.NewEncoder(w).Encode(map[string]interface{}{
					"authenticated": false,
				})
				return
			}

			// Extract Bearer token
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				//nolint:errcheck // Encoding a simple map should never fail
				json.NewEncoder(w).Encode(map[string]interface{}{
					"authenticated": false,
					"error":         "Invalid Authorization header format",
				})
				return
			}

			// Validate session token and get username
			username, err := authStore.ValidateSessionToken(token)
			if err != nil {
				//nolint:errcheck // Encoding a simple map should never fail
				json.NewEncoder(w).Encode(map[string]interface{}{
					"authenticated": false,
					"error":         "Invalid or expired session",
				})
				return
			}

			// Return user info as JSON
			//nolint:errcheck // Encoding a simple map should never fail
			json.NewEncoder(w).Encode(map[string]interface{}{
				"authenticated": true,
				"username":      username,
			})
		})

		// Add LLM proxy handlers if enabled
		if cfg.LLM.Enabled {
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
			mux.HandleFunc("/api/llm/providers",
				func(w http.ResponseWriter, r *http.Request) {
					llmproxy.HandleProviders(w, r, llmConfig)
				})
			mux.HandleFunc("/api/llm/models",
				func(w http.ResponseWriter, r *http.Request) {
					llmproxy.HandleModels(w, r, llmConfig)
				})
			// Chat endpoint requires auth (makes actual LLM API calls)
			mux.HandleFunc("/api/llm/chat",
				authWrapper(func(w http.ResponseWriter, r *http.Request) {
					llmproxy.HandleChat(w, r, llmConfig)
				}))
		}

		// Conversation history endpoints (only if store is available)
		if convStore != nil && authStore != nil {
			convHandler := conversations.NewHandler(convStore, authStore)
			convHandler.RegisterRoutes(mux, authWrapper)
			fmt.Fprintf(os.Stderr, "Conversation history: ENABLED\n")
		}

		// Connection management endpoints (for selecting monitored database connections)
		connHandler := api.NewConnectionHandler(datastore, authStore)
		connHandler.RegisterRoutes(mux, authWrapper)
		if datastore != nil {
			fmt.Fprintf(os.Stderr, "Connection management: ENABLED\n")
		} else {
			fmt.Fprintf(os.Stderr, "Connection management: DISABLED (datastore not configured)\n")
		}

		return nil
	}

	if cfg.HTTP.TLS.Enabled {
		fmt.Fprintf(os.Stderr, "Starting MCP server in HTTPS mode on %s\n", cfg.HTTP.Address)
		fmt.Fprintf(os.Stderr, "Certificate: %s\n", cfg.HTTP.TLS.CertFile)
		fmt.Fprintf(os.Stderr, "Key: %s\n", cfg.HTTP.TLS.KeyFile)
		if cfg.HTTP.TLS.ChainFile != "" {
			fmt.Fprintf(os.Stderr, "Chain: %s\n", cfg.HTTP.TLS.ChainFile)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Starting MCP server in HTTP mode on %s\n", cfg.HTTP.Address)
	}

	if cfg.LLM.Enabled {
		fmt.Fprintf(os.Stderr, "LLM Proxy: ENABLED (provider: %s, model: %s)\n", cfg.LLM.Provider, cfg.LLM.Model)
	} else {
		fmt.Fprintf(os.Stderr, "LLM Proxy: DISABLED\n")
	}

	if cfg.Knowledgebase.Enabled {
		apiKeyStatus := "not set"
		if cfg.Knowledgebase.EmbeddingVoyageAPIKey != "" {
			apiKeyStatus = "loaded"
		} else if cfg.Knowledgebase.EmbeddingOpenAIAPIKey != "" {
			apiKeyStatus = "loaded"
		}
		fmt.Fprintf(os.Stderr, "Knowledgebase: ENABLED (provider: %s, model: %s, API key: %s)\n",
			cfg.Knowledgebase.EmbeddingProvider, cfg.Knowledgebase.EmbeddingModel, apiKeyStatus)
	} else {
		fmt.Fprintf(os.Stderr, "Knowledgebase: DISABLED\n")
	}

	if *debug {
		fmt.Fprintf(os.Stderr, "Debug logging: ENABLED\n")
	}

	// Set up SIGHUP handler for configuration reload
	cliFlags = config.CLIFlags{
		DBHost:     *dbHost,
		DBPort:     *dbPort,
		DBName:     *dbName,
		DBUser:     *dbUser,
		DBPassword: *dbPassword,
		DBSSLMode:  *dbSSLMode,
	}
	reloadableCfg := config.NewReloadableConfig(cfg, configPath, cliFlags)

	// Register callback to update client manager when database config changes
	reloadableCfg.OnReload(func(newCfg *config.Config) {
		clientManager.UpdateDatabaseConfig(newCfg.Database)
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

	err = server.RunHTTP(httpConfig)

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Cleanup
	if clientManager != nil {
		// Close all per-token connections
		if err := clientManager.CloseAll(); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Error closing database connections: %v\n", err)
		}
	}
}
