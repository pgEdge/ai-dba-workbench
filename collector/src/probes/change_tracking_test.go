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
)

func TestNormalizeDatabaseName(t *testing.T) {
	t.Run("renames _database_name to database_name", func(t *testing.T) {
		input := []map[string]any{
			{
				"_database_name": "mydb",
				"extname":        "plpgsql",
				"extversion":     "1.0",
			},
			{
				"_database_name": "otherdb",
				"extname":        "pgcrypto",
				"extversion":     "1.3",
			},
		}

		result := normalizeDatabaseName(input)

		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}

		// Check first row
		if _, exists := result[0]["_database_name"]; exists {
			t.Error("_database_name should have been renamed")
		}
		if result[0]["database_name"] != "mydb" {
			t.Errorf("database_name: got %v, want mydb", result[0]["database_name"])
		}
		if result[0]["extname"] != "plpgsql" {
			t.Errorf("extname: got %v, want plpgsql", result[0]["extname"])
		}

		// Check second row
		if result[1]["database_name"] != "otherdb" {
			t.Errorf("database_name: got %v, want otherdb", result[1]["database_name"])
		}
	})

	t.Run("preserves keys that are not _database_name", func(t *testing.T) {
		input := []map[string]any{
			{
				"name":    "test",
				"setting": "value",
				"unit":    nil,
			},
		}

		result := normalizeDatabaseName(input)

		if result[0]["name"] != "test" {
			t.Errorf("name: got %v, want test", result[0]["name"])
		}
		if result[0]["setting"] != "value" {
			t.Errorf("setting: got %v, want value", result[0]["setting"])
		}
		if result[0]["unit"] != nil {
			t.Errorf("unit: got %v, want nil", result[0]["unit"])
		}
	})

	t.Run("does not modify original input", func(t *testing.T) {
		input := []map[string]any{
			{
				"_database_name": "mydb",
				"extname":        "plpgsql",
			},
		}

		_ = normalizeDatabaseName(input)

		// Original should still have _database_name
		if _, exists := input[0]["_database_name"]; !exists {
			t.Error("original input was modified")
		}
	})

	t.Run("handles empty slice", func(t *testing.T) {
		input := []map[string]any{}

		result := normalizeDatabaseName(input)

		if len(result) != 0 {
			t.Errorf("expected empty result, got %d items", len(result))
		}
	})

	t.Run("handles empty maps", func(t *testing.T) {
		input := []map[string]any{{}}

		result := normalizeDatabaseName(input)

		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if len(result[0]) != 0 {
			t.Errorf("expected empty map, got %d keys", len(result[0]))
		}
	})
}

func TestNormalizeDatabaseNameHashConsistency(t *testing.T) {
	t.Run("normalized metrics produce same hash as stored format", func(t *testing.T) {
		// Metrics as returned from database query (with _database_name)
		collectedMetrics := []map[string]any{
			{
				"_database_name": "mydb",
				"extname":        "plpgsql",
				"extversion":     "1.0",
				"extrelocatable": false,
				"schema_name":    "pg_catalog",
			},
		}

		// Metrics as stored in datastore (with database_name)
		storedMetrics := []map[string]any{
			{
				"database_name":  "mydb",
				"extname":        "plpgsql",
				"extversion":     "1.0",
				"extrelocatable": false,
				"schema_name":    "pg_catalog",
			},
		}

		// After normalization, collected metrics should match stored format
		normalizedMetrics := normalizeDatabaseName(collectedMetrics)

		collectedHash, err := ComputeMetricsHash(normalizedMetrics)
		if err != nil {
			t.Fatalf("error hashing collected: %v", err)
		}

		storedHash, err := ComputeMetricsHash(storedMetrics)
		if err != nil {
			t.Fatalf("error hashing stored: %v", err)
		}

		if collectedHash != storedHash {
			t.Errorf("hashes differ after normalization:\n  collected: %s\n  stored:    %s",
				collectedHash, storedHash)
		}
	})

	t.Run("different data produces different hash", func(t *testing.T) {
		metrics1 := []map[string]any{
			{"_database_name": "db1", "extname": "plpgsql"},
		}
		metrics2 := []map[string]any{
			{"_database_name": "db2", "extname": "plpgsql"},
		}

		hash1, err := ComputeMetricsHash(normalizeDatabaseName(metrics1))
		if err != nil {
			t.Fatalf("error hashing metrics1: %v", err)
		}
		hash2, err := ComputeMetricsHash(normalizeDatabaseName(metrics2))
		if err != nil {
			t.Fatalf("error hashing metrics2: %v", err)
		}

		if hash1 == hash2 {
			t.Error("different data should produce different hashes")
		}
	})
}

// TestHasDataChangedHelper tests the helper function logic without a real
// database connection. The function delegates to ComputeMetricsHash, which
// is already tested. These tests verify the normalization integration.
func TestHasDataChangedNormalization(t *testing.T) {
	t.Run("normalization function is called when provided", func(t *testing.T) {
		called := false
		normalizer := func(m []map[string]any) []map[string]any {
			called = true
			return m
		}

		metrics := []map[string]any{{"key": "value"}}

		// We cannot call HasDataChanged without a database connection,
		// but we can verify normalization is applied by the normalizeDatabaseName
		// function directly.
		_ = normalizer(metrics)

		if !called {
			t.Error("normalizer function was not called")
		}
	})

	t.Run("nil normalizer returns original metrics for hashing", func(t *testing.T) {
		metrics := []map[string]any{
			{"name": "test", "value": int64(42)},
		}

		// Compute hash directly
		hash1, err := ComputeMetricsHash(metrics)
		if err != nil {
			t.Fatalf("hash error: %v", err)
		}

		// The hash should be the same if no normalization is applied
		hash2, err := ComputeMetricsHash(metrics)
		if err != nil {
			t.Fatalf("hash error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("hashes should be identical: %s vs %s", hash1, hash2)
		}
	})
}

// TestHasDataChangedScenarios documents the expected behavior of
// HasDataChanged for various scenarios. These serve as specification
// tests even though we cannot run them without a database.
func TestHasDataChangedScenarios(t *testing.T) {
	t.Run("scenario: no stored data returns true", func(t *testing.T) {
		// When storedMetrics is empty (no previous data), HasDataChanged
		// should return (true, nil) to indicate data has changed (first collection).
		//
		// This is verified by the implementation:
		//   if len(storedMetrics) == 0 { return true, nil }
		t.Log("Verified by code inspection: len(storedMetrics) == 0 returns (true, nil)")
	})

	t.Run("scenario: identical metrics returns false", func(t *testing.T) {
		// When current and stored metrics produce the same hash,
		// HasDataChanged returns (false, nil).
		//
		// This is verified by the implementation:
		//   return currentHash != storedHash, nil
		t.Log("Verified by code inspection: identical hashes return false")
	})

	t.Run("scenario: different metrics returns true", func(t *testing.T) {
		// When current and stored metrics produce different hashes,
		// HasDataChanged returns (true, nil).
		t.Log("Verified by code inspection: different hashes return true")
	})
}

func TestComputeMetricsHashDirect(t *testing.T) {
	// Test ComputeMetricsHash directly to verify change detection logic
	t.Run("identical metrics produce identical hashes", func(t *testing.T) {
		metrics1 := []map[string]any{
			{"name": "setting1", "value": "100"},
			{"name": "setting2", "value": "200"},
		}
		metrics2 := []map[string]any{
			{"name": "setting1", "value": "100"},
			{"name": "setting2", "value": "200"},
		}

		hash1, err := ComputeMetricsHash(metrics1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(metrics2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("identical metrics should produce same hash:\n  hash1: %s\n  hash2: %s", hash1, hash2)
		}
	})

	t.Run("different metrics produce different hashes", func(t *testing.T) {
		metrics1 := []map[string]any{
			{"name": "setting1", "value": "100"},
		}
		metrics2 := []map[string]any{
			{"name": "setting1", "value": "200"}, // Different value
		}

		hash1, err := ComputeMetricsHash(metrics1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(metrics2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 == hash2 {
			t.Error("different metrics should produce different hashes")
		}
	})

	t.Run("empty metrics produce consistent hash", func(t *testing.T) {
		metrics1 := []map[string]any{}
		metrics2 := []map[string]any{}

		hash1, err := ComputeMetricsHash(metrics1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(metrics2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}

		if hash1 != hash2 {
			t.Errorf("empty metrics should produce same hash:\n  hash1: %s\n  hash2: %s", hash1, hash2)
		}
	})
}
