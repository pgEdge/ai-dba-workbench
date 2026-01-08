/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package tsv

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// FormatValue converts a value to a TSV-safe string.
// Handles NULLs, special characters, and complex types.
func FormatValue(v interface{}) string {
	if v == nil {
		return "" // NULL represented as empty string
	}

	var s string
	switch val := v.(type) {
	case string:
		s = val
	case []byte:
		s = string(val)
	case time.Time:
		s = val.Format(time.RFC3339)
	case bool:
		if val {
			s = "true"
		} else {
			s = "false"
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		s = fmt.Sprintf("%d", val)
	case float32, float64:
		s = fmt.Sprintf("%v", val)
	case pgtype.Numeric:
		s = formatNumeric(val)
	case []interface{}, map[string]interface{}:
		// Complex types (arrays, JSON objects) - serialize to JSON
		jsonBytes, err := json.Marshal(val)
		if err != nil {
			s = fmt.Sprintf("%v", val)
		} else {
			s = string(jsonBytes)
		}
	default:
		// For any other type, use default formatting
		s = fmt.Sprintf("%v", val)
	}

	// Escape special characters that would break TSV parsing
	// Replace tabs with \t and newlines with \n (literal backslash sequences)
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")

	return s
}

// FormatResults converts query results to TSV format.
// Returns header row followed by data rows, tab-separated.
func FormatResults(columnNames []string, results [][]interface{}) string {
	if len(columnNames) == 0 {
		return ""
	}

	var sb strings.Builder

	// Header row
	sb.WriteString(strings.Join(columnNames, "\t"))

	// Data rows
	for _, row := range results {
		sb.WriteString("\n")
		values := make([]string, len(row))
		for i, val := range row {
			values[i] = FormatValue(val)
		}
		sb.WriteString(strings.Join(values, "\t"))
	}

	return sb.String()
}

// BuildRow creates a single TSV row from string values.
// Values are escaped for TSV safety.
func BuildRow(values ...string) string {
	escaped := make([]string, len(values))
	for i, v := range values {
		escaped[i] = FormatValue(v)
	}
	return strings.Join(escaped, "\t")
}

// formatNumeric converts a pgtype.Numeric to a human-readable string
func formatNumeric(n pgtype.Numeric) string {
	if !n.Valid {
		return ""
	}

	// Handle special cases
	if n.NaN {
		return "NaN"
	}
	if n.InfinityModifier == pgtype.Infinity {
		return "Infinity"
	}
	if n.InfinityModifier == pgtype.NegativeInfinity {
		return "-Infinity"
	}

	// Convert to big.Float for accurate representation
	if n.Int == nil {
		return "0"
	}

	// Create a big.Float from the integer part
	f := new(big.Float).SetInt(n.Int)

	// Apply the exponent (n.Exp is the number of decimal places, negative means right of decimal)
	if n.Exp != 0 {
		// Calculate 10^|Exp|
		absExp := n.Exp
		if absExp < 0 {
			absExp = -absExp
		}
		exp := big.NewInt(10)
		exp.Exp(exp, big.NewInt(int64(absExp)), nil)
		expFloat := new(big.Float).SetInt(exp)

		if n.Exp > 0 {
			// Positive exponent: multiply by 10^Exp
			f.Mul(f, expFloat)
		} else {
			// Negative exponent: divide by 10^|Exp|
			f.Quo(f, expFloat)
		}
	}

	// Convert to float64 for formatting
	f64, _ := f.Float64()

	// Format without unnecessary trailing zeros
	if f64 == float64(int64(f64)) {
		return fmt.Sprintf("%d", int64(f64))
	}
	return fmt.Sprintf("%.6g", f64)
}
