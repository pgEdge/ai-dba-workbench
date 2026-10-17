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
	"math"
	"testing"
)

// TestComputeMetricsHash_RowOrderIndependent checks that rows with
// different key sets, and rows where one key set is a prefix of the
// other, hash identically whatever order they arrive in. Both cases
// exercise the tie-breaks in the canonical row sort.
func TestComputeMetricsHash_RowOrderIndependent(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]any
	}{
		{
			name: "different first key",
			a:    map[string]any{"alpha": 1},
			b:    map[string]any{"beta": 1},
		},
		{
			name: "one key set is a prefix of the other",
			a:    map[string]any{"alpha": 1},
			b:    map[string]any{"alpha": 1, "beta": 2},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forward, err := ComputeMetricsHash([]map[string]any{tc.a, tc.b})
			if err != nil {
				t.Fatalf("forward hash: %v", err)
			}
			reverse, err := ComputeMetricsHash([]map[string]any{tc.b, tc.a})
			if err != nil {
				t.Fatalf("reverse hash: %v", err)
			}
			if forward != reverse {
				t.Errorf("row order changed the hash: %s vs %s",
					forward, reverse)
			}
		})
	}
}

// TestNormalizeValue_UnsignedOverflow checks that unsigned values too
// large for int64 are returned unchanged rather than wrapped negative.
func TestNormalizeValue_UnsignedOverflow(t *testing.T) {
	if got := normalizeValue(uint64(math.MaxUint64)); got != uint64(math.MaxUint64) {
		t.Errorf("uint64 overflow: got %v [%T]", got, got)
	}
	if got := normalizeValue(uint(math.MaxUint)); got != uint(math.MaxUint) {
		t.Errorf("uint overflow: got %v [%T]", got, got)
	}
}
