/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

// MetricColumn represents a column in a metrics probe table
type MetricColumn struct {
	Name        string `json:"name"`
	DataType    string `json:"data_type"`
	Description string `json:"description"`
	IsMetric    bool   `json:"is_metric"` // True if this is a metric column (vs. dimension)
}

// DescribeProbeTool creates the describe_probe tool for getting probe details
func DescribeProbeTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "describe_probe",
			Description: `Get detailed information about a specific metrics probe.

<database_context>
This tool queries the DATASTORE to describe the structure and available metrics
in a specific probe table. Use this to understand what data is available
before querying with query_metrics.
</database_context>

<usecase>
Use this tool to:
- Understand what metrics are available in a probe
- Learn the column names to use in query_metrics
- See data types and descriptions for each metric
- Identify dimension columns (connection_id, database_name, etc.) vs metric columns
</usecase>

<provided_info>
Returns TSV with:
- column_name: Name of the column (use in query_metrics)
- data_type: PostgreSQL data type
- description: Human-readable description of the metric
- column_type: "metric" for numeric values, "dimension" for identifiers/keys
</provided_info>

<examples>
- describe_probe("pg_stat_database") - See database-level metrics
- describe_probe("pg_stat_statements") - See query statistics metrics
- describe_probe("pg_sys_memory_info") - See system memory metrics
</examples>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"probe_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the probe to describe (from list_probes output)",
					},
				},
				Required: []string{"probe_name"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The describe_probe tool requires a datastore connection.")
			}

			probeName, ok := args["probe_name"].(string)
			if !ok || probeName == "" {
				return mcp.NewToolError("Missing or invalid 'probe_name' parameter")
			}

			// Validate probe name (prevent SQL injection)
			if !isValidIdentifier(probeName) {
				return mcp.NewToolError("Invalid probe name. Probe names must contain only letters, numbers, and underscores.")
			}

			ctx := context.Background()

			// First check if the probe exists
			existsQuery := `
				SELECT COUNT(*) FROM information_schema.tables
				WHERE table_schema = 'metrics'
					AND table_name = $1
					AND table_type = 'BASE TABLE'
			`
			var count int
			if err := pool.QueryRow(ctx, existsQuery, probeName).Scan(&count); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to check probe existence: %v", err))
			}
			if count == 0 {
				return mcp.NewToolError(fmt.Sprintf("Probe '%s' not found. Use list_probes to see available probes.", probeName))
			}

			// Get column information
			query := `
				SELECT
					c.column_name,
					c.data_type,
					COALESCE(col_description((quote_ident('metrics') || '.' || quote_ident($1))::regclass, c.ordinal_position), '') as description
				FROM information_schema.columns c
				WHERE c.table_schema = 'metrics'
					AND c.table_name = $1
				ORDER BY c.ordinal_position
			`

			rows, err := pool.Query(ctx, query, probeName)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query probe columns: %v", err))
			}
			defer rows.Close()

			var columns []MetricColumn
			for rows.Next() {
				var col MetricColumn
				if err := rows.Scan(&col.Name, &col.DataType, &col.Description); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan column: %v", err))
				}
				col.IsMetric = isMetricColumn(col.Name, col.DataType)
				columns = append(columns, col)
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating columns: %v", err))
			}

			// Get table description (best effort - ignore errors)
			var tableDesc string
			tableDescQuery := `SELECT COALESCE(obj_description((quote_ident('metrics') || '.' || quote_ident($1))::regclass), '')`
			if err := pool.QueryRow(ctx, tableDescQuery, probeName).Scan(&tableDesc); err != nil {
				// Ignore error - table description is optional
				tableDesc = ""
			}

			// Format as TSV
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Probe: %s\n", probeName))
			if tableDesc != "" {
				sb.WriteString(fmt.Sprintf("Description: %s\n", tableDesc))
			}
			sb.WriteString(fmt.Sprintf("Columns: %d\n\n", len(columns)))

			sb.WriteString("column_name\tdata_type\tdescription\tcolumn_type\n")
			for _, col := range columns {
				colType := "dimension"
				if col.IsMetric {
					colType = "metric"
				}
				// Clean up description (remove newlines)
				desc := strings.ReplaceAll(col.Description, "\n", " ")
				sb.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\n",
					col.Name, col.DataType, desc, colType))
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// isValidIdentifier checks if a string is a valid SQL identifier
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		// First character must be letter or underscore
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
				return false
			}
		} else {
			// Subsequent characters can also be digits
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
				return false
			}
		}
	}
	return true
}

// isMetricColumn determines if a column is a metric (numeric data) vs a dimension (identifier)
func isMetricColumn(name, dataType string) bool {
	// Dimension columns (identifiers, timestamps, text)
	dimensionColumns := map[string]bool{
		"connection_id":    true,
		"collected_at":     true,
		"inserted_at":      true,
		"datid":            true,
		"datname":          true,
		"pid":              true,
		"usesysid":         true,
		"usename":          true,
		"application_name": true,
		"client_addr":      true,
		"client_hostname":  true,
		"client_port":      true,
		"backend_start":    true,
		"xact_start":       true,
		"query_start":      true,
		"state_change":     true,
		"wait_event_type":  true,
		"wait_event":       true,
		"state":            true,
		"backend_xid":      true,
		"backend_xmin":     true,
		"query":            true,
		"backend_type":     true,
		"relid":            true,
		"relname":          true,
		"schemaname":       true,
		"indexrelid":       true,
		"indexrelname":     true,
		"funcid":           true,
		"funcname":         true,
		"queryid":          true,
		"slot_name":        true,
		"plugin":           true,
		"slot_type":        true,
		"sender_host":      true,
		"sender_port":      true,
		"conninfo":         true,
		"status":           true,
		"name":             true,
		"setting":          true,
		"unit":             true,
		"category":         true,
		"short_desc":       true,
		"extra_desc":       true,
		"context":          true,
		"vartype":          true,
		"source":           true,
		"boot_val":         true,
		"reset_val":        true,
		"sourcefile":       true,
		"sourceline":       true,
	}

	if dimensionColumns[name] {
		return false
	}

	// Timestamp types are dimensions
	if strings.Contains(dataType, "timestamp") || dataType == "date" || dataType == "time" {
		return false
	}

	// Text types are typically dimensions
	if dataType == "text" || dataType == "character varying" || dataType == "name" || dataType == "inet" || dataType == "oid" {
		return false
	}

	// Numeric types are typically metrics
	if dataType == "bigint" || dataType == "integer" || dataType == "smallint" ||
		dataType == "numeric" || dataType == "double precision" || dataType == "real" {
		return true
	}

	return false
}
