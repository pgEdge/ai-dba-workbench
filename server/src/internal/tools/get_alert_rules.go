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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// GetAlertRulesTool creates the get_alert_rules tool for querying alert rules and thresholds
func GetAlertRulesTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "get_alert_rules",
			Description: `Query current alerting rules and their effective thresholds.

<database_context>
This tool queries the DATASTORE to retrieve alert rules and their configured
thresholds. Rules define what conditions trigger alerts, and thresholds can
be customized per connection.
</database_context>

<usecase>
Use this tool to:
- List all available alert rules
- View effective thresholds for a specific connection
- Filter rules by category (connections, replication, storage, etc.)
- Understand what conditions will trigger alerts
</usecase>

<parameters>
- connection_id: (optional) Get connection-specific thresholds. If provided, shows effective thresholds for that connection (connection overrides take precedence over defaults).
- category: (optional) Filter by rule category: connections, replication, storage, performance, transactions, locks, wal, maintenance, queries, errors
- enabled_only: (optional) Only show enabled rules. Default: true
</parameters>

<output>
Returns TSV data with:
- rule_id: Alert rule ID
- name: Rule name
- category: Rule category
- metric_name: Name of the monitored metric
- operator: Comparison operator (>, >=, <, <=, ==, !=)
- threshold: Threshold value
- severity: Alert severity (info, warning, critical)
- enabled: Whether the rule is enabled
- description: Human-readable description
</output>

<examples>
- get_alert_rules() - list all enabled rules with default thresholds
- get_alert_rules(connection_id=5) - rules with effective thresholds for connection 5
- get_alert_rules(category="performance") - only performance-related rules
- get_alert_rules(enabled_only=false) - include disabled rules
</examples>`,
			CompactDescription: `Query alerting rules and their effective thresholds. Filter by category or connection_id. Shows default thresholds and any connection-specific overrides.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "Get connection-specific thresholds. If provided, shows effective thresholds (connection overrides take precedence over defaults).",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Filter by rule category: connections, replication, storage, performance, transactions, locks, wal, maintenance, queries, errors",
						"enum":        []string{"connections", "replication", "storage", "performance", "transactions", "locks", "wal", "maintenance", "queries", "errors"},
					},
					"enabled_only": map[string]any{
						"type":        "boolean",
						"description": "Only return enabled rules. Default: true",
						"default":     true,
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The get_alert_rules tool requires a datastore connection.")
			}

			// Parse optional connection_id
			var connectionID *int
			if _, ok := args["connection_id"]; ok {
				cid, err := parseIntArg(args, "connection_id")
				if err != nil {
					return mcp.NewToolError("Invalid 'connection_id' parameter: must be an integer")
				}
				connectionID = &cid
			}

			// Parse optional category
			var category *string
			if cat, ok := args["category"].(string); ok && cat != "" {
				validCategories := map[string]bool{
					"connections":  true,
					"replication":  true,
					"storage":      true,
					"performance":  true,
					"transactions": true,
					"locks":        true,
					"wal":          true,
					"maintenance":  true,
					"queries":      true,
					"errors":       true,
				}
				if !validCategories[cat] {
					return mcp.NewToolError("Invalid 'category' parameter: must be one of connections, replication, storage, performance, transactions, locks, wal, maintenance, queries, errors")
				}
				category = &cat
			}

			// Parse enabled_only (default: true)
			enabledOnly := true
			if eo, ok := args["enabled_only"].(bool); ok {
				enabledOnly = eo
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Build the query
			// This query joins alert_rules with alert_thresholds to get effective config
			// Connection-specific thresholds override defaults
			query := `
                SELECT
                    r.id,
                    r.name,
                    r.category,
                    r.metric_name,
                    r.description,
                    COALESCE(t.operator, r.default_operator) as operator,
                    COALESCE(t.threshold, r.default_threshold) as threshold,
                    COALESCE(t.severity, r.default_severity) as severity,
                    COALESCE(t.enabled, r.default_enabled) as enabled
                FROM alert_rules r
                LEFT JOIN alert_thresholds t ON t.rule_id = r.id
                    AND (t.connection_id = $1 OR (t.connection_id IS NULL AND $1 IS NULL))
                WHERE ($2::text IS NULL OR r.category = $2)
                    AND ($3::boolean = false OR COALESCE(t.enabled, r.default_enabled) = true)
                ORDER BY r.category, r.name
            `

			rows, err := pool.Query(ctx, query, connectionID, category, enabledOnly)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query alert rules: %v", err))
			}
			defer rows.Close()

			// Build TSV output
			var sb strings.Builder
			connInfo := "default thresholds"
			if connectionID != nil {
				connInfo = fmt.Sprintf("effective thresholds for connection %d", *connectionID)
			}
			catInfo := "all categories"
			if category != nil {
				catInfo = fmt.Sprintf("category: %s", *category)
			}
			sb.WriteString(fmt.Sprintf("Alert Rules | %s | %s | enabled_only: %t\n\n",
				connInfo, catInfo, enabledOnly))

			// Header
			sb.WriteString("rule_id\tname\tcategory\tmetric_name\toperator\tthreshold\tseverity\tenabled\tdescription\n")

			// Data rows
			rowCount := 0
			for rows.Next() {
				var (
					id          int64
					name        string
					cat         string
					metricName  string
					description string
					operator    string
					threshold   float64
					severity    string
					enabled     bool
				)

				if err := rows.Scan(&id, &name, &cat, &metricName, &description,
					&operator, &threshold, &severity, &enabled); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
				}

				// Format row
				sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%v\t%s\t%t\t%s\n",
					id,
					tsv.FormatValue(name),
					cat,
					metricName,
					operator,
					threshold,
					severity,
					enabled,
					tsv.FormatValue(description),
				))
				rowCount++
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
			}

			if rowCount == 0 {
				return mcp.NewToolSuccess("No alert rules found matching the specified criteria.")
			}

			sb.WriteString(fmt.Sprintf("\n(%d rules)\n", rowCount))

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
