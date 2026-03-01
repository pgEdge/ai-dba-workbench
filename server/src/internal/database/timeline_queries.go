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
	allArgs := make([]any, 0, len(connArgs)+len(timeArgs))
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
func buildConnectionFilter(filter TimelineFilter) (string, []any, int) {
	var conditions []string
	var args []any
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
func buildTimeFilter(filter TimelineFilter, startArgNum int) (string, []any, int) {
	var conditions []string
	var args []any
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

	// Server restarts from pg_node_role (postmaster_start_time changes)
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
		subqueries = append(subqueries, buildBlackoutStartedQuery(connCondition, timeCondition))
	}

	// Blackout ended
	if includeTypes[EventTypeBlackoutEnded] {
		subqueries = append(subqueries, buildBlackoutEndedQuery(connCondition, timeCondition))
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
		countQueries = append(countQueries, buildBlackoutStartedCountQuery(connCondition, timeCondition))
	}
	if includeTypes[EventTypeBlackoutEnded] {
		countQueries = append(countQueries, buildBlackoutEndedCountQuery(connCondition, timeCondition))
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

// buildConfigChangeQuery creates the subquery for configuration changes.
// Uses LAG() window functions directly on value columns to detect changes
// in a single table scan without self-joins.
func buildConfigChangeQuery(whereClause string) string {
	// Build filter for the outer query (uses changes.connection_id, changes.collected_at)
	outerWhere := strings.ReplaceAll(whereClause, "event_time", "changes.collected_at")
	outerWhere = strings.ReplaceAll(outerWhere, "connection_id", "changes.connection_id")
	extraFilter := ""
	if outerWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(outerWhere, "WHERE ")
	}

	// Build filter for the inner subquery (no table alias)
	innerWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")
	innerFilter := ""
	if innerWhere != "" {
		innerFilter = strings.TrimPrefix(innerWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT
            'config-' || changes.connection_id || '-' || changes.collected_at::TEXT AS id,
            '%[1]s' AS event_type,
            changes.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            changes.collected_at AS event_time,
            'info' AS severity,
            'Configuration Changed' AS title,
            'Changed ' || COUNT(*) || ' settings' AS summary,
            jsonb_build_object(
                'change_count', COUNT(*),
                'changes', jsonb_agg(jsonb_build_object(
                    'name', changes.name,
                    'old_value', changes.prev_setting,
                    'new_value', changes.setting,
                    'change_type', 'modified'
                ) ORDER BY changes.name)
            )::TEXT AS details
        FROM (
            SELECT connection_id, collected_at, name, setting,
                   LAG(setting) OVER (
                       PARTITION BY connection_id, name ORDER BY collected_at
                   ) AS prev_setting,
                   ROW_NUMBER() OVER (
                       PARTITION BY connection_id, name ORDER BY collected_at
                   ) AS rn
            FROM metrics.pg_settings
            %[2]s
        ) changes
        JOIN connections c ON changes.connection_id = c.id
        WHERE changes.rn > 1
          AND changes.setting IS DISTINCT FROM changes.prev_setting
          %[3]s
        GROUP BY changes.connection_id, changes.collected_at, c.name
    `, EventTypeConfigChange, func() string {
		if innerFilter == "" {
			return ""
		}
		return "WHERE " + innerFilter
	}(), extraFilter)
}

// buildConfigChangeCountQuery creates the count query for configuration
// changes. Counts distinct (connection_id, collected_at) pairs that have
// at least one setting change compared to the previous snapshot.
func buildConfigChangeCountQuery(whereClause string) string {
	// Build filter for the outer query
	outerWhere := strings.ReplaceAll(whereClause, "event_time", "changes.collected_at")
	outerWhere = strings.ReplaceAll(outerWhere, "connection_id", "changes.connection_id")
	extraFilter := ""
	if outerWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(outerWhere, "WHERE ")
	}

	// Build filter for the inner subquery (no table alias)
	innerWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")
	innerFilter := ""
	if innerWhere != "" {
		innerFilter = strings.TrimPrefix(innerWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT COUNT(*) FROM (
            SELECT changes.connection_id, changes.collected_at
            FROM (
                SELECT connection_id, collected_at, setting,
                       LAG(setting) OVER (
                           PARTITION BY connection_id, name ORDER BY collected_at
                       ) AS prev_setting,
                       ROW_NUMBER() OVER (
                           PARTITION BY connection_id, name ORDER BY collected_at
                       ) AS rn
                FROM metrics.pg_settings
                %[1]s
            ) changes
            WHERE changes.rn > 1
              AND changes.setting IS DISTINCT FROM changes.prev_setting
              %[2]s
            GROUP BY changes.connection_id, changes.collected_at
        ) d
    `, func() string {
		if innerFilter == "" {
			return ""
		}
		return "WHERE " + innerFilter
	}(), extraFilter)
}

// buildHBAChangeQuery creates the subquery for HBA changes.
// Uses LAG() window functions directly on value columns to detect changes
// in a single table scan without self-joins.
func buildHBAChangeQuery(whereClause string) string {
	// Build filter for the outer query
	outerWhere := strings.ReplaceAll(whereClause, "event_time", "changes.collected_at")
	outerWhere = strings.ReplaceAll(outerWhere, "connection_id", "changes.connection_id")
	extraFilter := ""
	if outerWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(outerWhere, "WHERE ")
	}

	// Build filter for the inner subquery (no table alias)
	innerWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")
	innerFilter := ""
	if innerWhere != "" {
		innerFilter = strings.TrimPrefix(innerWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT
            'hba-' || changes.connection_id || '-' || changes.collected_at::TEXT AS id,
            '%[1]s' AS event_type,
            changes.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            changes.collected_at AS event_time,
            'info' AS severity,
            'HBA Configuration Changed' AS title,
            'Changed ' || COUNT(*) || ' HBA rules' AS summary,
            jsonb_build_object(
                'change_count', COUNT(*),
                'changes', jsonb_agg(jsonb_build_object(
                    'rule_number', changes.rule_number,
                    'type', changes.type,
                    'database', changes.database,
                    'user_name', changes.user_name,
                    'address', changes.address,
                    'auth_method', changes.auth_method,
                    'prev_type', changes.prev_type,
                    'prev_database', changes.prev_database,
                    'prev_user_name', changes.prev_user_name,
                    'prev_address', changes.prev_address,
                    'prev_auth_method', changes.prev_auth_method,
                    'change_type', 'modified'
                ) ORDER BY changes.rule_number)
            )::TEXT AS details
        FROM (
            SELECT connection_id, collected_at, rule_number,
                   type, database, user_name, address, auth_method,
                   LAG(type) OVER w AS prev_type,
                   LAG(database) OVER w AS prev_database,
                   LAG(user_name) OVER w AS prev_user_name,
                   LAG(address) OVER w AS prev_address,
                   LAG(auth_method) OVER w AS prev_auth_method,
                   ROW_NUMBER() OVER w AS rn
            FROM metrics.pg_hba_file_rules
            %[2]s
            WINDOW w AS (PARTITION BY connection_id, rule_number ORDER BY collected_at)
        ) changes
        JOIN connections c ON changes.connection_id = c.id
        WHERE changes.rn > 1
          AND (changes.type IS DISTINCT FROM changes.prev_type
               OR changes.database IS DISTINCT FROM changes.prev_database
               OR changes.user_name IS DISTINCT FROM changes.prev_user_name
               OR changes.address IS DISTINCT FROM changes.prev_address
               OR changes.auth_method IS DISTINCT FROM changes.prev_auth_method)
          %[3]s
        GROUP BY changes.connection_id, changes.collected_at, c.name
    `, EventTypeHBAChange, func() string {
		if innerFilter == "" {
			return ""
		}
		return "WHERE " + innerFilter
	}(), extraFilter)
}

// buildHBAChangeCountQuery creates the count query for HBA changes.
// Counts distinct (connection_id, collected_at) pairs that have at least
// one HBA rule change compared to the previous snapshot.
func buildHBAChangeCountQuery(whereClause string) string {
	// Build filter for the outer query
	outerWhere := strings.ReplaceAll(whereClause, "event_time", "changes.collected_at")
	outerWhere = strings.ReplaceAll(outerWhere, "connection_id", "changes.connection_id")
	extraFilter := ""
	if outerWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(outerWhere, "WHERE ")
	}

	// Build filter for the inner subquery (no table alias)
	innerWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")
	innerFilter := ""
	if innerWhere != "" {
		innerFilter = strings.TrimPrefix(innerWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT COUNT(*) FROM (
            SELECT changes.connection_id, changes.collected_at
            FROM (
                SELECT connection_id, collected_at,
                       type, database, user_name, address, auth_method,
                       LAG(type) OVER w AS prev_type,
                       LAG(database) OVER w AS prev_database,
                       LAG(user_name) OVER w AS prev_user_name,
                       LAG(address) OVER w AS prev_address,
                       LAG(auth_method) OVER w AS prev_auth_method,
                       ROW_NUMBER() OVER w AS rn
                FROM metrics.pg_hba_file_rules
                %[1]s
                WINDOW w AS (PARTITION BY connection_id, rule_number ORDER BY collected_at)
            ) changes
            WHERE changes.rn > 1
              AND (changes.type IS DISTINCT FROM changes.prev_type
                   OR changes.database IS DISTINCT FROM changes.prev_database
                   OR changes.user_name IS DISTINCT FROM changes.prev_user_name
                   OR changes.address IS DISTINCT FROM changes.prev_address
                   OR changes.auth_method IS DISTINCT FROM changes.prev_auth_method)
              %[2]s
            GROUP BY changes.connection_id, changes.collected_at
        ) d
    `, func() string {
		if innerFilter == "" {
			return ""
		}
		return "WHERE " + innerFilter
	}(), extraFilter)
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

// buildRestartQuery creates the subquery for server restarts (postmaster_start_time changes)
func buildRestartQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")

	// Use window function to detect postmaster_start_time changes between consecutive rows
	return fmt.Sprintf(`
        SELECT
            'restart-' || r.connection_id || '-' || r.collected_at::TEXT AS id,
            '%[1]s' AS event_type,
            r.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            r.collected_at AS event_time,
            'warning' AS severity,
            'Server Restart Detected' AS title,
            'Server started at ' || r.postmaster_start_time || '; previous start was ' || r.prev_start_time AS summary,
            jsonb_build_object(
                'previous_start_time', r.prev_start_time,
                'new_start_time', r.postmaster_start_time,
                'is_in_recovery', r.is_in_recovery,
                'primary_role', r.primary_role
            )::TEXT AS details
        FROM (
            SELECT
                connection_id,
                postmaster_start_time,
                is_in_recovery,
                primary_role,
                collected_at,
                LAG(postmaster_start_time) OVER (
                    PARTITION BY connection_id
                    ORDER BY collected_at
                ) AS prev_start_time
            FROM metrics.pg_node_role
            WHERE postmaster_start_time IS NOT NULL
              %[2]s
        ) r
        JOIN connections c ON r.connection_id = c.id
        WHERE r.prev_start_time IS NOT NULL
          AND r.postmaster_start_time IS NOT NULL
          AND r.prev_start_time != r.postmaster_start_time
          %[3]s
    `, EventTypeRestart, func() string {
		if tableWhere == "" {
			return ""
		}
		return "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}(), func() string {
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
                postmaster_start_time,
                collected_at,
                LAG(postmaster_start_time) OVER (
                    PARTITION BY connection_id
                    ORDER BY collected_at
                ) AS prev_start_time
            FROM metrics.pg_node_role
            WHERE postmaster_start_time IS NOT NULL
              %[1]s
        ) r
        WHERE r.prev_start_time IS NOT NULL
          AND r.postmaster_start_time IS NOT NULL
          AND r.prev_start_time != r.postmaster_start_time
          %[2]s
    `, func() string {
		if tableWhere == "" {
			return ""
		}
		return "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}(), func() string {
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

// buildExtensionChangeQuery creates the subquery for extension changes.
// Uses LAG() window functions directly on value columns to detect changes
// in a single table scan without self-joins.
func buildExtensionChangeQuery(whereClause string) string {
	// Build filter for the outer query
	outerWhere := strings.ReplaceAll(whereClause, "event_time", "changes.collected_at")
	outerWhere = strings.ReplaceAll(outerWhere, "connection_id", "changes.connection_id")
	extraFilter := ""
	if outerWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(outerWhere, "WHERE ")
	}

	// Build filter for the inner subquery (no table alias)
	innerWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")
	innerFilter := ""
	if innerWhere != "" {
		innerFilter = strings.TrimPrefix(innerWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT
            'ext-' || changes.connection_id || '-' || changes.collected_at::TEXT AS id,
            '%[1]s' AS event_type,
            changes.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            changes.collected_at AS event_time,
            'info' AS severity,
            'Extensions Changed' AS title,
            'Changed ' || COUNT(*) || ' extensions' AS summary,
            jsonb_build_object(
                'change_count', COUNT(*),
                'changes', jsonb_agg(jsonb_build_object(
                    'name', changes.extname,
                    'version', changes.extversion,
                    'old_version', changes.prev_extversion,
                    'database', changes.database_name,
                    'change_type', 'modified'
                ) ORDER BY changes.extname)
            )::TEXT AS details
        FROM (
            SELECT connection_id, collected_at,
                   database_name, extname, extversion,
                   LAG(extversion) OVER w AS prev_extversion,
                   ROW_NUMBER() OVER w AS rn
            FROM metrics.pg_extension
            %[2]s
            WINDOW w AS (PARTITION BY connection_id, database_name, extname ORDER BY collected_at)
        ) changes
        JOIN connections c ON changes.connection_id = c.id
        WHERE changes.rn > 1
          AND changes.extversion IS DISTINCT FROM changes.prev_extversion
          %[3]s
        GROUP BY changes.connection_id, changes.collected_at, c.name
    `, EventTypeExtensionChange, func() string {
		if innerFilter == "" {
			return ""
		}
		return "WHERE " + innerFilter
	}(), extraFilter)
}

// buildExtensionChangeCountQuery creates the count query for extension
// changes. Counts distinct (connection_id, collected_at) pairs that have
// at least one extension version change compared to the previous snapshot.
func buildExtensionChangeCountQuery(whereClause string) string {
	// Build filter for the outer query
	outerWhere := strings.ReplaceAll(whereClause, "event_time", "changes.collected_at")
	outerWhere = strings.ReplaceAll(outerWhere, "connection_id", "changes.connection_id")
	extraFilter := ""
	if outerWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(outerWhere, "WHERE ")
	}

	// Build filter for the inner subquery (no table alias)
	innerWhere := strings.ReplaceAll(whereClause, "event_time", "collected_at")
	innerFilter := ""
	if innerWhere != "" {
		innerFilter = strings.TrimPrefix(innerWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT COUNT(*) FROM (
            SELECT changes.connection_id, changes.collected_at
            FROM (
                SELECT connection_id, collected_at, extversion,
                       LAG(extversion) OVER w AS prev_extversion,
                       ROW_NUMBER() OVER w AS rn
                FROM metrics.pg_extension
                %[1]s
                WINDOW w AS (PARTITION BY connection_id, database_name, extname ORDER BY collected_at)
            ) changes
            WHERE changes.rn > 1
              AND changes.extversion IS DISTINCT FROM changes.prev_extversion
              %[2]s
            GROUP BY changes.connection_id, changes.collected_at
        ) d
    `, func() string {
		if innerFilter == "" {
			return ""
		}
		return "WHERE " + innerFilter
	}(), extraFilter)
}

// buildBlackoutScopeFilter builds a scope-aware connection filter for blackout
// queries. When connCondition is empty (no connection filter), all blackouts
// are returned. When connCondition is provided, it matches blackouts whose
// scope encompasses at least one of the filtered connections: server-scoped
// blackouts match directly, cluster-scoped blackouts match if any connection
// in the cluster matches, group-scoped blackouts match if any connection in
// any cluster within the group matches, and estate-scoped blackouts always
// match.
func buildBlackoutScopeFilter(connCondition string) string {
	if connCondition == "" {
		return ""
	}

	serverMatch := strings.ReplaceAll(connCondition, "connection_id", "b.connection_id")
	clusterMatch := strings.ReplaceAll(connCondition, "connection_id", "sc.id")
	groupMatch := strings.ReplaceAll(connCondition, "connection_id", "gc.id")

	return fmt.Sprintf(`AND (
            (b.scope = 'server' AND %s)
            OR (b.scope = 'cluster' AND EXISTS (
                SELECT 1 FROM connections sc
                WHERE sc.cluster_id = b.cluster_id AND %s
            ))
            OR (b.scope = 'group' AND EXISTS (
                SELECT 1 FROM connections gc
                JOIN clusters gcl ON gc.cluster_id = gcl.id
                WHERE gcl.group_id = b.group_id AND %s
            ))
            OR (b.scope = 'estate')
        )`, serverMatch, clusterMatch, groupMatch)
}

// buildBlackoutStartedQuery creates the subquery for blackout started events.
// It produces exactly one row per blackout by using scalar subqueries for
// server_name instead of joining to connections.
func buildBlackoutStartedQuery(connCondition, timeCondition string) string {
	timeFilter := ""
	if timeCondition != "" {
		timeFilter = "AND " + strings.ReplaceAll(timeCondition, "event_time", "b.start_time")
	}

	scopeFilter := buildBlackoutScopeFilter(connCondition)

	return fmt.Sprintf(`
        SELECT
            'blackout-start-' || b.id::TEXT AS id,
            '%s' AS event_type,
            CASE b.scope
                WHEN 'server' THEN b.connection_id
                ELSE 0
            END AS connection_id,
            CASE b.scope
                WHEN 'server' THEN COALESCE((SELECT name FROM connections WHERE id = b.connection_id), 'Unknown')
                WHEN 'cluster' THEN COALESCE((SELECT name FROM clusters WHERE id = b.cluster_id), 'Unknown Cluster')
                WHEN 'group' THEN COALESCE((SELECT name FROM cluster_groups WHERE id = b.group_id), 'Unknown Group')
                WHEN 'estate' THEN 'All Servers'
                ELSE 'Unknown'
            END AS server_name,
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
        WHERE TRUE
            %s
            %s
    `, EventTypeBlackoutStarted, timeFilter, scopeFilter)
}

// buildBlackoutStartedCountQuery creates the count query for blackout started
// events, producing exactly one count per blackout.
func buildBlackoutStartedCountQuery(connCondition, timeCondition string) string {
	timeFilter := ""
	if timeCondition != "" {
		timeFilter = "AND " + strings.ReplaceAll(timeCondition, "event_time", "b.start_time")
	}

	scopeFilter := buildBlackoutScopeFilter(connCondition)

	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM blackouts b
        WHERE TRUE
            %s
            %s
    `, timeFilter, scopeFilter)
}

// buildBlackoutEndedQuery creates the subquery for blackout ended events.
// It produces exactly one row per blackout by using scalar subqueries for
// server_name instead of joining to connections.
func buildBlackoutEndedQuery(connCondition, timeCondition string) string {
	timeFilter := ""
	if timeCondition != "" {
		timeFilter = "AND " + strings.ReplaceAll(timeCondition, "event_time", "b.end_time")
	}

	scopeFilter := buildBlackoutScopeFilter(connCondition)

	return fmt.Sprintf(`
        SELECT
            'blackout-end-' || b.id::TEXT AS id,
            '%s' AS event_type,
            CASE b.scope
                WHEN 'server' THEN b.connection_id
                ELSE 0
            END AS connection_id,
            CASE b.scope
                WHEN 'server' THEN COALESCE((SELECT name FROM connections WHERE id = b.connection_id), 'Unknown')
                WHEN 'cluster' THEN COALESCE((SELECT name FROM clusters WHERE id = b.cluster_id), 'Unknown Cluster')
                WHEN 'group' THEN COALESCE((SELECT name FROM cluster_groups WHERE id = b.group_id), 'Unknown Group')
                WHEN 'estate' THEN 'All Servers'
                ELSE 'Unknown'
            END AS server_name,
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
        WHERE b.end_time IS NOT NULL
            %s
            %s
    `, EventTypeBlackoutEnded, timeFilter, scopeFilter)
}

// buildBlackoutEndedCountQuery creates the count query for blackout ended
// events, producing exactly one count per blackout.
func buildBlackoutEndedCountQuery(connCondition, timeCondition string) string {
	timeFilter := ""
	if timeCondition != "" {
		timeFilter = "AND " + strings.ReplaceAll(timeCondition, "event_time", "b.end_time")
	}

	scopeFilter := buildBlackoutScopeFilter(connCondition)

	return fmt.Sprintf(`
        SELECT COUNT(*)
        FROM blackouts b
        WHERE b.end_time IS NOT NULL
            %s
            %s
    `, timeFilter, scopeFilter)
}
