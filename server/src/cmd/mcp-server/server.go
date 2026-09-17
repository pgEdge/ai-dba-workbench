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
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/pkg/fileutil"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/oidc"
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

	// oidcProvider is nil whenever federated login is switched off. When
	// it is non-nil, oidcStateKey holds the 32-byte key the login state
	// cookie is sealed with.
	oidcProvider *oidc.Provider
	oidcStateKey []byte

	// handlerClosers holds cleanup functions for background resources
	// created while wiring HTTP handlers. SetupHandlers runs on the
	// HTTP server's goroutine, whereas Close may run from the signal
	// handler, so access is guarded by closersMu.
	closersMu      sync.Mutex
	handlerClosers []func()
	closersDrained bool
}

// registerHandlerCloser records a cleanup function to be run by Close.
// It is passed to SetupHandlers so that handlers owning background
// goroutines can hand that ownership back to the server, which is the
// only component that knows when the process is shutting down.
// A closer registered after Close has already drained the list runs
// immediately, because nothing else will ever run it; that ordering is
// possible because SetupHandlers runs lazily on the HTTP server's
// goroutine and can therefore still be wiring handlers when the signal
// handler calls Close.
func (s *Server) registerHandlerCloser(closer func()) {
	if closer == nil {
		return
	}

	s.closersMu.Lock()
	drained := s.closersDrained
	if !drained {
		s.handlerClosers = append(s.handlerClosers, closer)
	}
	s.closersMu.Unlock()

	// Run outside the lock, so a closer that itself registers another
	// closer cannot deadlock against closersMu.
	if drained {
		closer()
	}
}

// runHandlerClosers invokes every registered handler cleanup function
// and then clears the list, so a second Close is a no-op rather than a
// double stop. It also marks the list drained, so a closer arriving
// afterwards is run by registerHandlerCloser rather than dropped.
func (s *Server) runHandlerClosers() {
	s.closersMu.Lock()
	closers := s.handlerClosers
	s.handlerClosers = nil
	s.closersDrained = true
	s.closersMu.Unlock()

	for _, closer := range closers {
		closer()
	}
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

	if err := s.initOIDC(serverSecret); err != nil {
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

	// Log auth store status with creation indicator
	if s.authStore.Created {
		fmt.Fprintf(os.Stderr, "Auth store: %s (new database created)\n", s.authStore.Path())
	} else {
		userCount, tokenCount := s.authStore.GetCounts()
		fmt.Fprintf(os.Stderr, "Auth store: %s (%d user(s), %d token(s))\n",
			s.authStore.Path(), userCount, tokenCount)
	}

	userCount, tokenCount := s.authStore.GetCounts()
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

// loadServerSecret loads the server secret for password decryption.
// Search order: explicit s.cfg.SecretFile > per-user config dir
// (e.g. ~/.config/pgedge/ai-dba-server.secret) > /etc/pgedge/
// ai-dba-server.secret. Falling out of all three is a fatal error
// because the server cannot decrypt stored passwords without it.
func (s *Server) loadServerSecret(execPath string) (string, error) {
	secretPath := s.cfg.SecretFile
	if secretPath == "" {
		secretPath = config.GetDefaultSecretPath(execPath)
	}

	if secretPath == "" {
		return "", fmt.Errorf("server secret file not found in any " +
			"default search path (per-user config dir, " +
			"/etc/pgedge/ai-dba-server.secret); set secret_file in " +
			"the config or place the file in one of those locations")
	}

	serverSecret, err := fileutil.ReadSecretFile(secretPath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file '%s': %w\n"+
			"       The secret file must match the collector's secret for password decryption", secretPath, err)
	}

	fmt.Fprintf(os.Stderr, "Server secret: loaded from %s\n", secretPath)
	return serverSecret, nil
}

// oidcStateKeySalt is the fixed PBKDF2 salt separating the OIDC login
// state key from every other key derived from the same server secret,
// most importantly the one that encrypts stored database passwords. It
// is a constant rather than a random value because the key has to be
// reproducible across restarts and across two servers sharing one
// secret; the salt is not a secret, the server secret is. Never reuse a
// salt from another subsystem here, and never change this string: doing
// so invalidates every login in flight at that moment.
const oidcStateKeySalt = "pgedge-ai-workbench/oidc-login-state/v1"

// oidcDiscoveryTimeout bounds OpenID Connect discovery at start-up.
// go-oidc issues that request through http.DefaultClient, which has no
// timeout, so without this an identity provider that accepts the
// connection and then says nothing would hang start-up forever, which
// looks exactly like a hung server rather than a misconfigured provider.
const oidcDiscoveryTimeout = 15 * time.Second

// initOIDC performs OpenID Connect discovery and derives the login state
// key, when federated login is enabled. It does nothing at all when it
// is not, leaving s.oidcProvider nil, which is what makes the endpoints
// answer 404.
//
// A discovery failure is fatal, deliberately and in the same spirit as
// an unusable TLS certificate: if the provider cannot be reached at
// start-up then nobody can log in through it, and a server that starts
// anyway serves a login page whose button silently does not work.
func (s *Server) initOIDC(serverSecret string) error {
	if s.cfg == nil || !s.cfg.HTTP.Auth.OIDC.IsEnabled() {
		return nil
	}

	stateKey := crypto.DeriveKey(serverSecret, []byte(oidcStateKeySalt))
	if len(stateKey) == 0 {
		return fmt.Errorf("cannot derive the OIDC login state key: the server secret is empty")
	}

	ctx, cancel := context.WithTimeout(s.ctx, oidcDiscoveryTimeout)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, s.cfg.HTTP.Auth.OIDC)
	if err != nil {
		return fmt.Errorf("failed to initialize OIDC login: %w", err)
	}

	s.oidcProvider = provider
	s.oidcStateKey = stateKey

	fmt.Fprintf(os.Stderr, "OIDC login: ENABLED (issuer: %s)\n", s.cfg.HTTP.Auth.OIDC.Issuer)
	logOIDCStartupWarnings(os.Stderr, s.cfg)

	return nil
}

// logOIDCStartupWarnings prints the start-up notices for a configuration
// with federated login enabled. Each of them names a consequence of the
// configuration that is correct as far as validation can tell but that
// an operator may not have intended, and nothing later in the life of
// the process will say any of it.
func logOIDCStartupWarnings(w io.Writer, cfg *config.Config) {
	oidcCfg := cfg.HTTP.Auth.OIDC

	// redirect_url is validated for shape, not pinned to this server's
	// own origin, because the server cannot know the address the browser
	// reaches it at. It is the address the identity provider delivers
	// every authorization code to, so a typo here starts cleanly and
	// sends every code to the wrong host; naming the host at start-up
	// is what catches the typo. PKCE means a leaked code cannot be
	// redeemed without the verifier, so the failure is broken login,
	// not account takeover.
	if redirect, err := url.Parse(oidcCfg.RedirectURL); err == nil && redirect.Host != "" {
		fmt.Fprintf(w,
			"OIDC login: the identity provider will deliver authorization codes to %s\n"+
				"           (http.auth.oidc.redirect_url); confirm that this is the address the\n"+
				"           browser reaches this server at.\n", redirect.Host)
	}

	// Without a trusted proxy list, every request behind a reverse proxy
	// arrives with that proxy's address, so the callback's rate limit
	// has one key for the whole deployment rather than one per client:
	// it stops nothing an attacker does and can be spent deliberately to
	// deny everyone else a login. The same list decides whether the OIDC
	// state cookie may use the "__Host-" name prefix, so without it the
	// cookie is written under its plain name.
	if len(cfg.HTTP.TrustedProxies) == 0 {
		fmt.Fprintf(w,
			"WARNING: http.trusted_proxies is empty, so per-client rate limiting of the OIDC\n"+
				"         callback is inoperative behind a reverse proxy: every request shares one\n"+
				"         allowance, and the login state cookie cannot use the __Host- prefix.\n"+
				"         Set http.trusted_proxies to the reverse proxy's address.\n")
	}

	// superuser_group hands the Workbench superuser flag to whoever can
	// add a member to one provider group, and superuser bypasses every
	// group grant and every API token connection scope (the pre-existing
	// short-circuit issue #482 tracks). Revocation takes effect at the
	// person's next login and not before, so a live session or an
	// existing token keeps full privilege until then.
	if oidcCfg.SuperuserGroup != "" {
		fmt.Fprintf(w,
			"WARNING: http.auth.oidc.superuser_group is set (%q): every member of that identity\n"+
				"         provider group holds Workbench superuser, which bypasses all group grants\n"+
				"         and all API token connection scopes. Removal at the provider takes effect\n"+
				"         at the member's next login; disable the account to revoke it sooner.\n",
			oidcCfg.SuperuserGroup)
	}
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
	s.purgeAuditEvents()

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
				s.purgeAuditEvents()
			}
		}
	}()
}

// purgeAuditEvents deletes RBAC audit events older than the
// configured retention period (http.auth.audit_retention_days). It
// runs once at startup and again on every token cleanup tick. A
// retention of zero disables the purge, keeping audit events forever.
func (s *Server) purgeAuditEvents() {
	if s.authStore == nil {
		return
	}

	days := s.cfg.HTTP.Auth.AuditRetentionDays()
	if days <= 0 {
		return
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	if removed, err := s.authStore.PurgeAuditEvents(cutoff); err != nil {
		log.Printf("[ERROR] Failed to purge audit events: %v", err)
	} else if removed > 0 {
		fmt.Fprintf(os.Stderr, "Removed %d audit event(s) older than %d days\n", removed, days)
	}
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
		LLMConfig:              &s.cfg.LLM,
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
		CORSOrigin:     s.cfg.HTTP.CORSOrigin,
		HSTSEnabled:    s.cfg.HTTP.HSTSEnabled,
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

		OIDCProvider: s.oidcProvider,
		OIDCStateKey: s.oidcStateKey,

		RegisterCloser: s.registerHandlerCloser,
	}
	httpConfig.SetupHandlers = SetupHandlers(deps)

	// Log startup information
	s.logStartupInfo()

	// Setup SIGHUP handler for configuration reload
	s.setupSIGHUP(flags, configPath)

	// Setup SIGTERM/SIGINT handler for graceful shutdown and Go
	// coverage-counter flush.
	s.setupShutdownHandler()

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
		} else if s.cfg.Knowledgebase.EmbeddingGeminiAPIKey != "" {
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

// setupShutdownHandler registers a SIGTERM/SIGINT handler that drains
// the active HTTP server and, when the binary was built with -cover,
// flushes coverage counters to $GOCOVERDIR before exit.
//
// Without this, the Go runtime kills the process on SIGTERM without
// running the -cover atexit hook, so E2E coverage reports come back
// empty for the long-running server. The CLI subcommand path does
// not call this helper; its short-lived os.Exit(0) already triggers
// the runtime's normal counter flush.
//
// The closer slot is intentionally left nil: main.go already defers
// s.Close on a clean RunHTTP return, and racing that defer against
// a goroutine-driven Close has caused double-close panics in past
// graceful-shutdown implementations. Flushing coverage and forcing
// exit(0) is the only thing the runtime cannot do on its own.
func (s *Server) setupShutdownHandler() {
	installShutdownHandler(shutdownDeps{
		server:      s.mcpServer,
		gocoverdir:  os.Getenv("GOCOVERDIR"),
		writeCounts: realCoverageWriter,
	})
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

// VerifySchemaHealth delegates to the underlying datastore's schema
// health check. The caller (main) is expected to log the returned
// error and exit non-zero so a server with a missing, partial, or
// out-of-date collector schema never comes up.
//
// The method returns nil immediately when the datastore is not
// configured, because the rest of NewServer would already have
// failed in that case; this keeps the helper safe to call
// unconditionally from main.
func (s *Server) VerifySchemaHealth(ctx context.Context) error {
	if s == nil || s.datastore == nil {
		return nil
	}
	return s.datastore.VerifySchemaHealth(ctx)
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

	// Stop background resources owned by the HTTP handlers, such as the
	// AuthHandler's internal login rate limiter.
	s.runHandlerClosers()

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
