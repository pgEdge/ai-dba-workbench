/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Sentinel errors for config operations
var (
	ErrProbeConfigNotFound = errors.New("probe config not found")
	ErrAlertRuleNotFound   = errors.New("alert rule not found")
)

// ProbeConfig represents a probe configuration row
type ProbeConfig struct {
	ID                        int       `json:"id"`
	ConnectionID              *int      `json:"connection_id"`
	IsEnabled                 bool      `json:"is_enabled"`
	Name                      string    `json:"name"`
	Description               string    `json:"description"`
	CollectionIntervalSeconds int       `json:"collection_interval_seconds"`
	RetentionDays             int       `json:"retention_days"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

// ProbeConfigUpdate represents a partial update to a probe config
type ProbeConfigUpdate struct {
	IsEnabled                 *bool `json:"is_enabled,omitempty"`
	CollectionIntervalSeconds *int  `json:"collection_interval_seconds,omitempty"`
	RetentionDays             *int  `json:"retention_days,omitempty"`
}

// AlertRule represents an alert rule definition
type AlertRule struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Category          string    `json:"category"`
	MetricName        string    `json:"metric_name"`
	MetricUnit        *string   `json:"metric_unit"`
	DefaultOperator   string    `json:"default_operator"`
	DefaultThreshold  float64   `json:"default_threshold"`
	DefaultSeverity   string    `json:"default_severity"`
	DefaultEnabled    bool      `json:"default_enabled"`
	RequiredExtension *string   `json:"required_extension"`
	IsBuiltIn         bool      `json:"is_built_in"`
	CreatedAt         time.Time `json:"created_at"`
}

// AlertRuleUpdate represents a partial update to an alert rule
type AlertRuleUpdate struct {
	DefaultOperator  *string  `json:"default_operator,omitempty"`
	DefaultThreshold *float64 `json:"default_threshold,omitempty"`
	DefaultSeverity  *string  `json:"default_severity,omitempty"`
	DefaultEnabled   *bool    `json:"default_enabled,omitempty"`
}

// AlertOverride represents a rule with its optional scope-level override
type AlertOverride struct {
	// Rule fields
	RuleID           int64   `json:"rule_id"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Category         string  `json:"category"`
	MetricName       string  `json:"metric_name"`
	MetricUnit       *string `json:"metric_unit"`
	DefaultOperator  string  `json:"default_operator"`
	DefaultThreshold float64 `json:"default_threshold"`
	DefaultSeverity  string  `json:"default_severity"`
	DefaultEnabled   bool    `json:"default_enabled"`
	// Override fields (nil if no override at this scope)
	HasOverride       bool     `json:"has_override"`
	OverrideOperator  *string  `json:"override_operator"`
	OverrideThreshold *float64 `json:"override_threshold"`
	OverrideSeverity  *string  `json:"override_severity"`
	OverrideEnabled   *bool    `json:"override_enabled"`
}

// AlertThresholdUpdate represents data for creating/updating a threshold override
type AlertThresholdUpdate struct {
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Severity  string  `json:"severity"`
	Enabled   bool    `json:"enabled"`
}

// Valid operators for alert rules
var validOperators = map[string]bool{
	">": true, ">=": true, "<": true, "<=": true, "=": true, "!=": true,
}

// Valid severities for alert rules
var validSeverities = map[string]bool{
	"info": true, "warning": true, "critical": true,
}

// GetProbeConfigs returns probe configs filtered by connection ID.
// If connectionID is nil, returns global defaults (WHERE connection_id IS NULL).
func (d *Datastore) GetProbeConfigs(ctx context.Context, connectionID *int) ([]ProbeConfig, error) {
	var rows pgx.Rows
	var err error

	if connectionID == nil {
		rows, err = d.pool.Query(ctx,
			`SELECT id, connection_id, is_enabled, name, description,
                    collection_interval_seconds, retention_days, created_at, updated_at
             FROM probe_configs WHERE connection_id IS NULL
             ORDER BY name`)
	} else {
		rows, err = d.pool.Query(ctx,
			`SELECT id, connection_id, is_enabled, name, description,
                    collection_interval_seconds, retention_days, created_at, updated_at
             FROM probe_configs WHERE connection_id = $1
             ORDER BY name`, *connectionID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query probe configs: %w", err)
	}
	defer rows.Close()

	var configs []ProbeConfig
	for rows.Next() {
		var c ProbeConfig
		if err := rows.Scan(&c.ID, &c.ConnectionID, &c.IsEnabled, &c.Name, &c.Description,
			&c.CollectionIntervalSeconds, &c.RetentionDays, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan probe config: %w", err)
		}
		configs = append(configs, c)
	}
	if configs == nil {
		configs = []ProbeConfig{}
	}
	return configs, rows.Err()
}

// GetProbeConfig returns a single probe config by ID.
func (d *Datastore) GetProbeConfig(ctx context.Context, id int64) (*ProbeConfig, error) {
	var c ProbeConfig
	err := d.pool.QueryRow(ctx,
		`SELECT id, connection_id, is_enabled, name, description,
                collection_interval_seconds, retention_days, created_at, updated_at
         FROM probe_configs WHERE id = $1`, id).
		Scan(&c.ID, &c.ConnectionID, &c.IsEnabled, &c.Name, &c.Description,
			&c.CollectionIntervalSeconds, &c.RetentionDays, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProbeConfigNotFound
		}
		return nil, fmt.Errorf("failed to get probe config: %w", err)
	}
	return &c, nil
}

// UpdateProbeConfig applies a partial update to a probe config.
func (d *Datastore) UpdateProbeConfig(ctx context.Context, id int64, update ProbeConfigUpdate) (*ProbeConfig, error) {
	if update.CollectionIntervalSeconds != nil && *update.CollectionIntervalSeconds <= 0 {
		return nil, fmt.Errorf("collection_interval_seconds must be greater than 0")
	}
	if update.RetentionDays != nil && *update.RetentionDays <= 0 {
		return nil, fmt.Errorf("retention_days must be greater than 0")
	}

	// Fetch existing to merge
	existing, err := d.GetProbeConfig(ctx, id)
	if err != nil {
		return nil, err
	}

	isEnabled := existing.IsEnabled
	if update.IsEnabled != nil {
		isEnabled = *update.IsEnabled
	}
	interval := existing.CollectionIntervalSeconds
	if update.CollectionIntervalSeconds != nil {
		interval = *update.CollectionIntervalSeconds
	}
	retention := existing.RetentionDays
	if update.RetentionDays != nil {
		retention = *update.RetentionDays
	}

	var c ProbeConfig
	err = d.pool.QueryRow(ctx,
		`UPDATE probe_configs
         SET is_enabled = $2, collection_interval_seconds = $3, retention_days = $4, updated_at = NOW()
         WHERE id = $1
         RETURNING id, connection_id, is_enabled, name, description,
                   collection_interval_seconds, retention_days, created_at, updated_at`,
		id, isEnabled, interval, retention).
		Scan(&c.ID, &c.ConnectionID, &c.IsEnabled, &c.Name, &c.Description,
			&c.CollectionIntervalSeconds, &c.RetentionDays, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProbeConfigNotFound
		}
		return nil, fmt.Errorf("failed to update probe config: %w", err)
	}
	return &c, nil
}

// GetAlertRules returns all alert rules.
func (d *Datastore) GetAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, name, description, category, metric_name, metric_unit,
                default_operator, default_threshold, default_severity, default_enabled,
                required_extension, is_built_in, created_at
         FROM alert_rules ORDER BY category, name`)
	if err != nil {
		return nil, fmt.Errorf("failed to query alert rules: %w", err)
	}
	defer rows.Close()

	var rules []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.MetricName,
			&r.MetricUnit, &r.DefaultOperator, &r.DefaultThreshold, &r.DefaultSeverity,
			&r.DefaultEnabled, &r.RequiredExtension, &r.IsBuiltIn, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan alert rule: %w", err)
		}
		rules = append(rules, r)
	}
	if rules == nil {
		rules = []AlertRule{}
	}
	return rules, rows.Err()
}

// GetAlertRule returns a single alert rule by ID.
func (d *Datastore) GetAlertRule(ctx context.Context, id int64) (*AlertRule, error) {
	var r AlertRule
	err := d.pool.QueryRow(ctx,
		`SELECT id, name, description, category, metric_name, metric_unit,
                default_operator, default_threshold, default_severity, default_enabled,
                required_extension, is_built_in, created_at
         FROM alert_rules WHERE id = $1`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.MetricName,
			&r.MetricUnit, &r.DefaultOperator, &r.DefaultThreshold, &r.DefaultSeverity,
			&r.DefaultEnabled, &r.RequiredExtension, &r.IsBuiltIn, &r.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAlertRuleNotFound
		}
		return nil, fmt.Errorf("failed to get alert rule: %w", err)
	}
	return &r, nil
}

// UpdateAlertRule updates the default settings for an alert rule.
func (d *Datastore) UpdateAlertRule(ctx context.Context, id int64, update AlertRuleUpdate) (*AlertRule, error) {
	if update.DefaultOperator != nil && !validOperators[*update.DefaultOperator] {
		return nil, fmt.Errorf("invalid operator: %s", *update.DefaultOperator)
	}
	if update.DefaultSeverity != nil && !validSeverities[*update.DefaultSeverity] {
		return nil, fmt.Errorf("invalid severity: %s", *update.DefaultSeverity)
	}

	existing, err := d.GetAlertRule(ctx, id)
	if err != nil {
		return nil, err
	}

	operator := existing.DefaultOperator
	if update.DefaultOperator != nil {
		operator = *update.DefaultOperator
	}
	threshold := existing.DefaultThreshold
	if update.DefaultThreshold != nil {
		threshold = *update.DefaultThreshold
	}
	severity := existing.DefaultSeverity
	if update.DefaultSeverity != nil {
		severity = *update.DefaultSeverity
	}
	enabled := existing.DefaultEnabled
	if update.DefaultEnabled != nil {
		enabled = *update.DefaultEnabled
	}

	var r AlertRule
	err = d.pool.QueryRow(ctx,
		`UPDATE alert_rules
         SET default_operator = $2, default_threshold = $3, default_severity = $4, default_enabled = $5
         WHERE id = $1
         RETURNING id, name, description, category, metric_name, metric_unit,
                   default_operator, default_threshold, default_severity, default_enabled,
                   required_extension, is_built_in, created_at`,
		id, operator, threshold, severity, enabled).
		Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.MetricName,
			&r.MetricUnit, &r.DefaultOperator, &r.DefaultThreshold, &r.DefaultSeverity,
			&r.DefaultEnabled, &r.RequiredExtension, &r.IsBuiltIn, &r.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAlertRuleNotFound
		}
		return nil, fmt.Errorf("failed to update alert rule: %w", err)
	}
	return &r, nil
}

// GetAlertOverridesForServer returns all alert rules with their server-level overrides.
func (d *Datastore) GetAlertOverridesForServer(ctx context.Context, connectionID int) ([]AlertOverride, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT ar.id, ar.name, ar.description, ar.category, ar.metric_name, ar.metric_unit,
		       ar.default_operator, ar.default_threshold, ar.default_severity, ar.default_enabled,
		       at.operator, at.threshold, at.severity, at.enabled
		FROM alert_rules ar
		LEFT JOIN alert_thresholds at ON at.rule_id = ar.id
		    AND at.scope = 'server' AND at.connection_id = $1
		ORDER BY ar.category, ar.name`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query server alert overrides: %w", err)
	}
	defer rows.Close()

	return scanAlertOverrides(rows)
}

// GetAlertOverridesForCluster returns all alert rules with their cluster-level overrides.
func (d *Datastore) GetAlertOverridesForCluster(ctx context.Context, clusterID int) ([]AlertOverride, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT ar.id, ar.name, ar.description, ar.category, ar.metric_name, ar.metric_unit,
		       ar.default_operator, ar.default_threshold, ar.default_severity, ar.default_enabled,
		       at.operator, at.threshold, at.severity, at.enabled
		FROM alert_rules ar
		LEFT JOIN alert_thresholds at ON at.rule_id = ar.id
		    AND at.scope = 'cluster' AND at.cluster_id = $1
		ORDER BY ar.category, ar.name`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster alert overrides: %w", err)
	}
	defer rows.Close()

	return scanAlertOverrides(rows)
}

// GetAlertOverridesForGroup returns all alert rules with their group-level overrides.
func (d *Datastore) GetAlertOverridesForGroup(ctx context.Context, groupID int) ([]AlertOverride, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT ar.id, ar.name, ar.description, ar.category, ar.metric_name, ar.metric_unit,
		       ar.default_operator, ar.default_threshold, ar.default_severity, ar.default_enabled,
		       at.operator, at.threshold, at.severity, at.enabled
		FROM alert_rules ar
		LEFT JOIN alert_thresholds at ON at.rule_id = ar.id
		    AND at.scope = 'group' AND at.group_id = $1
		ORDER BY ar.category, ar.name`, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query group alert overrides: %w", err)
	}
	defer rows.Close()

	return scanAlertOverrides(rows)
}

// scanAlertOverrides is a helper that scans rows from an alert overrides query.
func scanAlertOverrides(rows pgx.Rows) ([]AlertOverride, error) {
	var overrides []AlertOverride
	for rows.Next() {
		var o AlertOverride
		var overrideOp, overrideSev *string
		var overrideThresh *float64
		var overrideEnabled *bool
		if err := rows.Scan(
			&o.RuleID, &o.Name, &o.Description, &o.Category, &o.MetricName, &o.MetricUnit,
			&o.DefaultOperator, &o.DefaultThreshold, &o.DefaultSeverity, &o.DefaultEnabled,
			&overrideOp, &overrideThresh, &overrideSev, &overrideEnabled,
		); err != nil {
			return nil, fmt.Errorf("failed to scan alert override: %w", err)
		}
		o.HasOverride = overrideOp != nil
		o.OverrideOperator = overrideOp
		o.OverrideThreshold = overrideThresh
		o.OverrideSeverity = overrideSev
		o.OverrideEnabled = overrideEnabled
		overrides = append(overrides, o)
	}
	if overrides == nil {
		overrides = []AlertOverride{}
	}
	return overrides, rows.Err()
}

// UpsertAlertThreshold creates or updates an alert threshold override at the specified scope.
func (d *Datastore) UpsertAlertThreshold(ctx context.Context, scope string, scopeID int, ruleID int64, update AlertThresholdUpdate) error {
	if !validOperators[update.Operator] {
		return fmt.Errorf("invalid operator: %s", update.Operator)
	}
	if !validSeverities[update.Severity] {
		return fmt.Errorf("invalid severity: %s", update.Severity)
	}

	var query string
	var args []interface{}

	switch scope {
	case "server":
		query = `
			INSERT INTO alert_thresholds (rule_id, connection_id, scope, operator, threshold, severity, enabled)
			VALUES ($1, $2, 'server', $3, $4, $5, $6)
			ON CONFLICT (rule_id, connection_id, database_name) WHERE scope = 'server'
			DO UPDATE SET operator = $3, threshold = $4, severity = $5, enabled = $6, updated_at = NOW()`
		args = []interface{}{ruleID, scopeID, update.Operator, update.Threshold, update.Severity, update.Enabled}
	case "cluster":
		query = `
			INSERT INTO alert_thresholds (rule_id, cluster_id, scope, operator, threshold, severity, enabled)
			VALUES ($1, $2, 'cluster', $3, $4, $5, $6)
			ON CONFLICT (rule_id, cluster_id) WHERE scope = 'cluster'
			DO UPDATE SET operator = $3, threshold = $4, severity = $5, enabled = $6, updated_at = NOW()`
		args = []interface{}{ruleID, scopeID, update.Operator, update.Threshold, update.Severity, update.Enabled}
	case "group":
		query = `
			INSERT INTO alert_thresholds (rule_id, group_id, scope, operator, threshold, severity, enabled)
			VALUES ($1, $2, 'group', $3, $4, $5, $6)
			ON CONFLICT (rule_id, group_id) WHERE scope = 'group'
			DO UPDATE SET operator = $3, threshold = $4, severity = $5, enabled = $6, updated_at = NOW()`
		args = []interface{}{ruleID, scopeID, update.Operator, update.Threshold, update.Severity, update.Enabled}
	default:
		return fmt.Errorf("invalid scope: %s", scope)
	}

	_, err := d.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to upsert alert threshold: %w", err)
	}
	return nil
}

// DeleteAlertThreshold removes an alert threshold override at the specified scope.
func (d *Datastore) DeleteAlertThreshold(ctx context.Context, scope string, scopeID int, ruleID int64) error {
	var query string
	var args []interface{}

	switch scope {
	case "server":
		query = `DELETE FROM alert_thresholds WHERE scope = 'server' AND rule_id = $1 AND connection_id = $2`
		args = []interface{}{ruleID, scopeID}
	case "cluster":
		query = `DELETE FROM alert_thresholds WHERE scope = 'cluster' AND rule_id = $1 AND cluster_id = $2`
		args = []interface{}{ruleID, scopeID}
	case "group":
		query = `DELETE FROM alert_thresholds WHERE scope = 'group' AND rule_id = $1 AND group_id = $2`
		args = []interface{}{ruleID, scopeID}
	default:
		return fmt.Errorf("invalid scope: %s", scope)
	}

	_, err := d.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete alert threshold: %w", err)
	}
	return nil
}

// ProbeOverride represents a global probe default with its optional scope-level override
type ProbeOverride struct {
	// Global default fields
	Name                   string `json:"name"`
	Description            string `json:"description"`
	DefaultEnabled         bool   `json:"default_enabled"`
	DefaultIntervalSeconds int    `json:"default_interval_seconds"`
	DefaultRetentionDays   int    `json:"default_retention_days"`
	// Override fields (nil if no override at this scope)
	HasOverride             bool  `json:"has_override"`
	OverrideEnabled         *bool `json:"override_enabled"`
	OverrideIntervalSeconds *int  `json:"override_interval_seconds"`
	OverrideRetentionDays   *int  `json:"override_retention_days"`
}

// ProbeOverrideUpdate represents data for creating/updating a probe config override
type ProbeOverrideUpdate struct {
	IsEnabled                 bool `json:"is_enabled"`
	CollectionIntervalSeconds int  `json:"collection_interval_seconds"`
	RetentionDays             int  `json:"retention_days"`
}

// GetProbeOverridesForServer returns all probes with their server-level overrides.
func (d *Datastore) GetProbeOverridesForServer(ctx context.Context, connectionID int) ([]ProbeOverride, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT g.name, g.description, g.is_enabled, g.collection_interval_seconds, g.retention_days,
		       o.is_enabled, o.collection_interval_seconds, o.retention_days,
		       COALESCE(o.user_modified, FALSE)
		FROM probe_configs g
		LEFT JOIN probe_configs o ON o.name = g.name AND o.scope = 'server' AND o.connection_id = $1
		WHERE g.scope = 'global'
		ORDER BY g.name`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query server probe overrides: %w", err)
	}
	defer rows.Close()

	return scanProbeOverrides(rows)
}

// GetProbeOverridesForCluster returns all probes with their cluster-level overrides.
func (d *Datastore) GetProbeOverridesForCluster(ctx context.Context, clusterID int) ([]ProbeOverride, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT g.name, g.description, g.is_enabled, g.collection_interval_seconds, g.retention_days,
		       o.is_enabled, o.collection_interval_seconds, o.retention_days,
		       COALESCE(o.user_modified, FALSE)
		FROM probe_configs g
		LEFT JOIN probe_configs o ON o.name = g.name AND o.scope = 'cluster' AND o.cluster_id = $1
		WHERE g.scope = 'global'
		ORDER BY g.name`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster probe overrides: %w", err)
	}
	defer rows.Close()

	return scanProbeOverrides(rows)
}

// GetProbeOverridesForGroup returns all probes with their group-level overrides.
func (d *Datastore) GetProbeOverridesForGroup(ctx context.Context, groupID int) ([]ProbeOverride, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT g.name, g.description, g.is_enabled, g.collection_interval_seconds, g.retention_days,
		       o.is_enabled, o.collection_interval_seconds, o.retention_days,
		       COALESCE(o.user_modified, FALSE)
		FROM probe_configs g
		LEFT JOIN probe_configs o ON o.name = g.name AND o.scope = 'group' AND o.group_id = $1
		WHERE g.scope = 'global'
		ORDER BY g.name`, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query group probe overrides: %w", err)
	}
	defer rows.Close()

	return scanProbeOverrides(rows)
}

// scanProbeOverrides is a helper that scans rows from a probe overrides query.
func scanProbeOverrides(rows pgx.Rows) ([]ProbeOverride, error) {
	var overrides []ProbeOverride
	for rows.Next() {
		var o ProbeOverride
		var overrideEnabled *bool
		var overrideInterval, overrideRetention *int
		var userModified bool
		if err := rows.Scan(
			&o.Name, &o.Description, &o.DefaultEnabled, &o.DefaultIntervalSeconds, &o.DefaultRetentionDays,
			&overrideEnabled, &overrideInterval, &overrideRetention,
			&userModified,
		); err != nil {
			return nil, fmt.Errorf("failed to scan probe override: %w", err)
		}
		o.HasOverride = userModified
		o.OverrideEnabled = overrideEnabled
		o.OverrideIntervalSeconds = overrideInterval
		o.OverrideRetentionDays = overrideRetention
		overrides = append(overrides, o)
	}
	if overrides == nil {
		overrides = []ProbeOverride{}
	}
	return overrides, rows.Err()
}

// UpsertProbeOverride creates or updates a probe config override at the specified scope.
func (d *Datastore) UpsertProbeOverride(ctx context.Context, scope string, scopeID int, probeName string, update ProbeOverrideUpdate) error {
	if update.CollectionIntervalSeconds <= 0 {
		return fmt.Errorf("collection_interval_seconds must be greater than 0")
	}
	if update.RetentionDays <= 0 {
		return fmt.Errorf("retention_days must be greater than 0")
	}

	var query string
	var args []interface{}

	switch scope {
	case "server":
		query = `
			INSERT INTO probe_configs (name, description, scope, connection_id, is_enabled, collection_interval_seconds, retention_days, user_modified)
			VALUES ($1, (SELECT description FROM probe_configs WHERE name = $1 AND scope = 'global'), 'server', $2, $3, $4, $5, TRUE)
			ON CONFLICT (name, connection_id) WHERE scope = 'server'
			DO UPDATE SET is_enabled = $3, collection_interval_seconds = $4, retention_days = $5, user_modified = TRUE, updated_at = NOW()`
		args = []interface{}{probeName, scopeID, update.IsEnabled, update.CollectionIntervalSeconds, update.RetentionDays}
	case "cluster":
		query = `
			INSERT INTO probe_configs (name, description, scope, cluster_id, is_enabled, collection_interval_seconds, retention_days, user_modified)
			VALUES ($1, (SELECT description FROM probe_configs WHERE name = $1 AND scope = 'global'), 'cluster', $2, $3, $4, $5, TRUE)
			ON CONFLICT (name, cluster_id) WHERE scope = 'cluster'
			DO UPDATE SET is_enabled = $3, collection_interval_seconds = $4, retention_days = $5, user_modified = TRUE, updated_at = NOW()`
		args = []interface{}{probeName, scopeID, update.IsEnabled, update.CollectionIntervalSeconds, update.RetentionDays}
	case "group":
		query = `
			INSERT INTO probe_configs (name, description, scope, group_id, is_enabled, collection_interval_seconds, retention_days, user_modified)
			VALUES ($1, (SELECT description FROM probe_configs WHERE name = $1 AND scope = 'global'), 'group', $2, $3, $4, $5, TRUE)
			ON CONFLICT (name, group_id) WHERE scope = 'group'
			DO UPDATE SET is_enabled = $3, collection_interval_seconds = $4, retention_days = $5, user_modified = TRUE, updated_at = NOW()`
		args = []interface{}{probeName, scopeID, update.IsEnabled, update.CollectionIntervalSeconds, update.RetentionDays}
	default:
		return fmt.Errorf("invalid scope: %s", scope)
	}

	_, err := d.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to upsert probe override: %w", err)
	}
	return nil
}

// DeleteProbeOverride removes a probe config override at the specified scope.
func (d *Datastore) DeleteProbeOverride(ctx context.Context, scope string, scopeID int, probeName string) error {
	var query string
	var args []interface{}

	switch scope {
	case "server":
		query = `DELETE FROM probe_configs WHERE scope = 'server' AND name = $1 AND connection_id = $2`
		args = []interface{}{probeName, scopeID}
	case "cluster":
		query = `DELETE FROM probe_configs WHERE scope = 'cluster' AND name = $1 AND cluster_id = $2`
		args = []interface{}{probeName, scopeID}
	case "group":
		query = `DELETE FROM probe_configs WHERE scope = 'group' AND name = $1 AND group_id = $2`
		args = []interface{}{probeName, scopeID}
	default:
		return fmt.Errorf("invalid scope: %s", scope)
	}

	_, err := d.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete probe override: %w", err)
	}
	return nil
}
