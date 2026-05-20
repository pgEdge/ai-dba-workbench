/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// timelineEventTypes lists the event types accepted by the tool. Values
// match the constants in database/timeline_queries.go.
var timelineEventTypes = []string{
	database.EventTypeConfigChange,
	database.EventTypeHBAChange,
	database.EventTypeIdentChange,
	database.EventTypeRestart,
	database.EventTypeAlertFired,
	database.EventTypeAlertCleared,
	database.EventTypeAlertAcknowledged,
	database.EventTypeExtensionChange,
	database.EventTypeBlackoutStarted,
	database.EventTypeBlackoutEnded,
}

const (
	timelineDefaultLimit = 100
	timelineMaxLimit     = 500
	timelineDefaultRange = 24 * time.Hour
)

// GetTimelineEventsTool creates the get_timeline_events tool for querying
// the unified incident-investigation timeline.
//
// The tool wraps database.GetTimelineEvents and gates results behind the
// same RBAC checks used by get_alert_history: callers may only see events
// for connections they have access to. The visibilityLister argument may
// be nil in unit tests; auth.RBACChecker.VisibleConnectionIDs tolerates a
// nil lister by falling back to group/token-granted IDs only.
func GetTimelineEventsTool(datastore *database.Datastore, rbacChecker *auth.RBACChecker, visibilityLister auth.ConnectionVisibilityLister) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "get_timeline_events",
			Description: `Query the incident-investigation timeline of significant events.

<database_context>
This tool queries the DATASTORE for events that matter most when
investigating incidents: configuration changes, HBA edits, ident-mapping
updates, server restarts, extension changes, blackout start and end
markers, and alert fired/cleared/acknowledged events. Events from all
sources are merged and returned in descending time order.
</database_context>

<important_behavior>
ALWAYS check pg://connection_info first to find the current connection.

If a connection IS selected (connected: true):
- Specify connection_id to filter the timeline for that connection
- "My database" or "the database" means the currently selected connection

If NO connection is selected (connected: false):
- Omit connection_id to see events across ALL accessible connections
- The user can also specify a connection_id to filter to one connection

Use this tool when the user asks what changed before an incident, what
restarts or config changes have occurred, or to correlate alerts with
the underlying changes that may have caused them.
</important_behavior>

<parameters>
- connection_id: (optional) ID of a monitored connection. Omit to return events across all accessible connections.
- start_time: Start of time range (default: 24h). Relative duration (1h, 24h, 7d) or ISO 8601 format.
- end_time: End of time range (default: now). ISO 8601 format.
- event_types: (optional) Comma-separated list of event types to include. Valid values: config_change, hba_change, ident_change, restart, alert_fired, alert_cleared, alert_acknowledged, extension_change, blackout_started, blackout_ended.
- limit: Maximum number of events to return (1-500, default: 100).
</parameters>

<output>
Returns TSV data ordered by event time (most recent first):
- connection_id: Connection ID the event applies to (0 for non-server-scoped blackouts)
- server_name: Connection or scope name
- event_time: When the event occurred (RFC 3339)
- event_type: One of the timeline event types
- severity: info, warning, or critical
- title: Short human-readable title
- summary: One-line description of the event
- id: Event identifier (composite, unique per row)
</output>

<examples>
- get_timeline_events() - last 24 hours across all accessible connections
- get_timeline_events(connection_id=5) - timeline for connection 5
- get_timeline_events(start_time="7d") - last seven days
- get_timeline_events(event_types="config_change,restart") - only restarts and config changes
- get_timeline_events(start_time="2026-05-01T00:00:00Z", end_time="2026-05-02T00:00:00Z") - explicit window
</examples>`,
			CompactDescription: `Query the incident-investigation timeline of configuration changes, restarts, HBA/ident edits, extension changes, alerts, and blackouts across monitored connections. Defaults to the last 24 hours.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "ID of a monitored connection. Omit to return events across all accessible connections.",
					},
					"start_time": map[string]any{
						"type":        "string",
						"description": "Start of time range. Relative duration (e.g. '1h', '24h', '7d') or ISO 8601. Default: 24h.",
						"default":     "24h",
					},
					"end_time": map[string]any{
						"type":        "string",
						"description": "End of time range. ISO 8601. Default: now.",
					},
					"event_types": map[string]any{
						"type":        "string",
						"description": "Comma-separated event types to include. Valid: config_change, hba_change, ident_change, restart, alert_fired, alert_cleared, alert_acknowledged, extension_change, blackout_started, blackout_ended.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of events to return (1-500). Default: 100.",
						"default":     timelineDefaultLimit,
						"minimum":     1,
						"maximum":     timelineMaxLimit,
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			if datastore == nil {
				return mcp.NewToolError("Datastore not configured. The get_timeline_events tool requires a datastore connection.")
			}

			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Single-connection vs multi-connection mode
			singleConnection := false
			var connectionID int
			var connName string
			pool := datastore.GetPool()
			if _, hasConnID := args["connection_id"]; hasConnID {
				cid, err := parseIntArg(args, "connection_id")
				if err != nil {
					return mcp.NewToolError("Invalid 'connection_id' parameter: must be an integer. Use list_connections to find available connection IDs.")
				}
				connectionID = cid
				singleConnection = true

				if pool != nil {
					err = pool.QueryRow(ctx, "SELECT name FROM connections WHERE id = $1", connectionID).Scan(&connName)
					if err != nil {
						return mcp.NewToolError(fmt.Sprintf("Connection ID %d does not exist. Use list_connections to see available connections.", connectionID))
					}
				}

				if rbacChecker != nil {
					canAccess, _ := rbacChecker.CanAccessConnection(ctx, connectionID)
					if !canAccess {
						return mcp.NewToolError(fmt.Sprintf("Access denied: you do not have permission to access connection ID %d.", connectionID))
					}
				}
			}

			// Resolve accessible connections for multi-connection mode. The
			// semantics mirror get_alert_history: superusers / wildcard
			// tokens see allConnections=true; restricted users get an
			// explicit set; a zero-length set with allConnections=false
			// means "no access" and we short-circuit.
			var accessibleIDs []int
			allConnections := true
			if !singleConnection && rbacChecker != nil {
				ids, all, err := rbacChecker.VisibleConnectionIDs(ctx, visibilityLister)
				if err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to resolve accessible connections: %v", err))
				}
				accessibleIDs = ids
				allConnections = all
				if !allConnections && len(accessibleIDs) == 0 {
					return mcp.NewToolSuccess("No timeline events found. You do not have access to any connections.")
				}
			}

			// Parse limit (default 100, clamped to [1, 500]).
			limit := timelineDefaultLimit
			if limitVal, ok := args["limit"]; ok && limitVal != nil {
				l, err := parseIntArg(args, "limit")
				if err != nil {
					return mcp.NewToolError("Invalid 'limit' parameter: must be an integer between 1 and 500")
				}
				if l < 1 {
					return mcp.NewToolError("Invalid 'limit' parameter: must be an integer between 1 and 500")
				}
				if l > timelineMaxLimit {
					l = timelineMaxLimit
				}
				limit = l
			}

			// Parse end_time (default: now).
			endTime := time.Now().UTC()
			if endStr, ok := args["end_time"].(string); ok && endStr != "" && endStr != "now" {
				parsed, err := parseTimeArg(endStr)
				if err != nil {
					return mcp.NewToolError("Invalid 'end_time' parameter: use ISO 8601 format")
				}
				endTime = parsed
			}

			// Parse start_time (default: end_time - 24h). Accept either a
			// relative duration ("24h", "7d") or an absolute timestamp.
			startTime := endTime.Add(-timelineDefaultRange)
			if startStr, ok := args["start_time"].(string); ok && startStr != "" {
				if dur, err := parseRelativeDuration(startStr); err == nil {
					startTime = endTime.Add(-dur)
				} else if parsed, err := parseTimeArg(startStr); err == nil {
					startTime = parsed
				} else {
					return mcp.NewToolError("Invalid 'start_time' parameter: use relative duration (e.g., '24h', '7d') or ISO 8601 format")
				}
			}

			if !startTime.Before(endTime) {
				return mcp.NewToolError("Invalid time range: start_time must be before end_time")
			}

			// Parse and validate event_types filter.
			var eventTypes []string
			if etRaw, ok := args["event_types"].(string); ok && etRaw != "" {
				valid := make(map[string]bool, len(timelineEventTypes))
				for _, t := range timelineEventTypes {
					valid[t] = true
				}
				for _, raw := range strings.Split(etRaw, ",") {
					t := strings.TrimSpace(raw)
					if t == "" {
						continue
					}
					if !valid[t] {
						return mcp.NewToolError(fmt.Sprintf("Invalid event_type %q. Valid values: %s",
							t, strings.Join(timelineEventTypes, ", ")))
					}
					eventTypes = append(eventTypes, t)
				}
			}

			filter := database.TimelineFilter{
				StartTime:  startTime,
				EndTime:    endTime,
				EventTypes: eventTypes,
				Limit:      limit,
			}
			if singleConnection {
				cid := connectionID
				filter.ConnectionID = &cid
			} else if !allConnections {
				filter.ConnectionIDs = accessibleIDs
			}

			result, err := datastore.GetTimelineEvents(ctx, filter)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query timeline events: %v", err))
			}

			return mcp.NewToolSuccess(formatTimelineEvents(result, singleConnection, connectionID, connName, startTime, endTime, limit))
		},
	}
}

// formatTimelineEvents renders a TimelineResult as TSV with a header
// describing the request parameters and a trailing row count.
func formatTimelineEvents(result *database.TimelineResult, singleConnection bool, connectionID int, connName string, startTime, endTime time.Time, limit int) string {
	var sb strings.Builder

	if singleConnection {
		fmt.Fprintf(&sb, "Timeline Events | Connection: %d (%s) | Window: %s - %s | Limit: %d\n\n",
			connectionID, connName,
			startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), limit)
	} else {
		fmt.Fprintf(&sb, "Timeline Events | All accessible connections | Window: %s - %s | Limit: %d\n\n",
			startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), limit)
	}

	sb.WriteString("connection_id\tserver_name\tevent_time\tevent_type\tseverity\ttitle\tsummary\tid\n")

	rowCount := 0
	if result != nil {
		for i := range result.Events {
			e := &result.Events[i]
			fmt.Fprintf(&sb, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				e.ConnectionID,
				tsv.FormatValue(e.ServerName),
				e.OccurredAt.Format(time.RFC3339),
				e.EventType,
				e.Severity,
				tsv.FormatValue(e.Title),
				tsv.FormatValue(e.Summary),
				tsv.FormatValue(e.ID))
			rowCount++
		}
	}

	if rowCount == 0 {
		if singleConnection {
			return fmt.Sprintf("No timeline events for connection %d (%s) in the specified time range.", connectionID, connName)
		}
		return "No timeline events found across accessible connections in the specified time range."
	}

	total := rowCount
	if result != nil {
		total = result.TotalCount
	}
	fmt.Fprintf(&sb, "\n(%d rows, %d total in window)\n", rowCount, total)
	return sb.String()
}
