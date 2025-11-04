/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"database/sql"
	"fmt"
	"log"
)

// scanRowsToMaps scans all rows from a result set and returns them as a slice of maps
// Each map represents one row with column names as keys
func scanRowsToMaps(rows *sql.Rows) ([]map[string]interface{}, error) {
	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Prepare result set
	var results []map[string]interface{}

	for rows.Next() {
		// Create a slice of interface{} to hold values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan the row
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Create a map for this row
		rowMap := make(map[string]interface{})
		for i, colName := range columns {
			val := values[i]
			// Convert []byte to string for readability
			if b, ok := val.([]byte); ok {
				rowMap[colName] = string(b)
			} else {
				rowMap[colName] = val
			}
		}

		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// closeRows safely closes a sql.Rows object and logs any errors
// This is meant to be used with defer
func closeRows(rows *sql.Rows) {
	if cerr := rows.Close(); cerr != nil {
		log.Printf("Error closing rows: %v", cerr)
	}
}

// formatDatabaseInfo formats a connection name and optional database name
// into a display string like "ConnectionName" or "ConnectionName/database"
func formatDatabaseInfo(connectionName string, databaseName string) string {
	if databaseName != "" {
		return fmt.Sprintf("%s/%s", connectionName, databaseName)
	}
	return connectionName
}
