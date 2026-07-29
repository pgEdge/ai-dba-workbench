/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// ConnectionGroupsResponse is the top-level JSON response for
// GET /api/v1/metrics/connection-groups. CollectedAt carries the timestamp
// of the snapshot the counts were taken from, and is null when no snapshot
// was found inside the requested window.
type ConnectionGroupsResponse struct {
	CollectedAt *time.Time           `json:"collected_at"`
	Groups      []ConnectionGroupRow `json:"groups"`
}

// ConnectionGroupRow holds the connection counts for a single group, broken
// down by backend state. ClientHostname is only populated for the "client"
// grouping, and only when at least one backend in the group reported a
// reverse-resolved hostname.
type ConnectionGroupRow struct {
	GroupLabel        string  `json:"group_label"`
	ClientHostname    *string `json:"client_hostname"`
	Total             int64   `json:"total"`
	Active            int64   `json:"active"`
	Idle              int64   `json:"idle"`
	IdleInTransaction int64   `json:"idle_in_transaction"`
	Other             int64   `json:"other"`
}

// connectionGroupSpec holds the whitelisted SQL fragments used to build the
// grouping key and the per-group client hostname for one group_by value.
// Both fragments are compile-time constants; no user-supplied value is ever
// placed in either of them.
type connectionGroupSpec struct {
	// labelExpr is the expression that produces the group label.
	labelExpr string
	// hostnameExpr is the aggregate that produces the group's client
	// hostname, or a typed NULL for groupings where a hostname is
	// meaningless.
	hostnameExpr string
}

// defaultConnectionGroupBy is the group_by value used when the caller omits
// the parameter.
const defaultConnectionGroupBy = "user"

// maxConnectionGroups bounds the number of groups returned. Group cardinality
// is at worst the monitored server's max_connections, which is influenceable
// from outside the workbench: whoever can reach the monitored server from many
// source addresses can inflate the distinct-client count for
// group_by=client. The bound keeps one snapshot's worth of pathological
// activity from turning into an unbounded response body. Because the ordering
// is total-descending, truncation discards only the least significant groups,
// and no roll-up row is synthesized for the remainder.
const maxConnectionGroups = 200

// connectionGroupByColumns is the whitelist of accepted group_by values,
// mapped to the SQL fragments each one selects. A value absent from this map
// is rejected with a 400 before any SQL is built.
var connectionGroupByColumns = map[string]connectionGroupSpec{
	// A backend with no recorded role name is grouped under a placeholder
	// rather than silently dropped.
	"user": {
		labelExpr:    `COALESCE(NULLIF(usename, ''), '(unknown)')`,
		hostnameExpr: `NULL::text`,
	},
	// A NULL client_addr means the backend arrived over a Unix-domain
	// socket, so it is labeled "local" rather than treated as unknown.
	// host() is used in preference to a plain ::text cast because casting
	// an inet to text appends the netmask, which would label every client
	// as "192.0.2.10/32" rather than "192.0.2.10".
	"client": {
		labelExpr:    `COALESCE(host(client_addr), 'local')`,
		hostnameExpr: `MIN(client_hostname)`,
	},
	"database": {
		labelExpr:    `COALESCE(NULLIF(datname, ''), '(none)')`,
		hostnameExpr: `NULL::text`,
	},
}

// sortedConnectionGroupByValues returns the accepted group_by values in
// alphabetical order, so that the 400 response wording is stable rather than
// dependent on Go's randomized map iteration order.
func sortedConnectionGroupByValues() []string {
	values := make([]string, 0, len(connectionGroupByColumns))
	for value := range connectionGroupByColumns {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

// connectionGroupByError builds the 400 message listing the accepted
// group_by values.
func connectionGroupByError() string {
	return "Invalid group_by: must be one of " +
		strings.Join(sortedConnectionGroupByValues(), ", ")
}

// buildConnectionGroupsSQL returns the query and bound arguments that
// aggregate the most recent pg_stat_activity snapshot for a connection into
// per-group connection counts.
//
// The counts come from the single newest collected_at inside the requested
// window; the window only decides which snapshot counts as the latest, and
// the results are neither averaged nor peaked across it. Only real client
// connections are counted, because the probe also stores background workers
// such as the walwriter, which are not connections in any useful sense.
//
// The snapshot CTE repeats the window bounds even though the join to latest
// already pins collected_at to a value inside the window. The repetition is
// semantically a no-op, but metrics.pg_stat_activity is partitioned by range
// on collected_at, and without a direct bound on the partition key the planner
// has nothing to prune with: it keeps every retained partition in the plan.
// Measured against a 90-partition table with a generic plan, the bounds cut
// the snapshot Append from 90 sub-plans to 2.
//
// Only whitelisted SQL constants are interpolated into the query text; the
// connection ID and both window bounds are bound as $1, $2 and $3. An
// unrecognized groupBy falls back to the default grouping. Callers validate
// groupBy against connectionGroupByColumns and reject unknown values with a
// 400, so that fallback is a defensive measure rather than a reachable path.
func buildConnectionGroupsSQL(
	groupBy string,
	connectionID int,
	startTime, endTime time.Time,
) (string, []any) {
	spec, ok := connectionGroupByColumns[groupBy]
	if !ok {
		spec = connectionGroupByColumns[defaultConnectionGroupBy]
	}

	query := fmt.Sprintf(`
        WITH latest AS (
            SELECT MAX(collected_at) AS collected_at
            FROM metrics.pg_stat_activity
            WHERE connection_id = $1
              AND collected_at >= $2
              AND collected_at <= $3
        ),
        snapshot AS (
            SELECT psa.usename, psa.datname, psa.client_addr,
                   psa.client_hostname, psa.state, psa.collected_at
            FROM metrics.pg_stat_activity psa
            JOIN latest l ON psa.collected_at = l.collected_at
            WHERE psa.connection_id = $1
              AND psa.collected_at >= $2
              AND psa.collected_at <= $3
              AND psa.backend_type = 'client backend'
        )
        SELECT %s AS group_label,
               %s AS client_hostname,
               MAX(collected_at) AS collected_at,
               COUNT(*) AS total,
               COUNT(*) FILTER (WHERE state = 'active') AS active,
               COUNT(*) FILTER (WHERE state = 'idle') AS idle,
               COUNT(*) FILTER (
                   WHERE state LIKE 'idle in transaction%%'
               ) AS idle_in_transaction,
               COUNT(*) FILTER (
                   WHERE state IS NULL
                      OR (state <> 'active'
                          AND state <> 'idle'
                          AND state NOT LIKE 'idle in transaction%%')
               ) AS other
        FROM snapshot
        GROUP BY group_label
        ORDER BY total DESC, group_label ASC
        LIMIT %d
    `, spec.labelExpr, spec.hostnameExpr, maxConnectionGroups)

	return query, []any{connectionID, startTime, endTime}
}

// handleConnectionGroups handles GET /api/v1/metrics/connection-groups. It
// reports the active connections in the latest pg_stat_activity snapshot,
// grouped by database user, client address or database, and broken down by
// backend state.
func (h *PerfSummaryHandler) handleConnectionGroups(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	connectionIDs := h.parseConnectionIDs(w, r)
	if connectionIDs == nil {
		return
	}
	if len(connectionIDs) != 1 {
		RespondError(w, http.StatusBadRequest,
			"Exactly one connection_id is required")
		return
	}
	connID := connectionIDs[0]

	rbacChecker := auth.NewRBACCheckerWithSharing(h.authStore, h.datastore.GetConnectionSharingInfo)
	canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			fmt.Sprintf("Permission denied: you do not have access to connection %d", connID))
		return
	}

	groupBy := ParseQueryString(r, "group_by")
	if groupBy == "" {
		groupBy = defaultConnectionGroupBy
	}
	if _, ok := connectionGroupByColumns[groupBy]; !ok {
		RespondError(w, http.StatusBadRequest, connectionGroupByError())
		return
	}

	timeRange := ParseQueryString(r, "time_range")
	if timeRange == "" {
		timeRange = "24h"
	}
	duration, ok := validTimeRanges[timeRange]
	if !ok {
		RespondError(w, http.StatusBadRequest,
			"Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d")
		return
	}

	now := time.Now().UTC()
	startTime := now.Add(-duration)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		log.Printf("[ERROR] Failed to begin read-only transaction: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to query connection groups")
		return
	}
	// Rolled back with a fresh context so that the rollback still runs
	// even once ctx has been canceled or has timed out.
	defer tx.Rollback(context.Background()) //nolint:errcheck // Rollback after commit is a no-op

	response := h.queryConnectionGroups(ctx, tx, groupBy, connID, startTime,
		now)

	if err := tx.Commit(ctx); err != nil {
		log.Printf("[ERROR] Failed to commit read-only transaction: %v", err)
	}

	RespondJSON(w, http.StatusOK, response)
}

// queryConnectionGroups runs the connection-groups aggregation and returns
// the response body. A query error is treated as "no data" and yields an
// empty response, matching the behavior of the sibling metrics endpoints:
// the dashboard renders an empty panel rather than an error when a probe has
// never run against the connection.
func (h *PerfSummaryHandler) queryConnectionGroups(
	ctx context.Context,
	tx pgx.Tx,
	groupBy string,
	connectionID int,
	startTime, endTime time.Time,
) ConnectionGroupsResponse {
	response := ConnectionGroupsResponse{
		Groups: []ConnectionGroupRow{},
	}

	query, args := buildConnectionGroupsSQL(groupBy, connectionID, startTime,
		endTime)

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		log.Printf("[DEBUG] No connection group data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return response
	}
	defer rows.Close()

	for rows.Next() {
		var row ConnectionGroupRow
		var collectedAt *time.Time
		if err := rows.Scan(
			&row.GroupLabel, &row.ClientHostname, &collectedAt,
			&row.Total, &row.Active, &row.Idle, &row.IdleInTransaction,
			&row.Other,
		); err != nil {
			log.Printf("[DEBUG] Error scanning connection group row: %s", logging.SanitizeForLog(err.Error()))
			continue
		}
		// Every group aggregates the same snapshot, so the first
		// non-NULL timestamp describes the whole response.
		if response.CollectedAt == nil {
			response.CollectedAt = collectedAt
		}
		response.Groups = append(response.Groups, row)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating connection group rows: %s", logging.SanitizeForLog(err.Error()))
	}

	return response
}
