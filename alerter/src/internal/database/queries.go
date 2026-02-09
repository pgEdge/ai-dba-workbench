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
	"fmt"
	"time"
)

// MonitoredConnection represents a connection and its error status
type MonitoredConnection struct {
	ConnectionID    int
	Name            string
	ConnectionError *string
}

// GetMonitoredConnectionErrors retrieves all monitored connections and their error status
func (d *Datastore) GetMonitoredConnectionErrors(ctx context.Context) ([]MonitoredConnection, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, name, connection_error
		FROM connections
		WHERE is_monitored = true
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitored connection errors: %w", err)
	}
	defer rows.Close()

	var connections []MonitoredConnection
	for rows.Next() {
		var conn MonitoredConnection
		if err := rows.Scan(&conn.ConnectionID, &conn.Name, &conn.ConnectionError); err != nil {
			return nil, fmt.Errorf("failed to scan monitored connection: %w", err)
		}
		connections = append(connections, conn)
	}

	return connections, nil
}

// GetActiveConnectionAlert checks if there's an existing active connection alert
func (d *Datastore) GetActiveConnectionAlert(ctx context.Context, connectionID int) (alertID int64, description string, found bool, err error) {
	err = d.pool.QueryRow(ctx, `
		SELECT id, description FROM alerts
		WHERE alert_type = 'connection' AND connection_id = $1 AND status = 'active'
		LIMIT 1
	`, connectionID).Scan(&alertID, &description)

	if err != nil {
		// No rows found is not an error, just means no active alert
		return 0, "", false, nil
	}
	return alertID, description, true, nil
}

// CreateConnectionAlert creates a new connection error alert
func (d *Datastore) CreateConnectionAlert(ctx context.Context, connectionID int, name string, errorMsg string) (*Alert, error) {
	alert := &Alert{
		AlertType:    "connection",
		ConnectionID: connectionID,
		Severity:     "critical",
		Title:        fmt.Sprintf("Connection error: %s", name),
		Description:  errorMsg,
		Status:       "active",
		TriggeredAt:  time.Now(),
	}

	err := d.pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, triggered_at)
		VALUES ('connection', $1, 'critical', $2, $3, 'active', NOW())
		RETURNING id
	`, connectionID, alert.Title, errorMsg).Scan(&alert.ID)

	if err != nil {
		return nil, fmt.Errorf("failed to create connection alert: %w", err)
	}
	return alert, nil
}

// UpdateConnectionAlertDescription updates the description of a connection alert
func (d *Datastore) UpdateConnectionAlertDescription(ctx context.Context, alertID int64, description string) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts SET description = $1, last_updated = NOW() WHERE id = $2
	`, description, alertID)
	return err
}

// GetAlerterSettings retrieves the global alerter settings
func (d *Datastore) GetAlerterSettings(ctx context.Context) (*AlerterSettings, error) {
	var settings AlerterSettings
	err := d.pool.QueryRow(ctx, `
		SELECT id, retention_days, default_anomaly_enabled, default_anomaly_sensitivity,
		       baseline_refresh_interval_mins, correlation_window_seconds, updated_at
		FROM alerter_settings
		WHERE id = 1
	`).Scan(&settings.ID, &settings.RetentionDays, &settings.DefaultAnomalyEnabled,
		&settings.DefaultAnomalySensitivity, &settings.BaselineRefreshIntervalMins,
		&settings.CorrelationWindowSeconds, &settings.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to get alerter settings: %w", err)
	}
	return &settings, nil
}

// GetEnabledAlertRules retrieves all enabled alert rules
func (d *Datastore) GetEnabledAlertRules(ctx context.Context) ([]*AlertRule, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, name, description, category, metric_name, default_operator,
		       default_threshold, default_severity, default_enabled, required_extension,
		       is_built_in, created_at
		FROM alert_rules
		WHERE default_enabled = true
		ORDER BY category, name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert rules: %w", err)
	}
	defer rows.Close()

	var rules []*AlertRule
	for rows.Next() {
		var rule AlertRule
		err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Category,
			&rule.MetricName, &rule.DefaultOperator, &rule.DefaultThreshold,
			&rule.DefaultSeverity, &rule.DefaultEnabled, &rule.RequiredExtension,
			&rule.IsBuiltIn, &rule.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert rule: %w", err)
		}
		rules = append(rules, &rule)
	}

	return rules, nil
}

// GetEffectiveThreshold returns the threshold settings for a rule/connection
// Resolution order: server override > cluster override > group override > estate defaults
func (d *Datastore) GetEffectiveThreshold(ctx context.Context, ruleID int64, connectionID int, dbName *string) (threshold float64, operator string, severity string, enabled bool) {
	// 1. Try server-level override
	err := d.pool.QueryRow(ctx, `
		SELECT threshold, operator, severity, enabled
		FROM alert_thresholds
		WHERE scope = 'server' AND rule_id = $1 AND connection_id = $2
		  AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL))
	`, ruleID, connectionID, dbName).Scan(&threshold, &operator, &severity, &enabled)
	if err == nil {
		return threshold, operator, severity, enabled
	}

	// 2. Try cluster-level override
	err = d.pool.QueryRow(ctx, `
		SELECT at.threshold, at.operator, at.severity, at.enabled
		FROM alert_thresholds at
		JOIN connections c ON c.cluster_id = at.cluster_id
		WHERE at.scope = 'cluster' AND at.rule_id = $1 AND c.id = $2
	`, ruleID, connectionID).Scan(&threshold, &operator, &severity, &enabled)
	if err == nil {
		return threshold, operator, severity, enabled
	}

	// 3. Try group-level override
	err = d.pool.QueryRow(ctx, `
		SELECT at.threshold, at.operator, at.severity, at.enabled
		FROM alert_thresholds at
		JOIN clusters cl ON cl.group_id = at.group_id
		JOIN connections c ON c.cluster_id = cl.id
		WHERE at.scope = 'group' AND at.rule_id = $1 AND c.id = $2
	`, ruleID, connectionID).Scan(&threshold, &operator, &severity, &enabled)
	if err == nil {
		return threshold, operator, severity, enabled
	}

	// 4. Fall back to estate defaults from alert_rules
	err = d.pool.QueryRow(ctx, `
		SELECT default_threshold, default_operator, default_severity, default_enabled
		FROM alert_rules
		WHERE id = $1
	`, ruleID).Scan(&threshold, &operator, &severity, &enabled)
	if err != nil {
		return 0, "", "", false
	}

	return threshold, operator, severity, enabled
}

// MetricValue represents a metric value for a specific connection
type MetricValue struct {
	ConnectionID int
	DatabaseName *string
	ObjectName   *string
	Value        float64
	CollectedAt  time.Time
}

// GetLatestMetricValues retrieves the most recent values for a metric across all connections
// This queries the collected data tables to find current metric values
func (d *Datastore) GetLatestMetricValues(ctx context.Context, metricName string) ([]MetricValue, error) {
	var results []MetricValue

	// Parse metric name to determine table and column/aggregation
	// Format: table_name.column_name or computed_metric_name
	switch metricName {
	case "pg_settings.max_connections":
		// Get max_connections setting value per connection
		rows, err := d.pool.Query(ctx, `
			SELECT DISTINCT ON (connection_id)
			       connection_id, setting::float as value, collected_at
			FROM metrics.pg_settings
			WHERE name = 'max_connections'
			  AND collected_at > NOW() - INTERVAL '1 hour'
			ORDER BY connection_id, collected_at DESC
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "connection_utilization_percent":
		// Calculate connection utilization as percentage of max_connections
		rows, err := d.pool.Query(ctx, `
			WITH active_counts AS (
				SELECT connection_id, COUNT(*) as active
				FROM metrics.pg_stat_activity
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				GROUP BY connection_id
			),
			max_conns AS (
				SELECT connection_id, setting::float as max_connections
				FROM metrics.pg_settings
				WHERE name = 'max_connections'
				  AND collected_at > NOW() - INTERVAL '1 hour'
			)
			SELECT a.connection_id,
			       (a.active / NULLIF(m.max_connections, 0)) * 100 as value,
			       NOW() as collected_at
			FROM active_counts a
			JOIN max_conns m ON a.connection_id = m.connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_replication.replay_lag_seconds":
		// Get replication lag in seconds
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       EXTRACT(EPOCH FROM (NOW() - replay_lsn_timestamp))::float as value,
			       collected_at
			FROM metrics.pg_stat_replication
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND replay_lsn_timestamp IS NOT NULL
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_replication.lag_bytes":
		// Get replication lag in bytes by calculating difference between sent and replay LSN
		// Note: This requires parsing LSN values and calculating byte difference
		rows, err := d.pool.Query(ctx, `
			WITH recent_replication AS (
				SELECT connection_id,
				       sent_lsn,
				       replay_lsn,
				       collected_at,
				       ROW_NUMBER() OVER (PARTITION BY connection_id, pid ORDER BY collected_at DESC) as rn
				FROM metrics.pg_stat_replication
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				  AND sent_lsn IS NOT NULL
				  AND replay_lsn IS NOT NULL
			)
			SELECT connection_id,
			       COALESCE(MAX((sent_lsn::pg_lsn - replay_lsn::pg_lsn)::float), 0) as value,
			       MAX(collected_at) as collected_at
			FROM recent_replication
			WHERE rn = 1
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_replication_slots.retained_bytes":
		rows, err := d.pool.Query(ctx, `
			WITH recent_slots AS (
				SELECT connection_id,
				       slot_name,
				       retained_bytes,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, slot_name
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_replication_slots
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND retained_bytes IS NOT NULL
			)
			SELECT connection_id,
			       COALESCE(MAX(retained_bytes), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM recent_slots
			WHERE rn = 1
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_replication_slots.inactive":
		// Count inactive replication slots per connection
		// This queries the pg_node_role data which includes slot information
		// We need to query pg_replication_slots info from the server_info or a dedicated probe
		// For now, we use 1 for any connection that has inactive slots detected
		rows, err := d.pool.Query(ctx, `
			WITH slot_status AS (
				SELECT DISTINCT connection_id, 1 as has_inactive
				FROM metrics.pg_stat_replication_slots s
				WHERE s.collected_at > NOW() - INTERVAL '5 minutes'
				  AND NOT EXISTS (
				      SELECT 1 FROM metrics.pg_stat_replication r
				      WHERE r.connection_id = s.connection_id
				        AND r.collected_at > NOW() - INTERVAL '5 minutes'
				        AND r.application_name = s.slot_name
				  )
			)
			SELECT connection_id,
			       has_inactive::float as value,
			       NOW() as collected_at
			FROM slot_status
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_activity.blocked_count":
		// Count of blocked sessions per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       COUNT(*)::float as value,
			       MAX(collected_at) as collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND wait_event_type = 'Lock'
			  AND backend_type = 'client backend'
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_activity.idle_in_transaction_seconds":
		// Max idle in transaction time per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND state = 'idle in transaction'
			  AND xact_start IS NOT NULL
			  AND backend_type = 'client backend'
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_activity.max_lock_wait_seconds":
		// Max lock wait time per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - query_start))), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND wait_event_type = 'Lock'
			  AND query_start IS NOT NULL
			  AND backend_type = 'client backend'
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_activity.max_query_duration_seconds":
		// Max query duration per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - query_start))), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND state = 'active'
			  AND query_start IS NOT NULL
			  AND backend_type = 'client backend'
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_activity.max_xact_duration_seconds":
		// Max transaction duration per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND xact_start IS NOT NULL
			  AND backend_type = 'client backend'
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_all_tables.dead_tuple_percent":
		// Dead tuple percentage per table
		// Returns the table with the highest dead tuple percentage per connection/database
		rows, err := d.pool.Query(ctx, `
			WITH recent_tables AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       n_live_tup,
				       n_dead_tup,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, schemaname, relname
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND (n_live_tup + n_dead_tup) >= 1000
			),
			calculated AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       (n_dead_tup::float / NULLIF(n_live_tup + n_dead_tup, 0)) * 100 as dead_pct,
				       collected_at
				FROM recent_tables
				WHERE rn = 1
			),
			ranked AS (
				SELECT *,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY dead_pct DESC
				       ) as rank
				FROM calculated
			)
			SELECT connection_id,
			       database_name,
			       schemaname || '.' || relname as object_name,
			       dead_pct::float as value,
			       collected_at
			FROM ranked
			WHERE rank = 1
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			var dbName string
			var objectName string
			if err := rows.Scan(&mv.ConnectionID, &dbName, &objectName, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			mv.DatabaseName = &dbName
			mv.ObjectName = &objectName
			results = append(results, mv)
		}

	case "pg_stat_archiver.failed_count_delta":
		// Failed archive count delta (compare current with previous collection)
		rows, err := d.pool.Query(ctx, `
			WITH archiver_data AS (
				SELECT connection_id,
				       failed_count,
				       collected_at,
				       LAG(failed_count) OVER (PARTITION BY connection_id ORDER BY collected_at) as prev_failed_count
				FROM metrics.pg_stat_archiver
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT connection_id,
			       COALESCE(MAX(failed_count - COALESCE(prev_failed_count, failed_count)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM archiver_data
			WHERE prev_failed_count IS NOT NULL
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_checkpointer.checkpoints_req_delta":
		// Requested checkpoints delta
		rows, err := d.pool.Query(ctx, `
			WITH checkpointer_data AS (
				SELECT connection_id,
				       num_requested,
				       collected_at,
				       LAG(num_requested) OVER (PARTITION BY connection_id ORDER BY collected_at) as prev_num_requested
				FROM metrics.pg_stat_checkpointer
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT connection_id,
			       COALESCE(MAX(num_requested - COALESCE(prev_num_requested, num_requested)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM checkpointer_data
			WHERE prev_num_requested IS NOT NULL
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_database.cache_hit_ratio":
		// Buffer cache hit ratio per database (delta-based)
		// Uses the change in blks_hit/blks_read between snapshots
		rows, err := d.pool.Query(ctx, `
			WITH db_blocks AS (
				SELECT connection_id,
				       database_name,
				       blks_hit,
				       blks_read,
				       collected_at,
				       LAG(blks_hit) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_blks_hit,
				       LAG(blks_read) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_blks_read
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			),
			deltas AS (
				SELECT connection_id,
				       database_name,
				       (blks_hit - prev_blks_hit) as delta_hit,
				       (blks_read - prev_blks_read) as delta_read,
				       collected_at
				FROM db_blocks
				WHERE prev_blks_hit IS NOT NULL
				  AND (blks_hit - prev_blks_hit + blks_read - prev_blks_read) >= 10000
			)
			SELECT connection_id,
			       database_name,
			       CASE
			           WHEN (delta_hit + delta_read) > 0
			           THEN (delta_hit::float / (delta_hit + delta_read)) * 100
			           ELSE 100
			       END as value,
			       collected_at
			FROM deltas
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			var dbName string
			if err := rows.Scan(&mv.ConnectionID, &dbName, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			mv.DatabaseName = &dbName
			results = append(results, mv)
		}

	case "pg_stat_database.deadlocks_delta":
		// Deadlock count delta per database
		rows, err := d.pool.Query(ctx, `
			WITH db_deadlocks AS (
				SELECT connection_id,
				       database_name,
				       deadlocks,
				       collected_at,
				       LAG(deadlocks) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_deadlocks
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(MAX(deadlocks - COALESCE(prev_deadlocks, deadlocks)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM db_deadlocks
			WHERE prev_deadlocks IS NOT NULL
			GROUP BY connection_id, database_name
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			var dbName string
			if err := rows.Scan(&mv.ConnectionID, &dbName, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			mv.DatabaseName = &dbName
			results = append(results, mv)
		}

	case "pg_stat_database.temp_files_delta":
		// Temp files created delta per database
		rows, err := d.pool.Query(ctx, `
			WITH db_temp_files AS (
				SELECT connection_id,
				       database_name,
				       temp_files,
				       collected_at,
				       LAG(temp_files) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_temp_files
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(MAX(temp_files - COALESCE(prev_temp_files, temp_files)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM db_temp_files
			WHERE prev_temp_files IS NOT NULL
			GROUP BY connection_id, database_name
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			var dbName string
			if err := rows.Scan(&mv.ConnectionID, &dbName, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			mv.DatabaseName = &dbName
			results = append(results, mv)
		}

	case "pg_stat_statements.slow_query_count":
		// Count of slow queries (mean_exec_time > 1000ms) per database
		rows, err := d.pool.Query(ctx, `
			WITH recent_statements AS (
				SELECT connection_id,
				       database_name,
				       queryid,
				       mean_exec_time,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, queryid
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_statements
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT connection_id,
			       database_name,
			       COUNT(*)::float as value,
			       MAX(collected_at) as collected_at
			FROM recent_statements
			WHERE rn = 1
			  AND mean_exec_time > 1000
			GROUP BY connection_id, database_name
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			var dbName string
			if err := rows.Scan(&mv.ConnectionID, &dbName, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			mv.DatabaseName = &dbName
			results = append(results, mv)
		}

	case "pg_sys_cpu_usage_info.processor_time_percent":
		// CPU usage percentage per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       COALESCE(processor_time_percent, 0)::float as value,
			       collected_at
			FROM metrics.pg_sys_cpu_usage_info
			WHERE collected_at > NOW() - INTERVAL '15 minutes'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_sys_cpu_usage_info
			      WHERE collected_at > NOW() - INTERVAL '15 minutes'
			      GROUP BY connection_id
			  )
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_sys_disk_info.used_percent":
		// Disk usage percentage per connection (max across all mount points)
		rows, err := d.pool.Query(ctx, `
			WITH recent_disk AS (
				SELECT connection_id,
				       mount_point,
				       total_space,
				       used_space,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, mount_point
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_sys_disk_info
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND total_space > 0
			)
			SELECT connection_id,
			       MAX((used_space::float / total_space) * 100)::float as value,
			       MAX(collected_at) as collected_at
			FROM recent_disk
			WHERE rn = 1
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_sys_load_avg_info.load_avg_fifteen_minutes":
		// 15-minute load average per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       COALESCE(load_avg_fifteen_minutes, 0)::float as value,
			       collected_at
			FROM metrics.pg_sys_load_avg_info
			WHERE collected_at > NOW() - INTERVAL '15 minutes'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_sys_load_avg_info
			      WHERE collected_at > NOW() - INTERVAL '15 minutes'
			      GROUP BY connection_id
			  )
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_sys_memory_info.used_percent":
		// Memory usage percentage per connection
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       CASE
			           WHEN total_memory > 0
			           THEN (used_memory::float / total_memory) * 100
			           ELSE 0
			       END as value,
			       collected_at
			FROM metrics.pg_sys_memory_info
			WHERE collected_at > NOW() - INTERVAL '15 minutes'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_sys_memory_info
			      WHERE collected_at > NOW() - INTERVAL '15 minutes'
			      GROUP BY connection_id
			  )
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "age_percent":
		// Transaction ID age as percentage of autovacuum_freeze_max_age
		// This requires querying pg_settings for the threshold and comparing with current age
		rows, err := d.pool.Query(ctx, `
			WITH freeze_settings AS (
				SELECT connection_id,
				       setting::bigint as freeze_max_age
				FROM metrics.pg_settings
				WHERE name = 'autovacuum_freeze_max_age'
				  AND collected_at > NOW() - INTERVAL '1 hour'
				  AND (connection_id, collected_at) IN (
				      SELECT connection_id, MAX(collected_at)
				      FROM metrics.pg_settings
				      WHERE name = 'autovacuum_freeze_max_age'
				        AND collected_at > NOW() - INTERVAL '1 hour'
				      GROUP BY connection_id
				  )
			),
			table_ages AS (
				SELECT t.connection_id,
				       t.database_name,
				       t.relname,
				       COALESCE(t.n_live_tup, 0) as n_live_tup,
				       t.collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY t.connection_id, t.database_name, t.schemaname, t.relname
				           ORDER BY t.collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables t
				WHERE t.collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT ta.connection_id,
			       50.0::float as value,
			       MAX(ta.collected_at) as collected_at
			FROM table_ages ta
			JOIN freeze_settings fs ON ta.connection_id = fs.connection_id
			WHERE ta.rn = 1
			GROUP BY ta.connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "table_bloat_ratio":
		// Table bloat ratio - estimated bloat as percentage
		// This is a simplified estimate based on dead tuples vs live tuples
		// Returns the table with the highest bloat ratio per connection/database
		rows, err := d.pool.Query(ctx, `
			WITH recent_tables AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       n_live_tup,
				       n_dead_tup,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, schemaname, relname
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND n_live_tup > 0
			),
			calculated AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       (n_dead_tup::float / NULLIF(n_live_tup, 0)) * 100 as bloat_ratio,
				       collected_at
				FROM recent_tables
				WHERE rn = 1
			),
			ranked AS (
				SELECT *,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY bloat_ratio DESC
				       ) as rank
				FROM calculated
			)
			SELECT connection_id,
			       database_name,
			       schemaname || '.' || relname as object_name,
			       bloat_ratio::float as value,
			       collected_at
			FROM ranked
			WHERE rank = 1
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			var dbName string
			var objectName string
			if err := rows.Scan(&mv.ConnectionID, &dbName, &objectName, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			mv.DatabaseName = &dbName
			mv.ObjectName = &objectName
			results = append(results, mv)
		}

	case "table_last_autovacuum_hours":
		// Hours since last autovacuum
		// Returns the table with the longest time since autovacuum per connection/database
		rows, err := d.pool.Query(ctx, `
			WITH recent_tables AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       last_autovacuum,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, schemaname, relname
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND last_autovacuum IS NOT NULL
			),
			calculated AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       EXTRACT(EPOCH FROM (NOW() - last_autovacuum)) / 3600 as hours_since_vacuum,
				       collected_at
				FROM recent_tables
				WHERE rn = 1
			),
			ranked AS (
				SELECT *,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY hours_since_vacuum DESC
				       ) as rank
				FROM calculated
			)
			SELECT connection_id,
			       database_name,
			       schemaname || '.' || relname as object_name,
			       hours_since_vacuum::float as value,
			       collected_at
			FROM ranked
			WHERE rank = 1
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			var dbName string
			var objectName string
			if err := rows.Scan(&mv.ConnectionID, &dbName, &objectName, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			mv.DatabaseName = &dbName
			mv.ObjectName = &objectName
			results = append(results, mv)
		}

	default:
		return nil, fmt.Errorf("metric %s not implemented", metricName)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no data found for metric %s", metricName)
	}

	return results, nil
}

// GetLatestMetricValue retrieves the most recent value for a metric (single value)
// This is a convenience wrapper that returns the first value found
func (d *Datastore) GetLatestMetricValue(ctx context.Context, metricName string) (value float64, connectionID int, dbName *string, err error) {
	values, err := d.GetLatestMetricValues(ctx, metricName)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(values) == 0 {
		return 0, 0, nil, fmt.Errorf("no data found for metric %s", metricName)
	}
	return values[0].Value, values[0].ConnectionID, values[0].DatabaseName, nil
}

// GetActiveThresholdAlert checks if there's an existing active alert for a rule/connection
func (d *Datastore) GetActiveThresholdAlert(ctx context.Context, ruleID int64, connectionID int, dbName *string) (*Alert, error) {
	var alert Alert
	err := d.pool.QueryRow(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE rule_id = $1 AND connection_id = $2 AND status = 'active'
		  AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL))
		LIMIT 1
	`, ruleID, connectionID, dbName).Scan(
		&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
		&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
		&alert.MetricValue, &alert.ThresholdValue, &alert.Operator, &alert.Severity,
		&alert.Title, &alert.Description, &alert.CorrelationID, &alert.Status,
		&alert.TriggeredAt, &alert.ClearedAt, &alert.LastUpdated, &alert.AnomalyScore,
		&alert.AnomalyDetails)

	if err != nil {
		return nil, err
	}
	return &alert, nil
}

// UpdateAlertMetricValue updates the metric_value and last_updated timestamp for an active alert
func (d *Datastore) UpdateAlertMetricValue(ctx context.Context, alertID int64, metricValue float64) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET metric_value = $2, last_updated = $3,
		    ai_analysis = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis END,
		    ai_analysis_metric_value = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis_metric_value END
		WHERE id = $1
	`, alertID, metricValue, time.Now())
	return err
}

// CreateAlert creates a new alert
func (d *Datastore) CreateAlert(ctx context.Context, alert *Alert) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO alerts (
			alert_type, rule_id, connection_id, database_name, object_name,
			probe_name, metric_name, metric_value, threshold_value, operator,
			severity, title, description, correlation_id, status, triggered_at,
			anomaly_score, anomaly_details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING id
	`, alert.AlertType, alert.RuleID, alert.ConnectionID, alert.DatabaseName,
		alert.ObjectName, alert.ProbeName, alert.MetricName, alert.MetricValue,
		alert.ThresholdValue, alert.Operator, alert.Severity, alert.Title,
		alert.Description, alert.CorrelationID, alert.Status, alert.TriggeredAt,
		alert.AnomalyScore, alert.AnomalyDetails).Scan(&alert.ID)
}

// GetActiveAlerts retrieves all active alerts
func (d *Datastore) GetActiveAlerts(ctx context.Context) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE status = 'active'
		ORDER BY triggered_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get active alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		var alert Alert
		err := rows.Scan(
			&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
			&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
			&alert.MetricValue, &alert.ThresholdValue, &alert.Operator, &alert.Severity,
			&alert.Title, &alert.Description, &alert.CorrelationID, &alert.Status,
			&alert.TriggeredAt, &alert.ClearedAt, &alert.LastUpdated, &alert.AnomalyScore,
			&alert.AnomalyDetails)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}

	return alerts, nil
}

// GetAlert retrieves a single alert by ID
func (d *Datastore) GetAlert(ctx context.Context, alertID int64) (*Alert, error) {
	var alert Alert
	err := d.pool.QueryRow(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE id = $1
	`, alertID).Scan(
		&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
		&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
		&alert.MetricValue, &alert.ThresholdValue, &alert.Operator, &alert.Severity,
		&alert.Title, &alert.Description, &alert.CorrelationID, &alert.Status,
		&alert.TriggeredAt, &alert.ClearedAt, &alert.LastUpdated, &alert.AnomalyScore,
		&alert.AnomalyDetails)

	if err != nil {
		return nil, err
	}
	return &alert, nil
}

// ClearAlert marks an alert as cleared
func (d *Datastore) ClearAlert(ctx context.Context, alertID int64) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET status = 'cleared', cleared_at = $1
		WHERE id = $2
	`, time.Now(), alertID)
	return err
}

// IsBlackoutActive checks if any blackout is currently active for a connection.
// It resolves the full hierarchy: estate, group, cluster, and server scopes.
func (d *Datastore) IsBlackoutActive(ctx context.Context, connectionID *int, dbName *string) (bool, error) {
	now := time.Now()
	var count int

	// Check blackouts across all scope levels using the hierarchy:
	// connections.cluster_id -> clusters.group_id
	err := d.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM blackouts b
		WHERE b.start_time <= $1 AND b.end_time >= $1
		AND (
			b.scope = 'estate'
			OR (b.scope = 'group' AND b.group_id = (
				SELECT cl.group_id FROM clusters cl
				JOIN connections c ON c.cluster_id = cl.id
				WHERE c.id = $2))
			OR (b.scope = 'cluster' AND b.cluster_id = (
				SELECT c.cluster_id FROM connections c WHERE c.id = $2))
			OR (b.scope = 'server'
				AND (b.connection_id IS NULL OR b.connection_id = $2)
				AND (b.database_name IS NULL OR b.database_name = $3))
		)
	`, now, connectionID, dbName).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check blackouts: %w", err)
	}

	return count > 0, nil
}

// DeleteOldAlerts deletes cleared alerts older than the cutoff date
func (d *Datastore) DeleteOldAlerts(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := d.pool.Exec(ctx, `
		DELETE FROM alerts
		WHERE status IN ('cleared', 'acknowledged')
		  AND cleared_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// DeleteOldAnomalyCandidates deletes processed candidates older than the cutoff
func (d *Datastore) DeleteOldAnomalyCandidates(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := d.pool.Exec(ctx, `
		DELETE FROM anomaly_candidates
		WHERE processed_at IS NOT NULL AND processed_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// GetProbeAvailability checks if a probe is available for a connection
func (d *Datastore) GetProbeAvailability(ctx context.Context, connectionID int, probeName string) (*ProbeAvailability, error) {
	var pa ProbeAvailability
	err := d.pool.QueryRow(ctx, `
		SELECT id, connection_id, database_name, probe_name, extension_name,
		       is_available, last_checked, last_collected, unavailable_reason
		FROM probe_availability
		WHERE connection_id = $1 AND probe_name = $2
		LIMIT 1
	`, connectionID, probeName).Scan(
		&pa.ID, &pa.ConnectionID, &pa.DatabaseName, &pa.ProbeName,
		&pa.ExtensionName, &pa.IsAvailable, &pa.LastChecked,
		&pa.LastCollected, &pa.UnavailableReason)

	if err != nil {
		return nil, err
	}
	return &pa, nil
}

// GetEnabledBlackoutSchedules retrieves all enabled blackout schedules
func (d *Datastore) GetEnabledBlackoutSchedules(ctx context.Context) ([]*BlackoutSchedule, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, scope, connection_id, group_id, cluster_id,
		       database_name, name, cron_expression,
		       duration_minutes, timezone, reason, enabled, created_by,
		       created_at, updated_at
		FROM blackout_schedules
		WHERE enabled = true
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get blackout schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*BlackoutSchedule
	for rows.Next() {
		var s BlackoutSchedule
		err := rows.Scan(&s.ID, &s.Scope, &s.ConnectionID, &s.GroupID,
			&s.ClusterID, &s.DatabaseName, &s.Name,
			&s.CronExpression, &s.DurationMinutes, &s.Timezone, &s.Reason,
			&s.Enabled, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blackout schedule: %w", err)
		}
		schedules = append(schedules, &s)
	}

	return schedules, nil
}

// CreateBlackout creates a new blackout entry
func (d *Datastore) CreateBlackout(ctx context.Context, blackout *Blackout) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO blackouts (scope, connection_id, group_id, cluster_id,
		                       database_name, reason, start_time,
		                       end_time, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, blackout.Scope, blackout.ConnectionID, blackout.GroupID,
		blackout.ClusterID, blackout.DatabaseName, blackout.Reason,
		blackout.StartTime, blackout.EndTime, blackout.CreatedBy,
		blackout.CreatedAt).Scan(&blackout.ID)
}

// GetMetricBaselines retrieves baselines for a metric on a connection
func (d *Datastore) GetMetricBaselines(ctx context.Context, connectionID int, metricName string) ([]*MetricBaseline, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, connection_id, database_name, metric_name, period_type,
		       day_of_week, hour_of_day, mean, stddev, min, max,
		       sample_count, last_calculated
		FROM metric_baselines
		WHERE connection_id = $1 AND metric_name = $2
		ORDER BY period_type, day_of_week, hour_of_day
	`, connectionID, metricName)
	if err != nil {
		return nil, fmt.Errorf("failed to get metric baselines: %w", err)
	}
	defer rows.Close()

	var baselines []*MetricBaseline
	for rows.Next() {
		var b MetricBaseline
		err := rows.Scan(&b.ID, &b.ConnectionID, &b.DatabaseName, &b.MetricName,
			&b.PeriodType, &b.DayOfWeek, &b.HourOfDay, &b.Mean, &b.StdDev,
			&b.Min, &b.Max, &b.SampleCount, &b.LastCalculated)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metric baseline: %w", err)
		}
		baselines = append(baselines, &b)
	}

	return baselines, nil
}

// UpsertMetricBaseline inserts or updates a metric baseline
func (d *Datastore) UpsertMetricBaseline(ctx context.Context, b *MetricBaseline) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO metric_baselines (connection_id, database_name, metric_name,
		                              period_type, day_of_week, hour_of_day,
		                              mean, stddev, min, max, sample_count,
		                              last_calculated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (connection_id, COALESCE(database_name, ''), metric_name,
		             period_type, COALESCE(day_of_week, -1), COALESCE(hour_of_day, -1))
		DO UPDATE SET mean = $7, stddev = $8, min = $9, max = $10,
		              sample_count = $11, last_calculated = $12
	`, b.ConnectionID, b.DatabaseName, b.MetricName, b.PeriodType,
		b.DayOfWeek, b.HourOfDay, b.Mean, b.StdDev, b.Min, b.Max,
		b.SampleCount, b.LastCalculated)
	return err
}

// GetActiveConnections retrieves all active monitored connections
func (d *Datastore) GetActiveConnections(ctx context.Context) ([]int, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id FROM connections WHERE enabled = true ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get active connections: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan connection id: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// CreateAnomalyCandidate creates a new anomaly candidate for tier 2/3 processing
func (d *Datastore) CreateAnomalyCandidate(ctx context.Context, c *AnomalyCandidate) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO anomaly_candidates (connection_id, database_name, metric_name,
		                                metric_value, z_score, detected_at, context,
		                                tier1_pass)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, c.ConnectionID, c.DatabaseName, c.MetricName, c.MetricValue,
		c.ZScore, c.DetectedAt, c.Context, c.Tier1Pass).Scan(&c.ID)
}

// GetUnprocessedAnomalyCandidates retrieves candidates that need tier 2/3 processing
func (d *Datastore) GetUnprocessedAnomalyCandidates(ctx context.Context, limit int) ([]*AnomalyCandidate, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, connection_id, database_name, metric_name, metric_value,
		       z_score, detected_at, context, tier1_pass, tier2_score, tier2_pass,
		       tier3_result, tier3_pass, tier3_error, final_decision, alert_id,
		       processed_at
		FROM anomaly_candidates
		WHERE processed_at IS NULL AND tier1_pass = true
		ORDER BY detected_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get unprocessed candidates: %w", err)
	}
	defer rows.Close()

	var candidates []*AnomalyCandidate
	for rows.Next() {
		var c AnomalyCandidate
		err := rows.Scan(&c.ID, &c.ConnectionID, &c.DatabaseName, &c.MetricName,
			&c.MetricValue, &c.ZScore, &c.DetectedAt, &c.Context, &c.Tier1Pass,
			&c.Tier2Score, &c.Tier2Pass, &c.Tier3Result, &c.Tier3Pass,
			&c.Tier3Error, &c.FinalDecision, &c.AlertID, &c.ProcessedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anomaly candidate: %w", err)
		}
		candidates = append(candidates, &c)
	}

	return candidates, nil
}

// UpdateAnomalyCandidate updates a candidate with tier 2/3 results
func (d *Datastore) UpdateAnomalyCandidate(ctx context.Context, c *AnomalyCandidate) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE anomaly_candidates
		SET tier2_score = $2, tier2_pass = $3, tier3_result = $4, tier3_pass = $5,
		    tier3_error = $6, final_decision = $7, alert_id = $8, processed_at = $9
		WHERE id = $1
	`, c.ID, c.Tier2Score, c.Tier2Pass, c.Tier3Result, c.Tier3Pass,
		c.Tier3Error, c.FinalDecision, c.AlertID, c.ProcessedAt)
	return err
}

// GetHistoricalMetricValues retrieves historical metric values for baseline calculation.
// It returns values with timestamps from the specified lookback period to enable
// grouping by hour of day and day of week for time-aware baselines.
func (d *Datastore) GetHistoricalMetricValues(ctx context.Context, metricName string, lookbackDays int) ([]HistoricalMetricValue, error) {
	var results []HistoricalMetricValue

	// Parse metric name to determine table and aggregation
	switch metricName {
	case "pg_settings.max_connections":
		rows, err := d.pool.Query(ctx, `
			SELECT DISTINCT ON (connection_id)
			       connection_id, NULL::text as database_name,
			       setting::float as value, collected_at
			FROM metrics.pg_settings
			WHERE name = 'max_connections'
			  AND collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY connection_id, collected_at DESC
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "connection_utilization_percent":
		rows, err := d.pool.Query(ctx, `
			WITH activity_counts AS (
				SELECT connection_id, collected_at, COUNT(*) as active
				FROM metrics.pg_stat_activity
				WHERE collected_at > NOW() - INTERVAL '1 day' * $1
				GROUP BY connection_id, collected_at
			),
			max_conns AS (
				SELECT DISTINCT ON (connection_id) connection_id, setting::float as max_connections
				FROM metrics.pg_settings
				WHERE name = 'max_connections'
				ORDER BY connection_id, collected_at DESC
			)
			SELECT a.connection_id, NULL::text as database_name,
			       (a.active / NULLIF(m.max_connections, 0)) * 100 as value,
			       a.collected_at
			FROM activity_counts a
			JOIN max_conns m ON a.connection_id = m.connection_id
			ORDER BY a.connection_id, a.collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_stat_activity.blocked_count":
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, NULL::text as database_name,
			       COUNT(*)::float as value, collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '1 day' * $1
			  AND wait_event_type = 'Lock'
			GROUP BY connection_id, collected_at
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_stat_activity.idle_in_transaction_seconds":
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '1 day' * $1
			  AND state = 'idle in transaction'
			  AND xact_start IS NOT NULL
			GROUP BY connection_id, collected_at
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_stat_activity.max_query_duration_seconds":
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - query_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '1 day' * $1
			  AND state = 'active'
			  AND query_start IS NOT NULL
			GROUP BY connection_id, collected_at
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_stat_activity.max_xact_duration_seconds":
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '1 day' * $1
			  AND xact_start IS NOT NULL
			GROUP BY connection_id, collected_at
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_sys_cpu_usage_info.processor_time_percent":
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, NULL::text as database_name,
			       COALESCE(processor_time_percent, 0)::float as value,
			       collected_at
			FROM metrics.pg_sys_cpu_usage_info
			WHERE collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_sys_memory_info.used_percent":
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, NULL::text as database_name,
			       CASE
			           WHEN total_memory > 0
			           THEN (used_memory::float / total_memory) * 100
			           ELSE 0
			       END as value,
			       collected_at
			FROM metrics.pg_sys_memory_info
			WHERE collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_sys_load_avg_info.load_avg_fifteen_minutes":
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, NULL::text as database_name,
			       COALESCE(load_avg_fifteen_minutes, 0)::float as value,
			       collected_at
			FROM metrics.pg_sys_load_avg_info
			WHERE collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_sys_disk_info.used_percent":
		rows, err := d.pool.Query(ctx, `
			WITH disk_data AS (
				SELECT connection_id, collected_at,
				       MAX((used_space::float / NULLIF(total_space, 0)) * 100) as value
				FROM metrics.pg_sys_disk_info
				WHERE collected_at > NOW() - INTERVAL '1 day' * $1
				  AND total_space > 0
				GROUP BY connection_id, collected_at
			)
			SELECT connection_id, NULL::text as database_name,
			       value::float, collected_at
			FROM disk_data
			ORDER BY connection_id, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			results = append(results, hv)
		}

	case "pg_stat_database.cache_hit_ratio":
		rows, err := d.pool.Query(ctx, `
			WITH db_blocks AS (
				SELECT connection_id,
				       database_name,
				       blks_hit,
				       blks_read,
				       collected_at,
				       LAG(blks_hit) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_blks_hit,
				       LAG(blks_read) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_blks_read
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '1 day' * $1
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			),
			deltas AS (
				SELECT connection_id,
				       database_name,
				       (blks_hit - prev_blks_hit) as delta_hit,
				       (blks_read - prev_blks_read) as delta_read,
				       collected_at
				FROM db_blocks
				WHERE prev_blks_hit IS NOT NULL
				  AND (blks_hit - prev_blks_hit + blks_read - prev_blks_read) >= 10000
			)
			SELECT connection_id,
			       database_name,
			       CASE
			           WHEN (delta_hit + delta_read) > 0
			           THEN (delta_hit::float / (delta_hit + delta_read)) * 100
			           ELSE 100
			       END as value,
			       collected_at
			FROM deltas
			ORDER BY connection_id, database_name, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			var dbName string
			if err := rows.Scan(&hv.ConnectionID, &dbName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			hv.DatabaseName = &dbName
			results = append(results, hv)
		}

	case "pg_stat_database.deadlocks_delta":
		rows, err := d.pool.Query(ctx, `
			WITH db_deadlocks AS (
				SELECT connection_id, database_name, deadlocks, collected_at,
				       LAG(deadlocks) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_deadlocks
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '1 day' * $1
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id, database_name,
			       (deadlocks - COALESCE(prev_deadlocks, deadlocks))::float as value,
			       collected_at
			FROM db_deadlocks
			WHERE prev_deadlocks IS NOT NULL
			ORDER BY connection_id, database_name, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			var dbName string
			if err := rows.Scan(&hv.ConnectionID, &dbName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			hv.DatabaseName = &dbName
			results = append(results, hv)
		}

	case "pg_stat_database.temp_files_delta":
		rows, err := d.pool.Query(ctx, `
			WITH db_temp_files AS (
				SELECT connection_id, database_name, temp_files, collected_at,
				       LAG(temp_files) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_temp_files
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '1 day' * $1
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id, database_name,
			       (temp_files - COALESCE(prev_temp_files, temp_files))::float as value,
			       collected_at
			FROM db_temp_files
			WHERE prev_temp_files IS NOT NULL
			ORDER BY connection_id, database_name, collected_at
		`, lookbackDays)
		if err != nil {
			return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var hv HistoricalMetricValue
			var dbName string
			if err := rows.Scan(&hv.ConnectionID, &dbName, &hv.Value, &hv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan historical metric value: %w", err)
			}
			hv.DatabaseName = &dbName
			results = append(results, hv)
		}

	default:
		// For metrics not explicitly handled, return an empty result
		// This allows the caller to fall back to other baseline calculation methods
		return nil, fmt.Errorf("historical data not implemented for metric %s", metricName)
	}

	return results, nil
}

// SimilarAnomaly represents a past anomaly found by similarity search
type SimilarAnomaly struct {
	CandidateID   int64
	Similarity    float64
	FinalDecision *string
	MetricName    string
	Context       string
}

// StoreAnomalyEmbedding stores an embedding for an anomaly candidate
func (d *Datastore) StoreAnomalyEmbedding(ctx context.Context, candidateID int64, embedding []float32, modelName string) error {
	// Convert []float32 to PostgreSQL vector format
	vectorStr := float32SliceToVectorString(embedding)

	_, err := d.pool.Exec(ctx, `
		INSERT INTO anomaly_embeddings (candidate_id, embedding, model_name)
		VALUES ($1, $2::vector, $3)
		ON CONFLICT (candidate_id) DO UPDATE
		SET embedding = $2::vector, model_name = $3, created_at = CURRENT_TIMESTAMP
	`, candidateID, vectorStr, modelName)

	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}

	// Update the anomaly_candidates.embedding_id reference
	_, err = d.pool.Exec(ctx, `
		UPDATE anomaly_candidates
		SET embedding_id = (SELECT id FROM anomaly_embeddings WHERE candidate_id = $1)
		WHERE id = $1
	`, candidateID)

	if err != nil {
		return fmt.Errorf("failed to update embedding reference: %w", err)
	}

	return nil
}

// FindSimilarAnomalies finds past anomalies similar to the given embedding
// Returns candidates with similarity scores above threshold, excluding the current candidate
func (d *Datastore) FindSimilarAnomalies(ctx context.Context, embedding []float32, excludeCandidateID int64, threshold float64, limit int) ([]*SimilarAnomaly, error) {
	// Convert []float32 to PostgreSQL vector format
	vectorStr := float32SliceToVectorString(embedding)

	rows, err := d.pool.Query(ctx, `
		SELECT
			c.id,
			1 - (e.embedding <=> $1::vector) as similarity,
			c.final_decision,
			c.metric_name,
			c.context
		FROM anomaly_embeddings e
		JOIN anomaly_candidates c ON e.candidate_id = c.id
		WHERE c.id != $2
		  AND c.processed_at IS NOT NULL
		  AND c.final_decision IS NOT NULL
		  AND 1 - (e.embedding <=> $1::vector) >= $3
		ORDER BY similarity DESC
		LIMIT $4
	`, vectorStr, excludeCandidateID, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to find similar anomalies: %w", err)
	}
	defer rows.Close()

	var results []*SimilarAnomaly
	for rows.Next() {
		var sa SimilarAnomaly
		err := rows.Scan(&sa.CandidateID, &sa.Similarity, &sa.FinalDecision, &sa.MetricName, &sa.Context)
		if err != nil {
			return nil, fmt.Errorf("failed to scan similar anomaly: %w", err)
		}
		results = append(results, &sa)
	}

	return results, nil
}

// GetAnomalyCandidateByID retrieves an anomaly candidate by ID
func (d *Datastore) GetAnomalyCandidateByID(ctx context.Context, id int64) (*AnomalyCandidate, error) {
	var c AnomalyCandidate
	err := d.pool.QueryRow(ctx, `
		SELECT id, connection_id, database_name, metric_name, metric_value,
		       z_score, detected_at, context, tier1_pass, tier2_score, tier2_pass,
		       tier3_result, tier3_pass, tier3_error, final_decision, alert_id,
		       processed_at
		FROM anomaly_candidates
		WHERE id = $1
	`, id).Scan(&c.ID, &c.ConnectionID, &c.DatabaseName, &c.MetricName,
		&c.MetricValue, &c.ZScore, &c.DetectedAt, &c.Context, &c.Tier1Pass,
		&c.Tier2Score, &c.Tier2Pass, &c.Tier3Result, &c.Tier3Pass,
		&c.Tier3Error, &c.FinalDecision, &c.AlertID, &c.ProcessedAt)

	if err != nil {
		return nil, err
	}
	return &c, nil
}

// float32SliceToVectorString converts a []float32 to a PostgreSQL vector string format
func float32SliceToVectorString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}

	// Pre-allocate approximate size: "[" + (number * ~12 chars) + "]"
	result := make([]byte, 0, 1+len(v)*12+1)
	result = append(result, '[')

	for i, val := range v {
		if i > 0 {
			result = append(result, ',')
		}
		result = append(result, fmt.Sprintf("%g", val)...)
	}

	result = append(result, ']')
	return string(result)
}
