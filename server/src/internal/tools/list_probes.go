/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// ProbeInfo represents metadata about a metrics probe
type ProbeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	RowCount    int64  `json:"row_count"`
	Scope       string `json:"scope"` // "server" or "database"
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
- row_count: Approximate number of metric rows collected
- scope: "server" for server-wide metrics, "database" for per-database metrics
</provided_info>

<examples>
- List all probes to see what's being monitored
- Find probes related to "statement" or "io" to analyze query performance
- Check row counts to understand data volume
</examples>`,
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]interface{}{},
				Required:   []string{},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The list_probes tool requires a datastore connection.")
			}

			ctx := context.Background()

			// Query for all tables in the metrics schema with their descriptions and row counts
			query := `
				SELECT
					t.table_name,
					COALESCE(obj_description((quote_ident(t.table_schema) || '.' || quote_ident(t.table_name))::regclass), '') as description,
					COALESCE(s.n_live_tup, 0) as row_count
				FROM information_schema.tables t
				LEFT JOIN pg_stat_user_tables s
					ON s.schemaname = t.table_schema
					AND s.relname = t.table_name
				WHERE t.table_schema = 'metrics'
					AND t.table_type = 'BASE TABLE'
					AND t.table_name NOT LIKE '%_p%'  -- Exclude partition tables
				ORDER BY t.table_name
			`

			rows, err := pool.Query(ctx, query)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query metrics probes: %v", err))
			}
			defer rows.Close()

			var probes []ProbeInfo
			for rows.Next() {
				var probe ProbeInfo
				if err := rows.Scan(&probe.Name, &probe.Description, &probe.RowCount); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan probe: %v", err))
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
			sb.WriteString(fmt.Sprintf("Found %d metrics probes:\n\n", len(probes)))
			sb.WriteString("name\tdescription\trow_count\tscope\n")
			for _, probe := range probes {
				sb.WriteString(fmt.Sprintf("%s\t%s\t%d\t%s\n",
					probe.Name, probe.Description, probe.RowCount, probe.Scope))
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
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
