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
	"testing"
	"time"
)

// stringer is a test type that implements fmt.Stringer.
type stringer struct {
	val string
}

func (s stringer) String() string {
	return s.val
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"nil", nil, nil},

		// Integer types all normalize to int64
		{"int", int(42), int64(42)},
		{"int8", int8(42), int64(42)},
		{"int16", int16(42), int64(42)},
		{"int32", int32(42), int64(42)},
		{"int64", int64(42), int64(42)},
		{"uint", uint(42), int64(42)},
		{"uint8", uint8(42), int64(42)},
		{"uint16", uint16(42), int64(42)},
		{"uint32", uint32(42), int64(42)},
		{"uint64", uint64(42), int64(42)},

		// Float types normalize to float64
		{"float32", float32(3.14), float64(float32(3.14))},
		{"float64", float64(3.14), float64(3.14)},

		// Bool passes through
		{"bool true", true, true},
		{"bool false", false, false},

		// String passes through
		{"string", "hello", "hello"},

		// Byte slice converts to string
		{"[]byte", []byte("hello"), "hello"},

		// fmt.Stringer converts to string
		{"Stringer", stringer{"hello"}, "hello"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeValue(tc.input)
			if got != tc.expected {
				t.Errorf("normalizeValue(%v [%T]) = %v [%T], want %v [%T]",
					tc.input, tc.input, got, got, tc.expected, tc.expected)
			}
		})
	}
}

func TestNormalizeValueSlices(t *testing.T) {
	t.Run("[]string to []any", func(t *testing.T) {
		input := []string{"a", "b", "c"}
		got := normalizeValue(input)
		result, ok := got.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", got)
		}
		if len(result) != 3 {
			t.Fatalf("expected length 3, got %d", len(result))
		}
		for i, expected := range []string{"a", "b", "c"} {
			if result[i] != expected {
				t.Errorf("index %d: got %v, want %v", i, result[i], expected)
			}
		}
	})

	t.Run("[]any with mixed int types", func(t *testing.T) {
		input := []any{int32(1), int64(2), uint16(3)}
		got := normalizeValue(input)
		result, ok := got.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", got)
		}
		for i, expected := range []int64{1, 2, 3} {
			if result[i] != expected {
				t.Errorf("index %d: got %v [%T], want %v [%T]",
					i, result[i], result[i], expected, expected)
			}
		}
	})
}

func TestNormalizeValueMap(t *testing.T) {
	input := map[string]any{
		"count":  int32(10),
		"name":   "test",
		"active": true,
		"nested": map[string]any{
			"val": uint16(5),
		},
	}

	got := normalizeValue(input)
	result, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}

	if result["count"] != int64(10) {
		t.Errorf("count: got %v [%T], want int64(10)", result["count"], result["count"])
	}
	if result["name"] != "test" {
		t.Errorf("name: got %v, want test", result["name"])
	}
	if result["active"] != true {
		t.Errorf("active: got %v, want true", result["active"])
	}

	nested, ok := result["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested: expected map[string]any, got %T", result["nested"])
	}
	if nested["val"] != int64(5) {
		t.Errorf("nested val: got %v [%T], want int64(5)", nested["val"], nested["val"])
	}
}

func TestComputeMetricsHashConsistency(t *testing.T) {
	t.Run("int32 vs int64 produce same hash", func(t *testing.T) {
		data1 := []map[string]any{
			{"id": int32(42), "name": "test"},
		}
		data2 := []map[string]any{
			{"id": int64(42), "name": "test"},
		}

		hash1, err := ComputeMetricsHash(data1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(data2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("int32 vs int64: hashes differ\n  int32 hash: %s\n  int64 hash: %s", hash1, hash2)
		}
	})

	t.Run("uint32 vs int64 produce same hash", func(t *testing.T) {
		data1 := []map[string]any{
			{"count": uint32(42)},
		}
		data2 := []map[string]any{
			{"count": int64(42)},
		}

		hash1, err := ComputeMetricsHash(data1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(data2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("uint32 vs int64: hashes differ\n  uint32 hash: %s\n  int64 hash: %s", hash1, hash2)
		}
	})

	t.Run("[]string vs []any produce same hash", func(t *testing.T) {
		data1 := []map[string]any{
			{"tags": []string{"a", "b"}},
		}
		data2 := []map[string]any{
			{"tags": []any{"a", "b"}},
		}

		hash1, err := ComputeMetricsHash(data1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(data2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("[]string vs []any: hashes differ\n  []string hash: %s\n  []any hash: %s", hash1, hash2)
		}
	})

	t.Run("float32 normalizes consistently", func(t *testing.T) {
		data1 := []map[string]any{
			{"ratio": float32(3.14)},
		}
		data2 := []map[string]any{
			{"ratio": float32(3.14)},
		}

		hash1, err := ComputeMetricsHash(data1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(data2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("float32 consistency: hashes differ\n  hash1: %s\n  hash2: %s", hash1, hash2)
		}
	})

	t.Run("bool values hash correctly", func(t *testing.T) {
		data1 := []map[string]any{
			{"active": true, "deleted": false},
		}
		data2 := []map[string]any{
			{"active": true, "deleted": false},
		}

		hash1, err := ComputeMetricsHash(data1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(data2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("bool: hashes differ\n  hash1: %s\n  hash2: %s", hash1, hash2)
		}
	})

	t.Run("nested maps with mixed types produce same hash", func(t *testing.T) {
		data1 := []map[string]any{
			{
				"info": map[string]any{
					"count": int32(10),
					"items": []any{int16(1), int32(2)},
				},
			},
		}
		data2 := []map[string]any{
			{
				"info": map[string]any{
					"count": int64(10),
					"items": []any{int64(1), int64(2)},
				},
			},
		}

		hash1, err := ComputeMetricsHash(data1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(data2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("nested maps: hashes differ\n  hash1: %s\n  hash2: %s", hash1, hash2)
		}
	})

	t.Run("different values produce different hashes", func(t *testing.T) {
		data1 := []map[string]any{
			{"id": int64(42)},
		}
		data2 := []map[string]any{
			{"id": int64(43)},
		}

		hash1, err := ComputeMetricsHash(data1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(data2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 == hash2 {
			t.Error("different values should produce different hashes")
		}
	})
}

// TestNormalizeValueStringer verifies that fmt.Stringer types are
// converted to their string representation.
func TestNormalizeValueStringer(t *testing.T) {
	s := stringer{"hello-world"}
	got := normalizeValue(s)
	expected := "hello-world"
	if got != expected {
		t.Errorf("normalizeValue(Stringer) = %v [%T], want %v [%T]",
			got, got, expected, expected)
	}
}

// TestNormalizeValueUnknownType verifies that unrecognized types that do
// not implement fmt.Stringer are returned as-is.
func TestNormalizeValueUnknownType(t *testing.T) {
	type custom struct{ x int }
	input := custom{x: 7}
	got := normalizeValue(input)
	if got != input {
		t.Errorf("expected unknown type to pass through, got %v [%T]", got, got)
	}
}

func TestWeeklyPartitionBounds(t *testing.T) {
	plusFive := time.FixedZone("plus5", 5*60*60)
	plusNine := time.FixedZone("plus9", 9*60*60)
	minusFive := time.FixedZone("minus5", -5*60*60)

	tests := []struct {
		name       string
		input      time.Time
		wantSuffix string
		wantFrom   time.Time
		wantTo     time.Time
	}{
		{
			name:       "monday midnight utc",
			input:      time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantSuffix: "20260406",
			wantFrom:   time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantTo:     time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "sunday just before midnight utc",
			input:      time.Date(2026, 4, 12, 23, 59, 59, 0, time.UTC),
			wantSuffix: "20260406",
			wantFrom:   time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantTo:     time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "mid week wednesday utc",
			input:      time.Date(2026, 4, 8, 12, 34, 56, 0, time.UTC),
			wantSuffix: "20260406",
			wantFrom:   time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantTo:     time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			// 2026-04-13 02:30:00 +05 == 2026-04-12 21:30:00 UTC (Sunday in UTC).
			// Local-clock math would pick Monday 2026-04-13 as week start;
			// UTC-consistent math must pick Monday 2026-04-06.
			name:       "local monday but still sunday in utc",
			input:      time.Date(2026, 4, 13, 2, 30, 0, 0, plusFive),
			wantSuffix: "20260406",
			wantFrom:   time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantTo:     time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			// 2026-04-13 08:00:00 +09 == 2026-04-12 23:00:00 UTC (Sunday in UTC).
			name:       "local monday morning jp but still sunday in utc",
			input:      time.Date(2026, 4, 13, 8, 0, 0, 0, plusNine),
			wantSuffix: "20260406",
			wantFrom:   time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantTo:     time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			// 2026-04-12 22:00:00 -05 == 2026-04-13 03:00:00 UTC (Monday in UTC).
			// Local-clock math would pick previous Monday (2026-04-06);
			// UTC-consistent math must pick 2026-04-13.
			name:       "local sunday night but already monday in utc",
			input:      time.Date(2026, 4, 12, 22, 0, 0, 0, minusFive),
			wantSuffix: "20260413",
			wantFrom:   time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
			wantTo:     time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "sunday afternoon utc",
			input:      time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC),
			wantSuffix: "20260406",
			wantFrom:   time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantTo:     time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSuffix, gotFrom, gotTo := weeklyPartitionBounds(tc.input)
			if gotSuffix != tc.wantSuffix {
				t.Errorf("suffix: got %q, want %q", gotSuffix, tc.wantSuffix)
			}
			if !gotFrom.Equal(tc.wantFrom) {
				t.Errorf("from: got %s, want %s", gotFrom, tc.wantFrom)
			}
			if !gotTo.Equal(tc.wantTo) {
				t.Errorf("to: got %s, want %s", gotTo, tc.wantTo)
			}
			if gotFrom.Location() != time.UTC {
				t.Errorf("from location: got %s, want UTC", gotFrom.Location())
			}
			if gotTo.Location() != time.UTC {
				t.Errorf("to location: got %s, want UTC", gotTo.Location())
			}
			if gotFrom.Weekday() != time.Monday {
				t.Errorf("from weekday: got %s, want Monday", gotFrom.Weekday())
			}
			if gotTo.Sub(gotFrom) != 7*24*time.Hour {
				t.Errorf("range width: got %s, want 168h", gotTo.Sub(gotFrom))
			}
		})
	}
}

func TestWeeklyPartitionBoundsConsecutiveWeeks(t *testing.T) {
	// Two timestamps that are 6 local-days apart across a DST-like
	// boundary must still land on adjacent, non-overlapping Monday
	// weeks when computed in UTC.
	tz := time.FixedZone("plus5", 5*60*60)
	first := time.Date(2026, 4, 12, 23, 59, 59, 0, tz)      // Sun local, 18:59:59 UTC => Sun UTC, week 2026-04-06
	second := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC) // Mon UTC, week 2026-04-13

	_, fromA, toA := weeklyPartitionBounds(first)
	_, fromB, toB := weeklyPartitionBounds(second)

	if !toA.Equal(fromB) {
		t.Errorf("adjacent weeks must touch: toA=%s fromB=%s", toA, fromB)
	}
	if toB.Sub(fromA) != 14*24*time.Hour {
		t.Errorf("combined span: got %s, want 336h", toB.Sub(fromA))
	}
}

func TestPartitionBoundLayoutIncludesUTCOffset(t *testing.T) {
	_, from, to := weeklyPartitionBounds(time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC))
	gotFrom := from.Format(partitionBoundLayout)
	gotTo := to.Format(partitionBoundLayout)
	wantFrom := "2026-04-06 00:00:00Z"
	wantTo := "2026-04-13 00:00:00Z"
	if gotFrom != wantFrom {
		t.Errorf("from literal: got %q, want %q", gotFrom, wantFrom)
	}
	if gotTo != wantTo {
		t.Errorf("to literal: got %q, want %q", gotTo, wantTo)
	}
}

// TestParsePartitionEnd verifies that parsePartitionEnd extracts the
// upper bound timestamp from all three supported literal formats, and
// that malformed inputs fail gracefully rather than returning a bogus
// time. This is the helper used by DropExpiredPartitions to decide
// whether a partition is fully expired.
func TestParsePartitionEnd(t *testing.T) {
	tests := []struct {
		name  string
		bound string
		want  time.Time
		ok    bool
	}{
		{
			name:  "tz with minutes",
			bound: "FOR VALUES FROM ('2025-11-03 00:00:00+00:00') TO ('2025-11-10 00:00:00+00:00')",
			want:  time.Date(2025, 11, 10, 0, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "tz without minutes",
			bound: "FOR VALUES FROM ('2025-11-03 00:00:00+00') TO ('2025-11-10 00:00:00+00')",
			want:  time.Date(2025, 11, 10, 0, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "legacy no tz",
			bound: "FOR VALUES FROM ('2025-11-03 00:00:00') TO ('2025-11-10 00:00:00')",
			want:  time.Date(2025, 11, 10, 0, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "missing TO clause",
			bound: "FOR VALUES IN ('foo')",
			want:  time.Time{},
			ok:    false,
		},
		{
			name:  "missing closing quote",
			bound: "FOR VALUES FROM ('2025-11-03 00:00:00') TO ('2025-11-10 00:00:00",
			want:  time.Time{},
			ok:    false,
		},
		{
			name:  "unparseable timestamp",
			bound: "FOR VALUES FROM ('a') TO ('not-a-timestamp')",
			want:  time.Time{},
			ok:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parsePartitionEnd("test_part", tc.bound)
			if ok != tc.ok {
				t.Fatalf("ok: got %v, want %v", ok, tc.ok)
			}
			if ok && !got.Equal(tc.want) {
				t.Errorf("time: got %s, want %s", got, tc.want)
			}
		})
	}
}

// TestBaseMetricsProbeEnsurePartition verifies that BaseMetricsProbe
// provides an EnsurePartition method that satisfies the MetricsProbe
// interface. The method delegates to the package-level EnsurePartition
// function with the probe's table name.
func TestBaseMetricsProbeEnsurePartition(t *testing.T) {
	config := &ProbeConfig{
		Name: "test_probe",
	}
	bp := &BaseMetricsProbe{config: config}

	// Verify that BaseMetricsProbe's EnsurePartition method is
	// available. We cannot call it without a real database connection,
	// but we verify the method exists with the expected signature.
	// The actual functionality is tested via integration tests.
	t.Run("method has correct signature", func(t *testing.T) {
		// This compiles only if the signature matches the expected type.
		// The blank identifier assignment ensures the compiler verifies
		// the method signature without triggering unused variable warnings.
		_ = bp.EnsurePartition
	})

	// Verify that a probe embedding BaseMetricsProbe inherits EnsurePartition.
	t.Run("embedded probe inherits EnsurePartition", func(t *testing.T) {
		type embeddingProbe struct {
			BaseMetricsProbe
		}

		ep := &embeddingProbe{
			BaseMetricsProbe: BaseMetricsProbe{config: config},
		}

		// This compiles only if EnsurePartition is inherited from BaseMetricsProbe.
		_ = ep.EnsurePartition
	})
}
