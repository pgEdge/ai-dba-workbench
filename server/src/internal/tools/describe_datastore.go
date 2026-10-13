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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// describeDatastoreScopes lists every scoping rule in the order the
// legend reports them.
var describeDatastoreScopes = []datastoreScope{
	scopeConnection, scopeAlert, scopeViaAlert, scopeHierarchy,
	scopeCluster, scopeGroup, scopeGlobal,
}

// DescribeDatastoreTool creates the describe_datastore tool, which lists
// the sources query_datastore accepts and describes each one's columns.
// Its output comes from the same source catalog query_datastore
// enforces, so the two cannot disagree.
func DescribeDatastoreTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "describe_datastore",
			Description: `List the sources query_datastore can read, or describe one source's columns.

<usecase>
- With no arguments: every source, with its kind (metric or configuration),
  its time column (the column time_start and time_end apply to, if any),
  how its rows are limited to the connections you can access, and a
  description.
- With source: that source's columns, their data types, and whether each is
  a metric (numeric value) or a dimension (identifier or key). For a metric
  source it also lists the key columns shape latest uses.
</usecase>

<notes>
- Only the columns listed here can be selected, filtered, grouped or
  ordered on in query_datastore.
- For descriptions of individual probe columns, use describe_probe.
</notes>`,
			CompactDescription: `List the sources query_datastore can read (metric probes and configuration tables), or, with source, that source's columns. Call this before query_datastore.`,
			// The web client keeps its own copy of this schema in
			// client/src/hooks/chat/chatConstants.ts; change both together.
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"source": map[string]any{
						"type":        "string",
						"description": "Source to describe. Omit to list every source.",
					},
				},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The describe_datastore tool requires a datastore connection.")
			}
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			raw, present := args["source"]
			if !present || raw == nil {
				return describeDatastoreSources(ctx, pool)
			}
			name, ok := raw.(string)
			if !ok {
				return mcp.NewToolError("source must be a string")
			}
			name = strings.TrimSpace(name)
			if name == "" {
				return describeDatastoreSources(ctx, pool)
			}
			return describeDatastoreSource(ctx, pool, name)
		},
	}
}

// describeDatastoreSources lists every source and the scoping legend.
func describeDatastoreSources(ctx context.Context, pool *pgxpool.Pool) (mcp.ToolResponse, error) {
	sources, err := listDatastoreSources(ctx, pool)
	if err != nil {
		logging.Error("describe_datastore_list_failed", "error", err)
		return mcp.NewToolError("Failed to list the datastore sources")
	}

	rows := make([][]any, 0, len(sources))
	for _, s := range sources {
		rows = append(rows, []any{s.Name, s.Kind, s.TimeColumn, s.Scope.String(), s.Description})
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Datastore sources (%d):\n", len(sources))
	sb.WriteString(tsv.FormatResults([]string{"source", "kind", "time_column", "scoping", "description"}, rows))
	sb.WriteString("\n\nScoping rules:\n")
	for _, scope := range describeDatastoreScopes {
		fmt.Fprintf(&sb, "- %s: %s\n", scope.String(), scope.Description())
	}
	sb.WriteString("\nCall describe_datastore with source set to list a source's columns.")
	return mcp.NewToolSuccess(sb.String())
}

// describeDatastoreSource describes one source's permitted columns.
func describeDatastoreSource(ctx context.Context, pool *pgxpool.Pool, name string) (mcp.ToolResponse, error) {
	src, err := lookupDatastoreSource(ctx, pool, name)
	if err != nil {
		if errors.Is(err, errUnknownDatastoreSource) {
			return mcp.NewToolError(err.Error())
		}
		logging.Error("describe_datastore_lookup_failed", "source", name, "error", err)
		return mcp.NewToolError("Failed to look up the source")
	}

	rows := make([][]any, 0, len(src.Columns))
	colTypes := make(map[string]string, len(src.Columns))
	for _, c := range src.Columns {
		kind := "dimension"
		if c.IsMetric {
			kind = "metric"
		}
		rows = append(rows, []any{c.Name, c.DataType, kind})
		colTypes[c.Name] = c.DataType
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Source: %s\nKind: %s\nDescription: %s\n", src.Name, src.Kind, src.Description)
	if src.TimeColumn != "" {
		fmt.Fprintf(&sb, "Time column: %s\n", src.TimeColumn)
	} else {
		sb.WriteString("Time column: none (time_start and time_end do not apply)\n")
	}
	fmt.Fprintf(&sb, "Scoping: %s (%s)\n", src.Scope.String(), src.Scope.Description())
	if src.Kind == datastoreKindMetric {
		keys := metrics.EntityKeyColumns(src.columnNames(), colTypes)
		latest := "connection_id"
		if len(keys) > 0 {
			latest += ", " + strings.Join(keys, ", ")
		}
		fmt.Fprintf(&sb, "Shape latest returns the newest row per: %s\n", latest)
	}
	fmt.Fprintf(&sb, "\nColumns (%d):\n", len(src.Columns))
	sb.WriteString(tsv.FormatResults([]string{"column_name", "data_type", "column_type"}, rows))
	return mcp.NewToolSuccess(sb.String())
}
