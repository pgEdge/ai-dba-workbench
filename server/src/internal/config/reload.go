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
	if old.HTTP.Auth.OIDC.IsEnabled() != newConfig.HTTP.Auth.OIDC.IsEnabled() {
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

	// The remaining OIDC settings also require a restart, for a less
	// obvious reason than issuer/client id/secret/redirect. They decide
	// how an already-constructed provider's response is interpreted, so
	// they could in principle be applied live, but they are not:
	// NewOIDCHandler takes the OIDC configuration by value and keeps its
	// own copy, and Reload only swaps the pointer inside this wrapper, so
	// the running handler goes on reading the settings it started with.
	// The capabilities payload is frozen in the same way, being built
	// once at startup. Reporting these as merely "changed" told an
	// operator narrowing allowed_email_domains or dropping a compromised
	// group from group_map that their containment had landed, when
	// nothing had changed at all, so they are reported as what they are.
	if old.HTTP.Auth.OIDC.UsernameClaim != newConfig.HTTP.Auth.OIDC.UsernameClaim {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.username_claim changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.DisplayNameClaim != newConfig.HTTP.Auth.OIDC.DisplayNameClaim {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.display_name_claim changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.GroupsClaim != newConfig.HTTP.Auth.OIDC.GroupsClaim {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.groups_claim changed - requires restart\n")
	}
	if !reflect.DeepEqual(old.HTTP.Auth.OIDC.Scopes, newConfig.HTTP.Auth.OIDC.Scopes) {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.scopes changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.ProvisionUsersEnabled() != newConfig.HTTP.Auth.OIDC.ProvisionUsersEnabled() {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.provision_users changed - requires restart\n")
	}
	if !reflect.DeepEqual(old.HTTP.Auth.OIDC.AllowedEmailDomains, newConfig.HTTP.Auth.OIDC.AllowedEmailDomains) {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.allowed_email_domains changed - requires restart\n")
	}
	if old.HTTP.Auth.OIDC.SuperuserGroup != newConfig.HTTP.Auth.OIDC.SuperuserGroup {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.superuser_group changed - requires restart\n")
	}
	if !reflect.DeepEqual(old.HTTP.Auth.OIDC.GroupMap, newConfig.HTTP.Auth.OIDC.GroupMap) {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.group_map changed - requires restart\n")
	}

	// Local password login is captured by the login handler at startup in
	// exactly the same way, so switching it off by reload alone leaves
	// every password live.
	if old.HTTP.Auth.LocalEnabled() != newConfig.HTTP.Auth.LocalEnabled() {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.local.enabled changed - requires restart\n")
	}
}

// OnReload registers a callback to be called when configuration is reloaded
// The callback receives the new configuration
func (rc *ReloadableConfig) OnReload(fn func(*Config)) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.onReload = append(rc.onReload, fn)
}
