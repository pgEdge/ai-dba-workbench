/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"testing"
)

// TestCheckThresholdOperators verifies all comparison operators work correctly
func TestCheckThresholdOperators(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		// Greater than operator
		{"gt_above", 100.0, ">", 50.0, true},
		{"gt_below", 30.0, ">", 50.0, false},
		{"gt_equal", 50.0, ">", 50.0, false},

		// Greater than or equal operator
		{"gte_above", 100.0, ">=", 50.0, true},
		{"gte_equal", 50.0, ">=", 50.0, true},
		{"gte_below", 30.0, ">=", 50.0, false},

		// Less than operator
		{"lt_below", 30.0, "<", 50.0, true},
		{"lt_above", 100.0, "<", 50.0, false},
		{"lt_equal", 50.0, "<", 50.0, false},

		// Less than or equal operator
		{"lte_below", 30.0, "<=", 50.0, true},
		{"lte_equal", 50.0, "<=", 50.0, true},
		{"lte_above", 100.0, "<=", 50.0, false},

		// Equal operator
		{"eq_same", 50.0, "==", 50.0, true},
		{"eq_different_above", 100.0, "==", 50.0, false},
		{"eq_different_below", 30.0, "==", 50.0, false},

		// Not equal operator
		{"ne_different_above", 100.0, "!=", 50.0, true},
		{"ne_different_below", 30.0, "!=", 50.0, true},
		{"ne_same", 50.0, "!=", 50.0, false},

		// Unknown operators should return false
		{"unknown_tilde", 100.0, "~", 50.0, false},
		{"unknown_empty", 100.0, "", 50.0, false},
		{"unknown_invalid", 100.0, "???", 50.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.checkThreshold(tt.value, tt.operator, tt.threshold)
			if result != tt.expected {
				t.Errorf("checkThreshold(%v, %q, %v) = %v, expected %v",
					tt.value, tt.operator, tt.threshold, result, tt.expected)
			}
		})
	}
}

// TestCheckThresholdBoundaryConditions tests edge cases
func TestCheckThresholdBoundaryConditions(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		// Zero values
		{"zero_gt_zero", 0.0, ">", 0.0, false},
		{"zero_gte_zero", 0.0, ">=", 0.0, true},
		{"zero_lt_zero", 0.0, "<", 0.0, false},
		{"zero_lte_zero", 0.0, "<=", 0.0, true},
		{"zero_eq_zero", 0.0, "==", 0.0, true},
		{"zero_ne_zero", 0.0, "!=", 0.0, false},

		// Negative values
		{"negative_gt_negative", -10.0, ">", -20.0, true},
		{"negative_lt_positive", -10.0, "<", 10.0, true},
		{"negative_eq_negative", -50.0, "==", -50.0, true},

		// Very small differences
		{"small_diff_gt", 50.0001, ">", 50.0, true},
		{"small_diff_lt", 49.9999, "<", 50.0, true},

		// Large values
		{"large_gt", 1e15, ">", 1e14, true},
		{"large_lt", 1e14, "<", 1e15, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.checkThreshold(tt.value, tt.operator, tt.threshold)
			if result != tt.expected {
				t.Errorf("checkThreshold(%v, %q, %v) = %v, expected %v",
					tt.value, tt.operator, tt.threshold, result, tt.expected)
			}
		})
	}
}

// TestCheckThresholdRealScenarios tests real-world alert scenarios
func TestCheckThresholdRealScenarios(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		// CPU utilization above 80%
		{"cpu_critical", 95.5, ">", 80.0, true},
		{"cpu_normal", 45.0, ">", 80.0, false},

		// Replication lag above 10 seconds
		{"replication_lag_high", 15.5, ">", 10.0, true},
		{"replication_lag_ok", 5.0, ">", 10.0, false},

		// Disk space below 20%
		{"disk_space_critical", 10.0, "<", 20.0, true},
		{"disk_space_ok", 50.0, "<", 20.0, false},

		// Cache hit ratio below 90%
		{"cache_hit_low", 85.0, "<", 90.0, true},
		{"cache_hit_good", 95.0, "<", 90.0, false},

		// Connection count at limit
		{"conn_at_limit", 100.0, ">=", 100.0, true},
		{"conn_below_limit", 90.0, ">=", 100.0, false},

		// Transaction rate not zero
		{"tx_active", 100.0, "!=", 0.0, true},
		{"tx_idle", 0.0, "!=", 0.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.checkThreshold(tt.value, tt.operator, tt.threshold)
			if result != tt.expected {
				t.Errorf("checkThreshold(%v, %q, %v) = %v, expected %v",
					tt.value, tt.operator, tt.threshold, result, tt.expected)
			}
		})
	}
}
