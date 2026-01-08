/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the complete server configuration
type Config struct {
	// HTTP server configuration
	HTTP HTTPConfig `yaml:"http"`

	// Database connection configuration (single database - the datastore)
	Database *DatabaseConfig `yaml:"database"`

	// Embedding configuration
	Embedding EmbeddingConfig `yaml:"embedding"`

	// LLM configuration (for web client chat proxy)
	LLM LLMConfig `yaml:"llm"`

	// Knowledgebase configuration
	Knowledgebase KnowledgebaseConfig `yaml:"knowledgebase"`

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
}

// ResourcesConfig holds configuration for enabling/disabling built-in resources
// All resources are enabled by default
type ResourcesConfig struct {
	SystemInfo     *bool `yaml:"system_info"`     // pg://system_info (default: true)
	ConnectionInfo *bool `yaml:"connection_info"` // pg://connection_info (default: true)
}

// PromptsConfig holds configuration for enabling/disabling built-in prompts
// All prompts are enabled by default
type PromptsConfig struct {
	ExploreDatabase     *bool `yaml:"explore_database"`      // explore-database prompt (default: true)
	SetupSemanticSearch *bool `yaml:"setup_semantic_search"` // setup-semantic-search prompt (default: true)
	DiagnoseQueryIssue  *bool `yaml:"diagnose_query_issue"`  // diagnose-query-issue prompt (default: true)
	DesignSchema        *bool `yaml:"design_schema"`         // design-schema prompt (default: true)
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
func (c *PromptsConfig) IsPromptEnabled(promptName string) bool {
	switch promptName {
	case "explore-database":
		return c.ExploreDatabase == nil || *c.ExploreDatabase
	case "setup-semantic-search":
		return c.SetupSemanticSearch == nil || *c.SetupSemanticSearch
	case "diagnose-query-issue":
		return c.DiagnoseQueryIssue == nil || *c.DiagnoseQueryIssue
	case "design-schema":
		return c.DesignSchema == nil || *c.DesignSchema
	default:
		return true // Unknown prompts are enabled by default
	}
}

// HTTPConfig holds HTTP/HTTPS server settings
type HTTPConfig struct {
	Address string     `yaml:"address"`
	TLS     TLSConfig  `yaml:"tls"`
	Auth    AuthConfig `yaml:"auth"`
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	Enabled                        bool `yaml:"enabled"`                            // Whether authentication is required
	MaxUserTokenDays               int  `yaml:"max_user_token_days"`                // Maximum lifetime for user-created tokens in days (0 = unlimited)
	MaxFailedAttemptsBeforeLockout int  `yaml:"max_failed_attempts_before_lockout"` // Number of failed login attempts before account lockout (0 = disabled)
	RateLimitWindowMinutes         int  `yaml:"rate_limit_window_minutes"`          // Time window in minutes for rate limiting (default: 15)
	RateLimitMaxAttempts           int  `yaml:"rate_limit_max_attempts"`            // Maximum failed attempts per IP in the time window (default: 10)
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

// BuildConnectionString creates a PostgreSQL connection string from DatabaseConfig
// If password is not set, pgx will automatically look it up from .pgpass file
func (cfg *DatabaseConfig) BuildConnectionString() string {
	// Build connection string components
	connStr := fmt.Sprintf("postgres://%s", cfg.User)

	// Add password only if explicitly set
	// If not set, pgx will use .pgpass file automatically
	if cfg.Password != "" {
		connStr += ":" + cfg.Password
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
	OpenAIAPIKey     string `yaml:"-"`                   // API key for OpenAI (loaded from file, not config)
	OpenAIAPIKeyFile string `yaml:"openai_api_key_file"` // Path to file containing OpenAI API key
	OllamaURL        string `yaml:"ollama_url"`          // URL for Ollama service (default: http://localhost:11434)
}

// LLMConfig holds LLM configuration for web client chat proxy
// LLM proxy is always enabled - API keys must be configured for the chosen provider
type LLMConfig struct {
	Provider            string  `yaml:"provider"`               // "anthropic", "openai", or "ollama"
	Model               string  `yaml:"model"`                  // Provider-specific model name
	AnthropicAPIKey     string  `yaml:"-"`                      // API key for Anthropic (loaded from file, not config)
	AnthropicAPIKeyFile string  `yaml:"anthropic_api_key_file"` // Path to file containing Anthropic API key
	OpenAIAPIKey        string  `yaml:"-"`                      // API key for OpenAI (loaded from file, not config)
	OpenAIAPIKeyFile    string  `yaml:"openai_api_key_file"`    // Path to file containing OpenAI API key
	OllamaURL           string  `yaml:"ollama_url"`             // URL for Ollama service (default: http://localhost:11434)
	MaxTokens           int     `yaml:"max_tokens"`             // Maximum tokens for LLM response (default: 4096)
	Temperature         float64 `yaml:"temperature"`            // Temperature for LLM sampling (default: 0.7)
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
	EmbeddingOpenAIAPIKey     string `yaml:"-"`                             // API key for OpenAI (loaded from file, not config)
	EmbeddingOpenAIAPIKeyFile string `yaml:"embedding_openai_api_key_file"` // Path to file containing OpenAI API key
	EmbeddingOllamaURL        string `yaml:"embedding_ollama_url"`          // URL for Ollama service (default: http://localhost:11434)
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

	// Auth flags
	AuthEnabled    bool
	AuthEnabledSet bool

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
				Enabled:                        true, // Authentication enabled by default
				MaxUserTokenDays:               0,    // Unlimited by default
				MaxFailedAttemptsBeforeLockout: 0,    // Disabled by default (0 = no lockout)
				RateLimitWindowMinutes:         15,   // 15 minute window for rate limiting
				RateLimitMaxAttempts:           10,   // 10 attempts per IP per window
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
			OllamaURL:       "http://localhost:11434", // Default Ollama URL
			MaxTokens:       4096,                     // Default max tokens
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

	// Auth - note: we need to preserve false values
	// Use a simple heuristic: if any auth config is set, assume auth config is intentional
	if !src.HTTP.Auth.Enabled {
		dest.HTTP.Auth.Enabled = src.HTTP.Auth.Enabled
	}
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
	if src.LLM.OpenAIAPIKey != "" {
		dest.LLM.OpenAIAPIKey = src.LLM.OpenAIAPIKey
	}
	if src.LLM.OpenAIAPIKeyFile != "" {
		dest.LLM.OpenAIAPIKeyFile = src.LLM.OpenAIAPIKeyFile
	}
	if src.LLM.OllamaURL != "" {
		dest.LLM.OllamaURL = src.LLM.OllamaURL
	}
	if src.LLM.MaxTokens != 0 {
		dest.LLM.MaxTokens = src.LLM.MaxTokens
	}
	if src.LLM.Temperature != 0 {
		dest.LLM.Temperature = src.LLM.Temperature
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
		if src.Knowledgebase.EmbeddingOllamaURL != "" {
			dest.Knowledgebase.EmbeddingOllamaURL = src.Knowledgebase.EmbeddingOllamaURL
		}
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
	// Resources
	if src.Builtins.Resources.SystemInfo != nil {
		dest.Builtins.Resources.SystemInfo = src.Builtins.Resources.SystemInfo
	}
	// Prompts
	if src.Builtins.Prompts.ExploreDatabase != nil {
		dest.Builtins.Prompts.ExploreDatabase = src.Builtins.Prompts.ExploreDatabase
	}
	if src.Builtins.Prompts.SetupSemanticSearch != nil {
		dest.Builtins.Prompts.SetupSemanticSearch = src.Builtins.Prompts.SetupSemanticSearch
	}
	if src.Builtins.Prompts.DiagnoseQueryIssue != nil {
		dest.Builtins.Prompts.DiagnoseQueryIssue = src.Builtins.Prompts.DiagnoseQueryIssue
	}
	if src.Builtins.Prompts.DesignSchema != nil {
		dest.Builtins.Prompts.DesignSchema = src.Builtins.Prompts.DesignSchema
	}
}

// loadAPIKeysFromFiles loads API keys from files if specified in config
func loadAPIKeysFromFiles(cfg *Config) {
	// Embedding API keys
	if cfg.Embedding.VoyageAPIKey == "" && cfg.Embedding.VoyageAPIKeyFile != "" {
		if key, err := readAPIKeyFromFile(cfg.Embedding.VoyageAPIKeyFile); err == nil && key != "" {
			cfg.Embedding.VoyageAPIKey = key
		}
	}
	if cfg.Embedding.OpenAIAPIKey == "" && cfg.Embedding.OpenAIAPIKeyFile != "" {
		if key, err := readAPIKeyFromFile(cfg.Embedding.OpenAIAPIKeyFile); err == nil && key != "" {
			cfg.Embedding.OpenAIAPIKey = key
		}
	}

	// LLM API keys
	if cfg.LLM.AnthropicAPIKey == "" && cfg.LLM.AnthropicAPIKeyFile != "" {
		if key, err := readAPIKeyFromFile(cfg.LLM.AnthropicAPIKeyFile); err == nil && key != "" {
			cfg.LLM.AnthropicAPIKey = key
		}
	}
	if cfg.LLM.OpenAIAPIKey == "" && cfg.LLM.OpenAIAPIKeyFile != "" {
		if key, err := readAPIKeyFromFile(cfg.LLM.OpenAIAPIKeyFile); err == nil && key != "" {
			cfg.LLM.OpenAIAPIKey = key
		}
	}

	// Knowledgebase API keys
	if cfg.Knowledgebase.EmbeddingVoyageAPIKey == "" && cfg.Knowledgebase.EmbeddingVoyageAPIKeyFile != "" {
		if key, err := readAPIKeyFromFile(cfg.Knowledgebase.EmbeddingVoyageAPIKeyFile); err == nil && key != "" {
			cfg.Knowledgebase.EmbeddingVoyageAPIKey = key
		}
	}
	if cfg.Knowledgebase.EmbeddingOpenAIAPIKey == "" && cfg.Knowledgebase.EmbeddingOpenAIAPIKeyFile != "" {
		if key, err := readAPIKeyFromFile(cfg.Knowledgebase.EmbeddingOpenAIAPIKeyFile); err == nil && key != "" {
			cfg.Knowledgebase.EmbeddingOpenAIAPIKey = key
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

	// Auth
	if flags.AuthEnabledSet {
		cfg.HTTP.Auth.Enabled = flags.AuthEnabled
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

	// Auth enabled - auth store will be created in data_dir
	// No additional validation needed here

	// Database configuration validation
	if cfg.Database != nil && cfg.Database.User == "" {
		return fmt.Errorf("database user is required (set via -db-user flag or config file)")
	}

	return nil
}

// readAPIKeyFromFile reads an API key from a file
// Returns the key with whitespace trimmed, or empty string if file doesn't exist or is empty
func readAPIKeyFromFile(filePath string) (string, error) {
	if filePath == "" {
		return "", nil
	}

	// Expand tilde to home directory
	if filePath != "" && filePath[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		filePath = filepath.Join(homeDir, filePath[1:])
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", nil // File doesn't exist, return empty (not an error)
	}

	// Read file contents
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read API key file %s: %w", filePath, err)
	}

	// Return trimmed contents (remove whitespace/newlines)
	key := strings.TrimSpace(string(data))
	return key, nil
}

// GetDefaultConfigPath returns the default config file path
// Searches /etc/pgedge/ first, then binary directory
func GetDefaultConfigPath(binaryPath string) string {
	systemPath := "/etc/pgedge/ai-dba-server.yaml"
	if _, err := os.Stat(systemPath); err == nil {
		return systemPath
	}

	dir := filepath.Dir(binaryPath)
	return filepath.Join(dir, "ai-dba-server.yaml")
}

// GetDefaultSecretPath returns the default secret file path
// Searches /etc/pgedge/ first, then binary directory
func GetDefaultSecretPath(binaryPath string) string {
	systemPath := "/etc/pgedge/ai-dba-server.secret"
	if _, err := os.Stat(systemPath); err == nil {
		return systemPath
	}

	dir := filepath.Dir(binaryPath)
	return filepath.Join(dir, "ai-dba-server.secret")
}

// ConfigFileExists checks if a config file exists at the given path
func ConfigFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

	// Write with appropriate permissions
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
