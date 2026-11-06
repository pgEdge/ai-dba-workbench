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
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// ProbeInfo represents metadata about a metrics probe
type ProbeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// RowCount is nil when the count is unknown: a probe stored as a
	// view, as on a pgEdge ColdFront datastore, is not counted.
	RowCount *int64 `json:"row_count"`
	Scope    string `json:"scope"` // "server" or "database"
}

// ListProbesTool creates the list_probes tool for listing available metrics probes
func ListProbesTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "list_probes",
			Description: `List available metrics probes in the datastore.

<database_context>
This tool queries the DATASTORE (not monitored databases) to list metrics
probes that have been configured and are collecting data from monitored servers.
</database_context>

<usecase>
Use this tool to:
- Discover what metrics are being collected
- Find probe names to use with describe_probe and query_metrics tools
- Understand the scope of monitoring (server-wide vs database-level probes)
</usecase>

<provided_info>
Returns a TSV table with:
- name: Probe name (use with describe_probe and query_metrics)
- description: Human-readable description of what the probe collects
- row_count: Approximate number of metric rows collected, or "unknown" for a
  probe stored in pgEdge ColdFront (a view over Apache Iceberg storage), whose
  rows are not counted because counting them would scan the whole history
- scope: "server" for server-wide metrics, "database" for per-database metrics
</provided_info>

<examples>
- List all probes to see what's being monitored
- Find probes related to "statement" or "io" to analyze query performance
- Check row counts to understand data volume
</examples>`,
			CompactDescription: `List available metrics probes in the datastore. Returns probe names, descriptions, and collection intervals. Use this to discover what metrics are available before calling query_metrics or describe_probe.`,
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]any{},
				Required:   []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The list_probes tool requires a datastore connection.")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// The probes come from metrics.ListProbeRelations, the catalog
			// lookup every route shares, so list_probes lists exactly what
			// describe_probe, query_metrics and the datastore tools accept:
			// known probes only, never a partition (issue #644) or a
			// relation the datastore role cannot read. The masking of
			// statement text and settings is keyed by probe name, so a
			// relation under any other name could not be read safely.
			//
			// On a pgEdge ColdFront datastore every probe is a view over
			// Apache Iceberg storage (issue #755). A view is listed only
			// when it carries the connection_id and collected_at columns
			// every probe has, and its row count is reported as unknown.
			relations, err := metrics.ListProbeRelations(ctx, pool)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query metrics probes: %v", err))
			}

			var probes []ProbeInfo
			for _, rel := range relations {
				if rel.IsView && !rel.HasBookkeeping {
					continue
				}
				probes = append(probes, ProbeInfo{
					Name:        rel.Name,
					Description: rel.Description,
					RowCount:    rel.RowCount,
					Scope:       determineProbeScope(rel.Name),
				})
			}

			if len(probes) == 0 {
				return mcp.NewToolSuccess("No metrics probes found in the datastore. The collector may not have created any probe tables yet.")
			}

			// Format as TSV
			var sb strings.Builder
			fmt.Fprintf(&sb, "Found %d metrics probes:\n\n", len(probes))
			sb.WriteString("name\tdescription\trow_count\tscope\n")
			for _, probe := range probes {
				fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\n",
					sanitizeTSVField(probe.Name),
					sanitizeTSVField(probe.Description),
					formatProbeRowCount(probe.RowCount),
					sanitizeTSVField(probe.Scope))
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// unknownRowCount is what list_probes reports as the row count of a probe
// it does not count.
const unknownRowCount = "unknown"

// formatProbeRowCount renders a probe's row count for the TSV output:
// the number, or unknownRowCount when count is nil.
func formatProbeRowCount(count *int64) string {
	if count == nil {
		return unknownRowCount
	}
	return strconv.FormatInt(*count, 10)
}

// determineProbeScope determines if a probe is server-wide or database-scoped
func determineProbeScope(probeName string) string {
	// Database-scoped probes (have database_name or per-database data)
	databaseProbes := map[string]bool{
		"pg_stat_database":           true,
		"pg_stat_database_conflicts": true,
		"pg_stat_user_tables":        true,
		"pg_stat_user_indexes":       true,
		"pg_stat_user_functions":     true,
		"pg_statio_user_tables":      true,
		"pg_statio_user_indexes":     true,
		"pg_statio_user_sequences":   true,
		"pg_stat_statements":         true,
		"pg_stat_all_tables":         true,
		"pg_stat_all_indexes":        true,
		"pg_statio_all_tables":       true,
		"pg_statio_all_indexes":      true,
		"pg_statio_all_sequences":    true,
	}

	if databaseProbes[probeName] {
		return "database"
	}
	return "server"
}
