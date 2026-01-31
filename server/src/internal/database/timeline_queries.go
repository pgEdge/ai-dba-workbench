/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TimelineEvent represents a single event on the timeline
type TimelineEvent struct {
	ID           string          `json:"id"`
	EventType    string          `json:"event_type"`
	ConnectionID int             `json:"connection_id"`
	ServerName   string          `json:"server_name"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Severity     string          `json:"severity"`
	Title        string          `json:"title"`
	Summary      string          `json:"summary"`
	Details      json.RawMessage `json:"details,omitempty"`
}

// TimelineFilter holds filter options for querying timeline events
type TimelineFilter struct {
	ConnectionID  *int
	ConnectionIDs []int
	StartTime     time.Time
	EndTime       time.Time
	EventTypes    []string
	Limit         int
}

// TimelineResult holds the result of a timeline query
type TimelineResult struct {
	Events     []TimelineEvent `json:"events"`
	TotalCount int             `json:"total_count"`
}

// Event type constants
const (
	EventTypeConfigChange      = "config_change"
	EventTypeHBAChange         = "hba_change"
	EventTypeIdentChange       = "ident_change"
	EventTypeRestart           = "restart"
	EventTypeAlertFired        = "alert_fired"
	EventTypeAlertCleared      = "alert_cleared"
	EventTypeAlertAcknowledged = "alert_acknowledged"
	EventTypeExtensionChange   = "extension_change"
	EventTypeBlackoutStarted   = "blackout_started"
	EventTypeBlackoutEnded     = "blackout_ended"
)

// GetTimelineEvents retrieves timeline events matching the filter criteria
func (d *Datastore) GetTimelineEvents(ctx context.Context, filter TimelineFilter) (*TimelineResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Apply query timeout
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Set default limit
	limit := filter.Limit
	if limit <= 0 {
		limit = 500
	}

	// Build connection filter conditions
	connCondition, connArgs, argNum := buildConnectionFilter(filter)

	// Build time filter conditions
	timeCondition, timeArgs, argNum := buildTimeFilter(filter, argNum)

	// Note: Event type filtering is handled by including/excluding subqueries
	// in buildUnionQuery, not via SQL WHERE clause placeholders. We don't need
	// to build a typeCondition or add typeArgs to the query arguments.

	// Combine all conditions - create new slice to avoid modifying originals
	allArgs := make([]interface{}, 0, len(connArgs)+len(timeArgs))
	allArgs = append(allArgs, connArgs...)
	allArgs = append(allArgs, timeArgs...)

	// Build the UNION ALL query
	query := buildUnionQuery(connCondition, timeCondition, "", filter, limit, argNum)

	// Execute main query
	rows, err := d.pool.Query(queryCtx, query, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query timeline events: %w", err)
	}
	defer rows.Close()

	var events []TimelineEvent
	for rows.Next() {
		var event TimelineEvent
		var details *string
		err := rows.Scan(
			&event.ID, &event.EventType, &event.ConnectionID, &event.ServerName,
			&event.OccurredAt, &event.Severity, &event.Title, &event.Summary,
			&details,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan timeline event: %w", err)
		}
		if details != nil {
			event.Details = json.RawMessage(*details)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating timeline events: %w", err)
	}

	if events == nil {
		events = []TimelineEvent{}
	}

	// Get total count (without limit)
	countQuery := buildCountQuery(connCondition, timeCondition, "", filter)
	var totalCount int
	err = d.pool.QueryRow(queryCtx, countQuery, allArgs...).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count timeline events: %w", err)
	}

	return &TimelineResult{
		Events:     events,
		TotalCount: totalCount,
	}, nil
}

// buildConnectionFilter creates the connection filter portion of the WHERE clause
func buildConnectionFilter(filter TimelineFilter) (string, []interface{}, int) {
	var conditions []string
	var args []interface{}
	argNum := 1

	if filter.ConnectionID != nil {
		conditions = append(conditions, fmt.Sprintf("connection_id = $%d", argNum))
		args = append(args, *filter.ConnectionID)
		argNum++
	}

	if len(filter.ConnectionIDs) > 0 {
		placeholders := make([]string, len(filter.ConnectionIDs))
		for i, id := range filter.ConnectionIDs {
			placeholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, id)
			argNum++
		}
		conditions = append(conditions, fmt.Sprintf("connection_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	if len(conditions) == 0 {
		return "", args, argNum
	}

	return strings.Join(conditions, " AND "), args, argNum
}

// buildTimeFilter creates the time filter portion of the WHERE clause
func buildTimeFilter(filter TimelineFilter, startArgNum int) (string, []interface{}, int) {
	var conditions []string
	var args []interface{}
	argNum := startArgNum

	if !filter.StartTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("event_time >= $%d", argNum))
		args = append(args, filter.StartTime)
		argNum++
	}

	if !filter.EndTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("event_time <= $%d", argNum))
		args = append(args, filter.EndTime)
		argNum++
	}

	if len(conditions) == 0 {
		return "", args, argNum
	}

	return strings.Join(conditions, " AND "), args, argNum
}

// buildUnionQuery constructs the full UNION ALL query for timeline events
// Note: Event type filtering is handled by conditionally including/excluding
// subqueries based on filter.EventTypes, not via SQL WHERE clause placeholders.
func buildUnionQuery(connCondition, timeCondition, typeCondition string, filter TimelineFilter, limit int, argNum int) string {
	// Build WHERE clause for each subquery
	whereClause := buildWhereClause(connCondition, timeCondition, "")

	// Determine which event types to include
	includeTypes := make(map[string]bool)
	if len(filter.EventTypes) > 0 {
		for _, t := range filter.EventTypes {
			includeTypes[t] = true
		}
	} else {
		// Include all types by default
		includeTypes[EventTypeConfigChange] = true
		includeTypes[EventTypeHBAChange] = true
		includeTypes[EventTypeIdentChange] = true
		includeTypes[EventTypeRestart] = true
		includeTypes[EventTypeAlertFired] = true
		includeTypes[EventTypeAlertCleared] = true
		includeTypes[EventTypeAlertAcknowledged] = true
		includeTypes[EventTypeExtensionChange] = true
		includeTypes[EventTypeBlackoutStarted] = true
		includeTypes[EventTypeBlackoutEnded] = true
	}

	var subqueries []string

	// Configuration changes from pg_settings
	if includeTypes[EventTypeConfigChange] {
		subqueries = append(subqueries, buildConfigChangeQuery(whereClause))
	}

	// HBA changes from pg_hba_file_rules
	if includeTypes[EventTypeHBAChange] {
		subqueries = append(subqueries, buildHBAChangeQuery(whereClause))
	}

	// Ident changes from pg_ident_file_mappings
	if includeTypes[EventTypeIdentChange] {
		subqueries = append(subqueries, buildIdentChangeQuery(whereClause))
	}

	// Server restarts from pg_node_role (timeline_id changes)
	if includeTypes[EventTypeRestart] {
		subqueries = append(subqueries, buildRestartQuery(whereClause))
	}

	// Alerts fired
	if includeTypes[EventTypeAlertFired] {
		subqueries = append(subqueries, buildAlertFiredQuery(whereClause))
	}

	// Alerts cleared
	if includeTypes[EventTypeAlertCleared] {
		subqueries = append(subqueries, buildAlertClearedQuery(whereClause))
	}

	// Alerts acknowledged
	if includeTypes[EventTypeAlertAcknowledged] {
		subqueries = append(subqueries, buildAlertAcknowledgedQuery(whereClause))
	}

	// Extension changes from pg_server_info
	if includeTypes[EventTypeExtensionChange] {
		subqueries = append(subqueries, buildExtensionChangeQuery(whereClause))
	}

	// Blackout started
	if includeTypes[EventTypeBlackoutStarted] {
		subqueries = append(subqueries, buildBlackoutStartedQuery(whereClause))
	}

	// Blackout ended
	if includeTypes[EventTypeBlackoutEnded] {
		subqueries = append(subqueries, buildBlackoutEndedQuery(whereClause))
	}

	if len(subqueries) == 0 {
		// Return an empty result query if no types are selected
		return `SELECT '' AS id, '' AS event_type, 0 AS connection_id, '' AS server_name,
                       NOW() AS event_time, '' AS severity, '' AS title, '' AS summary,
                       NULL::TEXT AS details WHERE FALSE`
	}

	// Combine with UNION ALL, order by timestamp DESC, and apply limit
	query := fmt.Sprintf(`
        WITH timeline_events AS (
            %s
        )
        SELECT id, event_type, connection_id, server_name, event_time, severity, title, summary, details
        FROM timeline_events
        ORDER BY event_time DESC
        LIMIT %d
    `, strings.Join(subqueries, "\n        UNION ALL\n        "), limit)

	return query
}

// buildCountQuery constructs the count query for total events
func buildCountQuery(connCondition, timeCondition, typeCondition string, filter TimelineFilter) string {
	whereClause := buildWhereClause(connCondition, timeCondition, "")

	// Determine which event types to include
	includeTypes := make(map[string]bool)
	if len(filter.EventTypes) > 0 {
		for _, t := range filter.EventTypes {
			includeTypes[t] = true
		}
	} else {
		includeTypes[EventTypeConfigChange] = true
		includeTypes[EventTypeHBAChange] = true
		includeTypes[EventTypeIdentChange] = true
		includeTypes[EventTypeRestart] = true
		includeTypes[EventTypeAlertFired] = true
		includeTypes[EventTypeAlertCleared] = true
		includeTypes[EventTypeAlertAcknowledged] = true
		includeTypes[EventTypeExtensionChange] = true
		includeTypes[EventTypeBlackoutStarted] = true
		includeTypes[EventTypeBlackoutEnded] = true
	}

	var countQueries []string

	if includeTypes[EventTypeConfigChange] {
		countQueries = append(countQueries, buildConfigChangeCountQuery(whereClause))
	}
	if includeTypes[EventTypeHBAChange] {
		countQueries = append(countQueries, buildHBAChangeCountQuery(whereClause))
	}
	if includeTypes[EventTypeIdentChange] {
		countQueries = append(countQueries, buildIdentChangeCountQuery(whereClause))
	}
	if includeTypes[EventTypeRestart] {
		countQueries = append(countQueries, buildRestartCountQuery(whereClause))
	}
	if includeTypes[EventTypeAlertFired] {
		countQueries = append(countQueries, buildAlertFiredCountQuery(whereClause))
	}
	if includeTypes[EventTypeAlertCleared] {
		countQueries = append(countQueries, buildAlertClearedCountQuery(whereClause))
	}
	if includeTypes[EventTypeAlertAcknowledged] {
		countQueries = append(countQueries, buildAlertAcknowledgedCountQuery(whereClause))
	}
	if includeTypes[EventTypeExtensionChange] {
		countQueries = append(countQueries, buildExtensionChangeCountQuery(whereClause))
	}
	if includeTypes[EventTypeBlackoutStarted] {
		countQueries = append(countQueries, buildBlackoutStartedCountQuery(whereClause))
	}
	if includeTypes[EventTypeBlackoutEnded] {
		countQueries = append(countQueries, buildBlackoutEndedCountQuery(whereClause))
	}

	if len(countQueries) == 0 {
		return "SELECT 0"
	}

	// Wrap each count subquery in parentheses for the addition
	for i, q := range countQueries {
		countQueries[i] = "(" + strings.TrimSpace(q) + ")"
	}

	return fmt.Sprintf("SELECT %s AS total_count", strings.Join(countQueries, " + "))
}

// buildWhereClause combines conditions into a WHERE clause
func buildWhereClause(connCondition, timeCondition, extraCondition string) string {
	var conditions []string
	if connCondition != "" {
		conditions = append(conditions, connCondition)
	}
	if timeCondition != "" {
		conditions = append(conditions, timeCondition)
	}
	if extraCondition != "" {
		conditions = append(conditions, extraCondition)
	}
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

// buildConfigChangeQuery creates the subquery for configuration changes
// Excludes initial collection (first snapshot) since that's not really a "change"
func buildConfigChangeQuery(whereClause string) string {
	// Replace generic column names with actual column names for this table
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "s.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "s.connection_id")

	return fmt.Sprintf(`
        SELECT
            'config-' || s.connection_id || '-' || s.collected_at::TEXT AS id,
            '%s' AS event_type,
            s.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            s.collected_at AS event_time,
            'info' AS severity,
            'Configuration Changed' AS title,
            'Updated ' || COUNT(*) || ' PostgreSQL settings' AS summary,
            jsonb_build_object(
                'setting_count', COUNT(*),
                'settings', jsonb_agg(jsonb_build_object(
                    'name', s.name,
                    'value', s.setting,
                    'source', s.source
                ) ORDER BY s.name) FILTER (WHERE s.name IS NOT NULL)
            )::TEXT AS details
        FROM metrics.pg_settings s
        JOIN connections c ON s.connection_id = c.id
        %s
        AND s.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_settings
            WHERE connection_id = s.connection_id
        )
        GROUP BY s.connection_id, s.collected_at, c.name
    `, EventTypeConfigChange, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return tableWhere
	}())
}

// buildConfigChangeCountQuery creates the count query for configuration changes
// Excludes initial collection to match the main query
func buildConfigChangeCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")

	return fmt.Sprintf(`
        SELECT COUNT(DISTINCT (s.connection_id, s.collected_at))
        FROM metrics.pg_settings s
        %s
        AND s.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_settings
            WHERE connection_id = s.connection_id
        )
    `, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return strings.ReplaceAll(tableWhere, "connection_id", "s.connection_id")
	}())
}

// buildHBAChangeQuery creates the subquery for HBA changes
// Excludes initial collection (first snapshot) since that's not really a "change"
func buildHBAChangeQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "h.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "h.connection_id")

	return fmt.Sprintf(`
        SELECT
            'hba-' || h.connection_id || '-' || h.collected_at::TEXT AS id,
            '%s' AS event_type,
            h.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            h.collected_at AS event_time,
            'info' AS severity,
            'HBA Configuration Changed' AS title,
            'Updated pg_hba.conf with ' || COUNT(*) || ' rules' AS summary,
            jsonb_build_object(
                'rule_count', COUNT(*),
                'rules', jsonb_agg(jsonb_build_object(
                    'rule_number', h.rule_number,
                    'type', h.type,
                    'database', h.database,
                    'user_name', h.user_name,
                    'address', h.address,
                    'auth_method', h.auth_method
                ) ORDER BY h.rule_number) FILTER (WHERE h.rule_number IS NOT NULL)
            )::TEXT AS details
        FROM metrics.pg_hba_file_rules h
        JOIN connections c ON h.connection_id = c.id
        %s
        AND h.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_hba_file_rules
            WHERE connection_id = h.connection_id
        )
        GROUP BY h.connection_id, h.collected_at, c.name
    `, EventTypeHBAChange, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return tableWhere
	}())
}

// buildHBAChangeCountQuery creates the count query for HBA changes
// Excludes initial collection to match the main query
func buildHBAChangeCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")

	return fmt.Sprintf(`
        SELECT COUNT(DISTINCT (h.connection_id, h.collected_at))
        FROM metrics.pg_hba_file_rules h
        %s
        AND h.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_hba_file_rules
            WHERE connection_id = h.connection_id
        )
    `, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return strings.ReplaceAll(tableWhere, "connection_id", "h.connection_id")
	}())
}

// buildIdentChangeQuery creates the subquery for ident mapping changes
// Excludes initial collection (first snapshot) since that's not really a "change"
func buildIdentChangeQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "i.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "i.connection_id")

	return fmt.Sprintf(`
        SELECT
            'ident-' || i.connection_id || '-' || i.collected_at::TEXT AS id,
            '%s' AS event_type,
            i.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            i.collected_at AS event_time,
            'info' AS severity,
            'Ident Mappings Changed' AS title,
            'Updated pg_ident.conf with ' || COUNT(*) || ' mappings' AS summary,
            jsonb_build_object(
                'mapping_count', COUNT(*),
                'mappings', jsonb_agg(jsonb_build_object(
                    'map_number', i.map_number,
                    'map_name', i.map_name,
                    'sys_name', i.sys_name,
                    'pg_username', i.pg_username
                ) ORDER BY i.map_number) FILTER (WHERE i.map_number IS NOT NULL)
            )::TEXT AS details
        FROM metrics.pg_ident_file_mappings i
        JOIN connections c ON i.connection_id = c.id
        %s
        AND i.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_ident_file_mappings
            WHERE connection_id = i.connection_id
        )
        GROUP BY i.connection_id, i.collected_at, c.name
    `, EventTypeIdentChange, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return tableWhere
	}())
}

// buildIdentChangeCountQuery creates the count query for ident mapping changes
// Excludes initial collection to match the main query
func buildIdentChangeCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")

	return fmt.Sprintf(`
        SELECT COUNT(DISTINCT (i.connection_id, i.collected_at))
        FROM metrics.pg_ident_file_mappings i
        %s
        AND i.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_ident_file_mappings
            WHERE connection_id = i.connection_id
        )
    `, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return strings.ReplaceAll(tableWhere, "connection_id", "i.connection_id")
	}())
}

// buildRestartQuery creates the subquery for server restarts (timeline_id changes)
func buildRestartQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")

	// Use window function to detect timeline_id changes between consecutive rows
	return fmt.Sprintf(`
        SELECT
            'restart-' || r.connection_id || '-' || r.collected_at::TEXT AS id,
            '%s' AS event_type,
            r.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            r.collected_at AS event_time,
            'warning' AS severity,
            'Server Restart Detected' AS title,
            'Timeline changed from ' || r.prev_timeline || ' to ' || r.timeline_id AS summary,
            jsonb_build_object(
                'previous_timeline', r.prev_timeline,
                'new_timeline', r.timeline_id,
                'is_in_recovery', r.is_in_recovery,
                'primary_role', r.primary_role
            )::TEXT AS details
        FROM (
            SELECT
                connection_id,
                timeline_id,
                is_in_recovery,
                primary_role,
                collected_at,
                LAG(timeline_id) OVER (
                    PARTITION BY connection_id
                    ORDER BY collected_at
                ) AS prev_timeline
            FROM metrics.pg_node_role
        ) r
        JOIN connections c ON r.connection_id = c.id
        WHERE r.prev_timeline IS NOT NULL
          AND r.timeline_id IS NOT NULL
          AND r.prev_timeline != r.timeline_id
          %s
    `, EventTypeRestart, func() string {
		if tableWhere == "" {
			return ""
		}
		// Remove the "WHERE" prefix since we already have WHERE clause
		return "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}())
}

// buildRestartCountQuery creates the count query for server restarts
func buildRestartCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")

	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM (
            SELECT
                connection_id,
                timeline_id,
                collected_at,
                LAG(timeline_id) OVER (
                    PARTITION BY connection_id
                    ORDER BY collected_at
                ) AS prev_timeline
            FROM metrics.pg_node_role
        ) r
        WHERE r.prev_timeline IS NOT NULL
          AND r.timeline_id IS NOT NULL
          AND r.prev_timeline != r.timeline_id
          %s
    `, func() string {
		if tableWhere == "" {
			return ""
		}
		return "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}())
}

// buildAlertFiredQuery creates the subquery for fired alerts
func buildAlertFiredQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "triggered_at")

	return fmt.Sprintf(`
        SELECT
            'alert-fired-' || a.id || '-' || a.triggered_at::TEXT AS id,
            '%s' AS event_type,
            a.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            a.triggered_at AS event_time,
            a.severity,
            'Alert Fired: ' || a.title AS title,
            a.description AS summary,
            jsonb_build_object(
                'alert_id', a.id,
                'alert_type', a.alert_type,
                'metric_name', a.metric_name,
                'metric_value', a.metric_value,
                'metric_unit', r.metric_unit,
                'threshold_value', a.threshold_value,
                'operator', a.operator,
                'database_name', a.database_name,
                'probe_name', a.probe_name
            )::TEXT AS details
        FROM alerts a
        JOIN connections c ON a.connection_id = c.id
        LEFT JOIN alert_rules r ON a.rule_id = r.id
        %s
    `, EventTypeAlertFired, tableWhere)
}

// buildAlertFiredCountQuery creates the count query for fired alerts
func buildAlertFiredCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "triggered_at")
	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM alerts
        %s
    `, tableWhere)
}

// buildAlertClearedQuery creates the subquery for cleared alerts
func buildAlertClearedQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "cleared_at")

	// Add the cleared_at IS NOT NULL condition
	clearedCondition := "cleared_at IS NOT NULL"
	if tableWhere != "" {
		tableWhere = tableWhere + " AND " + clearedCondition
	} else {
		tableWhere = "WHERE " + clearedCondition
	}

	return fmt.Sprintf(`
        SELECT
            'alert-cleared-' || a.id || '-' || a.cleared_at::TEXT AS id,
            '%s' AS event_type,
            a.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            a.cleared_at AS event_time,
            'info' AS severity,
            'Alert Cleared: ' || a.title AS title,
            'Alert condition no longer active' AS summary,
            jsonb_build_object(
                'alert_id', a.id,
                'original_severity', a.severity,
                'triggered_at', a.triggered_at,
                'duration_seconds', EXTRACT(EPOCH FROM (a.cleared_at - a.triggered_at))
            )::TEXT AS details
        FROM alerts a
        JOIN connections c ON a.connection_id = c.id
        %s
    `, EventTypeAlertCleared, tableWhere)
}

// buildAlertClearedCountQuery creates the count query for cleared alerts
func buildAlertClearedCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "cleared_at")

	clearedCondition := "cleared_at IS NOT NULL"
	if tableWhere != "" {
		tableWhere = tableWhere + " AND " + clearedCondition
	} else {
		tableWhere = "WHERE " + clearedCondition
	}

	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM alerts
        %s
    `, tableWhere)
}

// buildAlertAcknowledgedQuery creates the subquery for acknowledged alerts
func buildAlertAcknowledgedQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "ack.acknowledged_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "a.connection_id")

	return fmt.Sprintf(`
        SELECT
            'alert-ack-' || ack.id || '-' || ack.acknowledged_at::TEXT AS id,
            '%s' AS event_type,
            a.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            ack.acknowledged_at AS event_time,
            'info' AS severity,
            CASE
                WHEN ack.false_positive THEN 'Alert Marked False Positive: ' || a.title
                ELSE 'Alert Acknowledged: ' || a.title
            END AS title,
            CASE
                WHEN ack.message != '' THEN ack.message
                ELSE 'Acknowledged by ' || ack.acknowledged_by
            END AS summary,
            jsonb_build_object(
                'alert_id', a.id,
                'acknowledged_by', ack.acknowledged_by,
                'acknowledge_type', ack.acknowledge_type,
                'false_positive', ack.false_positive,
                'message', ack.message,
                'original_severity', a.severity,
                'alert_title', a.title
            )::TEXT AS details
        FROM alert_acknowledgments ack
        JOIN alerts a ON ack.alert_id = a.id
        JOIN connections c ON a.connection_id = c.id
        %s
    `, EventTypeAlertAcknowledged, tableWhere)
}

// buildAlertAcknowledgedCountQuery creates the count query for acknowledged alerts
func buildAlertAcknowledgedCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "ack.acknowledged_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "a.connection_id")

	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM alert_acknowledgments ack
        JOIN alerts a ON ack.alert_id = a.id
        %s
    `, tableWhere)
}

// buildExtensionChangeQuery creates the subquery for extension changes from pg_extension
// The table is change-tracked, so each row represents an extension when a change was detected
// We exclude the initial collection (first snapshot) since that's not really a "change"
func buildExtensionChangeQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "e.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "e.connection_id")

	return fmt.Sprintf(`
        SELECT
            'ext-' || e.connection_id || '-' || e.collected_at::TEXT AS id,
            '%s' AS event_type,
            e.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            e.collected_at AS event_time,
            'info' AS severity,
            'Extensions Changed' AS title,
            COUNT(*) || ' extensions installed' AS summary,
            jsonb_build_object(
                'extension_count', COUNT(*),
                'extensions', jsonb_agg(jsonb_build_object(
                    'name', e.extname,
                    'version', e.extversion,
                    'schema', e.schema_name,
                    'database', e.database_name
                ) ORDER BY e.extname) FILTER (WHERE e.extname IS NOT NULL)
            )::TEXT AS details
        FROM metrics.pg_extension e
        JOIN connections c ON e.connection_id = c.id
        %s
        AND e.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_extension
            WHERE connection_id = e.connection_id
        )
        GROUP BY e.connection_id, e.collected_at, c.name
    `, EventTypeExtensionChange, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return tableWhere
	}())
}

// buildExtensionChangeCountQuery creates the count query for extension changes
// Excludes initial collection (first snapshot) to match the main query
func buildExtensionChangeCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")

	return fmt.Sprintf(`
        SELECT COUNT(DISTINCT (e.connection_id, e.collected_at))
        FROM metrics.pg_extension e
        %s
        AND e.collected_at > (
            SELECT MIN(collected_at)
            FROM metrics.pg_extension
            WHERE connection_id = e.connection_id
        )
    `, func() string {
		if tableWhere == "" {
			return "WHERE 1=1"
		}
		return strings.ReplaceAll(tableWhere, "connection_id", "e.connection_id")
	}())
}

// buildBlackoutStartedQuery creates the subquery for blackout started events
func buildBlackoutStartedQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "b.start_time")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "COALESCE(b.connection_id, c.id)")

	return fmt.Sprintf(`
        SELECT
            'blackout-start-' || b.id::TEXT AS id,
            '%s' AS event_type,
            COALESCE(b.connection_id, c.id) AS connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            b.start_time AS event_time,
            'info' AS severity,
            'Blackout Started' AS title,
            COALESCE(b.reason, '') AS summary,
            jsonb_build_object(
                'blackout_id', b.id,
                'scope', b.scope,
                'reason', b.reason,
                'created_by', b.created_by,
                'end_time', b.end_time
            )::TEXT AS details
        FROM blackouts b
        JOIN connections c ON (
            CASE
                WHEN b.connection_id IS NOT NULL THEN c.id = b.connection_id
                ELSE TRUE
            END
        )
        %s
    `, EventTypeBlackoutStarted, tableWhere)
}

// buildBlackoutStartedCountQuery creates the count query for blackout started events
func buildBlackoutStartedCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "b.start_time")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "COALESCE(b.connection_id, c.id)")

	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM blackouts b
        JOIN connections c ON (
            CASE
                WHEN b.connection_id IS NOT NULL THEN c.id = b.connection_id
                ELSE TRUE
            END
        )
        %s
    `, tableWhere)
}

// buildBlackoutEndedQuery creates the subquery for blackout ended events
func buildBlackoutEndedQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "b.end_time")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "COALESCE(b.connection_id, c.id)")

	// Add the end_time IS NOT NULL condition
	endedCondition := "b.end_time IS NOT NULL"
	if tableWhere != "" {
		tableWhere = tableWhere + " AND " + endedCondition
	} else {
		tableWhere = "WHERE " + endedCondition
	}

	return fmt.Sprintf(`
        SELECT
            'blackout-end-' || b.id::TEXT AS id,
            '%s' AS event_type,
            COALESCE(b.connection_id, c.id) AS connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            b.end_time AS event_time,
            'info' AS severity,
            'Blackout Ended' AS title,
            COALESCE(b.reason, '') AS summary,
            jsonb_build_object(
                'blackout_id', b.id,
                'scope', b.scope,
                'reason', b.reason,
                'created_by', b.created_by,
                'end_time', b.end_time
            )::TEXT AS details
        FROM blackouts b
        JOIN connections c ON (
            CASE
                WHEN b.connection_id IS NOT NULL THEN c.id = b.connection_id
                ELSE TRUE
            END
        )
        %s
    `, EventTypeBlackoutEnded, tableWhere)
}

// buildBlackoutEndedCountQuery creates the count query for blackout ended events
func buildBlackoutEndedCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "b.end_time")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "COALESCE(b.connection_id, c.id)")

	endedCondition := "b.end_time IS NOT NULL"
	if tableWhere != "" {
		tableWhere = tableWhere + " AND " + endedCondition
	} else {
		tableWhere = "WHERE " + endedCondition
	}

	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM blackouts b
        JOIN connections c ON (
            CASE
                WHEN b.connection_id IS NOT NULL THEN c.id = b.connection_id
                ELSE TRUE
            END
        )
        %s
    `, tableWhere)
}
