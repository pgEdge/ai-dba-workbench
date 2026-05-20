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

	"github.com/jackc/pgx/v5/pgxpool"
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

// timelineToolDescription is the long-form description shown to clients
// when they introspect the get_timeline_events tool. It lives at package
// scope so the schema builder stays short enough to fit under Lizard's
// NLOC limit; the contents are unchanged from the previous in-function
// literal.
const timelineToolDescription = `Query the incident-investigation timeline of significant events.

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
</examples>`

// timelineToolCompactDescription is the short single-line description.
const timelineToolCompactDescription = `Query the incident-investigation timeline of configuration changes, restarts, HBA/ident edits, extension changes, alerts, and blackouts across monitored connections. Defaults to the last 24 hours.`

// timelineRequest captures the parsed and RBAC-validated inputs for a
// single get_timeline_events invocation. It is constructed by
// buildTimelineRequest and consumed by the tool handler.
type timelineRequest struct {
	singleConnection bool
	connectionID     int
	connName         string
	allConnections   bool
	accessibleIDs    []int
	startTime        time.Time
	endTime          time.Time
	eventTypes       []string
	limit            int
}

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
		Definition: timelineToolDefinition(),
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			return runTimelineHandler(args, datastore, rbacChecker, visibilityLister)
		},
	}
}

// timelineToolDefinition returns the mcp.Tool definition (name, schema,
// and prose descriptions) for get_timeline_events. The prose lives in
// timelineToolDescription / timelineToolCompactDescription so this
// builder stays short enough to satisfy the Lizard NLOC limit.
func timelineToolDefinition() mcp.Tool {
	return mcp.Tool{
		Name:               "get_timeline_events",
		Description:        timelineToolDescription,
		CompactDescription: timelineToolCompactDescription,
		InputSchema:        timelineToolInputSchema(),
	}
}

// timelineToolInputSchema describes the parameters accepted by
// get_timeline_events. Each entry mirrors the documentation in
// timelineToolDescription; both must stay in sync.
func timelineToolInputSchema() mcp.InputSchema {
	return mcp.InputSchema{
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
	}
}

// runTimelineHandler executes a single get_timeline_events call. It
// validates the datastore, builds the parsed timelineRequest, runs the
// query, and formats the result. All parse/RBAC errors short-circuit
// here via the *mcp.ToolResponse returned by buildTimelineRequest.
func runTimelineHandler(
	args map[string]any,
	datastore *database.Datastore,
	rbacChecker *auth.RBACChecker,
	visibilityLister auth.ConnectionVisibilityLister,
) (mcp.ToolResponse, error) {
	if datastore == nil {
		return mcp.NewToolError("Datastore not configured. The get_timeline_events tool requires a datastore connection.")
	}

	ctx, ok := args["__context"].(context.Context)
	if !ok {
		ctx = context.Background()
	}

	req, earlyResp, err := buildTimelineRequest(ctx, args, datastore, rbacChecker, visibilityLister)
	if earlyResp != nil {
		return *earlyResp, err
	}

	filter := timelineFilterFromRequest(req)
	result, qerr := datastore.GetTimelineEvents(ctx, filter)
	if qerr != nil {
		return mcp.NewToolError(fmt.Sprintf("Failed to query timeline events: %v", qerr))
	}

	return mcp.NewToolSuccess(formatTimelineEvents(result, req.singleConnection, req.connectionID, req.connName, req.startTime, req.endTime, req.limit))
}

// buildTimelineRequest parses and validates every argument map entry the
// handler cares about. When a parse, RBAC, or short-circuit response is
// required it returns that response (with the accompanying error from
// the mcp helper, which is always nil today) so the caller can forward
// it verbatim. On success it returns a populated timelineRequest and a
// nil response pointer.
func buildTimelineRequest(
	ctx context.Context,
	args map[string]any,
	datastore *database.Datastore,
	rbacChecker *auth.RBACChecker,
	visibilityLister auth.ConnectionVisibilityLister,
) (timelineRequest, *mcp.ToolResponse, error) {
	var req timelineRequest

	single, connID, connName, resp, err := resolveTimelineConnection(ctx, args, datastore.GetPool(), rbacChecker)
	if resp != nil {
		return req, resp, err
	}
	req.singleConnection = single
	req.connectionID = connID
	req.connName = connName

	accessibleIDs, allConnections, resp, err := resolveTimelineAccessibleIDs(ctx, single, rbacChecker, visibilityLister)
	if resp != nil {
		return req, resp, err
	}
	req.accessibleIDs = accessibleIDs
	req.allConnections = allConnections

	limit, resp, err := parseTimelineLimit(args)
	if resp != nil {
		return req, resp, err
	}
	req.limit = limit

	start, end, resp, err := parseTimelineWindow(args)
	if resp != nil {
		return req, resp, err
	}
	req.startTime = start
	req.endTime = end

	eventTypes, resp, err := parseTimelineEventTypes(args)
	if resp != nil {
		return req, resp, err
	}
	req.eventTypes = eventTypes

	return req, nil, nil
}

// timelineFilterFromRequest projects a validated timelineRequest onto
// the database.TimelineFilter struct used by GetTimelineEvents. The
// connection scoping rules mirror the original handler: a single
// connection populates ConnectionID; a restricted multi-connection mode
// populates ConnectionIDs; an unrestricted multi-connection mode leaves
// both unset so the underlying query sees no connection filter.
func timelineFilterFromRequest(req timelineRequest) database.TimelineFilter {
	filter := database.TimelineFilter{
		StartTime:  req.startTime,
		EndTime:    req.endTime,
		EventTypes: req.eventTypes,
		Limit:      req.limit,
	}
	if req.singleConnection {
		cid := req.connectionID
		filter.ConnectionID = &cid
	} else if !req.allConnections {
		filter.ConnectionIDs = req.accessibleIDs
	}
	return filter
}

// resolveTimelineConnection parses an optional connection_id argument,
// looks up the connection name when a pool is available, and performs
// the per-connection RBAC check. When connection_id is absent it
// returns singleConnection=false and a nil short-circuit response. A
// non-nil *mcp.ToolResponse indicates the caller should return that
// response immediately.
func resolveTimelineConnection(
	ctx context.Context,
	args map[string]any,
	pool *pgxpool.Pool,
	rbacChecker *auth.RBACChecker,
) (bool, int, string, *mcp.ToolResponse, error) {
	if _, hasConnID := args["connection_id"]; !hasConnID {
		return false, 0, "", nil, nil
	}

	cid, err := parseIntArg(args, "connection_id")
	if err != nil {
		resp, rerr := mcp.NewToolError("Invalid 'connection_id' parameter: must be an integer. Use list_connections to find available connection IDs.")
		return false, 0, "", &resp, rerr
	}

	var connName string
	if pool != nil {
		if qerr := pool.QueryRow(ctx, "SELECT name FROM connections WHERE id = $1", cid).Scan(&connName); qerr != nil {
			resp, rerr := mcp.NewToolError(fmt.Sprintf("Connection ID %d does not exist. Use list_connections to see available connections.", cid))
			return false, 0, "", &resp, rerr
		}
	}

	if rbacChecker != nil {
		canAccess, _ := rbacChecker.CanAccessConnection(ctx, cid)
		if !canAccess {
			resp, rerr := mcp.NewToolError(fmt.Sprintf("Access denied: you do not have permission to access connection ID %d.", cid))
			return false, 0, "", &resp, rerr
		}
	}

	return true, cid, connName, nil, nil
}

// resolveTimelineAccessibleIDs wraps RBACChecker.VisibleConnectionIDs
// for the multi-connection mode. When singleConnection is true the
// caller has already scoped the query, so this returns the "all
// connections" default unchanged. When the caller has no visible
// connections at all the function returns the explicit "no access"
// success message used by the original handler; tests check for this
// exact wording.
func resolveTimelineAccessibleIDs(
	ctx context.Context,
	singleConnection bool,
	rbacChecker *auth.RBACChecker,
	visibilityLister auth.ConnectionVisibilityLister,
) ([]int, bool, *mcp.ToolResponse, error) {
	if singleConnection || rbacChecker == nil {
		return nil, true, nil, nil
	}

	ids, all, err := rbacChecker.VisibleConnectionIDs(ctx, visibilityLister)
	if err != nil {
		resp, rerr := mcp.NewToolError(fmt.Sprintf("Failed to resolve accessible connections: %v", err))
		return nil, false, &resp, rerr
	}

	if !all && len(ids) == 0 {
		resp, rerr := mcp.NewToolSuccess("No timeline events found. You do not have access to any connections.")
		return nil, false, &resp, rerr
	}

	return ids, all, nil, nil
}

// parseTimelineLimit parses, validates, and clamps the optional limit
// argument. It rejects non-integer values and integers below 1 with
// the exact error string the integration tests check for, and clamps
// values above timelineMaxLimit to the maximum.
func parseTimelineLimit(args map[string]any) (int, *mcp.ToolResponse, error) {
	limitVal, ok := args["limit"]
	if !ok || limitVal == nil {
		return timelineDefaultLimit, nil, nil
	}

	l, err := parseIntArg(args, "limit")
	if err != nil || l < 1 {
		resp, rerr := mcp.NewToolError("Invalid 'limit' parameter: must be an integer between 1 and 500")
		return 0, &resp, rerr
	}
	if l > timelineMaxLimit {
		l = timelineMaxLimit
	}
	return l, nil, nil
}

// parseTimelineWindow parses optional end_time and start_time arguments
// and validates the resulting range. end_time defaults to now and
// accepts ISO 8601; start_time defaults to end_time - 24h and accepts
// either a relative duration ("24h", "7d") or an ISO 8601 timestamp.
// An inverted range yields the documented error.
func parseTimelineWindow(args map[string]any) (time.Time, time.Time, *mcp.ToolResponse, error) {
	endTime := time.Now().UTC()
	if endStr, ok := args["end_time"].(string); ok && endStr != "" && endStr != "now" {
		parsed, err := parseTimeArg(endStr)
		if err != nil {
			resp, rerr := mcp.NewToolError("Invalid 'end_time' parameter: use ISO 8601 format")
			return time.Time{}, time.Time{}, &resp, rerr
		}
		endTime = parsed
	}

	startTime := endTime.Add(-timelineDefaultRange)
	if startStr, ok := args["start_time"].(string); ok && startStr != "" {
		parsed, perr := parseTimelineStart(startStr, endTime)
		if perr != nil {
			resp, rerr := mcp.NewToolError("Invalid 'start_time' parameter: use relative duration (e.g., '24h', '7d') or ISO 8601 format")
			return time.Time{}, time.Time{}, &resp, rerr
		}
		startTime = parsed
	}

	if !startTime.Before(endTime) {
		resp, rerr := mcp.NewToolError("Invalid time range: start_time must be before end_time")
		return time.Time{}, time.Time{}, &resp, rerr
	}
	return startTime, endTime, nil, nil
}

// parseTimelineStart resolves the start_time string against the
// already-parsed endTime anchor. It first tries to read the value as a
// relative duration (e.g. "24h", "7d") which is subtracted from
// endTime, then falls back to an absolute ISO 8601 timestamp. The
// caller maps a non-nil error onto the documented error response.
func parseTimelineStart(startStr string, endTime time.Time) (time.Time, error) {
	if dur, err := parseRelativeDuration(startStr); err == nil {
		return endTime.Add(-dur), nil
	}
	if parsed, err := parseTimeArg(startStr); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid start_time")
}

// parseTimelineEventTypes parses an optional comma-separated event_types
// filter against the canonical timelineEventTypes set. Empty entries
// (e.g. trailing commas or whitespace) are silently skipped; an
// unrecognized entry produces the documented error.
func parseTimelineEventTypes(args map[string]any) ([]string, *mcp.ToolResponse, error) {
	etRaw, ok := args["event_types"].(string)
	if !ok || etRaw == "" {
		return nil, nil, nil
	}

	valid := make(map[string]bool, len(timelineEventTypes))
	for _, t := range timelineEventTypes {
		valid[t] = true
	}

	var eventTypes []string
	for _, raw := range strings.Split(etRaw, ",") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if !valid[t] {
			resp, rerr := mcp.NewToolError(fmt.Sprintf("Invalid event_type %q. Valid values: %s",
				t, strings.Join(timelineEventTypes, ", ")))
			return nil, &resp, rerr
		}
		eventTypes = append(eventTypes, t)
	}
	return eventTypes, nil, nil
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
