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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// queryDatastoreDescription is the full description of query_datastore.
const queryDatastoreDescription = `Query the DATASTORE (the workbench's own store of collected metrics, alerts and configuration) with a structured request. The tool does not accept SQL: it builds the query itself from the parameters below, so it can only return data about the connections you can access.

<database_context>
The datastore is the workbench's internal PostgreSQL database, not a monitored
database. It holds two kinds of source:
- metric sources: one per probe (pg_stat_database, pg_stat_activity, ...),
  keyed by connection_id and collected_at.
- configuration sources: connections, alerts, blackouts, thresholds, probe
  settings, baselines, clusters and similar tables, each limited to the
  columns that are safe to read. Stored credentials are never available.
</database_context>

<workflow>
1. Call describe_datastore with no arguments to list the sources.
2. Call describe_datastore with source set to see that source's columns.
3. Call query_datastore with the source and the columns you need.
</workflow>

<parameters>
- source (required): a source name from describe_datastore.
- columns: columns to return; defaults to every column of the source.
- connection_ids: limit to these connections. Omit to cover every connection
  you can access. Naming a connection you cannot access is an error.
- time_start / time_end: a relative duration ("1h", "24h", "7d"), "now", or
  an RFC 3339 timestamp. Only for sources with a time column. Metric sources
  default to the last hour.
- filters: [{"column": "datname", "op": "=", "value": "app"}]. Operators:
  =, !=, <, <=, >, >=, in, not in (value is a list), like, is null,
  is not null (no value).
- shape: "rows" (default), "latest" (most recent row per connection and
  entity; metric sources only) or "aggregate".
- group_by, aggregation (avg, sum, min, max, count) and buckets (time
  buckets): only with shape "aggregate".
- order_by: [{"column": "collected_at", "direction": "desc"}].
- page / page_size: page_size defaults to 100, at most 1000.
- explain: true returns the query plan instead of rows.
</parameters>

<when_not_to_use>
- Counter rates and deltas over time: use query_metrics.
- Querying a monitored database: use query_database.
- Formatted views: list_connections, get_alert_history, get_blackouts,
  get_metric_baselines.
</when_not_to_use>

<examples>
- Latest database statistics for connection 3:
  {"source": "pg_stat_database", "connection_ids": [3], "shape": "latest", "columns": ["datname", "numbackends", "xact_commit"]}
- Hourly average backends over a day:
  {"source": "pg_stat_database", "time_start": "24h", "shape": "aggregate", "aggregation": "avg", "columns": ["numbackends"], "group_by": ["connection_id"], "buckets": 24}
- Active alerts: {"source": "alerts", "filters": [{"column": "status", "op": "=", "value": "active"}]}
</examples>

<important>
- Results are TSV. When more rows exist, the response says which page to
  ask for next.
- Queries run in read-only transactions with a statement timeout.
</important>`

// QueryDatastoreTool creates the query_datastore tool, which runs a
// structured, server-built query against one datastore source. Results
// are limited to the connections rbacChecker reports the caller can
// read, and to the columns the source catalog permits. visibilityLister
// may be nil when no datastore is configured.
func QueryDatastoreTool(pool *pgxpool.Pool, rbacChecker *auth.RBACChecker, visibilityLister auth.ConnectionVisibilityLister) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name:               "query_datastore",
			Description:        queryDatastoreDescription,
			CompactDescription: `Query one DATASTORE source (a metrics probe, or alerts, blackouts, connections and other configuration) with structured parameters; SQL is not accepted. Call describe_datastore first to list sources and columns. Use query_metrics for counter rates.`,
			InputSchema:        queryDatastoreInputSchema(),
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			req, err := parseDatastoreRequest(args)
			if err != nil {
				return mcp.NewToolError(err.Error())
			}
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The query_datastore tool requires a datastore connection.")
			}

			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			src, err := lookupDatastoreSource(ctx, pool, req.Source)
			if err != nil {
				if errors.Is(err, errUnknownDatastoreSource) {
					return mcp.NewToolError(err.Error())
				}
				logging.Error("query_datastore_lookup_failed", "source", req.Source, "error", err)
				return mcp.NewToolError("Failed to look up the source")
			}

			// The visible set fails closed: without it the query cannot
			// be scoped, so it does not run at all.
			ids, all, err := rbacChecker.VisibleConnectionIDs(ctx, visibilityLister)
			if err != nil {
				logging.Error("query_datastore_visibility_failed", "error", err)
				return mcp.NewToolError("Failed to resolve accessible connections")
			}
			vis := visibleSet{All: all, IDs: ids, SystemAlerts: rbacChecker.CanSeeSystemAlerts(ctx)}

			sql, queryArgs, err := buildDatastoreQuery(src, req, vis, time.Now())
			if err != nil {
				if errors.Is(err, errConnectionNotAccessible) {
					return connectionNotAccessibleResponse(ctx, pool, rbacChecker, visibilityLister), nil
				}
				return mcp.NewToolError(err.Error())
			}

			if req.Explain {
				return explainDatastoreQuery(ctx, pool, src, sql, queryArgs)
			}

			qr, errResp := executeReadOnlyQuery(ctx, pool, sql, req.PageSize, false, "", queryArgs...)
			if errResp != nil {
				return *errResp, nil
			}

			var sb strings.Builder
			fmt.Fprintf(&sb, "Datastore Query: %s\n\n", src.Name)
			writeDatastorePage(&sb, qr, req)

			logging.Info("query_datastore_executed",
				"source", src.Name,
				"shape", req.Shape,
				"rows_returned", len(qr.Rows),
				"page", req.Page,
				"was_truncated", qr.WasTruncated,
				"estimated_tokens", len(qr.ResultsTSV)/4,
			)

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// writeDatastorePage writes one page of results with its row range and,
// when more rows exist, the page to ask for next.
func writeDatastorePage(sb *strings.Builder, qr *queryResult, req datastoreRequest) {
	if len(qr.Rows) == 0 {
		fmt.Fprintf(sb, "Results (0 rows):\n%s", qr.ResultsTSV)
		return
	}
	first := (req.Page-1)*req.PageSize + 1
	last := first + len(qr.Rows) - 1
	if qr.WasTruncated {
		fmt.Fprintf(sb, "Results (rows %d-%d, more available - use page=%d for the next page):\n%s",
			first, last, req.Page+1, qr.ResultsTSV)
		return
	}
	fmt.Fprintf(sb, "Results (rows %d-%d):\n%s", first, last, qr.ResultsTSV)
}

// explainDatastoreQuery runs EXPLAIN (never ANALYZE, so the statement is
// planned but not executed) with the statement's own parameters and
// returns the plan text.
func explainDatastoreQuery(ctx context.Context, pool *pgxpool.Pool, src datastoreSource, sql string, queryArgs []any) (mcp.ToolResponse, error) {
	qr, errResp := executeReadOnlyQuery(ctx, pool, "EXPLAIN "+sql, 0, true, "", queryArgs...)
	if errResp != nil {
		return *errResp, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Datastore Query Plan: %s\n\n", src.Name)
	for _, row := range qr.Rows {
		for _, v := range row {
			fmt.Fprintf(&sb, "%v\n", v)
		}
	}
	logging.Info("query_datastore_explained", "source", src.Name)
	return mcp.NewToolSuccess(sb.String())
}

// queryDatastoreInputSchema describes query_datastore's parameters.
func queryDatastoreInputSchema() mcp.InputSchema {
	columnList := func(description string) map[string]any {
		return map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": description,
		}
	}
	return mcp.InputSchema{
		Type: "object",
		Properties: map[string]any{
			"source": map[string]any{
				"type":        "string",
				"description": "Source to query, as listed by describe_datastore.",
			},
			"columns": columnList("Columns to return. Defaults to every column of the source; with shape aggregate, the columns to aggregate."),
			"connection_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "integer"},
				"description": "Limit to these connection IDs. Omit to cover every connection you can access.",
			},
			"time_start": map[string]any{
				"type":        "string",
				"description": "Start of the time range: a relative duration such as 1h or 7d, now, or an RFC 3339 timestamp. Metric sources default to one hour before time_end.",
			},
			"time_end": map[string]any{
				"type":        "string",
				"description": "End of the time range, in the same forms as time_start. Defaults to now for metric sources.",
			},
			"filters": map[string]any{
				"type":        "array",
				"description": "Conditions every returned row must meet.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"column": map[string]any{"type": "string"},
						"op": map[string]any{
							"type": "string",
							"enum": []string{"=", "!=", "<", "<=", ">", ">=", "in", "not in", "like", "is null", "is not null"},
						},
						"value": map[string]any{
							"description": "The value to compare with: a list for in and not in, omitted for is null and is not null.",
						},
					},
					"required": []string{"column", "op"},
				},
			},
			"shape": map[string]any{
				"type":        "string",
				"enum":        []string{datastoreShapeRows, datastoreShapeLatest, datastoreShapeAggregate},
				"description": "rows (default), latest (most recent row per connection and entity; metric sources only) or aggregate.",
			},
			"group_by": columnList("Columns to group by; shape aggregate only."),
			"aggregation": map[string]any{
				"type":        "string",
				"enum":        []string{"avg", "sum", "min", "max", "count"},
				"description": "Aggregate function; shape aggregate only.",
			},
			"buckets": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     datastoreMaxBuckets,
				"description": "Divide the time range into this many buckets; shape aggregate on a source with a time column only.",
			},
			"order_by": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"column":    map[string]any{"type": "string"},
						"direction": map[string]any{"type": "string", "enum": []string{"asc", "desc"}},
					},
					"required": []string{"column"},
				},
				"description": "Ordering terms.",
			},
			"page": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"default":     1,
				"description": "Page of results to return.",
			},
			"page_size": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     datastoreMaxPageSize,
				"default":     datastoreDefaultPageSize,
				"description": "Rows per page. Keep it small to save tokens.",
			},
			"explain": map[string]any{
				"type":        "boolean",
				"description": "Return the query plan (EXPLAIN, not ANALYZE) instead of rows.",
			},
		},
		Required: []string{"source"},
	}
}
