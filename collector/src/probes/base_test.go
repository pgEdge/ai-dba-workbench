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
	"fmt"
	"testing"
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
		input    interface{}
		expected interface{}
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
	t.Run("[]string to []interface{}", func(t *testing.T) {
		input := []string{"a", "b", "c"}
		got := normalizeValue(input)
		result, ok := got.([]interface{})
		if !ok {
			t.Fatalf("expected []interface{}, got %T", got)
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

	t.Run("[]interface{} with mixed int types", func(t *testing.T) {
		input := []interface{}{int32(1), int64(2), uint16(3)}
		got := normalizeValue(input)
		result, ok := got.([]interface{})
		if !ok {
			t.Fatalf("expected []interface{}, got %T", got)
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
	input := map[string]interface{}{
		"count":  int32(10),
		"name":   "test",
		"active": true,
		"nested": map[string]interface{}{
			"val": uint16(5),
		},
	}

	got := normalizeValue(input)
	result, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", got)
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

	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested: expected map[string]interface{}, got %T", result["nested"])
	}
	if nested["val"] != int64(5) {
		t.Errorf("nested val: got %v [%T], want int64(5)", nested["val"], nested["val"])
	}
}

func TestComputeMetricsHashConsistency(t *testing.T) {
	t.Run("int32 vs int64 produce same hash", func(t *testing.T) {
		data1 := []map[string]interface{}{
			{"id": int32(42), "name": "test"},
		}
		data2 := []map[string]interface{}{
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
		data1 := []map[string]interface{}{
			{"count": uint32(42)},
		}
		data2 := []map[string]interface{}{
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

	t.Run("[]string vs []interface{} produce same hash", func(t *testing.T) {
		data1 := []map[string]interface{}{
			{"tags": []string{"a", "b"}},
		}
		data2 := []map[string]interface{}{
			{"tags": []interface{}{"a", "b"}},
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
			t.Errorf("[]string vs []interface{}: hashes differ\n  []string hash: %s\n  []interface{} hash: %s", hash1, hash2)
		}
	})

	t.Run("float32 normalizes consistently", func(t *testing.T) {
		data1 := []map[string]interface{}{
			{"ratio": float32(3.14)},
		}
		data2 := []map[string]interface{}{
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
		data1 := []map[string]interface{}{
			{"active": true, "deleted": false},
		}
		data2 := []map[string]interface{}{
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
		data1 := []map[string]interface{}{
			{
				"info": map[string]interface{}{
					"count": int32(10),
					"items": []interface{}{int16(1), int32(2)},
				},
			},
		}
		data2 := []map[string]interface{}{
			{
				"info": map[string]interface{}{
					"count": int64(10),
					"items": []interface{}{int64(1), int64(2)},
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
		data1 := []map[string]interface{}{
			{"id": int64(42)},
		}
		data2 := []map[string]interface{}{
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

// Ensure the unused import does not cause a build error.
var _ = fmt.Sprintf
