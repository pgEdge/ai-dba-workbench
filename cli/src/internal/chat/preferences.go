/*-------------------------------------------------------------------------
*
 * pgEdge AI DBA Workbench
*
* Portions copyright (c) 2025 - 2026, pgEdge, Inc.
* This software is released under The PostgreSQL License
*
*-------------------------------------------------------------------------
*/

package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// CurrentPreferencesVersion is the current preferences file format version
// Version history:
// - 1: Initial version (no version field in file)
// - 2: Added version field, fixed Color default (was missing in v1 files)
const CurrentPreferencesVersion = 2

// Preferences holds user preferences that persist across sessions
type Preferences struct {
	Version         int               `yaml:"version,omitempty"` // File format version for migrations
	UI              UIPreferences     `yaml:"ui"`
	ProviderModels  map[string]string `yaml:"provider_models"`
	LastProvider    string            `yaml:"last_provider"`
	ServerDatabases map[string]string `yaml:"server_databases,omitempty"` // server key -> database name
}

// UIPreferences holds UI-related preferences
type UIPreferences struct {
	DisplayStatusMessages bool `yaml:"display_status_messages"`
	RenderMarkdown        bool `yaml:"render_markdown"`
	Debug                 bool `yaml:"debug"`
	Color                 bool `yaml:"color"`
}

// GetPreferencesPath returns the path to the user preferences file
func GetPreferencesPath() string {
	return filepath.Join(os.Getenv("HOME"), ".ai-dba-workbench-cli-prefs")
}

// LoadPreferences loads user preferences from the preferences file
// Returns default preferences if file doesn't exist
func LoadPreferences() (*Preferences, error) {
	path := GetPreferencesPath()

	// If file doesn't exist, return defaults
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return getDefaultPreferences(), nil
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read preferences file: %w", err)
	}

	// Parse YAML
	prefs := &Preferences{}
	if err := yaml.Unmarshal(data, prefs); err != nil {
		return nil, fmt.Errorf("failed to parse preferences file: %w", err)
	}

	// Check if migration is needed before sanitization
	needsSave := prefs.Version < CurrentPreferencesVersion

	// Sanitize and validate loaded preferences
	prefs = sanitizePreferences(prefs)

	// If migration occurred, persist the updated version to disk
	// This prevents re-migration on every startup
	if needsSave {
		if err := SavePreferences(prefs); err != nil {
			// Log warning but don't fail - in-memory prefs are still valid
			fmt.Fprintf(os.Stderr, "Warning: failed to save migrated preferences: %v\n", err)
		}
	}

	return prefs, nil
}

// SavePreferences saves user preferences to the preferences file
func SavePreferences(prefs *Preferences) error {
	path := GetPreferencesPath()

	// Marshal to YAML
	data, err := yaml.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("failed to marshal preferences: %w", err)
	}

	// Write to temporary file first for atomic write
	// Use unique filename to prevent race conditions with concurrent CLI instances
	tempPath := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write preferences file: %w", err)
	}

	// Rename to final location (atomic on Unix)
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("failed to save preferences file: %w", err)
	}

	return nil
}

// getDefaultPreferences returns default preferences
func getDefaultPreferences() *Preferences {
	return &Preferences{
		Version: CurrentPreferencesVersion,
		UI: UIPreferences{
			DisplayStatusMessages: true,
			RenderMarkdown:        true,
			Debug:                 false,
			Color:                 true,
		},
		ProviderModels: map[string]string{
			"anthropic": "claude-sonnet-4-5-20250929",
			"openai":    "gpt-4o",
			"ollama":    "qwen3-coder:latest",
		},
		LastProvider: "anthropic",
	}
}

// sanitizePreferences validates and fixes corrupted preference data
// Only validates structure, not model validity (done at runtime in initializeLLM)
func sanitizePreferences(prefs *Preferences) *Preferences {
	defaults := getDefaultPreferences()

	// Migrate older preference file versions
	if prefs.Version < CurrentPreferencesVersion {
		prefs = migratePreferences(prefs, defaults)
	}

	// Ensure provider_models map exists
	if prefs.ProviderModels == nil {
		prefs.ProviderModels = make(map[string]string)
	}

	// Validate LastProvider is a known provider name
	validProviders := map[string]bool{
		"anthropic": true,
		"openai":    true,
		"ollama":    true,
	}
	if !validProviders[prefs.LastProvider] {
		// Invalid provider - use default
		prefs.LastProvider = defaults.LastProvider
	}

	// Don't validate models here - that requires API access
	// Model validation happens at runtime in initializeLLM()

	return prefs
}

// migratePreferences handles migration from older preference file versions
func migratePreferences(prefs *Preferences, defaults *Preferences) *Preferences {
	// v1 -> v2: Fix Color field default (was missing in v1 files, defaulting to false)
	// In v1 files, Color was not present, so it defaults to false (Go zero value)
	// We want it to default to true for new users who haven't explicitly disabled it
	if prefs.Version < 2 {
		// Only set Color to default if the entire UI struct appears to be from v1
		// (i.e., Color is false which could be the zero value from missing field)
		// We can't distinguish "explicitly set to false" from "missing", so we
		// apply the default. Users who explicitly disabled color will need to
		// disable it again after this migration.
		prefs.UI.Color = defaults.UI.Color
	}

	prefs.Version = CurrentPreferencesVersion
	return prefs
}

// GetModelForProvider returns the preferred model for a provider
func (p *Preferences) GetModelForProvider(provider string) string {
	if model, exists := p.ProviderModels[provider]; exists {
		return model
	}

	// Fall back to defaults
	defaults := getDefaultPreferences()
	if model, exists := defaults.ProviderModels[provider]; exists {
		return model
	}

	return ""
}

// SetModelForProvider sets the preferred model for a provider
func (p *Preferences) SetModelForProvider(provider, model string) {
	if p.ProviderModels == nil {
		p.ProviderModels = make(map[string]string)
	}
	p.ProviderModels[provider] = model
}

// GetDatabaseForServer returns the preferred database for a server
func (p *Preferences) GetDatabaseForServer(serverKey string) string {
	if p.ServerDatabases == nil {
		return ""
	}
	return p.ServerDatabases[serverKey]
}

// SetDatabaseForServer sets the preferred database for a server
func (p *Preferences) SetDatabaseForServer(serverKey, database string) {
	if p.ServerDatabases == nil {
		p.ServerDatabases = make(map[string]string)
	}
	if database == "" {
		delete(p.ServerDatabases, serverKey)
	} else {
		p.ServerDatabases[serverKey] = database
	}
}
