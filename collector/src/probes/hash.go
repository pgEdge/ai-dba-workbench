/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// ComputeMetricsHash computes a canonical hash of metrics for change detection.
// This function normalizes the data to ensure consistent hashing regardless of
// map iteration order or minor type differences between database drivers.
func ComputeMetricsHash(metrics []map[string]any) (string, error) {
	// Build a canonical representation by sorting keys and normalizing values
	var canonicalData []map[string]any
	for _, m := range metrics {
		normalized := make(map[string]any)
		for k, v := range m {
			normalized[k] = normalizeValue(v)
		}
		canonicalData = append(canonicalData, normalized)
	}

	// Sort the slice by a deterministic key (first key alphabetically, then value)
	// This ensures consistent ordering even if rows come in different order
	sort.Slice(canonicalData, func(i, j int) bool {
		// Get sorted keys for comparison
		keysI := getSortedKeys(canonicalData[i])
		keysJ := getSortedKeys(canonicalData[j])

		// Compare by first key's value, then second, etc.
		for idx := 0; idx < len(keysI) && idx < len(keysJ); idx++ {
			if keysI[idx] != keysJ[idx] {
				return keysI[idx] < keysJ[idx]
			}
			valI := fmt.Sprintf("%v", canonicalData[i][keysI[idx]])
			valJ := fmt.Sprintf("%v", canonicalData[j][keysJ[idx]])
			if valI != valJ {
				return valI < valJ
			}
		}
		return len(keysI) < len(keysJ)
	})

	// Marshal to JSON (Go's json.Marshal sorts map keys)
	jsonBytes, err := json.Marshal(canonicalData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metrics: %w", err)
	}

	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:]), nil
}

// normalizeValue converts a value to a canonical form for comparison.
// This ensures that logically equivalent values from different sources
// (e.g., pgx returning int32 vs datastore returning int64) produce
// identical JSON serialization and therefore identical hashes.
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	// Integer types — normalize to int64
	case int:
		return int64(val)
	case int8:
		return int64(val)
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case uint:
		if uint64(val) > math.MaxInt64 {
			return val
		}
		return int64(val) // #nosec G115 -- overflow checked above
	case uint8:
		return int64(val)
	case uint16:
		return int64(val)
	case uint32:
		return int64(val)
	case uint64:
		if val > math.MaxInt64 {
			return val
		}
		return int64(val) // #nosec G115 -- overflow checked above

	// Float types — normalize to float64
	case float32:
		return float64(val)
	case float64:
		return val

	// Bool — pass through explicitly
	case bool:
		return val

	// String — pass through explicitly
	case string:
		return val

	// Byte slices — convert to string
	case []byte:
		return string(val)

	// Slices — normalize elements recursively
	case []any:
		result := make([]any, len(val))
		for i, elem := range val {
			result[i] = normalizeValue(elem)
		}
		return result
	case []string:
		result := make([]any, len(val))
		for i, elem := range val {
			result[i] = elem
		}
		return result

	// Maps — normalize values recursively
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, elem := range val {
			result[k] = normalizeValue(elem)
		}
		return result

	default:
		// For types implementing fmt.Stringer, use their string
		// representation for consistent serialization.
		if s, ok := v.(fmt.Stringer); ok {
			return s.String()
		}
		return v
	}
}

// getSortedKeys returns the keys of a map in sorted order
func getSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
