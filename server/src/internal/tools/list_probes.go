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

			// Query for the probe tables in the metrics schema with their
			// descriptions and row counts. The collector stores each probe
			// in a partitioned parent table with weekly partitions named
			// <probe>_<YYYYMMDD>; partitions are excluded by catalog state
			// (relispartition) rather than by name, because a name pattern
			// cannot tell a partition from a probe reliably (issue #644).
			// relkind 'r' and 'p' are the ordinary and partitioned tables
			// that information_schema.tables reports as BASE TABLE, and the
			// privilege check keeps tables the datastore role cannot read
			// out of the list, as information_schema.tables did. A
			// partitioned table holds no rows itself, so its row count is the
			// sum over the leaf partitions at every level of its tree.
			//
			// On a pgEdge ColdFront datastore every probe is instead a view
			// (relkind 'v') over Apache Iceberg storage (issue #755). A view
			// is listed when it carries the connection_id and collected_at
			// bookkeeping columns every probe has, so an unrelated view in
			// the schema is not mistaken for one, and only under a known
			// probe's name (metrics.AdmitsProbeRelation, applied below to
			// is_view): the masking of statement text and settings is keyed
			// by probe name, so a view under another name could not be
			// read safely. A view has no statistics, and count(*) would
			// scan the probe's whole history in object storage, so its row
			// count is NULL and reported as unknown.
			query := `
				SELECT
					c.relname::text AS table_name,
					COALESCE(obj_description(c.oid, 'pg_class'), '') AS description,
					c.relkind = 'v' AS is_view,
					CASE WHEN c.relkind = 'v' THEN NULL
					WHEN c.relkind = 'p' THEN (
						SELECT COALESCE(SUM(ps.n_live_tup), 0)
						FROM pg_partition_tree(c.oid) pt
						JOIN pg_stat_user_tables ps ON ps.relid = pt.relid
						WHERE pt.isleaf
					)::bigint
					ELSE COALESCE(s.n_live_tup, 0)
					END AS row_count
				FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
				WHERE n.nspname = 'metrics'
					AND NOT c.relispartition
					AND (c.relkind IN ('r', 'p')
						OR (c.relkind = 'v'
							AND EXISTS (SELECT 1 FROM pg_attribute a
								WHERE a.attrelid = c.oid AND a.attname = 'connection_id'
									AND a.attnum > 0 AND NOT a.attisdropped)
							AND EXISTS (SELECT 1 FROM pg_attribute a
								WHERE a.attrelid = c.oid AND a.attname = 'collected_at'
									AND a.attnum > 0 AND NOT a.attisdropped)))
					AND has_table_privilege(c.oid, 'SELECT')
				ORDER BY c.relname
			`

			rows, err := pool.Query(ctx, query)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query metrics probes: %v", err))
			}
			defer rows.Close()

			var probes []ProbeInfo
			for rows.Next() {
				var probe ProbeInfo
				var isView bool
				if err := rows.Scan(&probe.Name, &probe.Description, &isView, &probe.RowCount); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan probe: %v", err))
				}
				if !metrics.AdmitsProbeRelation(probe.Name, isView) {
					continue
				}
				// Determine scope based on probe name patterns
				probe.Scope = determineProbeScope(probe.Name)
				probes = append(probes, probe)
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating probes: %v", err))
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
