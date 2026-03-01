/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
	"gopkg.in/yaml.v3"
)

// Config represents the complete server configuration
type Config struct {
	// HTTP server configuration
	HTTP HTTPConfig `yaml:"http"`

	// Database connection configuration (single database - the datastore)
	Database *DatabaseConfig `yaml:"database"`

	// Connection security configuration (for user-created connections, SSRF protection)
	ConnectionSecurity ConnectionSecurityConfig `yaml:"connection_security"`

	// Embedding configuration
	Embedding EmbeddingConfig `yaml:"embedding"`

	// LLM configuration (for web client chat proxy)
	LLM LLMConfig `yaml:"llm"`

	// Knowledgebase configuration
	Knowledgebase KnowledgebaseConfig `yaml:"knowledgebase"`

	// Memory configuration
	Memory MemoryConfig `yaml:"memory"`

	// Built-in tools, resources, and prompts configuration
	Builtins BuiltinsConfig `yaml:"builtins"`

	// Secret file path (for encryption key)
	SecretFile string `yaml:"secret_file"`

	// Custom definitions file path (for user-defined prompts and resources)
	CustomDefinitionsPath string `yaml:"custom_definitions_path"`

	// Data directory path (for conversation history, etc.)
	DataDir string `yaml:"data_dir"`

	// Trace file path (for MCP request/response tracing)
	TraceFile string `yaml:"trace_file"`
}

// BuiltinsConfig holds configuration for enabling/disabling built-in tools, resources, and prompts
type BuiltinsConfig struct {
	Tools     ToolsConfig     `yaml:"tools"`
	Resources ResourcesConfig `yaml:"resources"`
	Prompts   PromptsConfig   `yaml:"prompts"`
}

// ToolsConfig holds configuration for enabling/disabling built-in tools
// All tools are enabled by default
// Note: read_resource tool is always enabled as it's used to list resources
type ToolsConfig struct {
	QueryDatabase       *bool `yaml:"query_database"`       // Execute SQL queries (default: true)
	GetSchemaInfo       *bool `yaml:"get_schema_info"`      // Get detailed schema information (default: true)
	SimilaritySearch    *bool `yaml:"similarity_search"`    // Vector similarity search (default: true)
	ExecuteExplain      *bool `yaml:"execute_explain"`      // Execute EXPLAIN queries (default: true)
	GenerateEmbedding   *bool `yaml:"generate_embedding"`   // Generate text embeddings (default: true)
	SearchKnowledgebase *bool `yaml:"search_knowledgebase"` // Search knowledgebase (default: true)
	CountRows           *bool `yaml:"count_rows"`           // Count table rows (default: true)
	ListProbes          *bool `yaml:"list_probes"`          // List available metrics probes (default: true)
	DescribeProbe       *bool `yaml:"describe_probe"`       // Describe metrics in a probe (default: true)
	QueryMetrics        *bool `yaml:"query_metrics"`        // Query collected metrics (default: true)
	ListConnections     *bool `yaml:"list_connections"`     // List available connections (default: true)
	GetAlertHistory     *bool `yaml:"get_alert_history"`    // Query historic alerts (default: true)
	GetAlertRules       *bool `yaml:"get_alert_rules"`      // Query alert rules and thresholds (default: true)
	GetMetricBaselines  *bool `yaml:"get_metric_baselines"` // Query metric baselines for anomaly context (default: true)
	QueryDatastore      *bool `yaml:"query_datastore"`      // Execute read-only SQL against the datastore (default: true)
	StoreMemory         *bool `yaml:"store_memory"`         // Store chat memories for future recall (default: true)
	RecallMemories      *bool `yaml:"recall_memories"`      // Recall stored chat memories by similarity (default: true)
	DeleteMemory        *bool `yaml:"delete_memory"`        // Delete a stored chat memory by ID (default: true)
}

// ResourcesConfig holds configuration for enabling/disabling built-in resources
// All resources are enabled by default
type ResourcesConfig struct {
	SystemInfo     *bool `yaml:"system_info"`     // pg://system_info (default: true)
	ConnectionInfo *bool `yaml:"connection_info"` // pg://connection_info (default: true)
}

// PromptsConfig holds configuration for enabling/disabling built-in prompts
// Currently no built-in prompts are registered; this struct is kept for future use
type PromptsConfig struct {
	// Future prompt configuration fields can be added here
}

// IsToolEnabled returns true if the specified tool is enabled (defaults to true if not set)
func (c *ToolsConfig) IsToolEnabled(toolName string) bool {
	switch toolName {
	case "query_database":
		return c.QueryDatabase == nil || *c.QueryDatabase
	case "get_schema_info":
		return c.GetSchemaInfo == nil || *c.GetSchemaInfo
	case "similarity_search":
		return c.SimilaritySearch == nil || *c.SimilaritySearch
	case "execute_explain":
		return c.ExecuteExplain == nil || *c.ExecuteExplain
	case "generate_embedding":
		return c.GenerateEmbedding == nil || *c.GenerateEmbedding
	case "search_knowledgebase":
		return c.SearchKnowledgebase == nil || *c.SearchKnowledgebase
	case "count_rows":
		return c.CountRows == nil || *c.CountRows
	case "list_probes":
		return c.ListProbes == nil || *c.ListProbes
	case "describe_probe":
		return c.DescribeProbe == nil || *c.DescribeProbe
	case "query_metrics":
		return c.QueryMetrics == nil || *c.QueryMetrics
	case "list_connections":
		return c.ListConnections == nil || *c.ListConnections
	case "get_alert_history":
		return c.GetAlertHistory == nil || *c.GetAlertHistory
	case "get_alert_rules":
		return c.GetAlertRules == nil || *c.GetAlertRules
	case "get_metric_baselines":
		return c.GetMetricBaselines == nil || *c.GetMetricBaselines
	case "query_datastore":
		return c.QueryDatastore == nil || *c.QueryDatastore
	case "store_memory":
		return c.StoreMemory == nil || *c.StoreMemory
	case "recall_memories":
		return c.RecallMemories == nil || *c.RecallMemories
	case "delete_memory":
		return c.DeleteMemory == nil || *c.DeleteMemory
	default:
		return true // Unknown tools are enabled by default
	}
}

// IsResourceEnabled returns true if the specified resource is enabled (defaults to true if not set)
func (c *ResourcesConfig) IsResourceEnabled(resourceURI string) bool {
	switch resourceURI {
	case "pg://system_info":
		return c.SystemInfo == nil || *c.SystemInfo
	case "pg://connection_info":
		return c.ConnectionInfo == nil || *c.ConnectionInfo
	default:
		return true // Unknown resources are enabled by default
	}
}

// IsPromptEnabled returns true if the specified prompt is enabled (defaults to true if not set)
// Currently no built-in prompts are registered; this method is kept for future use
func (c *PromptsConfig) IsPromptEnabled(promptName string) bool {
	// All prompts are enabled by default
	return true
}

// HTTPConfig holds HTTP/HTTPS server settings
type HTTPConfig struct {
	Address        string     `yaml:"address"`
	TLS            TLSConfig  `yaml:"tls"`
	Auth           AuthConfig `yaml:"auth"`
	TrustedProxies []string   `yaml:"trusted_proxies"` // CIDR ranges of trusted reverse proxies (e.g., ["10.0.0.0/8", "172.16.0.0/12"])
	CORSOrigin     string     `yaml:"cors_origin"`     // Allowed CORS origin (e.g., "https://app.example.com"); empty disables CORS headers
	HSTSEnabled    bool       `yaml:"hsts_enabled"`    // Enable Strict-Transport-Security header (default: false)
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	MaxUserTokenDays               int `yaml:"max_user_token_days"`                // Maximum lifetime for user-created tokens in days (0 = unlimited)
	MaxFailedAttemptsBeforeLockout int `yaml:"max_failed_attempts_before_lockout"` // Number of failed login attempts before account lockout (0 = disabled)
	RateLimitWindowMinutes         int `yaml:"rate_limit_window_minutes"`          // Time window in minutes for rate limiting (default: 15)
	RateLimitMaxAttempts           int `yaml:"rate_limit_max_attempts"`            // Maximum failed attempts per IP in the time window (default: 10)
}

// TLSConfig holds TLS/HTTPS settings
type TLSConfig struct {
	Enabled   bool   `yaml:"enabled"`
	CertFile  string `yaml:"cert_file"`
	KeyFile   string `yaml:"key_file"`
	ChainFile string `yaml:"chain_file"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Host     string `yaml:"host"`     // Database host (default: localhost)
	Port     int    `yaml:"port"`     // Database port (default: 5432)
	Database string `yaml:"database"` // Database name (default: postgres)
	User     string `yaml:"user"`     // Database user (required)
	Password string `yaml:"password"` // Database password (optional, will use PGEDGE_DB_PASSWORD env var or .pgpass if not set)
	SSLMode  string `yaml:"sslmode"`  // SSL mode: disable, require, verify-ca, verify-full (default: prefer)

	// Connection pool settings
	PoolMaxConns        int    `yaml:"pool_max_conns"`          // Maximum number of connections (default: 4)
	PoolMinConns        int    `yaml:"pool_min_conns"`          // Minimum number of connections (default: 0)
	PoolMaxConnIdleTime string `yaml:"pool_max_conn_idle_time"` // Max time a connection can be idle before being closed (default: 30m)
}

// ConnectionSecurityConfig holds settings for user-created database connections (SSRF protection)
type ConnectionSecurityConfig struct {
	// AllowInternalNetworks permits connections to RFC 1918 private addresses,
	// localhost, link-local, and other internal network ranges.
	// Default: false (connections to internal networks are blocked)
	AllowInternalNetworks bool `yaml:"allow_internal_networks"`

	// AllowedHosts is an optional allowlist of hosts/IPs/CIDRs that are always permitted.
	// Example: ["db.example.com", "192.168.1.0/24"]
	AllowedHosts []string `yaml:"allowed_hosts"`

	// BlockedHosts is an optional blocklist of hosts/IPs/CIDRs that are never permitted.
	// Evaluated after AllowedHosts.
	// Example: ["metadata.internal", "169.254.169.254"]
	BlockedHosts []string `yaml:"blocked_hosts"`
}

// BuildConnectionString creates a PostgreSQL connection string from DatabaseConfig
// If password is not set, pgx will automatically look it up from .pgpass file
func (cfg *DatabaseConfig) BuildConnectionString() string {
	// Build connection string components with URL-encoded user/password
	connStr := fmt.Sprintf("postgres://%s", url.PathEscape(cfg.User))

	// Add password only if explicitly set
	// If not set, pgx will use .pgpass file automatically
	if cfg.Password != "" {
		connStr += ":" + url.PathEscape(cfg.Password)
	}

	connStr += fmt.Sprintf("@%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)

	// Add SSL mode
	if cfg.SSLMode != "" {
		connStr += "?sslmode=" + cfg.SSLMode
	}

	return connStr
}

// EmbeddingConfig holds embedding generation settings
type EmbeddingConfig struct {
	Enabled          bool   `yaml:"enabled"`             // Whether embedding generation is enabled (default: false)
	Provider         string `yaml:"provider"`            // "voyage", "openai", or "ollama"
	Model            string `yaml:"model"`               // Provider-specific model name
	VoyageAPIKey     string `yaml:"-"`                   // API key for Voyage AI (loaded from file, not config)
	VoyageAPIKeyFile string `yaml:"voyage_api_key_file"` // Path to file containing Voyage API key
	VoyageBaseURL    string `yaml:"voyage_base_url"`     // Base URL for Voyage AI API (default: https://api.voyageai.com/v1)
	OpenAIAPIKey     string `yaml:"-"`                   // API key for OpenAI (loaded from file, not config)
	OpenAIAPIKeyFile string `yaml:"openai_api_key_file"` // Path to file containing OpenAI API key
	OpenAIBaseURL    string `yaml:"openai_base_url"`     // Base URL for OpenAI API (default: https://api.openai.com/v1)
	OllamaURL        string `yaml:"ollama_url"`          // URL for Ollama service (default: http://localhost:11434)
}

// LLMConfig holds LLM configuration for web client chat proxy
// LLM proxy is always enabled - API keys must be configured for the chosen provider
type LLMConfig struct {
	Provider                string  `yaml:"provider"`                  // "anthropic", "openai", "gemini", or "ollama"
	Model                   string  `yaml:"model"`                     // Provider-specific model name
	AnthropicAPIKey         string  `yaml:"-"`                         // API key for Anthropic (loaded from file, not config)
	AnthropicAPIKeyFile     string  `yaml:"anthropic_api_key_file"`    // Path to file containing Anthropic API key
	AnthropicBaseURL        string  `yaml:"anthropic_base_url"`        // Base URL for Anthropic API (default: https://api.anthropic.com/v1)
	OpenAIAPIKey            string  `yaml:"-"`                         // API key for OpenAI (loaded from file, not config)
	OpenAIAPIKeyFile        string  `yaml:"openai_api_key_file"`       // Path to file containing OpenAI API key
	OpenAIBaseURL           string  `yaml:"openai_base_url"`           // Base URL for OpenAI API (default: https://api.openai.com/v1)
	GeminiAPIKey            string  `yaml:"-"`                         // API key for Google Gemini (loaded from file, not config)
	GeminiAPIKeyFile        string  `yaml:"gemini_api_key_file"`       // Path to file containing Gemini API key
	GeminiBaseURL           string  `yaml:"gemini_base_url"`           // Base URL for Gemini API (default: https://generativelanguage.googleapis.com)
	OllamaURL               string  `yaml:"ollama_url"`                // URL for Ollama service (default: http://localhost:11434)
	MaxTokens               int     `yaml:"max_tokens"`                // Maximum tokens for LLM response (default: 4096)
	MaxIterations           int     `yaml:"max_iterations"`            // Maximum agentic loop iterations (default: 50)
	Temperature             float64 `yaml:"temperature"`               // Temperature for LLM sampling (default: 0.7)
	CompactToolDescriptions string  `yaml:"compact_tool_descriptions"` // "auto" (default), "true", or "false"
}

// UseCompactDescriptions resolves the compact_tool_descriptions setting
// to a boolean. In "auto" mode, compact descriptions are used when the
// active LLM endpoint resolves to a localhost address.
func (c *LLMConfig) UseCompactDescriptions() bool {
	switch strings.ToLower(c.CompactToolDescriptions) {
	case "true":
		return true
	case "false":
		return false
	default:
		// "auto" or empty: check the active endpoint URL
		endpointURL := c.activeEndpointURL()
		return isLocalhostURL(endpointURL)
	}
}

// activeEndpointURL returns the endpoint URL for the configured provider.
func (c *LLMConfig) activeEndpointURL() string {
	switch c.Provider {
	case "openai":
		if c.OpenAIBaseURL != "" {
			return c.OpenAIBaseURL
		}
		return "https://api.openai.com/v1"
	case "ollama":
		if c.OllamaURL != "" {
			return c.OllamaURL
		}
		return "http://localhost:11434"
	case "anthropic":
		if c.AnthropicBaseURL != "" {
			return c.AnthropicBaseURL
		}
		return "https://api.anthropic.com/v1"
	case "gemini":
		if c.GeminiBaseURL != "" {
			return c.GeminiBaseURL
		}
		return "https://generativelanguage.googleapis.com"
	default:
		return ""
	}
}

// isLocalhostURL parses a URL and returns true when the hostname is a
// loopback or unspecified address (localhost, 127.x.x.x, ::1, 0.0.0.0).
func isLocalhostURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	hostname := parsed.Hostname()

	if strings.EqualFold(hostname, "localhost") {
		return true
	}

	if strings.HasPrefix(hostname, "127.") {
		return true
	}

	ip := net.ParseIP(hostname)
	if ip == nil {
		return false
	}

	if ip.IsLoopback() {
		return true
	}

	if ip.Equal(net.IPv4zero) {
		return true
	}

	return false
}

// KnowledgebaseConfig holds knowledgebase configuration
type KnowledgebaseConfig struct {
	Enabled      bool   `yaml:"enabled"`       // Whether knowledgebase search is enabled (default: false)
	DatabasePath string `yaml:"database_path"` // Path to SQLite knowledgebase database

	// Embedding provider configuration for KB similarity search (independent of generate_embeddings tool)
	EmbeddingProvider         string `yaml:"embedding_provider"`            // "voyage", "openai", or "ollama"
	EmbeddingModel            string `yaml:"embedding_model"`               // Provider-specific model name
	EmbeddingVoyageAPIKey     string `yaml:"-"`                             // API key for Voyage AI (loaded from file, not config)
	EmbeddingVoyageAPIKeyFile string `yaml:"embedding_voyage_api_key_file"` // Path to file containing Voyage API key
	EmbeddingVoyageBaseURL    string `yaml:"embedding_voyage_base_url"`     // Base URL for Voyage AI API (default: https://api.voyageai.com/v1)
	EmbeddingOpenAIAPIKey     string `yaml:"-"`                             // API key for OpenAI (loaded from file, not config)
	EmbeddingOpenAIAPIKeyFile string `yaml:"embedding_openai_api_key_file"` // Path to file containing OpenAI API key
	EmbeddingOpenAIBaseURL    string `yaml:"embedding_openai_base_url"`     // Base URL for OpenAI API (default: https://api.openai.com/v1)
	EmbeddingOllamaURL        string `yaml:"embedding_ollama_url"`          // URL for Ollama service (default: http://localhost:11434)
}

// MemoryConfig holds chat memory configuration
type MemoryConfig struct {
	Enabled *bool `yaml:"enabled"` // Whether chat memory is enabled (default: true)
}

// IsEnabled returns the effective value of the Enabled field,
// defaulting to true when the pointer is nil (omitted from config).
func (m MemoryConfig) IsEnabled() bool {
	if m.Enabled == nil {
		return true
	}
	return *m.Enabled
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool {
	return &b
}

// LoadConfig loads configuration with proper priority:
// 1. Command line flags (highest priority)
// 2. Environment variables
// 3. Configuration file
// 4. Hard-coded defaults (lowest priority)
func LoadConfig(configPath string, cliFlags CLIFlags) (*Config, error) {
	// Start with defaults
	cfg := defaultConfig()

	// Load config file if it exists
	if configPath != "" {
		fileCfg, err := loadConfigFile(configPath)
		if err != nil {
			// If file was explicitly specified, error out
			if cliFlags.ConfigFileSet {
				return nil, fmt.Errorf("failed to load config file %s: %w", configPath, err)
			}
			// Otherwise just use defaults (file may not exist and that's ok)
		} else {
			// Merge file config into defaults
			mergeConfig(cfg, fileCfg)
		}
	}

	// Load API keys from files if specified
	loadAPIKeysFromFiles(cfg)

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Override with command line flags (highest priority)
	applyCLIFlags(cfg, cliFlags)

	// Validate final configuration
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// CLIFlags represents command line flag values and whether they were explicitly set
type CLIFlags struct {
	ConfigFileSet bool
	ConfigFile    string

	// HTTP flags
	HTTPAddr    string
	HTTPAddrSet bool

	// TLS flags
	TLSEnabled    bool
	TLSEnabledSet bool
	TLSCertFile   string
	TLSCertSet    bool
	TLSKeyFile    string
	TLSKeySet     bool
	TLSChainFile  string
	TLSChainSet   bool

	// Database flags
	DBHost     string
	DBHostSet  bool
	DBPort     int
	DBPortSet  bool
	DBName     string
	DBNameSet  bool
	DBUser     string
	DBUserSet  bool
	DBPassword string
	DBPassSet  bool
	DBSSLMode  string
	DBSSLSet   bool

	// Secret file flags
	SecretFile    string
	SecretFileSet bool

	// Trace file flags
	TraceFile    string
	TraceFileSet bool
}

// defaultConfig returns configuration with hard-coded defaults
func defaultConfig() *Config {
	return &Config{
		HTTP: HTTPConfig{
			Address: ":8080",
			TLS: TLSConfig{
				Enabled:   false,
				CertFile:  "./server.crt",
				KeyFile:   "./server.key",
				ChainFile: "",
			},
			Auth: AuthConfig{
				MaxUserTokenDays:               0,  // Unlimited by default
				MaxFailedAttemptsBeforeLockout: 10, // Lock account after 10 failed attempts
				RateLimitWindowMinutes:         15, // 15 minute window for rate limiting
				RateLimitMaxAttempts:           10, // 10 attempts per IP per window
			},
		},
		Database: nil, // No database configured by default
		Embedding: EmbeddingConfig{
			Enabled:      false,                    // Disabled by default (opt-in)
			Provider:     "ollama",                 // Default provider
			Model:        "nomic-embed-text",       // Default Ollama model
			VoyageAPIKey: "",                       // Must be provided if using Voyage AI
			OllamaURL:    "http://localhost:11434", // Default Ollama URL
		},
		LLM: LLMConfig{
			Provider:        "anthropic",              // Default provider
			Model:           "claude-sonnet-4-5",      // Default Anthropic model
			AnthropicAPIKey: "",                       // Must be provided if using Anthropic
			OpenAIAPIKey:    "",                       // Must be provided if using OpenAI
			GeminiAPIKey:    "",                       // Must be provided if using Gemini
			OllamaURL:       "http://localhost:11434", // Default Ollama URL
			MaxTokens:       4096,                     // Default max tokens
			MaxIterations:   50,                       // Default max agentic loop iterations
			Temperature:     0.7,                      // Default temperature
		},
		Knowledgebase: KnowledgebaseConfig{
			Enabled:               false,                    // Disabled by default (opt-in)
			DatabasePath:          "",                       // Must be provided if enabled
			EmbeddingProvider:     "ollama",                 // Default provider for KB embeddings
			EmbeddingModel:        "nomic-embed-text",       // Default Ollama model
			EmbeddingOllamaURL:    "http://localhost:11434", // Default Ollama URL
			EmbeddingVoyageAPIKey: "",                       // Must be provided if using Voyage
			EmbeddingOpenAIAPIKey: "",                       // Must be provided if using OpenAI
		},
		Memory: MemoryConfig{
			Enabled: boolPtr(true), // Enabled by default
		},
		SecretFile: "", // Will be set to default path if not specified
	}
}

// loadConfigFile loads configuration from a YAML file
func loadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &cfg, nil
}

// mergeConfig merges source config into dest, only overriding non-zero values
func mergeConfig(dest, src *Config) {
	// HTTP
	if src.HTTP.Address != "" {
		dest.HTTP.Address = src.HTTP.Address
	}

	// TLS
	if src.HTTP.TLS.Enabled {
		dest.HTTP.TLS.Enabled = src.HTTP.TLS.Enabled
	}
	if src.HTTP.TLS.CertFile != "" {
		dest.HTTP.TLS.CertFile = src.HTTP.TLS.CertFile
	}
	if src.HTTP.TLS.KeyFile != "" {
		dest.HTTP.TLS.KeyFile = src.HTTP.TLS.KeyFile
	}
	if src.HTTP.TLS.ChainFile != "" {
		dest.HTTP.TLS.ChainFile = src.HTTP.TLS.ChainFile
	}

	// Trusted proxies for secure IP extraction
	if len(src.HTTP.TrustedProxies) > 0 {
		dest.HTTP.TrustedProxies = src.HTTP.TrustedProxies
	}

	// CORS origin
	if src.HTTP.CORSOrigin != "" {
		dest.HTTP.CORSOrigin = src.HTTP.CORSOrigin
	}

	// HSTS
	if src.HTTP.HSTSEnabled {
		dest.HTTP.HSTSEnabled = src.HTTP.HSTSEnabled
	}

	// Auth - authentication is always required; the Enabled field is not overridable
	if src.HTTP.Auth.MaxUserTokenDays > 0 {
		dest.HTTP.Auth.MaxUserTokenDays = src.HTTP.Auth.MaxUserTokenDays
	}
	if src.HTTP.Auth.MaxFailedAttemptsBeforeLockout >= 0 {
		dest.HTTP.Auth.MaxFailedAttemptsBeforeLockout = src.HTTP.Auth.MaxFailedAttemptsBeforeLockout
	}
	if src.HTTP.Auth.RateLimitWindowMinutes > 0 {
		dest.HTTP.Auth.RateLimitWindowMinutes = src.HTTP.Auth.RateLimitWindowMinutes
	}
	if src.HTTP.Auth.RateLimitMaxAttempts > 0 {
		dest.HTTP.Auth.RateLimitMaxAttempts = src.HTTP.Auth.RateLimitMaxAttempts
	}

	// Database - if source has database defined, use it
	if src.Database != nil {
		dest.Database = src.Database
	}

	// Embedding - merge if any embedding fields are set
	if src.Embedding.Provider != "" || src.Embedding.Enabled {
		dest.Embedding.Enabled = src.Embedding.Enabled
		if src.Embedding.Provider != "" {
			dest.Embedding.Provider = src.Embedding.Provider
		}
		if src.Embedding.Model != "" {
			dest.Embedding.Model = src.Embedding.Model
		}
		if src.Embedding.VoyageAPIKey != "" {
			dest.Embedding.VoyageAPIKey = src.Embedding.VoyageAPIKey
		}
		if src.Embedding.VoyageAPIKeyFile != "" {
			dest.Embedding.VoyageAPIKeyFile = src.Embedding.VoyageAPIKeyFile
		}
		if src.Embedding.OpenAIAPIKey != "" {
			dest.Embedding.OpenAIAPIKey = src.Embedding.OpenAIAPIKey
		}
		if src.Embedding.OpenAIAPIKeyFile != "" {
			dest.Embedding.OpenAIAPIKeyFile = src.Embedding.OpenAIAPIKeyFile
		}
		if src.Embedding.VoyageBaseURL != "" {
			dest.Embedding.VoyageBaseURL = src.Embedding.VoyageBaseURL
		}
		if src.Embedding.OpenAIBaseURL != "" {
			dest.Embedding.OpenAIBaseURL = src.Embedding.OpenAIBaseURL
		}
		if src.Embedding.OllamaURL != "" {
			dest.Embedding.OllamaURL = src.Embedding.OllamaURL
		}
	}

	// LLM - merge LLM fields (LLM proxy is always enabled)
	if src.LLM.Provider != "" {
		dest.LLM.Provider = src.LLM.Provider
	}
	if src.LLM.Model != "" {
		dest.LLM.Model = src.LLM.Model
	}
	if src.LLM.AnthropicAPIKey != "" {
		dest.LLM.AnthropicAPIKey = src.LLM.AnthropicAPIKey
	}
	if src.LLM.AnthropicAPIKeyFile != "" {
		dest.LLM.AnthropicAPIKeyFile = src.LLM.AnthropicAPIKeyFile
	}
	if src.LLM.AnthropicBaseURL != "" {
		dest.LLM.AnthropicBaseURL = src.LLM.AnthropicBaseURL
	}
	if src.LLM.OpenAIAPIKey != "" {
		dest.LLM.OpenAIAPIKey = src.LLM.OpenAIAPIKey
	}
	if src.LLM.OpenAIAPIKeyFile != "" {
		dest.LLM.OpenAIAPIKeyFile = src.LLM.OpenAIAPIKeyFile
	}
	if src.LLM.OpenAIBaseURL != "" {
		dest.LLM.OpenAIBaseURL = src.LLM.OpenAIBaseURL
	}
	if src.LLM.GeminiAPIKey != "" {
		dest.LLM.GeminiAPIKey = src.LLM.GeminiAPIKey
	}
	if src.LLM.GeminiAPIKeyFile != "" {
		dest.LLM.GeminiAPIKeyFile = src.LLM.GeminiAPIKeyFile
	}
	if src.LLM.GeminiBaseURL != "" {
		dest.LLM.GeminiBaseURL = src.LLM.GeminiBaseURL
	}
	if src.LLM.OllamaURL != "" {
		dest.LLM.OllamaURL = src.LLM.OllamaURL
	}
	if src.LLM.MaxTokens != 0 {
		dest.LLM.MaxTokens = src.LLM.MaxTokens
	}
	if src.LLM.MaxIterations != 0 {
		dest.LLM.MaxIterations = src.LLM.MaxIterations
	}
	if src.LLM.Temperature != 0 {
		dest.LLM.Temperature = src.LLM.Temperature
	}
	if src.LLM.CompactToolDescriptions != "" {
		dest.LLM.CompactToolDescriptions = src.LLM.CompactToolDescriptions
	}

	// Knowledgebase - merge if any KB fields are set
	if src.Knowledgebase.DatabasePath != "" || src.Knowledgebase.Enabled {
		dest.Knowledgebase.Enabled = src.Knowledgebase.Enabled
		if src.Knowledgebase.DatabasePath != "" {
			dest.Knowledgebase.DatabasePath = src.Knowledgebase.DatabasePath
		}
		if src.Knowledgebase.EmbeddingProvider != "" {
			dest.Knowledgebase.EmbeddingProvider = src.Knowledgebase.EmbeddingProvider
		}
		if src.Knowledgebase.EmbeddingModel != "" {
			dest.Knowledgebase.EmbeddingModel = src.Knowledgebase.EmbeddingModel
		}
		if src.Knowledgebase.EmbeddingVoyageAPIKey != "" {
			dest.Knowledgebase.EmbeddingVoyageAPIKey = src.Knowledgebase.EmbeddingVoyageAPIKey
		}
		if src.Knowledgebase.EmbeddingVoyageAPIKeyFile != "" {
			dest.Knowledgebase.EmbeddingVoyageAPIKeyFile = src.Knowledgebase.EmbeddingVoyageAPIKeyFile
		}
		if src.Knowledgebase.EmbeddingOpenAIAPIKey != "" {
			dest.Knowledgebase.EmbeddingOpenAIAPIKey = src.Knowledgebase.EmbeddingOpenAIAPIKey
		}
		if src.Knowledgebase.EmbeddingOpenAIAPIKeyFile != "" {
			dest.Knowledgebase.EmbeddingOpenAIAPIKeyFile = src.Knowledgebase.EmbeddingOpenAIAPIKeyFile
		}
		if src.Knowledgebase.EmbeddingVoyageBaseURL != "" {
			dest.Knowledgebase.EmbeddingVoyageBaseURL = src.Knowledgebase.EmbeddingVoyageBaseURL
		}
		if src.Knowledgebase.EmbeddingOpenAIBaseURL != "" {
			dest.Knowledgebase.EmbeddingOpenAIBaseURL = src.Knowledgebase.EmbeddingOpenAIBaseURL
		}
		if src.Knowledgebase.EmbeddingOllamaURL != "" {
			dest.Knowledgebase.EmbeddingOllamaURL = src.Knowledgebase.EmbeddingOllamaURL
		}
	}

	// Memory - only override when explicitly set in the source config
	if src.Memory.Enabled != nil {
		dest.Memory.Enabled = src.Memory.Enabled
	}

	// Secret file
	if src.SecretFile != "" {
		dest.SecretFile = src.SecretFile
	}

	// Custom definitions path
	if src.CustomDefinitionsPath != "" {
		dest.CustomDefinitionsPath = src.CustomDefinitionsPath
	}

	// Data directory
	if src.DataDir != "" {
		dest.DataDir = src.DataDir
	}

	// Trace file
	if src.TraceFile != "" {
		dest.TraceFile = src.TraceFile
	}

	// Connection security - merge if any fields are set
	if src.ConnectionSecurity.AllowInternalNetworks {
		dest.ConnectionSecurity.AllowInternalNetworks = src.ConnectionSecurity.AllowInternalNetworks
	}
	if len(src.ConnectionSecurity.AllowedHosts) > 0 {
		dest.ConnectionSecurity.AllowedHosts = src.ConnectionSecurity.AllowedHosts
	}
	if len(src.ConnectionSecurity.BlockedHosts) > 0 {
		dest.ConnectionSecurity.BlockedHosts = src.ConnectionSecurity.BlockedHosts
	}

	// Builtins - merge individual settings (pointer fields preserve explicit false values)
	// Tools
	if src.Builtins.Tools.QueryDatabase != nil {
		dest.Builtins.Tools.QueryDatabase = src.Builtins.Tools.QueryDatabase
	}
	if src.Builtins.Tools.GetSchemaInfo != nil {
		dest.Builtins.Tools.GetSchemaInfo = src.Builtins.Tools.GetSchemaInfo
	}
	if src.Builtins.Tools.SimilaritySearch != nil {
		dest.Builtins.Tools.SimilaritySearch = src.Builtins.Tools.SimilaritySearch
	}
	if src.Builtins.Tools.ExecuteExplain != nil {
		dest.Builtins.Tools.ExecuteExplain = src.Builtins.Tools.ExecuteExplain
	}
	if src.Builtins.Tools.GenerateEmbedding != nil {
		dest.Builtins.Tools.GenerateEmbedding = src.Builtins.Tools.GenerateEmbedding
	}
	if src.Builtins.Tools.SearchKnowledgebase != nil {
		dest.Builtins.Tools.SearchKnowledgebase = src.Builtins.Tools.SearchKnowledgebase
	}
	if src.Builtins.Tools.CountRows != nil {
		dest.Builtins.Tools.CountRows = src.Builtins.Tools.CountRows
	}
	if src.Builtins.Tools.ListProbes != nil {
		dest.Builtins.Tools.ListProbes = src.Builtins.Tools.ListProbes
	}
	if src.Builtins.Tools.DescribeProbe != nil {
		dest.Builtins.Tools.DescribeProbe = src.Builtins.Tools.DescribeProbe
	}
	if src.Builtins.Tools.QueryMetrics != nil {
		dest.Builtins.Tools.QueryMetrics = src.Builtins.Tools.QueryMetrics
	}
	if src.Builtins.Tools.StoreMemory != nil {
		dest.Builtins.Tools.StoreMemory = src.Builtins.Tools.StoreMemory
	}
	if src.Builtins.Tools.RecallMemories != nil {
		dest.Builtins.Tools.RecallMemories = src.Builtins.Tools.RecallMemories
	}
	if src.Builtins.Tools.DeleteMemory != nil {
		dest.Builtins.Tools.DeleteMemory = src.Builtins.Tools.DeleteMemory
	}
	// Resources
	if src.Builtins.Resources.SystemInfo != nil {
		dest.Builtins.Resources.SystemInfo = src.Builtins.Resources.SystemInfo
	}
	if src.Builtins.Resources.ConnectionInfo != nil {
		dest.Builtins.Resources.ConnectionInfo = src.Builtins.Resources.ConnectionInfo
	}
	// Prompts - no built-in prompts currently, merge logic can be added here for future prompts
}

// loadAPIKeysFromFiles loads API keys from files if specified in config
func loadAPIKeysFromFiles(cfg *Config) {
	// Embedding API keys
	if cfg.Embedding.VoyageAPIKey == "" && cfg.Embedding.VoyageAPIKeyFile != "" {
		if key, err := fileutil.ReadOptionalTrimmedFile(cfg.Embedding.VoyageAPIKeyFile); err == nil && key != "" {
			cfg.Embedding.VoyageAPIKey = key
		}
	}
	if cfg.Embedding.OpenAIAPIKey == "" && cfg.Embedding.OpenAIAPIKeyFile != "" {
		if key, err := fileutil.ReadOptionalTrimmedFile(cfg.Embedding.OpenAIAPIKeyFile); err == nil && key != "" {
			cfg.Embedding.OpenAIAPIKey = key
		}
	}

	// LLM API keys
	if cfg.LLM.AnthropicAPIKey == "" && cfg.LLM.AnthropicAPIKeyFile != "" {
		if key, err := fileutil.ReadOptionalTrimmedFile(cfg.LLM.AnthropicAPIKeyFile); err == nil && key != "" {
			cfg.LLM.AnthropicAPIKey = key
		}
	}
	if cfg.LLM.OpenAIAPIKey == "" && cfg.LLM.OpenAIAPIKeyFile != "" {
		if key, err := fileutil.ReadOptionalTrimmedFile(cfg.LLM.OpenAIAPIKeyFile); err == nil && key != "" {
			cfg.LLM.OpenAIAPIKey = key
		}
	}
	if cfg.LLM.GeminiAPIKey == "" && cfg.LLM.GeminiAPIKeyFile != "" {
		if key, err := fileutil.ReadOptionalTrimmedFile(cfg.LLM.GeminiAPIKeyFile); err == nil && key != "" {
			cfg.LLM.GeminiAPIKey = key
		}
	}

	// Knowledgebase API keys
	if cfg.Knowledgebase.EmbeddingVoyageAPIKey == "" && cfg.Knowledgebase.EmbeddingVoyageAPIKeyFile != "" {
		if key, err := fileutil.ReadOptionalTrimmedFile(cfg.Knowledgebase.EmbeddingVoyageAPIKeyFile); err == nil && key != "" {
			cfg.Knowledgebase.EmbeddingVoyageAPIKey = key
		}
	}
	if cfg.Knowledgebase.EmbeddingOpenAIAPIKey == "" && cfg.Knowledgebase.EmbeddingOpenAIAPIKeyFile != "" {
		if key, err := fileutil.ReadOptionalTrimmedFile(cfg.Knowledgebase.EmbeddingOpenAIAPIKeyFile); err == nil && key != "" {
			cfg.Knowledgebase.EmbeddingOpenAIAPIKey = key
		}
	}
}

// applyEnvOverrides applies environment variable overrides to the configuration.
// Environment variables take priority over config file values but are overridden by CLI flags.
func applyEnvOverrides(cfg *Config) {
	if val := os.Getenv("PGEDGE_MEMORY_ENABLED"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.Memory.Enabled = boolPtr(b)
		}
	}
}

// applyCLIFlags overrides config with CLI flags if they were explicitly set
func applyCLIFlags(cfg *Config, flags CLIFlags) {
	// HTTP
	if flags.HTTPAddrSet {
		cfg.HTTP.Address = flags.HTTPAddr
	}

	// TLS
	if flags.TLSEnabledSet {
		cfg.HTTP.TLS.Enabled = flags.TLSEnabled
	}
	if flags.TLSCertSet {
		cfg.HTTP.TLS.CertFile = flags.TLSCertFile
	}
	if flags.TLSKeySet {
		cfg.HTTP.TLS.KeyFile = flags.TLSKeyFile
	}
	if flags.TLSChainSet {
		cfg.HTTP.TLS.ChainFile = flags.TLSChainFile
	}

	// Database CLI flags
	// Create a default database if none exists and any DB flag is set
	if cfg.Database == nil && (flags.DBHostSet || flags.DBPortSet || flags.DBNameSet || flags.DBUserSet || flags.DBPassSet || flags.DBSSLSet) {
		cfg.Database = &DatabaseConfig{
			Host:                "localhost",
			Port:                5432,
			Database:            "postgres",
			SSLMode:             "prefer",
			PoolMaxConns:        4,
			PoolMinConns:        0,
			PoolMaxConnIdleTime: "30m",
		}
	}

	if cfg.Database != nil {
		if flags.DBHostSet {
			cfg.Database.Host = flags.DBHost
		}
		if flags.DBPortSet {
			cfg.Database.Port = flags.DBPort
		}
		if flags.DBNameSet {
			cfg.Database.Database = flags.DBName
		}
		if flags.DBUserSet {
			cfg.Database.User = flags.DBUser
		}
		if flags.DBPassSet {
			cfg.Database.Password = flags.DBPassword
		}
		if flags.DBSSLSet {
			cfg.Database.SSLMode = flags.DBSSLMode
		}
	}

	// Secret file
	if flags.SecretFileSet {
		cfg.SecretFile = flags.SecretFile
	}

	// Trace file
	if flags.TraceFileSet {
		cfg.TraceFile = flags.TraceFile
	}
}

// validateConfig checks if the configuration is valid
func validateConfig(cfg *Config) error {
	// If HTTPS is enabled, cert and key are required
	if cfg.HTTP.TLS.Enabled {
		if cfg.HTTP.TLS.CertFile == "" {
			return fmt.Errorf("TLS certificate file is required when HTTPS is enabled")
		}
		if cfg.HTTP.TLS.KeyFile == "" {
			return fmt.Errorf("TLS key file is required when HTTPS is enabled")
		}
	}

	// Database configuration validation
	if cfg.Database != nil && cfg.Database.User == "" {
		return fmt.Errorf("database user is required (set via -db-user flag or config file)")
	}

	return nil
}

// GetDefaultConfigPath returns the default config file path
// Searches /etc/pgedge/ first, then binary directory
func GetDefaultConfigPath(binaryPath string) string {
	return fileutil.GetDefaultConfigPath(binaryPath, "ai-dba-server.yaml")
}

// GetDefaultSecretPath returns the default secret file path
// Searches /etc/pgedge/ first, then binary directory
func GetDefaultSecretPath(binaryPath string) string {
	return fileutil.GetDefaultConfigPath(binaryPath, "ai-dba-server.secret")
}

// ConfigFileExists checks if a config file exists at the given path
func ConfigFileExists(path string) bool {
	return fileutil.FileExists(path)
}

// SaveConfig saves the configuration to a YAML file
func SaveConfig(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write with restrictive permissions (owner read/write only)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
