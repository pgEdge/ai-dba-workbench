/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package tools

import (
	"fmt"
	"regexp"

	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// Regex patterns for SQL identifier validation
var (
	// simpleIdentifierRegex matches valid PostgreSQL identifiers:
	// - Must start with a letter or underscore
	// - Can contain letters, digits, and underscores
	// - Maximum length of 63 characters (PostgreSQL default NAMEDATALEN - 1)
	simpleIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

	// qualifiedNameRegex matches schema.table format:
	// - Each part must be a valid simple identifier
	// - Schema part is optional
	qualifiedNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}(\.[a-zA-Z_][a-zA-Z0-9_]{0,62})?$`)
)

// ValidateIdentifier validates a simple SQL identifier (table name, column name, schema name).
// Valid identifiers must:
// - Start with a letter (a-z, A-Z) or underscore (_)
// - Contain only letters, digits (0-9), and underscores
// - Be at most 63 characters long (PostgreSQL's NAMEDATALEN - 1)
// Returns nil if valid, or an error describing the validation failure.
func ValidateIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("identifier cannot be empty")
	}

	if len(name) > 63 {
		return fmt.Errorf("identifier '%s' exceeds maximum length of 63 characters", name)
	}

	if !simpleIdentifierRegex.MatchString(name) {
		return fmt.Errorf("identifier '%s' contains invalid characters; must start with a letter or underscore and contain only letters, digits, and underscores", name)
	}

	return nil
}

// ValidateQualifiedTableName validates a table name that may include a schema prefix.
// Accepts formats:
// - "table_name" (simple identifier)
// - "schema.table_name" (qualified name)
// Both schema and table parts must be valid simple identifiers.
// Returns nil if valid, or an error describing the validation failure.
func ValidateQualifiedTableName(name string) error {
	if name == "" {
		return fmt.Errorf("table name cannot be empty")
	}

	if !qualifiedNameRegex.MatchString(name) {
		return fmt.Errorf("table name '%s' is invalid; must be a valid identifier or schema.table format with only letters, digits, and underscores", name)
	}

	return nil
}

// ValidateColumnNames validates a slice of column names.
// Each column name must be a valid simple identifier.
// Returns nil if all columns are valid, or an error for the first invalid column.
func ValidateColumnNames(columns []string) error {
	for _, col := range columns {
		if err := ValidateIdentifier(col); err != nil {
			return fmt.Errorf("invalid column name: %w", err)
		}
	}
	return nil
}

// ValidateStringParam validates and extracts a required string parameter from args
// Returns the string value and a ToolResponse error if validation fails
func ValidateStringParam(args map[string]interface{}, name string) (string, *mcp.ToolResponse) {
	value, ok := args[name].(string)
	if !ok || value == "" {
		resp, err := mcp.NewToolError(fmt.Sprintf("Missing or invalid '%s' argument", name))
		if err != nil {
			return "", &resp
		}
		return "", &resp
	}
	return value, nil
}

// ValidateOptionalStringParam validates and extracts an optional string parameter
// Returns the string value (or defaultValue if not present) and no error
func ValidateOptionalStringParam(args map[string]interface{}, name string, defaultValue string) string {
	value, ok := args[name].(string)
	if !ok {
		return defaultValue
	}
	return value
}

// ValidateNumberParam validates and extracts a required number parameter from args
// Returns the float64 value and a ToolResponse error if validation fails
func ValidateNumberParam(args map[string]interface{}, name string) (float64, *mcp.ToolResponse) {
	value, ok := args[name].(float64)
	if !ok {
		resp, err := mcp.NewToolError(fmt.Sprintf("Error: %s must be a number", name))
		if err != nil {
			return 0, &resp
		}
		return 0, &resp
	}
	return value, nil
}

// ValidateOptionalNumberParam validates and extracts an optional number parameter
// Returns the float64 value (or defaultValue if not present) and no error
func ValidateOptionalNumberParam(args map[string]interface{}, name string, defaultValue float64) float64 {
	value, ok := args[name].(float64)
	if !ok {
		return defaultValue
	}
	return value
}

// ValidateBoolParam validates and extracts an optional boolean parameter
// Returns the bool value (or defaultValue if not present)
func ValidateBoolParam(args map[string]interface{}, name string, defaultValue bool) bool {
	value, ok := args[name].(bool)
	if !ok {
		return defaultValue
	}
	return value
}

// ValidatePositiveNumber checks if a number is greater than zero
// Returns a ToolResponse error if validation fails, nil otherwise
func ValidatePositiveNumber(value float64, name string) *mcp.ToolResponse {
	if value <= 0 {
		resp, err := mcp.NewToolError(fmt.Sprintf("Error: %s must be greater than 0", name))
		if err != nil {
			return &resp
		}
		return &resp
	}
	return nil
}
