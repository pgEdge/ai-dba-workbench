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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
		// We use a type alias to make the explicit type check intentional
		// and avoid the linter's "type will be inferred" warning.
		type ensurePartitionFunc func(
			context.Context,
			*pgxpool.Conn,
			time.Time,
		) error

		var fn ensurePartitionFunc = bp.EnsurePartition
		_ = fn
	})

	// Verify that a probe embedding BaseMetricsProbe inherits EnsurePartition.
	t.Run("embedded probe inherits EnsurePartition", func(t *testing.T) {
		type embeddingProbe struct {
			BaseMetricsProbe
		}

		ep := &embeddingProbe{
			BaseMetricsProbe: BaseMetricsProbe{config: config},
		}

		// This compiles only if EnsurePartition is inherited from BaseMetricsProbe
		// with the expected signature. We use a type alias to make the explicit
		// type check intentional.
		type ensurePartitionFunc func(
			context.Context,
			*pgxpool.Conn,
			time.Time,
		) error

		var fn ensurePartitionFunc = ep.EnsurePartition
		_ = fn
	})
}

// TestInvalidateFeatureCache verifies that InvalidateFeatureCache removes
// all cached entries for a specific connection while leaving entries for
// other connections untouched.
func TestInvalidateFeatureCache(t *testing.T) {
	// Seed the cache with entries for two connections.
	featureCache.Store(featureCacheKey{connectionName: "conn1", checkName: "view_x"}, true)
	featureCache.Store(featureCacheKey{connectionName: "conn1", checkName: "view_y"}, false)
	featureCache.Store(featureCacheKey{connectionName: "conn2", checkName: "view_x"}, true)

	InvalidateFeatureCache("conn1")

	// conn1 entries are gone.
	if _, ok := featureCache.Load(featureCacheKey{connectionName: "conn1", checkName: "view_x"}); ok {
		t.Error("expected conn1:view_x to be invalidated")
	}
	if _, ok := featureCache.Load(featureCacheKey{connectionName: "conn1", checkName: "view_y"}); ok {
		t.Error("expected conn1:view_y to be invalidated")
	}
	// conn2 entry is untouched.
	if _, ok := featureCache.Load(featureCacheKey{connectionName: "conn2", checkName: "view_x"}); !ok {
		t.Error("expected conn2:view_x to survive")
	}

	// Clean up.
	featureCache.Delete(featureCacheKey{connectionName: "conn2", checkName: "view_x"})
}

// TestInvalidateFeatureCache_NoEntries verifies that InvalidateFeatureCache
// is a no-op when called for a connection with no cached entries. It should
// not panic or cause any issues.
func TestInvalidateFeatureCache_NoEntries(t *testing.T) {
	// Should not panic
	InvalidateFeatureCache("nonexistent")
}

// TestCheckViewExistsSignature verifies that the CheckViewExists function
// has the expected signature and can be referenced. This is a structural
// test since we cannot mock the database easily here.
func TestCheckViewExistsSignature(t *testing.T) {
	// Verify the function signature by assigning it to a typed variable.
	// This compiles only if the signature matches the expected type.
	// We use a type alias to make the explicit type check intentional
	// and avoid the linter's "type will be inferred" warning.
	type viewExistsFunc func(
		context.Context,
		*pgxpool.Conn,
		string,
	) (bool, error)

	var fn viewExistsFunc = CheckViewExists

	// Use the function reference to prevent "declared and not used" error.
	_ = fn
}

// TestWrapQuery verifies that WrapQuery wraps a SQL query with a probe
// marker column that allows the server to identify collector queries.
func TestWrapQuery(t *testing.T) {
	tests := []struct {
		name      string
		probeName string
		query     string
		want      string
	}{
		{
			name:      "simple select",
			probeName: "pg_stat_activity",
			query:     "SELECT * FROM pg_stat_activity",
			want:      "SELECT 'pg_stat_activity' AS ai_dba_wb_probe, subq.* FROM (SELECT * FROM pg_stat_activity) AS subq",
		},
		{
			name:      "complex query with joins",
			probeName: "replication_slots",
			query:     "SELECT s.slot_name, s.active FROM pg_replication_slots s JOIN pg_stat_replication r ON s.slot_name = r.application_name",
			want:      "SELECT 'replication_slots' AS ai_dba_wb_probe, subq.* FROM (SELECT s.slot_name, s.active FROM pg_replication_slots s JOIN pg_stat_replication r ON s.slot_name = r.application_name) AS subq",
		},
		{
			name:      "empty query returns empty string",
			probeName: "test",
			query:     "",
			want:      "",
		},
		{
			name:      "whitespace-only query returns empty string",
			probeName: "test",
			query:     "   ",
			want:      "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WrapQuery(tc.probeName, tc.query)
			if got != tc.want {
				t.Errorf("WrapQuery(%q, %q) =\n  %q\nwant:\n  %q",
					tc.probeName, tc.query, got, tc.want)
			}
		})
	}
}

// TestCachedCheck verifies the cachedCheck function behavior for caching
// feature detection results.
func TestCachedCheck(t *testing.T) {
	t.Run("cache miss calls checkFn and caches result", func(t *testing.T) {
		key := featureCacheKey{connectionName: "test_conn", checkName: "view_exists"}
		defer featureCache.Delete(key)

		callCount := 0
		checkFn := func() (bool, error) {
			callCount++
			return true, nil
		}

		result, err := cachedCheck("test_conn", "view_exists", checkFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result {
			t.Error("expected result to be true")
		}
		if callCount != 1 {
			t.Errorf("checkFn should be called once, got %d", callCount)
		}

		// Verify the value was cached.
		if val, ok := featureCache.Load(key); !ok {
			t.Error("expected value to be cached")
		} else if val != true {
			t.Errorf("cached value: got %v, want true", val)
		}
	})

	t.Run("cache hit returns cached value without calling checkFn", func(t *testing.T) {
		key := featureCacheKey{connectionName: "test_conn2", checkName: "cached_view"}
		defer featureCache.Delete(key)

		// Pre-populate the cache.
		featureCache.Store(key, false)

		callCount := 0
		checkFn := func() (bool, error) {
			callCount++
			return true, nil
		}

		result, err := cachedCheck("test_conn2", "cached_view", checkFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result {
			t.Error("expected result to be false (from cache)")
		}
		if callCount != 0 {
			t.Errorf("checkFn should not be called on cache hit, got %d calls", callCount)
		}
	})

	t.Run("error from checkFn returns error without caching", func(t *testing.T) {
		key := featureCacheKey{connectionName: "test_conn3", checkName: "error_check"}
		defer featureCache.Delete(key)

		expectedErr := fmt.Errorf("database connection failed")
		checkFn := func() (bool, error) {
			return false, expectedErr
		}

		result, err := cachedCheck("test_conn3", "error_check", checkFn)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err != expectedErr {
			t.Errorf("error: got %v, want %v", err, expectedErr)
		}
		if result {
			t.Error("expected result to be false on error")
		}

		// Verify the value was NOT cached.
		if _, ok := featureCache.Load(key); ok {
			t.Error("error result should not be cached")
		}
	})

	t.Run("type assertion failure returns error for invalid cached type", func(t *testing.T) {
		key := featureCacheKey{connectionName: "test_conn4", checkName: "invalid_type"}
		defer featureCache.Delete(key)

		// Pre-populate the cache with an invalid type (string instead of bool).
		featureCache.Store(key, "not a bool")

		checkFn := func() (bool, error) {
			t.Error("checkFn should not be called when cache has invalid type")
			return true, nil
		}

		result, err := cachedCheck("test_conn4", "invalid_type", checkFn)
		if err == nil {
			t.Fatal("expected error for invalid cached type, got nil")
		}
		if result {
			t.Error("expected result to be false on type assertion failure")
		}
		// Verify the error message mentions the connection and check names.
		if !strings.Contains(err.Error(), "test_conn4") || !strings.Contains(err.Error(), "invalid_type") {
			t.Errorf("error should mention connection and check names: %v", err)
		}
	})

	t.Run("caches false result", func(t *testing.T) {
		key := featureCacheKey{connectionName: "test_conn5", checkName: "false_result"}
		defer featureCache.Delete(key)

		callCount := 0
		checkFn := func() (bool, error) {
			callCount++
			return false, nil
		}

		// First call should execute checkFn.
		result, err := cachedCheck("test_conn5", "false_result", checkFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result {
			t.Error("expected result to be false")
		}
		if callCount != 1 {
			t.Errorf("checkFn should be called once, got %d", callCount)
		}

		// Second call should return cached false without calling checkFn.
		result, err = cachedCheck("test_conn5", "false_result", checkFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result {
			t.Error("expected cached result to be false")
		}
		if callCount != 1 {
			t.Errorf("checkFn should not be called again, got %d calls", callCount)
		}
	})
}
