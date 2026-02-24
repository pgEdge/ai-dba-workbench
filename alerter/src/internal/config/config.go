/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package config handles configuration for the alerter service.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration options for the alerter
type Config struct {
	// Datastore connection settings
	Datastore DatastoreConfig `yaml:"datastore"`

	// Connection pool settings
	Pool PoolConfig `yaml:"pool"`

	// Threshold engine settings
	Threshold ThresholdConfig `yaml:"threshold"`

	// Anomaly detection settings
	Anomaly AnomalyConfig `yaml:"anomaly"`

	// Baseline calculation settings
	Baselines BaselineConfig `yaml:"baselines"`

	// Correlation settings
	Correlation CorrelationConfig `yaml:"correlation"`

	// LLM provider settings
	LLM LLMConfig `yaml:"llm"`

	// Notifications settings
	Notifications NotificationsConfig `yaml:"notifications"`
}

// DatastoreConfig holds PostgreSQL connection settings for the datastore
type DatastoreConfig struct {
	Host         string `yaml:"host"`
	HostAddr     string `yaml:"hostaddr"`
	Database     string `yaml:"database"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	PasswordFile string `yaml:"password_file"`
	Port         int    `yaml:"port"`
	SSLMode      string `yaml:"sslmode"`
	SSLCert      string `yaml:"sslcert"`
	SSLKey       string `yaml:"sslkey"`
	SSLRootCert  string `yaml:"sslrootcert"`
}

// PoolConfig holds connection pool settings
type PoolConfig struct {
	MaxConnections int `yaml:"max_connections"`
	MaxIdleSeconds int `yaml:"max_idle_seconds"`
}

// ThresholdConfig holds threshold engine settings
type ThresholdConfig struct {
	EvaluationIntervalSeconds int `yaml:"evaluation_interval_seconds"`
}

// AnomalyConfig holds anomaly detection settings
type AnomalyConfig struct {
	Enabled      bool               `yaml:"enabled"`
	Tier1        Tier1Config        `yaml:"tier1"`
	Tier2        Tier2Config        `yaml:"tier2"`
	Tier3        Tier3Config        `yaml:"tier3"`
	Reevaluation ReevaluationConfig `yaml:"reevaluation"`
}

// Tier1Config holds Tier 1 statistical detection settings
type Tier1Config struct {
	Enabled                   bool    `yaml:"enabled"`
	DefaultSensitivity        float64 `yaml:"default_sensitivity"`
	EvaluationIntervalSeconds int     `yaml:"evaluation_interval_seconds"`
}

// Tier2Config holds Tier 2 embedding similarity settings
type Tier2Config struct {
	Enabled              bool    `yaml:"enabled"`
	SuppressionThreshold float64 `yaml:"suppression_threshold"`
	SimilarityThreshold  float64 `yaml:"similarity_threshold"`
}

// Tier3Config holds Tier 3 LLM classification settings
type Tier3Config struct {
	Enabled        bool `yaml:"enabled"`
	TimeoutSeconds int  `yaml:"timeout_seconds"`
}

// ReevaluationConfig holds re-evaluation settings for acknowledged anomaly alerts
type ReevaluationConfig struct {
	Enabled         bool `yaml:"enabled"`
	IntervalSeconds int  `yaml:"interval_seconds"`
	TimeoutSeconds  int  `yaml:"timeout_seconds"`
	MaxPerCycle     int  `yaml:"max_per_cycle"`
}

// BaselineConfig holds baseline calculation settings
type BaselineConfig struct {
	RefreshIntervalSeconds int `yaml:"refresh_interval_seconds"`
	LookbackDays           int `yaml:"lookback_days"`
}

// CorrelationConfig holds correlation detection settings
type CorrelationConfig struct {
	WindowSeconds int `yaml:"window_seconds"`
}

// LLMConfig holds LLM provider settings
type LLMConfig struct {
	EmbeddingProvider string          `yaml:"embedding_provider"`
	ReasoningProvider string          `yaml:"reasoning_provider"`
	Ollama            OllamaConfig    `yaml:"ollama"`
	OpenAI            OpenAIConfig    `yaml:"openai"`
	Anthropic         AnthropicConfig `yaml:"anthropic"`
	Voyage            VoyageConfig    `yaml:"voyage"`
	Gemini            GeminiConfig    `yaml:"gemini"`
}

// NotificationsConfig holds notification settings
type NotificationsConfig struct {
	// Enabled enables/disables notifications
	Enabled bool `yaml:"enabled"`

	// SecretFile is the path to the file containing the encryption key
	// The file should contain a hex-encoded 32-byte key (64 hex characters)
	SecretFile string `yaml:"secret_file"`

	// ProcessIntervalSeconds is how often to process pending notifications
	// Default: 30
	ProcessIntervalSeconds int `yaml:"process_interval_seconds"`

	// ReminderCheckIntervalMinutes is how often to check for due reminders
	// Default: 60
	ReminderCheckIntervalMinutes int `yaml:"reminder_check_interval_minutes"`

	// MaxRetryAttempts is the maximum number of retry attempts for failed notifications
	// Default: 3
	MaxRetryAttempts int `yaml:"max_retry_attempts"`

	// RetryBackoffMinutes is the backoff schedule for retries (array of minutes)
	// Default: [5, 15, 60]
	RetryBackoffMinutes []int `yaml:"retry_backoff_minutes"`

	// HTTPTimeoutSeconds is the timeout for HTTP requests (webhooks, Slack, Mattermost)
	// Default: 30
	HTTPTimeoutSeconds int `yaml:"http_timeout_seconds"`

	// HTTPMaxIdleConns is the maximum number of idle HTTP connections
	// Default: 10
	HTTPMaxIdleConns int `yaml:"http_max_idle_conns"`
}

// OllamaConfig holds Ollama provider settings
type OllamaConfig struct {
	BaseURL        string `yaml:"base_url"`
	EmbeddingModel string `yaml:"embedding_model"`
	ReasoningModel string `yaml:"reasoning_model"`
}

// OpenAIConfig holds OpenAI provider settings
type OpenAIConfig struct {
	APIKeyFile     string `yaml:"api_key_file"`
	BaseURL        string `yaml:"base_url"`
	EmbeddingModel string `yaml:"embedding_model"`
	ReasoningModel string `yaml:"reasoning_model"`
	apiKey         string
}

// AnthropicConfig holds Anthropic provider settings
type AnthropicConfig struct {
	APIKeyFile     string `yaml:"api_key_file"`
	BaseURL        string `yaml:"base_url"`
	ReasoningModel string `yaml:"reasoning_model"`
	apiKey         string
}

// VoyageConfig holds Voyage provider settings
type VoyageConfig struct {
	APIKeyFile     string `yaml:"api_key_file"`
	BaseURL        string `yaml:"base_url"`
	EmbeddingModel string `yaml:"embedding_model"`
	apiKey         string
}

// GeminiConfig holds Google Gemini provider settings
type GeminiConfig struct {
	APIKeyFile     string `yaml:"api_key_file"`
	BaseURL        string `yaml:"base_url"`
	ReasoningModel string `yaml:"reasoning_model"`
	apiKey         string
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		Datastore: DatastoreConfig{
			Host:     "localhost",
			Database: "ai_workbench",
			Username: "postgres",
			Port:     5432,
			SSLMode:  "prefer",
		},
		Pool: PoolConfig{
			MaxConnections: 10,
			MaxIdleSeconds: 300,
		},
		Threshold: ThresholdConfig{
			EvaluationIntervalSeconds: 60,
		},
		Anomaly: AnomalyConfig{
			Enabled: true,
			Tier1: Tier1Config{
				Enabled:                   true,
				DefaultSensitivity:        3.0,
				EvaluationIntervalSeconds: 60,
			},
			Tier2: Tier2Config{
				Enabled:              true,
				SuppressionThreshold: 0.85,
				SimilarityThreshold:  0.3,
			},
			Tier3: Tier3Config{
				Enabled:        true,
				TimeoutSeconds: 30,
			},
			Reevaluation: ReevaluationConfig{
				Enabled:         true,
				IntervalSeconds: 300,
				TimeoutSeconds:  30,
				MaxPerCycle:     10,
			},
		},
		Baselines: BaselineConfig{
			RefreshIntervalSeconds: 3600,
			LookbackDays:           7,
		},
		Correlation: CorrelationConfig{
			WindowSeconds: 120,
		},
		LLM: LLMConfig{
			EmbeddingProvider: "ollama",
			ReasoningProvider: "ollama",
			Ollama: OllamaConfig{
				BaseURL:        "http://localhost:11434",
				EmbeddingModel: "nomic-embed-text",
				ReasoningModel: "qwen2.5:7b-instruct",
			},
			OpenAI: OpenAIConfig{
				EmbeddingModel: "text-embedding-3-small",
				ReasoningModel: "gpt-4o-mini",
			},
			Anthropic: AnthropicConfig{
				ReasoningModel: "claude-3-5-haiku-20241022",
			},
			Voyage: VoyageConfig{
				EmbeddingModel: "voyage-3-lite",
			},
			Gemini: GeminiConfig{
				ReasoningModel: "gemini-2.0-flash",
			},
		},
	}
}

// LoadFromFile loads configuration from a YAML file
func (c *Config) LoadFromFile(filename string) error {
	data, err := os.ReadFile(filename) // #nosec G304 - Config file path is provided by administrator
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Apply defaults for notification settings
	c.SetNotificationDefaults()

	return nil
}

// LoadFromEnv applies environment variable overrides
func (c *Config) LoadFromEnv() {
	if v := os.Getenv("AI_DBA_PG_HOST"); v != "" {
		c.Datastore.Host = v
	}
	if v := os.Getenv("AI_DBA_PG_HOSTADDR"); v != "" {
		c.Datastore.HostAddr = v
	}
	if v := os.Getenv("AI_DBA_PG_DATABASE"); v != "" {
		c.Datastore.Database = v
	}
	if v := os.Getenv("AI_DBA_PG_USERNAME"); v != "" {
		c.Datastore.Username = v
	}
	if v := os.Getenv("AI_DBA_PG_PASSWORD"); v != "" {
		c.Datastore.Password = v
	}
	if v := os.Getenv("AI_DBA_PG_SSLMODE"); v != "" {
		c.Datastore.SSLMode = v
	}
	if v := os.Getenv("AI_DBA_PG_SSLCERT"); v != "" {
		c.Datastore.SSLCert = v
	}
	if v := os.Getenv("AI_DBA_PG_SSLKEY"); v != "" {
		c.Datastore.SSLKey = v
	}
	if v := os.Getenv("AI_DBA_PG_SSLROOTCERT"); v != "" {
		c.Datastore.SSLRootCert = v
	}
}

// LoadPassword loads the password from password file if specified
func (c *Config) LoadPassword() error {
	if c.Datastore.Password != "" {
		return nil
	}

	if c.Datastore.PasswordFile != "" {
		password, err := readSecretFile(c.Datastore.PasswordFile)
		if err != nil {
			return fmt.Errorf("failed to read password file: %w", err)
		}
		c.Datastore.Password = password
	}

	return nil
}

// LoadAPIKeys loads API keys from their respective files
func (c *Config) LoadAPIKeys() error {
	if c.LLM.OpenAI.APIKeyFile != "" {
		key, err := readSecretFile(c.LLM.OpenAI.APIKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read OpenAI API key: %w", err)
		}
		c.LLM.OpenAI.apiKey = key
	}

	if c.LLM.Anthropic.APIKeyFile != "" {
		key, err := readSecretFile(c.LLM.Anthropic.APIKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read Anthropic API key: %w", err)
		}
		c.LLM.Anthropic.apiKey = key
	}

	if c.LLM.Voyage.APIKeyFile != "" {
		key, err := readSecretFile(c.LLM.Voyage.APIKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read Voyage API key: %w", err)
		}
		c.LLM.Voyage.apiKey = key
	}

	if c.LLM.Gemini.APIKeyFile != "" {
		key, err := readSecretFile(c.LLM.Gemini.APIKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read Gemini API key: %w", err)
		}
		c.LLM.Gemini.apiKey = key
	}

	return nil
}

// GetOpenAIAPIKey returns the loaded OpenAI API key
func (c *Config) GetOpenAIAPIKey() string {
	return c.LLM.OpenAI.apiKey
}

// GetAnthropicAPIKey returns the loaded Anthropic API key
func (c *Config) GetAnthropicAPIKey() string {
	return c.LLM.Anthropic.apiKey
}

// GetVoyageAPIKey returns the loaded Voyage API key
func (c *Config) GetVoyageAPIKey() string {
	return c.LLM.Voyage.apiKey
}

// GetGeminiAPIKey returns the loaded Gemini API key
func (c *Config) GetGeminiAPIKey() string {
	return c.LLM.Gemini.apiKey
}

// readSecretFile reads a secret from a file
func readSecretFile(filename string) (string, error) {
	return fileutil.ReadTrimmedFileWithTilde(filename)
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Datastore.Host == "" {
		return fmt.Errorf("datastore.host is required")
	}
	if c.Datastore.Database == "" {
		return fmt.Errorf("datastore.database is required")
	}
	if c.Datastore.Username == "" {
		return fmt.Errorf("datastore.username is required")
	}
	if c.Datastore.Port <= 0 || c.Datastore.Port > 65535 {
		return fmt.Errorf("datastore.port must be between 1 and 65535")
	}
	if c.Pool.MaxConnections <= 0 {
		return fmt.Errorf("pool.max_connections must be greater than 0")
	}
	return nil
}

// GetDefaultConfigPath returns the default config file path
func GetDefaultConfigPath(binaryPath string) string {
	systemPath := "/etc/pgedge/ai-dba-alerter.yaml"
	if _, err := os.Stat(systemPath); err == nil {
		return systemPath
	}

	dir := filepath.Dir(binaryPath)
	return filepath.Join(dir, "ai-dba-alerter.yaml")
}

// ConfigFileExists checks if a config file exists at the given path
func ConfigFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// SetNotificationDefaults sets default values for notification config
func (c *Config) SetNotificationDefaults() {
	if c.Notifications.ProcessIntervalSeconds == 0 {
		c.Notifications.ProcessIntervalSeconds = 30
	}
	if c.Notifications.ReminderCheckIntervalMinutes == 0 {
		c.Notifications.ReminderCheckIntervalMinutes = 60
	}
	if c.Notifications.MaxRetryAttempts == 0 {
		c.Notifications.MaxRetryAttempts = 3
	}
	if len(c.Notifications.RetryBackoffMinutes) == 0 {
		c.Notifications.RetryBackoffMinutes = []int{5, 15, 60}
	}
	if c.Notifications.HTTPTimeoutSeconds == 0 {
		c.Notifications.HTTPTimeoutSeconds = 30
	}
	if c.Notifications.HTTPMaxIdleConns == 0 {
		c.Notifications.HTTPMaxIdleConns = 10
	}
}
