/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package pgutil provides PostgreSQL utility functions
package pgutil

import (
    "fmt"
    "strings"
)

// BuildConnectionString builds a PostgreSQL connection string from a map of parameters
// The parameters are formatted as key='value' pairs separated by spaces
func BuildConnectionString(params map[string]string) string {
    var parts []string
    for key, value := range params {
        parts = append(parts, fmt.Sprintf("%s='%s'", key, value))
    }
    return strings.Join(parts, " ")
}
