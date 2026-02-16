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
// Uses LAG() to find previous snapshots and compares settings between
// consecutive snapshots, returning only settings that actually changed.
func buildConfigChangeQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "d.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "d.connection_id")

	extraFilter := ""
	if tableWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT
            'config-' || d.connection_id || '-' || d.collected_at::TEXT AS id,
            '%s' AS event_type,
            d.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            d.collected_at AS event_time,
            'info' AS severity,
            'Configuration Changed' AS title,
            'Changed ' || COUNT(*) || ' settings' AS summary,
            jsonb_build_object(
                'change_count', COUNT(*),
                'changes', jsonb_agg(jsonb_build_object(
                    'name', COALESCE(d.curr_name, d.prev_name),
                    'old_value', d.prev_setting,
                    'new_value', d.curr_setting,
                    'change_type', d.change_type
                ) ORDER BY COALESCE(d.curr_name, d.prev_name))
            )::TEXT AS details
        FROM (
            SELECT
                snap.connection_id,
                snap.collected_at,
                curr.name AS curr_name,
                curr.setting AS curr_setting,
                prev.name AS prev_name,
                prev.setting AS prev_setting,
                CASE
                    WHEN prev.name IS NULL THEN 'added'
                    ELSE 'modified'
                END AS change_type
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_settings
            ) snap
            JOIN metrics.pg_settings curr
                ON curr.connection_id = snap.connection_id
                AND curr.collected_at = snap.collected_at
            LEFT JOIN metrics.pg_settings prev
                ON prev.connection_id = snap.connection_id
                AND prev.collected_at = snap.prev_collected_at
                AND prev.name = curr.name
            WHERE snap.prev_collected_at IS NOT NULL
              AND (prev.name IS NULL
                   OR curr.setting IS DISTINCT FROM prev.setting)

            UNION ALL

            SELECT
                snap.connection_id,
                snap.collected_at,
                NULL AS curr_name,
                NULL AS curr_setting,
                prev.name AS prev_name,
                prev.setting AS prev_setting,
                'removed' AS change_type
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_settings
            ) snap
            JOIN metrics.pg_settings prev
                ON prev.connection_id = snap.connection_id
                AND prev.collected_at = snap.prev_collected_at
            WHERE snap.prev_collected_at IS NOT NULL
              AND NOT EXISTS (
                  SELECT 1 FROM metrics.pg_settings curr
                  WHERE curr.connection_id = snap.connection_id
                    AND curr.collected_at = snap.collected_at
                    AND curr.name = prev.name
              )
        ) d
        JOIN connections c ON d.connection_id = c.id
        WHERE TRUE %s
        GROUP BY d.connection_id, d.collected_at, c.name
    `, EventTypeConfigChange, extraFilter)
}

// buildConfigChangeCountQuery creates the count query for configuration
// changes. Counts only snapshots that have at least one actual diff from
// the previous snapshot.
func buildConfigChangeCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "d.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "d.connection_id")

	extraFilter := ""
	if tableWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT COUNT(*) FROM (
            SELECT snap.connection_id, snap.collected_at
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_settings
            ) snap
            WHERE snap.prev_collected_at IS NOT NULL
              AND (
                  EXISTS (
                      SELECT 1
                      FROM metrics.pg_settings curr
                      LEFT JOIN metrics.pg_settings prev
                          ON prev.connection_id = snap.connection_id
                          AND prev.collected_at = snap.prev_collected_at
                          AND prev.name = curr.name
                      WHERE curr.connection_id = snap.connection_id
                        AND curr.collected_at = snap.collected_at
                        AND (prev.name IS NULL
                             OR curr.setting IS DISTINCT FROM prev.setting)
                  )
                  OR EXISTS (
                      SELECT 1
                      FROM metrics.pg_settings prev
                      WHERE prev.connection_id = snap.connection_id
                        AND prev.collected_at = snap.prev_collected_at
                        AND NOT EXISTS (
                            SELECT 1 FROM metrics.pg_settings curr
                            WHERE curr.connection_id = snap.connection_id
                              AND curr.collected_at = snap.collected_at
                              AND curr.name = prev.name
                        )
                  )
              )
        ) d
        WHERE TRUE %s
    `, extraFilter)
}

// buildHBAChangeQuery creates the subquery for HBA changes.
// Uses LAG() to find previous snapshots and compares rules between
// consecutive snapshots, returning only rules that actually changed.
func buildHBAChangeQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "d.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "d.connection_id")

	extraFilter := ""
	if tableWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT
            'hba-' || d.connection_id || '-' || d.collected_at::TEXT AS id,
            '%s' AS event_type,
            d.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            d.collected_at AS event_time,
            'info' AS severity,
            'HBA Configuration Changed' AS title,
            'Changed ' || COUNT(*) || ' HBA rules' AS summary,
            jsonb_build_object(
                'change_count', COUNT(*),
                'changes', jsonb_agg(jsonb_build_object(
                    'rule_number', COALESCE(d.curr_rule_number, d.prev_rule_number),
                    'type', d.curr_type,
                    'database', d.curr_database,
                    'user_name', d.curr_user_name,
                    'address', d.curr_address,
                    'auth_method', d.curr_auth_method,
                    'prev_type', d.prev_type,
                    'prev_database', d.prev_database,
                    'prev_user_name', d.prev_user_name,
                    'prev_address', d.prev_address,
                    'prev_auth_method', d.prev_auth_method,
                    'change_type', d.change_type
                ) ORDER BY COALESCE(d.curr_rule_number, d.prev_rule_number))
            )::TEXT AS details
        FROM (
            SELECT
                snap.connection_id,
                snap.collected_at,
                curr.rule_number AS curr_rule_number,
                curr.type AS curr_type,
                curr.database AS curr_database,
                curr.user_name AS curr_user_name,
                curr.address AS curr_address,
                curr.auth_method AS curr_auth_method,
                prev.rule_number AS prev_rule_number,
                prev.type AS prev_type,
                prev.database AS prev_database,
                prev.user_name AS prev_user_name,
                prev.address AS prev_address,
                prev.auth_method AS prev_auth_method,
                CASE
                    WHEN prev.rule_number IS NULL THEN 'added'
                    ELSE 'modified'
                END AS change_type
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_hba_file_rules
            ) snap
            JOIN metrics.pg_hba_file_rules curr
                ON curr.connection_id = snap.connection_id
                AND curr.collected_at = snap.collected_at
            LEFT JOIN metrics.pg_hba_file_rules prev
                ON prev.connection_id = snap.connection_id
                AND prev.collected_at = snap.prev_collected_at
                AND prev.rule_number = curr.rule_number
            WHERE snap.prev_collected_at IS NOT NULL
              AND (prev.rule_number IS NULL
                   OR curr.type IS DISTINCT FROM prev.type
                   OR curr.database IS DISTINCT FROM prev.database
                   OR curr.user_name IS DISTINCT FROM prev.user_name
                   OR curr.address IS DISTINCT FROM prev.address
                   OR curr.auth_method IS DISTINCT FROM prev.auth_method)

            UNION ALL

            SELECT
                snap.connection_id,
                snap.collected_at,
                NULL AS curr_rule_number,
                NULL AS curr_type,
                NULL AS curr_database,
                NULL AS curr_user_name,
                NULL AS curr_address,
                NULL AS curr_auth_method,
                prev.rule_number AS prev_rule_number,
                prev.type AS prev_type,
                prev.database AS prev_database,
                prev.user_name AS prev_user_name,
                prev.address AS prev_address,
                prev.auth_method AS prev_auth_method,
                'removed' AS change_type
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_hba_file_rules
            ) snap
            JOIN metrics.pg_hba_file_rules prev
                ON prev.connection_id = snap.connection_id
                AND prev.collected_at = snap.prev_collected_at
            WHERE snap.prev_collected_at IS NOT NULL
              AND NOT EXISTS (
                  SELECT 1 FROM metrics.pg_hba_file_rules curr
                  WHERE curr.connection_id = snap.connection_id
                    AND curr.collected_at = snap.collected_at
                    AND curr.rule_number = prev.rule_number
              )
        ) d
        JOIN connections c ON d.connection_id = c.id
        WHERE TRUE %s
        GROUP BY d.connection_id, d.collected_at, c.name
    `, EventTypeHBAChange, extraFilter)
}

// buildHBAChangeCountQuery creates the count query for HBA changes.
// Counts only snapshots that have at least one actual diff from the
// previous snapshot.
func buildHBAChangeCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "d.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "d.connection_id")

	extraFilter := ""
	if tableWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT COUNT(*) FROM (
            SELECT snap.connection_id, snap.collected_at
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_hba_file_rules
            ) snap
            WHERE snap.prev_collected_at IS NOT NULL
              AND (
                  EXISTS (
                      SELECT 1
                      FROM metrics.pg_hba_file_rules curr
                      LEFT JOIN metrics.pg_hba_file_rules prev
                          ON prev.connection_id = snap.connection_id
                          AND prev.collected_at = snap.prev_collected_at
                          AND prev.rule_number = curr.rule_number
                      WHERE curr.connection_id = snap.connection_id
                        AND curr.collected_at = snap.collected_at
                        AND (prev.rule_number IS NULL
                             OR curr.type IS DISTINCT FROM prev.type
                             OR curr.database IS DISTINCT FROM prev.database
                             OR curr.user_name IS DISTINCT FROM prev.user_name
                             OR curr.address IS DISTINCT FROM prev.address
                             OR curr.auth_method IS DISTINCT FROM prev.auth_method)
                  )
                  OR EXISTS (
                      SELECT 1
                      FROM metrics.pg_hba_file_rules prev
                      WHERE prev.connection_id = snap.connection_id
                        AND prev.collected_at = snap.prev_collected_at
                        AND NOT EXISTS (
                            SELECT 1 FROM metrics.pg_hba_file_rules curr
                            WHERE curr.connection_id = snap.connection_id
                              AND curr.collected_at = snap.collected_at
                              AND curr.rule_number = prev.rule_number
                        )
                  )
              )
        ) d
        WHERE TRUE %s
    `, extraFilter)
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
            '%s' AS event_type,
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
        ) r
        JOIN connections c ON r.connection_id = c.id
        WHERE r.prev_start_time IS NOT NULL
          AND r.postmaster_start_time IS NOT NULL
          AND r.prev_start_time != r.postmaster_start_time
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
                postmaster_start_time,
                collected_at,
                LAG(postmaster_start_time) OVER (
                    PARTITION BY connection_id
                    ORDER BY collected_at
                ) AS prev_start_time
            FROM metrics.pg_node_role
            WHERE postmaster_start_time IS NOT NULL
        ) r
        WHERE r.prev_start_time IS NOT NULL
          AND r.postmaster_start_time IS NOT NULL
          AND r.prev_start_time != r.postmaster_start_time
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

// buildExtensionChangeQuery creates the subquery for extension changes.
// Uses LAG() to find previous snapshots and compares extensions between
// consecutive snapshots, returning only extensions that actually changed.
func buildExtensionChangeQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "d.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "d.connection_id")

	extraFilter := ""
	if tableWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT
            'ext-' || d.connection_id || '-' || d.collected_at::TEXT AS id,
            '%s' AS event_type,
            d.connection_id,
            COALESCE(c.name, 'Unknown') AS server_name,
            d.collected_at AS event_time,
            'info' AS severity,
            'Extensions Changed' AS title,
            'Changed ' || COUNT(*) || ' extensions' AS summary,
            jsonb_build_object(
                'change_count', COUNT(*),
                'changes', jsonb_agg(jsonb_build_object(
                    'name', COALESCE(d.curr_extname, d.prev_extname),
                    'version', d.curr_extversion,
                    'old_version', d.prev_extversion,
                    'database', COALESCE(d.curr_database, d.prev_database),
                    'change_type', d.change_type
                ) ORDER BY COALESCE(d.curr_extname, d.prev_extname))
            )::TEXT AS details
        FROM (
            SELECT
                snap.connection_id,
                snap.collected_at,
                curr.extname AS curr_extname,
                curr.extversion AS curr_extversion,
                curr.database_name AS curr_database,
                prev.extname AS prev_extname,
                prev.extversion AS prev_extversion,
                prev.database_name AS prev_database,
                CASE
                    WHEN prev.extname IS NULL THEN 'added'
                    ELSE 'modified'
                END AS change_type
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_extension
            ) snap
            JOIN metrics.pg_extension curr
                ON curr.connection_id = snap.connection_id
                AND curr.collected_at = snap.collected_at
            LEFT JOIN metrics.pg_extension prev
                ON prev.connection_id = snap.connection_id
                AND prev.collected_at = snap.prev_collected_at
                AND prev.database_name = curr.database_name
                AND prev.extname = curr.extname
            WHERE snap.prev_collected_at IS NOT NULL
              AND (prev.extname IS NULL
                   OR curr.extversion IS DISTINCT FROM prev.extversion)

            UNION ALL

            SELECT
                snap.connection_id,
                snap.collected_at,
                NULL AS curr_extname,
                NULL AS curr_extversion,
                NULL AS curr_database,
                prev.extname AS prev_extname,
                prev.extversion AS prev_extversion,
                prev.database_name AS prev_database,
                'removed' AS change_type
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_extension
            ) snap
            JOIN metrics.pg_extension prev
                ON prev.connection_id = snap.connection_id
                AND prev.collected_at = snap.prev_collected_at
            WHERE snap.prev_collected_at IS NOT NULL
              AND NOT EXISTS (
                  SELECT 1 FROM metrics.pg_extension curr
                  WHERE curr.connection_id = snap.connection_id
                    AND curr.collected_at = snap.collected_at
                    AND curr.database_name = prev.database_name
                    AND curr.extname = prev.extname
              )
        ) d
        JOIN connections c ON d.connection_id = c.id
        WHERE TRUE %s
        GROUP BY d.connection_id, d.collected_at, c.name
    `, EventTypeExtensionChange, extraFilter)
}

// buildExtensionChangeCountQuery creates the count query for extension
// changes. Counts only snapshots that have at least one actual diff from
// the previous snapshot.
func buildExtensionChangeCountQuery(whereClause string) string {
	tableWhere := strings.ReplaceAll(whereClause, "event_time", "d.collected_at")
	tableWhere = strings.ReplaceAll(tableWhere, "connection_id", "d.connection_id")

	extraFilter := ""
	if tableWhere != "" {
		extraFilter = "AND " + strings.TrimPrefix(tableWhere, "WHERE ")
	}

	return fmt.Sprintf(`
        SELECT COUNT(*) FROM (
            SELECT snap.connection_id, snap.collected_at
            FROM (
                SELECT DISTINCT connection_id, collected_at,
                    LAG(collected_at) OVER (
                        PARTITION BY connection_id ORDER BY collected_at
                    ) AS prev_collected_at
                FROM metrics.pg_extension
            ) snap
            WHERE snap.prev_collected_at IS NOT NULL
              AND (
                  EXISTS (
                      SELECT 1
                      FROM metrics.pg_extension curr
                      LEFT JOIN metrics.pg_extension prev
                          ON prev.connection_id = snap.connection_id
                          AND prev.collected_at = snap.prev_collected_at
                          AND prev.database_name = curr.database_name
                          AND prev.extname = curr.extname
                      WHERE curr.connection_id = snap.connection_id
                        AND curr.collected_at = snap.collected_at
                        AND (prev.extname IS NULL
                             OR curr.extversion IS DISTINCT FROM prev.extversion)
                  )
                  OR EXISTS (
                      SELECT 1
                      FROM metrics.pg_extension prev
                      WHERE prev.connection_id = snap.connection_id
                        AND prev.collected_at = snap.prev_collected_at
                        AND NOT EXISTS (
                            SELECT 1 FROM metrics.pg_extension curr
                            WHERE curr.connection_id = snap.connection_id
                              AND curr.collected_at = snap.collected_at
                              AND curr.database_name = prev.database_name
                              AND curr.extname = prev.extname
                        )
                  )
              )
        ) d
        WHERE TRUE %s
    `, extraFilter)
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
