/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"strings"
	"testing"
)

func TestServerStatusCaseSQL(t *testing.T) {
	t.Run("contains correct expressions for m.collected_at alias", func(t *testing.T) {
		result := serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL")

		// Check that the nullCheckExpr appears in the initializing condition
		if !strings.Contains(result, "WHEN c.is_monitored AND m.collected_at IS NULL") {
			t.Error("Expected nullCheckExpr 'm.collected_at IS NULL' in initializing condition")
		}

		// Check that collectedAtExpr appears in the time comparisons
		if !strings.Contains(result, "WHEN m.collected_at > NOW() - INTERVAL '60 seconds'") {
			t.Error("Expected collectedAtExpr 'm.collected_at' in 60 seconds comparison")
		}
		if !strings.Contains(result, "WHEN m.collected_at > NOW() - INTERVAL '150 seconds'") {
			t.Error("Expected collectedAtExpr 'm.collected_at' in 150 seconds comparison")
		}
		if !strings.Contains(result, "WHEN m.collected_at IS NOT NULL") {
			t.Error("Expected collectedAtExpr 'm.collected_at' in IS NOT NULL check")
		}
	})

	t.Run("contains correct expressions for lc alias pattern", func(t *testing.T) {
		result := serverStatusCaseSQL("lc.collected_at", "lc.connection_id IS NULL")

		// Check that the nullCheckExpr appears in the initializing condition
		if !strings.Contains(result, "WHEN c.is_monitored AND lc.connection_id IS NULL") {
			t.Error("Expected nullCheckExpr 'lc.connection_id IS NULL' in initializing condition")
		}

		// Check that collectedAtExpr appears in the time comparisons
		if !strings.Contains(result, "WHEN lc.collected_at > NOW() - INTERVAL '60 seconds'") {
			t.Error("Expected collectedAtExpr 'lc.collected_at' in 60 seconds comparison")
		}
		if !strings.Contains(result, "WHEN lc.collected_at > NOW() - INTERVAL '150 seconds'") {
			t.Error("Expected collectedAtExpr 'lc.collected_at' in 150 seconds comparison")
		}
		if !strings.Contains(result, "WHEN lc.collected_at IS NOT NULL") {
			t.Error("Expected collectedAtExpr 'lc.collected_at' in IS NOT NULL check")
		}
	})

	t.Run("output starts with CASE and ends with END", func(t *testing.T) {
		result := serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL")

		trimmed := strings.TrimSpace(result)
		if !strings.HasPrefix(trimmed, "CASE") {
			t.Errorf("Expected output to start with CASE, got: %s", trimmed)
		}
		if !strings.HasSuffix(trimmed, "END") {
			t.Errorf("Expected output to end with END, got: %s", trimmed)
		}
	})

	t.Run("contains all status values", func(t *testing.T) {
		result := serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL")

		statusValues := []string{"'online'", "'offline'", "'warning'", "'initialising'", "'unknown'"}
		for _, status := range statusValues {
			if !strings.Contains(result, status) {
				t.Errorf("Expected status value %s in output", status)
			}
		}
	})

	t.Run("contains time intervals", func(t *testing.T) {
		result := serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL")

		if !strings.Contains(result, "INTERVAL '60 seconds'") {
			t.Error("Expected INTERVAL '60 seconds' in output")
		}
		if !strings.Contains(result, "INTERVAL '150 seconds'") {
			t.Error("Expected INTERVAL '150 seconds' in output")
		}
	})

	t.Run("contains connection error check", func(t *testing.T) {
		result := serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL")

		if !strings.Contains(result, "c.connection_error IS NOT NULL") {
			t.Error("Expected connection error check in output")
		}
		if !strings.Contains(result, "c.is_monitored") {
			t.Error("Expected is_monitored check in output")
		}
	})

	t.Run("offline appears twice for different conditions", func(t *testing.T) {
		result := serverStatusCaseSQL("m.collected_at", "m.collected_at IS NULL")

		// Count occurrences of 'offline'
		count := strings.Count(result, "'offline'")
		if count != 2 {
			t.Errorf("Expected 'offline' to appear 2 times (connection_error and stale data), got %d", count)
		}
	})
}
