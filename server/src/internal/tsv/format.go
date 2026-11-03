/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tsv

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// formatFloat renders f in plain decimal notation with the fewest digits
// that read back as the same value of the given bit size (32 or 64), so
// 1431655765 stays 1431655765 rather than %v's 1.431655765e+09. That
// keeps a float8 result reading as the numeric one it replaced: avg() is
// cast to float8 so a pgEdge ColdFront datastore and PostgreSQL type it
// alike (issue #755), and the numeric avg() was always printed in full.
// NaN and the infinities read NaN, +Inf and -Inf, as %v printed them.
func formatFloat(f float64, bitSize int) string {
	return strconv.FormatFloat(f, 'f', -1, bitSize)
}

// formatGoFloat renders a Go float32 or float64 with formatFloat at its
// own precision; any other value falls back to %v.
func formatGoFloat(v any) string {
	switch f := v.(type) {
	case float32:
		return formatFloat(float64(f), 32)
	case float64:
		return formatFloat(f, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// FormatValue converts a value to a TSV-safe string.
// Handles NULLs, special characters, and complex types.
func FormatValue(v any) string {
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
		s = formatGoFloat(val)
	case pgtype.Numeric:
		s = formatNumeric(val)
	// Integer types
	case pgtype.Int2:
		if !val.Valid {
			return ""
		}
		s = fmt.Sprintf("%d", val.Int16)
	case pgtype.Int4:
		if !val.Valid {
			return ""
		}
		s = fmt.Sprintf("%d", val.Int32)
	case pgtype.Int8:
		if !val.Valid {
			return ""
		}
		s = fmt.Sprintf("%d", val.Int64)
	// Float types
	case pgtype.Float4:
		if !val.Valid {
			return ""
		}
		s = formatFloat(float64(val.Float32), 32)
	case pgtype.Float8:
		if !val.Valid {
			return ""
		}
		s = formatFloat(val.Float64, 64)
	// Text and Bool types
	case pgtype.Text:
		if !val.Valid {
			return ""
		}
		s = val.String
	case pgtype.Bool:
		if !val.Valid {
			return ""
		}
		if val.Bool {
			s = "true"
		} else {
			s = "false"
		}
	// Date/time types
	case pgtype.Timestamp:
		if !val.Valid {
			return ""
		}
		s = val.Time.Format(time.RFC3339)
	case pgtype.Timestamptz:
		if !val.Valid {
			return ""
		}
		s = val.Time.Format(time.RFC3339)
	case pgtype.Date:
		if !val.Valid {
			return ""
		}
		s = val.Time.Format("2006-01-02")
	case pgtype.Interval:
		if !val.Valid {
			return ""
		}
		s = formatInterval(val)
	case pgtype.UUID:
		if !val.Valid {
			return ""
		}
		s = formatUUID(val.Bytes)
	case [16]byte:
		// Raw UUID bytes
		s = formatUUID(val)
	case []any, map[string]any:
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
func FormatResults(columnNames []string, results [][]any) string {
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

// formatInterval converts a pgtype.Interval to a human-readable string
func formatInterval(i pgtype.Interval) string {
	var parts []string

	// Handle months (converted to years and months)
	if i.Months != 0 {
		years := i.Months / 12
		months := i.Months % 12
		if years != 0 {
			if years == 1 {
				parts = append(parts, "1 year")
			} else {
				parts = append(parts, fmt.Sprintf("%d years", years))
			}
		}
		if months != 0 {
			if months == 1 {
				parts = append(parts, "1 mon")
			} else {
				parts = append(parts, fmt.Sprintf("%d mons", months))
			}
		}
	}

	// Handle days
	if i.Days != 0 {
		if i.Days == 1 {
			parts = append(parts, "1 day")
		} else {
			parts = append(parts, fmt.Sprintf("%d days", i.Days))
		}
	}

	// Handle microseconds (converted to hours, minutes, seconds)
	if i.Microseconds != 0 {
		totalSeconds := i.Microseconds / 1000000
		microsRemainder := i.Microseconds % 1000000

		hours := totalSeconds / 3600
		minutes := (totalSeconds % 3600) / 60
		seconds := totalSeconds % 60

		if hours != 0 || minutes != 0 || seconds != 0 || microsRemainder != 0 {
			if microsRemainder != 0 {
				// Include fractional seconds
				parts = append(parts, fmt.Sprintf("%02d:%02d:%02d.%06d",
					hours, minutes, seconds, microsRemainder))
			} else {
				parts = append(parts, fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds))
			}
		}
	}

	if len(parts) == 0 {
		return "00:00:00"
	}

	return strings.Join(parts, " ")
}

// formatUUID formats a UUID byte array as a standard UUID string
func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
