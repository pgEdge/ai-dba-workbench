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
	"os"
	"reflect"
	"sync"
)

// ReloadableConfig wraps a Config with thread-safe access and reload capability
type ReloadableConfig struct {
	mu       sync.RWMutex
	config   *Config
	path     string
	cliFlags CLIFlags
	onReload []func(*Config)
}

// NewReloadableConfig creates a new reloadable configuration
func NewReloadableConfig(config *Config, path string, cliFlags CLIFlags) *ReloadableConfig {
	return &ReloadableConfig{
		config:   config,
		path:     path,
		cliFlags: cliFlags,
		onReload: make([]func(*Config), 0),
	}
}

// Get returns the current configuration (read-only access)
func (rc *ReloadableConfig) Get() *Config {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.config
}

// Reload reloads the configuration from the file
// Returns an error if the reload fails, but keeps the old config
func (rc *ReloadableConfig) Reload() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.path == "" {
		return fmt.Errorf("no configuration file path set")
	}

	// Load the new configuration (LoadConfig applies CLI flags internally)
	newConfig, err := LoadConfig(rc.path, rc.cliFlags)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate the new configuration
	if err := validateConfig(newConfig); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Resolve the datastore password from a password_file before the new
	// config is swapped in or handed to any onReload callback. LoadConfig
	// applies CLI-flag and inline-YAML passwords, but a YAML password_file
	// is only materialized here; without this step a reload would leave
	// DatabaseConfig.Password empty and silently fall back to .pgpass.
	// Doing it at this single chokepoint guarantees every reload consumer
	// (including the SIGHUP client-manager update) sees the resolved
	// password. On failure we abort the reload and keep the old config.
	if newConfig.Database != nil {
		if err := newConfig.Database.LoadPassword(); err != nil {
			return fmt.Errorf("failed to resolve database password: %w", err)
		}
	}

	// Log what settings require restart (won't be applied)
	rc.logRestartRequiredSettings(newConfig)

	// Update the config
	oldConfig := rc.config
	rc.config = newConfig

	// Notify all registered callbacks
	for _, callback := range rc.onReload {
		callback(newConfig)
	}

	// Log successful reload
	fmt.Fprintf(os.Stderr, "Configuration reloaded successfully from %s\n", rc.path)
	if newConfig.Database != nil {
		fmt.Fprintf(os.Stderr, "  Database: %s@%s:%d/%s\n",
			newConfig.Database.User, newConfig.Database.Host,
			newConfig.Database.Port, newConfig.Database.Database)
	} else {
		fmt.Fprintf(os.Stderr, "  Database: not configured\n")
	}

	// Log if database connection changed
	oldHasDB := oldConfig.Database != nil
	newHasDB := newConfig.Database != nil
	if oldHasDB != newHasDB {
		fmt.Fprintf(os.Stderr, "  Database configuration changed\n")
	}

	return nil
}

// logRestartRequiredSettings logs settings that changed but require a restart
func (rc *ReloadableConfig) logRestartRequiredSettings(newConfig *Config) {
	old := rc.config

	// HTTP changes require restart
	if old.HTTP.Address != newConfig.HTTP.Address {
		fmt.Fprintf(os.Stderr, "  WARNING: http.address changed - requires restart\n")
	}

	// TLS changes require restart
	if old.HTTP.TLS.Enabled != newConfig.HTTP.TLS.Enabled {
		fmt.Fprintf(os.Stderr, "  WARNING: http.tls.enabled changed - requires restart\n")
	}
	if old.HTTP.TLS.CertFile != newConfig.HTTP.TLS.CertFile {
		fmt.Fprintf(os.Stderr, "  WARNING: http.tls.cert_file changed - requires restart\n")
	}
	if old.HTTP.TLS.KeyFile != newConfig.HTTP.TLS.KeyFile {
		fmt.Fprintf(os.Stderr, "  WARNING: http.tls.key_file changed - requires restart\n")
	}

	// OIDC changes require restart: the provider is constructed once at
	// startup from these settings, so a running server and a reloaded
	// configuration would otherwise silently disagree.
	if old.HTTP.Auth.OIDC.Enabled != newConfig.HTTP.Auth.OIDC.Enabled {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.enabled changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.Issuer != newConfig.HTTP.Auth.OIDC.Issuer {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.issuer changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.ClientID != newConfig.HTTP.Auth.OIDC.ClientID {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.client_id changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.EffectiveClientSecret() != newConfig.HTTP.Auth.OIDC.EffectiveClientSecret() {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.client_secret changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.RedirectURL != newConfig.HTTP.Auth.OIDC.RedirectURL {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.redirect_url changed - requires restart\n")
	}

	// LLM/embedding provider changes are logged (may work but connections need reset)
	if old.LLM.Provider != newConfig.LLM.Provider {
		fmt.Fprintf(os.Stderr, "  NOTE: llm.provider changed to %s\n", newConfig.LLM.Provider)
	}
	if old.LLM.Model != newConfig.LLM.Model {
		fmt.Fprintf(os.Stderr, "  NOTE: llm.model changed to %s\n", newConfig.LLM.Model)
	}
	if old.Embedding.Provider != newConfig.Embedding.Provider {
		fmt.Fprintf(os.Stderr, "  NOTE: embedding.provider changed to %s\n", newConfig.Embedding.Provider)
	}

	// OIDC claim/authorization-mapping changes are logged but do not claim
	// a restart is required: unlike issuer/client id/secret/redirect,
	// which construct the OIDC provider once at startup, these settings
	// decide how an already-constructed provider's response is
	// interpreted (identity, display name, group membership, superuser
	// grant). Without a NOTE here a change to any of them would take
	// effect - or fail to - with no signal to the operator either way.
	if old.HTTP.Auth.OIDC.UsernameClaim != newConfig.HTTP.Auth.OIDC.UsernameClaim {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.username_claim changed to %s\n", newConfig.HTTP.Auth.OIDC.UsernameClaim)
	}
	if old.HTTP.Auth.OIDC.DisplayNameClaim != newConfig.HTTP.Auth.OIDC.DisplayNameClaim {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.display_name_claim changed to %s\n", newConfig.HTTP.Auth.OIDC.DisplayNameClaim)
	}
	if old.HTTP.Auth.OIDC.GroupsClaim != newConfig.HTTP.Auth.OIDC.GroupsClaim {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.groups_claim changed to %s\n", newConfig.HTTP.Auth.OIDC.GroupsClaim)
	}
	if !reflect.DeepEqual(old.HTTP.Auth.OIDC.Scopes, newConfig.HTTP.Auth.OIDC.Scopes) {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.scopes changed to %v\n", newConfig.HTTP.Auth.OIDC.Scopes)
	}
	if old.HTTP.Auth.OIDC.ProvisionUsersEnabled() != newConfig.HTTP.Auth.OIDC.ProvisionUsersEnabled() {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.provision_users changed to %v\n",
			newConfig.HTTP.Auth.OIDC.ProvisionUsersEnabled())
	}
	if !reflect.DeepEqual(old.HTTP.Auth.OIDC.AllowedEmailDomains, newConfig.HTTP.Auth.OIDC.AllowedEmailDomains) {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.allowed_email_domains changed to %v\n", newConfig.HTTP.Auth.OIDC.AllowedEmailDomains)
	}
	if old.HTTP.Auth.OIDC.SuperuserGroup != newConfig.HTTP.Auth.OIDC.SuperuserGroup {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.superuser_group changed to %s\n", newConfig.HTTP.Auth.OIDC.SuperuserGroup)
	}
	if !reflect.DeepEqual(old.HTTP.Auth.OIDC.GroupMap, newConfig.HTTP.Auth.OIDC.GroupMap) {
		fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.group_map changed to %v\n", newConfig.HTTP.Auth.OIDC.GroupMap)
	}
}

// OnReload registers a callback to be called when configuration is reloaded
// The callback receives the new configuration
func (rc *ReloadableConfig) OnReload(fn func(*Config)) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.onReload = append(rc.onReload, fn)
}
