/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package utils provides utility functions for the collector
package utils

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ScanRowsToMaps scans all rows from a result set and returns them as a slice of maps.
// Each map represents one row with column names as keys.
func ScanRowsToMaps(rows pgx.Rows) ([]map[string]interface{}, error) {
	// Get column descriptions
	fieldDescs := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		columns[i] = string(fd.Name)
	}

	// Prepare result set
	var results []map[string]interface{}

	for rows.Next() {
		// Scan the row values
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Create a map for this row
		rowMap := make(map[string]interface{})
		for i, colName := range columns {
			rowMap[colName] = values[i]
		}

		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// FormatDatabaseInfo formats a connection name and optional database name
// into a display string like "ConnectionName" or "ConnectionName/database".
func FormatDatabaseInfo(connectionName string, databaseName string) string {
	if databaseName != "" {
		return fmt.Sprintf("%s/%s", connectionName, databaseName)
	}
	return connectionName
}
