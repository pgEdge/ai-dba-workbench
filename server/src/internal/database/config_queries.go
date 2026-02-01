/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
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
	ErrProbeConfigNotFound    = errors.New("probe config not found")
	ErrAlertRuleNotFound      = errors.New("alert rule not found")
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

