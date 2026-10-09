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
	"sort"
	"strings"
	"sync"
)

// ReloadableConfig wraps a Config with thread-safe access and reload capability
type ReloadableConfig struct {
	mu     sync.RWMutex
	config *Config

	// startup is the configuration the server started with, kept for
	// the settings whose reload behavior depends on it rather than on
	// the previous reload: switching OIDC on works live only if the
	// server discovered a provider at start-up.
	startup *Config

	path     string
	cliFlags CLIFlags
	onReload []func(*Config)
}

// NewReloadableConfig creates a new reloadable configuration
func NewReloadableConfig(config *Config, path string, cliFlags CLIFlags) *ReloadableConfig {
	return &ReloadableConfig{
		config:   config,
		startup:  config,
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

	if err := rc.checkALoginMethodSurvives(newConfig); err != nil {
		return err
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

	// Some OIDC settings need a restart and some apply live; see
	// logOIDCChanges.
	rc.logOIDCChanges(newConfig)

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

	// Local password login is captured by the login handler at startup,
	// and Reload swaps only the pointer inside this wrapper, so switching
	// it off by reload alone leaves every password live.
	if old.HTTP.Auth.LocalEnabled() != newConfig.HTTP.Auth.LocalEnabled() {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.local.enabled changed - requires restart\n")
	}

	// The account lockout threshold is read once, when NewAuthStore is
	// constructed at startup, and the store keeps its own copy: a reload
	// swaps the pointer inside this wrapper and never reaches it. An
	// operator switching lockout on, or tightening the threshold after
	// an attack, would otherwise be told the reload succeeded whilst
	// every login went on being counted against the value the server
	// started with, which for an installation predating the fix to the
	// omitted-key default meant no lockout at all.
	if old.HTTP.Auth.MaxFailedAttemptsBeforeLockout() != newConfig.HTTP.Auth.MaxFailedAttemptsBeforeLockout() {
		fmt.Fprintf(os.Stderr,
			"  WARNING: http.auth.max_failed_attempts_before_lockout changed - requires restart\n")
	}
}

// checkALoginMethodSurvives refuses a reload that would leave the
// server with no way to sign in. validateConfig already rejects a file
// with both local and federated login off, but local login is captured
// at start-up and a reload cannot switch it back on, whilst switching
// federated login off applies at once. A server that started with local
// login off would therefore lock every user out if a reload switched
// federated login off, even when the new file switches local login on.
func (rc *ReloadableConfig) checkALoginMethodSurvives(newConfig *Config) error {
	if rc.startup == nil || rc.startup.HTTP.Auth.LocalEnabled() {
		return nil
	}
	if newConfig.HTTP.Auth.OIDC.IsEnabled() {
		return nil
	}
	return fmt.Errorf("invalid configuration: http.auth.oidc.enabled cannot be switched off " +
		"by reload because the server started with http.auth.local.enabled off, " +
		"which only a restart can change; restart the server instead")
}

// logOIDCChanges reports the changes to http.auth.oidc that a reload
// detects. The provider is built once at start-up from the issuer,
// client credentials, redirect URL, scopes and claim names, so each of
// those gets a warning that only a restart applies it; they are compared
// against the start-up configuration, which the provider was built from,
// so the warning persists across reloads until a restart. The policy
// settings (provision_users, allowed_email_domains, superuser_group,
// group_map, button_label and switching federated login off) are read
// on every request by the federated login handler and the capabilities
// endpoint, through the accessor the server wires to Get, so a reload
// applies them at once and each gets a note saying so.
func (rc *ReloadableConfig) logOIDCChanges(newConfig *Config) {
	old := rc.config.HTTP.Auth.OIDC
	cur := newConfig.HTTP.Auth.OIDC

	// Switching federated login off always applies, since the handler
	// checks enabled on every request. Switching it on applies only when
	// the server started with it on, because otherwise no provider was
	// discovered and no handler was built.
	startedWithOIDC := rc.startup != nil && rc.startup.HTTP.Auth.OIDC.IsEnabled()
	if old.IsEnabled() != cur.IsEnabled() {
		if cur.IsEnabled() && !startedWithOIDC {
			restartRequiredOIDCSetting("enabled")
		} else {
			appliedOIDCSetting("enabled")
		}
	}

	built := old
	if rc.startup != nil {
		built = rc.startup.HTTP.Auth.OIDC
	}
	for _, name := range changedOIDCSettings(oidcProviderSettings(built, cur)) {
		restartRequiredOIDCSetting(name)
	}
	for _, name := range changedOIDCSettings(oidcPolicySettings(old, cur)) {
		appliedOIDCSetting(name)
	}

	// A change that widens who may sign in, or what they get, is applied
	// like any other, but is logged as a warning as well so that it stands
	// out in the journal of a reload that also carried routine changes.
	for _, detail := range oidcAccessWarnings(old, cur) {
		fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.%s\n", detail)
	}
}

// oidcSettingChange records whether one http.auth.oidc setting differs
// between two configurations.
type oidcSettingChange struct {
	name    string
	changed bool
}

// oidcProviderSettings compares the settings the identity provider is
// built from at start-up, which only a restart can apply.
func oidcProviderSettings(old, cur OIDCConfig) []oidcSettingChange {
	return []oidcSettingChange{
		{"issuer", old.Issuer != cur.Issuer},
		{"client_id", old.ClientID != cur.ClientID},
		{"client_secret", old.EffectiveClientSecret() != cur.EffectiveClientSecret()},
		{"redirect_url", old.RedirectURL != cur.RedirectURL},
		{"scopes", !reflect.DeepEqual(old.Scopes, cur.Scopes)},
		{"username_claim", old.UsernameClaim != cur.UsernameClaim},
		{"display_name_claim", old.DisplayNameClaim != cur.DisplayNameClaim},
		{"groups_claim", old.GroupsClaim != cur.GroupsClaim},
	}
}

// oidcPolicySettings compares the login policy settings, which the
// handlers read on every request and a reload therefore applies.
func oidcPolicySettings(old, cur OIDCConfig) []oidcSettingChange {
	return []oidcSettingChange{
		{"provision_users", old.ProvisionUsersEnabled() != cur.ProvisionUsersEnabled()},
		{"allowed_email_domains", !reflect.DeepEqual(old.AllowedEmailDomains, cur.AllowedEmailDomains)},
		{"superuser_group", old.SuperuserGroup != cur.SuperuserGroup},
		{"group_map", !reflect.DeepEqual(old.GroupMap, cur.GroupMap)},
		{"button_label", old.ButtonLabel != cur.ButtonLabel},
	}
}

// changedOIDCSettings returns, in order, the names of the settings that
// changed.
func changedOIDCSettings(settings []oidcSettingChange) []string {
	var names []string
	for _, s := range settings {
		if s.changed {
			names = append(names, s.name)
		}
	}
	return names
}

func restartRequiredOIDCSetting(name string) {
	fmt.Fprintf(os.Stderr, "  WARNING: http.auth.oidc.%s changed - requires restart\n", name)
}

func appliedOIDCSetting(name string) {
	fmt.Fprintf(os.Stderr, "  NOTE: http.auth.oidc.%s changed - applied\n", name)
}

// oidcAccessWarnings describes the policy changes that widen access, or
// that look like a revocation but revoke nothing.
func oidcAccessWarnings(old, cur OIDCConfig) []string {
	var warnings []string
	if !old.ProvisionUsersEnabled() && cur.ProvisionUsersEnabled() {
		warnings = append(warnings, "provision_users switched on: any identity "+
			"the provider vouches for may now create an account")
	}
	if len(old.AllowedEmailDomains) > 0 && len(cur.AllowedEmailDomains) == 0 {
		warnings = append(warnings, "allowed_email_domains emptied: identities "+
			"from any email domain may now sign in")
	}
	if warning := superuserGroupWarning(old.SuperuserGroup, cur.SuperuserGroup); warning != "" {
		warnings = append(warnings, warning)
	}
	if domains := addedEmailDomains(old.AllowedEmailDomains, cur.AllowedEmailDomains); len(domains) > 0 {
		warnings = append(warnings, "allowed_email_domains widened: identities "+
			"from "+strings.Join(domains, ", ")+" may now sign in")
	}
	for _, mapping := range addedGroupMappings(old.GroupMap, cur.GroupMap) {
		warnings = append(warnings, "group_map "+mapping)
	}
	// Only the Workbench groups group_map names are reconciled, so a group
	// that drops out of it keeps the members it has; see the SSO guide.
	for _, group := range unmanagedWorkbenchGroups(old.GroupMap, cur.GroupMap) {
		warnings = append(warnings, "group_map no longer manages Workbench group "+
			group+": its federated members keep it; map it to another provider "+
			"group instead to revoke membership")
	}
	return warnings
}

// superuserGroupWarning describes a superuser_group change that hands
// superuser to a provider group's members, or that looks like a
// revocation but revokes nothing, or returns "" for any other change.
func superuserGroupWarning(oldGroup, newGroup string) string {
	switch {
	case oldGroup == newGroup:
		return ""
	case newGroup == "":
		// ReconcileFederatedGroups leaves is_superuser alone when no
		// superuser group is configured, so clearing it freezes every
		// federated superuser rather than demoting anyone.
		return "superuser_group cleared: federated users keep their current " +
			"superuser status, which no login will now change"
	default:
		// Retargeting one group to another hands superuser to the new
		// group's members just as setting it from empty does.
		return "superuser_group set: members of " + newGroup +
			" become superusers at their next login"
	}
}

// addedEmailDomains returns, sorted, the domains that newList allows and
// a non-empty oldList did not, compared as the login handler compares
// them: trimmed, without a leading "@" and ignoring case. An empty
// oldList already allows every domain, so nothing is added to it, and
// an empty newList is reported as emptied rather than here.
func addedEmailDomains(oldList, newList []string) []string {
	if len(oldList) == 0 || len(newList) == 0 {
		return nil
	}
	normalise := func(domain string) string {
		return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "@"))
	}
	allowed := make(map[string]bool, len(oldList))
	for _, domain := range oldList {
		allowed[normalise(domain)] = true
	}
	var added []string
	for _, domain := range newList {
		if d := normalise(domain); d != "" && !allowed[d] {
			allowed[d] = true
			added = append(added, d)
		}
	}
	sort.Strings(added)
	return added
}

// addedGroupMappings describes, sorted by provider group, each provider
// group that newMap maps to a Workbench group oldMap did not map it to,
// since the provider group's members join that Workbench group at their
// next login.
func addedGroupMappings(oldMap, newMap map[string]string) []string {
	providerGroups := make([]string, 0, len(newMap))
	for providerGroup, group := range newMap {
		if oldGroup, ok := oldMap[providerGroup]; !ok || oldGroup != group {
			providerGroups = append(providerGroups, providerGroup)
		}
	}
	sort.Strings(providerGroups)
	var added []string
	for _, providerGroup := range providerGroups {
		added = append(added, "maps provider group "+providerGroup+
			" to Workbench group "+newMap[providerGroup]+
			": its members join that group at their next login")
	}
	return added
}

// unmanagedWorkbenchGroups returns, sorted, the Workbench groups that
// oldMap maps a provider group to and newMap no longer does.
func unmanagedWorkbenchGroups(oldMap, newMap map[string]string) []string {
	stillManaged := make(map[string]bool, len(newMap))
	for _, group := range newMap {
		stillManaged[group] = true
	}
	seen := make(map[string]bool)
	var dropped []string
	for _, group := range oldMap {
		if !stillManaged[group] && !seen[group] {
			seen[group] = true
			dropped = append(dropped, group)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// OnReload registers a callback to be called when configuration is reloaded
// The callback receives the new configuration
func (rc *ReloadableConfig) OnReload(fn func(*Config)) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.onReload = append(rc.onReload, fn)
}
