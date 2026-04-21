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

	"github.com/jackc/pgx/v5"
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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
		if err == pgx.ErrNoRows {
			return 0, "", false, nil
		}
		return 0, "", false, fmt.Errorf("failed to get active connection alert: %w", err)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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
		  AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL) OR database_name IS NULL)
		ORDER BY database_name IS NULL ASC
		LIMIT 1
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

// queryMetricValues executes a SQL query that returns rows with three columns
// (connection_id, value, collected_at) and scans them into MetricValue structs.
func (d *Datastore) queryMetricValues(ctx context.Context, sql string) ([]MetricValue, error) {
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MetricValue
	for rows.Next() {
		var mv MetricValue
		if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
			return nil, err
		}
		results = append(results, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// queryMetricValuesWithDB executes a SQL query that returns rows with four columns
// (connection_id, database_name, value, collected_at) and scans them into MetricValue structs.
func (d *Datastore) queryMetricValuesWithDB(ctx context.Context, sql string) ([]MetricValue, error) {
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MetricValue
	for rows.Next() {
		var mv MetricValue
		var dbName string
		if err := rows.Scan(&mv.ConnectionID, &dbName, &mv.Value, &mv.CollectedAt); err != nil {
			return nil, err
		}
		mv.DatabaseName = &dbName
		results = append(results, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// queryMetricValuesWithDBAndObject executes a SQL query that returns rows with five columns
// (connection_id, database_name, object_name, value, collected_at) and scans them into MetricValue structs.
func (d *Datastore) queryMetricValuesWithDBAndObject(ctx context.Context, sql string) ([]MetricValue, error) {
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MetricValue
	for rows.Next() {
		var mv MetricValue
		var dbName string
		var objectName string
		if err := rows.Scan(&mv.ConnectionID, &dbName, &objectName, &mv.Value, &mv.CollectedAt); err != nil {
			return nil, err
		}
		mv.DatabaseName = &dbName
		mv.ObjectName = &objectName
		results = append(results, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// GetLatestMetricValues retrieves the most recent values for a metric across all connections
// This queries the collected data tables to find current metric values
func (d *Datastore) GetLatestMetricValues(ctx context.Context, metricName string) ([]MetricValue, error) {
	var results []MetricValue
	var err error

	// Parse metric name to determine table and column/aggregation
	// Format: table_name.column_name or computed_metric_name
	switch metricName {
	case "pg_settings.max_connections":
		results, err = d.queryMetricValues(ctx, `
			SELECT DISTINCT ON (connection_id)
			       connection_id, setting::float as value, collected_at
			FROM metrics.pg_settings
			WHERE name = 'max_connections'
			  AND collected_at > NOW() - INTERVAL '1 hour'
			ORDER BY connection_id, collected_at DESC
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "connection_utilization_percent":
		results, err = d.queryMetricValues(ctx, `
			WITH active_counts AS (
				SELECT connection_id, COUNT(*) as active
				FROM metrics.pg_stat_activity
				WHERE backend_type = 'client backend'
				  AND (connection_id, collected_at) IN (
				      SELECT connection_id, MAX(collected_at)
				      FROM metrics.pg_stat_activity
				      WHERE collected_at > NOW() - INTERVAL '5 minutes'
				      GROUP BY connection_id
				  )
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_activity.count":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COUNT(*)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_replication.replay_lag_seconds":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       EXTRACT(EPOCH FROM (NOW() - replay_lsn_timestamp))::float as value,
			       collected_at
			FROM metrics.pg_stat_replication
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND replay_lsn_timestamp IS NOT NULL
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_replication.lag_bytes":
		results, err = d.queryMetricValues(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_replication_slots.retained_bytes":
		results, err = d.queryMetricValues(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_replication_slots.inactive":
		results, err = d.queryMetricValues(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_replication.standby_disconnected":
		results, err = d.queryMetricValues(ctx, `
			WITH recent_standby AS (
				SELECT connection_id,
				       receiver_status,
				       collected_at,
				       ROW_NUMBER() OVER (PARTITION BY connection_id ORDER BY collected_at DESC) as rn
				FROM metrics.pg_stat_replication
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				  AND role = 'standby'
			)
			SELECT connection_id,
			       CASE WHEN receiver_status IS NULL THEN 1 ELSE 0 END::float as value,
			       collected_at
			FROM recent_standby
			WHERE rn = 1
			  AND receiver_status IS NULL
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_node_role.subscription_worker_down":
		results, err = d.queryMetricValues(ctx, `
			WITH recent_node_role AS (
				SELECT connection_id,
				       subscription_count,
				       active_subscription_count,
				       collected_at,
				       ROW_NUMBER() OVER (PARTITION BY connection_id ORDER BY collected_at DESC) as rn
				FROM metrics.pg_node_role
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				  AND subscription_count > 0
			)
			SELECT connection_id,
			       1::float as value,
			       collected_at
			FROM recent_node_role
			WHERE rn = 1
			  AND active_subscription_count < subscription_count
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_activity.blocked_count":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COUNT(*)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_activity.idle_in_transaction_seconds":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE state = 'idle in transaction'
			  AND xact_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_activity.max_lock_wait_seconds":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - query_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND query_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_activity.max_query_duration_seconds":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - query_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE state = 'active'
			  AND query_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_activity.max_xact_duration_seconds":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE xact_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_all_tables.dead_tuple_percent":
		results, err = d.queryMetricValuesWithDBAndObject(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_archiver.failed_count_delta":
		results, err = d.queryMetricValues(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_checkpointer.checkpoints_req_delta":
		results, err = d.queryMetricValues(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_database.cache_hit_ratio":
		results, err = d.queryMetricValuesWithDB(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_database.deadlocks_delta":
		results, err = d.queryMetricValuesWithDB(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_database.temp_files_delta":
		results, err = d.queryMetricValuesWithDB(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_stat_statements.slow_query_count":
		results, err = d.queryMetricValuesWithDB(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_sys_cpu_usage_info.processor_time_percent":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COALESCE(processor_time_percent, 0)::float as value,
			       collected_at
			FROM (
			    SELECT DISTINCT ON (connection_id)
			           connection_id, processor_time_percent, collected_at
			    FROM metrics.pg_sys_cpu_usage_info
			    WHERE collected_at > NOW() - INTERVAL '15 minutes'
			    ORDER BY connection_id, collected_at DESC
			) latest
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_sys_disk_info.used_percent":
		results, err = d.queryMetricValues(ctx, `
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_sys_load_avg_info.load_avg_fifteen_minutes":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       COALESCE(load_avg_fifteen_minutes, 0)::float as value,
			       collected_at
			FROM (
			    SELECT DISTINCT ON (connection_id)
			           connection_id, load_avg_fifteen_minutes, collected_at
			    FROM metrics.pg_sys_load_avg_info
			    WHERE collected_at > NOW() - INTERVAL '15 minutes'
			    ORDER BY connection_id, collected_at DESC
			) latest
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "pg_sys_memory_info.used_percent":
		results, err = d.queryMetricValues(ctx, `
			SELECT connection_id,
			       CASE
			           WHEN total_memory > 0
			           THEN (used_memory::float / total_memory) * 100
			           ELSE 0
			       END as value,
			       collected_at
			FROM (
			    SELECT DISTINCT ON (connection_id)
			           connection_id, total_memory, used_memory, collected_at
			    FROM metrics.pg_sys_memory_info
			    WHERE collected_at > NOW() - INTERVAL '15 minutes'
			    ORDER BY connection_id, collected_at DESC
			) latest
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "age_percent":
		results, err = d.queryMetricValues(ctx, `
			WITH freeze_settings AS (
				SELECT DISTINCT ON (connection_id)
				       connection_id,
				       setting::bigint as freeze_max_age
				FROM metrics.pg_settings
				WHERE name = 'autovacuum_freeze_max_age'
				  AND collected_at > NOW() - INTERVAL '1 hour'
				ORDER BY connection_id, collected_at DESC
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "table_bloat_ratio":
		results, err = d.queryMetricValuesWithDBAndObject(ctx, `
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
				  AND n_live_tup >= 1000
				  AND schemaname NOT IN ('pg_catalog', 'pg_toast', 'information_schema')
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
		}

	case "table_last_autovacuum_hours":
		results, err = d.queryMetricValuesWithDBAndObject(ctx, `
			WITH recent_tables AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       n_live_tup,
				       n_dead_tup,
				       last_autovacuum,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, schemaname, relname
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND schemaname NOT IN ('pg_catalog', 'pg_toast', 'information_schema')
			),
			av_settings AS (
				SELECT DISTINCT ON (connection_id)
				       connection_id,
				       MAX(CASE WHEN name = 'autovacuum_vacuum_threshold'
				           THEN setting::float ELSE NULL END) as av_threshold,
				       MAX(CASE WHEN name = 'autovacuum_vacuum_scale_factor'
				           THEN setting::float ELSE NULL END) as av_scale_factor
				FROM metrics.pg_settings
				WHERE name IN ('autovacuum_vacuum_threshold', 'autovacuum_vacuum_scale_factor')
				  AND collected_at > NOW() - INTERVAL '1 hour'
				GROUP BY connection_id
			),
			exceeding AS (
				SELECT t.connection_id,
				       t.database_name,
				       t.schemaname,
				       t.relname,
				       t.n_dead_tup,
				       COALESCE(s.av_threshold, 50) + COALESCE(s.av_scale_factor, 0.2) * t.n_live_tup as calc_threshold,
				       EXTRACT(EPOCH FROM (NOW() - COALESCE(t.last_autovacuum, '1970-01-01'::timestamptz))) / 3600 as hours_since_vacuum,
				       t.collected_at
				FROM recent_tables t
				LEFT JOIN av_settings s ON t.connection_id = s.connection_id
				WHERE t.rn = 1
				  AND t.n_dead_tup > (COALESCE(s.av_threshold, 50) + COALESCE(s.av_scale_factor, 0.2) * t.n_live_tup)
			),
			ranked AS (
				SELECT *,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY hours_since_vacuum DESC
				       ) as rank
				FROM exceeding
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
			return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
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
		WHERE rule_id = $1 AND connection_id = $2 AND status IN ('active', 'acknowledged')
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
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &alert, nil
}

// GetActiveAnomalyAlert checks for an existing active anomaly alert for the
// given metric name, connection, and optional database name.
func (d *Datastore) GetActiveAnomalyAlert(ctx context.Context, metricName string, connectionID int, dbName *string) (*Alert, error) {
	var alert Alert
	err := d.pool.QueryRow(ctx, `
        SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
               probe_name, metric_name, metric_value, threshold_value, operator,
               severity, title, description, correlation_id, status, triggered_at,
               cleared_at, last_updated, anomaly_score, anomaly_details
        FROM alerts
        WHERE alert_type = 'anomaly' AND metric_name = $1 AND connection_id = $2
          AND status IN ('active', 'acknowledged')
          AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL))
        LIMIT 1
    `, metricName, connectionID, dbName).Scan(
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

// GetRecentlyClearedAlert checks if there's a recently cleared alert for the
// same rule, connection, and database within the cooldown period.
func (d *Datastore) GetRecentlyClearedAlert(ctx context.Context, ruleID int64, connectionID int, dbName *string, cooldown time.Duration) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM alerts
            WHERE rule_id = $1 AND connection_id = $2 AND status = 'cleared'
              AND cleared_at > NOW() - $3::interval
              AND (database_name = $4 OR ($4 IS NULL AND database_name IS NULL))
        )
    `, ruleID, connectionID, fmt.Sprintf("%d seconds", int(cooldown.Seconds())), dbName).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check recently cleared alert: %w", err)
	}
	return exists, nil
}

// GetReevaluationSuppressedAlert checks if a recently cleared anomaly alert
// for this metric+connection was cleared by the re-evaluation system (based on
// user feedback). These alerts are suppressed for longer to respect the user's
// assessment.
func (d *Datastore) GetReevaluationSuppressedAlert(ctx context.Context, metricName string, connectionID int, dbName *string, cooldown time.Duration) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM alerts
            WHERE alert_type = 'anomaly'
              AND metric_name = $1
              AND connection_id = $2
              AND status = 'cleared'
              AND reevaluation_count > 0
              AND cleared_at > NOW() - $3::interval
              AND (database_name = $4 OR ($4 IS NULL AND database_name IS NULL))
        )
    `, metricName, connectionID, fmt.Sprintf("%d seconds", int(cooldown.Seconds())), dbName).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check re-evaluation suppressed alert: %w", err)
	}
	return exists, nil
}

// GetFalsePositiveSuppressedAlert checks if there is an acknowledged anomaly
// alert for this metric+connection that was marked as a false positive within
// the suppression window. This prevents re-firing alerts that users have
// explicitly dismissed.
func (d *Datastore) GetFalsePositiveSuppressedAlert(ctx context.Context, metricName string, connectionID int, dbName *string, cooldown time.Duration) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM alerts a
            JOIN alert_acknowledgments ak ON ak.alert_id = a.id
            WHERE a.alert_type = 'anomaly'
              AND a.metric_name = $1
              AND a.connection_id = $2
              AND a.status = 'acknowledged'
              AND ak.false_positive = true
              AND ak.acknowledged_at > NOW() - $3::interval
              AND (a.database_name = $4 OR ($4 IS NULL AND a.database_name IS NULL))
        )
    `, metricName, connectionID, fmt.Sprintf("%d seconds", int(cooldown.Seconds())), dbName).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check false positive suppressed alert: %w", err)
	}
	return exists, nil
}

// UpdateAlertValues updates the metric_value, threshold, operator, severity,
// and last_updated timestamp for an active alert. AI analysis is cleared when
// the metric value changes.
func (d *Datastore) UpdateAlertValues(ctx context.Context, alertID int64, metricValue, thresholdValue float64, operator, severity string) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET metric_value = $2, last_updated = $3,
		    threshold_value = $4, operator = $5, severity = $6,
		    ai_analysis = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis END,
		    ai_analysis_metric_value = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis_metric_value END
		WHERE id = $1
	`, alertID, metricValue, time.Now(), thresholdValue, operator, severity)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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

// ReactivateAlert sets an acknowledged alert back to active status and
// clears any acknowledgment rows so the UI no longer shows the alert as
// acknowledged. This is used when the severity of a threshold alert
// changes while the alert is acknowledged, so the user sees the severity
// change. The update is gated on status = 'acknowledged' to avoid
// clobbering ack rows for alerts that are not currently acknowledged;
// the delete only runs when the update actually reactivated the alert.
// Both statements execute in a single transaction so the alert cannot be
// left in a half-reactivated state (active status with a stale ack row,
// or vice versa).
func (d *Datastore) ReactivateAlert(ctx context.Context, alertID int64) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:errcheck // Rollback is a no-op if the tx was already committed.
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE alerts
		SET status = 'active', last_updated = $1
		WHERE id = $2 AND status = 'acknowledged'
	`, time.Now(), alertID)
	if err != nil {
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	// Only clear acknowledgment rows when the UPDATE actually flipped an
	// alert back to active. A no-op UPDATE (alert not found, already
	// active, or cleared) must not delete ack history for unrelated
	// state.
	if result.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM alert_acknowledgments WHERE alert_id = $1
	`, alertID); err != nil {
		return fmt.Errorf("failed to clear alert acknowledgments: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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
//
// Every query branch INNER JOINs against the connections table so rows for
// connections that have been deleted (but whose metric rows have not yet aged
// out of the metrics.* tables) are filtered at query time. Without that
// filter, downstream UpsertMetricBaseline calls would fail with foreign key
// violations on metric_baselines.connection_id. See GitHub issue #56.
func (d *Datastore) GetHistoricalMetricValues(ctx context.Context, metricName string, lookbackDays int) ([]HistoricalMetricValue, error) {
	var results []HistoricalMetricValue

	// Parse metric name to determine table and aggregation
	switch metricName {
	case "pg_settings.max_connections":
		// INNER JOIN against connections filters out orphaned metric rows
		// whose connection_id no longer exists (e.g., after a connection
		// was deleted). Otherwise baseline upserts fail with FK violations.
		rows, err := d.pool.Query(ctx, `
			SELECT DISTINCT ON (ps.connection_id)
			       ps.connection_id, NULL::text as database_name,
			       ps.setting::float as value, ps.collected_at
			FROM metrics.pg_settings ps
			JOIN connections c ON c.id = ps.connection_id
			WHERE ps.name = 'max_connections'
			  AND ps.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY ps.connection_id, ps.collected_at DESC
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "connection_utilization_percent":
		// INNER JOIN against connections filters out orphaned metric rows
		// whose connection_id no longer exists in the connections table.
		rows, err := d.pool.Query(ctx, `
			WITH activity_counts AS (
				SELECT psa.connection_id, psa.collected_at, COUNT(*) as active
				FROM metrics.pg_stat_activity psa
				JOIN connections c ON c.id = psa.connection_id
				WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
				GROUP BY psa.connection_id, psa.collected_at
			),
			max_conns AS (
				SELECT DISTINCT ON (ps.connection_id) ps.connection_id, ps.setting::float as max_connections
				FROM metrics.pg_settings ps
				JOIN connections c ON c.id = ps.connection_id
				WHERE ps.name = 'max_connections'
				ORDER BY ps.connection_id, ps.collected_at DESC
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_activity.count":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT psa.connection_id, NULL::text as database_name,
			       COUNT(*)::float as value, psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_activity.blocked_count":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT psa.connection_id, NULL::text as database_name,
			       COUNT(*)::float as value, psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.wait_event_type = 'Lock'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_activity.idle_in_transaction_seconds":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.xact_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.state = 'idle in transaction'
			  AND psa.xact_start IS NOT NULL
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_activity.max_query_duration_seconds":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.query_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.state = 'active'
			  AND psa.query_start IS NOT NULL
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_activity.max_xact_duration_seconds":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.xact_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.xact_start IS NOT NULL
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_sys_cpu_usage_info.processor_time_percent":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT m.connection_id, NULL::text as database_name,
			       COALESCE(m.processor_time_percent, 0)::float as value,
			       m.collected_at
			FROM metrics.pg_sys_cpu_usage_info m
			JOIN connections c ON c.id = m.connection_id
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY m.connection_id, m.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_sys_memory_info.used_percent":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT m.connection_id, NULL::text as database_name,
			       CASE
			           WHEN m.total_memory > 0
			           THEN (m.used_memory::float / m.total_memory) * 100
			           ELSE 0
			       END as value,
			       m.collected_at
			FROM metrics.pg_sys_memory_info m
			JOIN connections c ON c.id = m.connection_id
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY m.connection_id, m.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_sys_load_avg_info.load_avg_fifteen_minutes":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			SELECT m.connection_id, NULL::text as database_name,
			       COALESCE(m.load_avg_fifteen_minutes, 0)::float as value,
			       m.collected_at
			FROM metrics.pg_sys_load_avg_info m
			JOIN connections c ON c.id = m.connection_id
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY m.connection_id, m.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_sys_disk_info.used_percent":
		// INNER JOIN against connections filters out orphaned metric rows.
		rows, err := d.pool.Query(ctx, `
			WITH disk_data AS (
				SELECT m.connection_id, m.collected_at,
				       MAX((m.used_space::float / NULLIF(m.total_space, 0)) * 100) as value
				FROM metrics.pg_sys_disk_info m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.total_space > 0
				GROUP BY m.connection_id, m.collected_at
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_database.cache_hit_ratio":
		// INNER JOIN against connections filters out orphaned metric rows
		// before computing window-function deltas, so rows whose
		// connection_id no longer exists never reach the baseline upsert.
		rows, err := d.pool.Query(ctx, `
			WITH db_blocks AS (
				SELECT m.connection_id,
				       m.database_name,
				       m.blks_hit,
				       m.blks_read,
				       m.collected_at,
				       LAG(m.blks_hit) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_blks_hit,
				       LAG(m.blks_read) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_blks_read
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_database.deadlocks_delta":
		// INNER JOIN against connections filters out orphaned metric rows
		// before computing window-function deltas.
		rows, err := d.pool.Query(ctx, `
			WITH db_deadlocks AS (
				SELECT m.connection_id, m.database_name, m.deadlocks, m.collected_at,
				       LAG(m.deadlocks) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_deadlocks
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
		}

	case "pg_stat_database.temp_files_delta":
		// INNER JOIN against connections filters out orphaned metric rows
		// before computing window-function deltas.
		rows, err := d.pool.Query(ctx, `
			WITH db_temp_files AS (
				SELECT m.connection_id, m.database_name, m.temp_files, m.collected_at,
				       LAG(m.temp_files) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_temp_files
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
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

		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("row iteration error: %w", err)
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
		ORDER BY e.embedding <=> $1::vector
		LIMIT $3
	`, vectorStr, excludeCandidateID, limit)
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
		// Apply similarity threshold filter in Go so the SQL query
		// can use the HNSW index via ORDER BY <=> without a WHERE
		// clause on the computed similarity value.
		if sa.Similarity >= threshold {
			results = append(results, &sa)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
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

// ProbeStaleness represents a probe's staleness ratio for a connection
type ProbeStaleness struct {
	ConnectionID       int
	ConnectionName     string
	ProbeName          string
	CollectionInterval int     // seconds
	StalenessRatio     float64 // elapsed / interval
}

// GetProbeStalenessByConnection retrieves staleness ratios for all enabled probes
// on monitored connections. The caller decides which entries exceed the threshold.
func (d *Datastore) GetProbeStalenessByConnection(ctx context.Context) ([]ProbeStaleness, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT c.id, c.name, pa.probe_name, pc.collection_interval_seconds,
		       EXTRACT(EPOCH FROM (NOW() - pa.last_collected)) / pc.collection_interval_seconds AS staleness_ratio
		FROM probe_availability pa
		JOIN probe_configs pc ON pc.name = pa.probe_name AND pc.connection_id IS NULL
		JOIN connections c ON c.id = pa.connection_id
		WHERE pa.is_available = TRUE
		  AND pc.is_enabled = TRUE
		  AND c.is_monitored = TRUE
		  AND pa.last_collected IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get probe staleness: %w", err)
	}
	defer rows.Close()

	var results []ProbeStaleness
	for rows.Next() {
		var ps ProbeStaleness
		if err := rows.Scan(&ps.ConnectionID, &ps.ConnectionName, &ps.ProbeName,
			&ps.CollectionInterval, &ps.StalenessRatio); err != nil {
			return nil, fmt.Errorf("failed to scan probe staleness: %w", err)
		}
		results = append(results, ps)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

// GetAlertRuleByName retrieves an alert rule by its unique name
func (d *Datastore) GetAlertRuleByName(ctx context.Context, name string) (*AlertRule, error) {
	var rule AlertRule
	err := d.pool.QueryRow(ctx, `
		SELECT id, name, description, category, metric_name, default_operator,
		       default_threshold, default_severity, default_enabled, required_extension,
		       is_built_in, created_at
		FROM alert_rules
		WHERE name = $1
	`, name).Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Category,
		&rule.MetricName, &rule.DefaultOperator, &rule.DefaultThreshold,
		&rule.DefaultSeverity, &rule.DefaultEnabled, &rule.RequiredExtension,
		&rule.IsBuiltIn, &rule.CreatedAt)

	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetAcknowledgedAnomalyAlerts retrieves acknowledged anomaly alerts that are
// due for re-evaluation. An alert is due if it has never been re-evaluated or
// if the last re-evaluation was longer ago than the specified interval.
func (d *Datastore) GetAcknowledgedAnomalyAlerts(ctx context.Context, intervalSeconds int, limit int) ([]*AcknowledgedAnomalyAlert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT a.id, a.connection_id, a.title, a.severity, a.metric_name,
		       a.metric_value, a.anomaly_score, a.anomaly_details, a.triggered_at,
		       ack.message, ack.false_positive, ack.acknowledged_by,
		       ack.acknowledged_at, a.last_reevaluated_at, a.reevaluation_count
		FROM alerts a
		LEFT JOIN LATERAL (
		    SELECT message, false_positive, acknowledged_by, acknowledged_at
		    FROM alert_acknowledgments
		    WHERE alert_id = a.id
		    ORDER BY acknowledged_at DESC
		    LIMIT 1
		) ack ON true
		WHERE a.status = 'acknowledged' AND a.alert_type = 'anomaly'
		  AND (a.last_reevaluated_at IS NULL
		       OR a.last_reevaluated_at < NOW() - INTERVAL '1 second' * $1)
		ORDER BY a.triggered_at ASC
		LIMIT $2
	`, intervalSeconds, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get acknowledged anomaly alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*AcknowledgedAnomalyAlert
	for rows.Next() {
		var a AcknowledgedAnomalyAlert
		err := rows.Scan(
			&a.ID, &a.ConnectionID, &a.Title, &a.Severity, &a.MetricName,
			&a.MetricValue, &a.ZScore, &a.AnomalyDetails, &a.TriggeredAt,
			&a.AckMessage, &a.FalsePositive, &a.AcknowledgedBy,
			&a.AcknowledgedAt, &a.LastReevaluatedAt, &a.ReevaluationCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan acknowledged anomaly alert: %w", err)
		}
		alerts = append(alerts, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// GetAcknowledgmentHistoryForMetric retrieves past acknowledgements for the
// same metric and connection across different alert instances. This reveals
// recurring patterns useful for LLM re-evaluation context.
func (d *Datastore) GetAcknowledgmentHistoryForMetric(ctx context.Context, metricName string, connectionID int, excludeAlertID int64, limit int) ([]*AcknowledgedAnomalyAlert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT a.id, a.connection_id, a.title, a.severity, a.metric_name,
		       a.metric_value, a.anomaly_score, a.anomaly_details, a.triggered_at,
		       ack.message, ack.false_positive, ack.acknowledged_by,
		       ack.acknowledged_at, a.last_reevaluated_at, a.reevaluation_count
		FROM alerts a
		JOIN alert_acknowledgments ack ON ack.alert_id = a.id
		WHERE a.metric_name = $1 AND a.connection_id = $2
		  AND a.id != $3 AND a.alert_type = 'anomaly'
		ORDER BY ack.acknowledged_at DESC
		LIMIT $4
	`, metricName, connectionID, excludeAlertID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get acknowledgment history: %w", err)
	}
	defer rows.Close()

	var alerts []*AcknowledgedAnomalyAlert
	for rows.Next() {
		var a AcknowledgedAnomalyAlert
		err := rows.Scan(
			&a.ID, &a.ConnectionID, &a.Title, &a.Severity, &a.MetricName,
			&a.MetricValue, &a.ZScore, &a.AnomalyDetails, &a.TriggeredAt,
			&a.AckMessage, &a.FalsePositive, &a.AcknowledgedBy,
			&a.AcknowledgedAt, &a.LastReevaluatedAt, &a.ReevaluationCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan acknowledgment history: %w", err)
		}
		alerts = append(alerts, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// GetClusterPeers returns information about other connections in the same
// cluster as the given connection, including their latest node role.
// Returns an empty slice if the connection has no cluster.
func (d *Datastore) GetClusterPeers(ctx context.Context, connectionID int) ([]*ClusterPeerInfo, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT c.id, c.name, COALESCE(lr.primary_role, 'unknown')
		FROM connections c
		LEFT JOIN LATERAL (
			SELECT primary_role
			FROM metrics.pg_node_role
			WHERE connection_id = c.id
			ORDER BY collected_at DESC
			LIMIT 1
		) lr ON true
		WHERE c.cluster_id = (SELECT cluster_id FROM connections WHERE id = $1)
		  AND c.cluster_id IS NOT NULL
		  AND c.id != $1
		ORDER BY c.name
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster peers: %w", err)
	}
	defer rows.Close()

	var peers []*ClusterPeerInfo
	for rows.Next() {
		var peer ClusterPeerInfo
		if err := rows.Scan(&peer.ConnectionID, &peer.ConnectionName, &peer.NodeRole); err != nil {
			return nil, fmt.Errorf("failed to scan cluster peer: %w", err)
		}
		peers = append(peers, &peer)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return peers, nil
}

// GetAlertsByCluster retrieves all active or acknowledged alerts across all
// connections in the same cluster as the given connection, excluding alerts
// from the given connection itself. Returns an empty slice if the connection
// has no cluster.
func (d *Datastore) GetAlertsByCluster(ctx context.Context, connectionID int) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE connection_id IN (
			SELECT id FROM connections
			WHERE cluster_id = (SELECT cluster_id FROM connections WHERE id = $1)
			  AND cluster_id IS NOT NULL
		)
		  AND connection_id != $1
		  AND status IN ('active', 'acknowledged')
		ORDER BY triggered_at DESC
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get alerts by cluster: %w", err)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// GetAlertsByConnection retrieves all active or acknowledged alerts for a
// given connection, ordered by triggered time descending.
func (d *Datastore) GetAlertsByConnection(ctx context.Context, connectionID int) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE connection_id = $1 AND status IN ('active', 'acknowledged')
		ORDER BY triggered_at DESC
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get alerts by connection: %w", err)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// UpdateAlertReevaluation increments the re-evaluation count and updates the
// last re-evaluated timestamp for an alert.
func (d *Datastore) UpdateAlertReevaluation(ctx context.Context, alertID int64) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET reevaluation_count = reevaluation_count + 1,
		    last_reevaluated_at = NOW()
		WHERE id = $1
	`, alertID)
	return err
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
