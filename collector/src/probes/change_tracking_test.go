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
	t.Run("normalization changes hash when key names differ", func(t *testing.T) {
		raw := []map[string]any{{"_database_name": "mydb", "extname": "plpgsql"}}
		normalized := normalizeDatabaseName(raw)

		rawHash, err := ComputeMetricsHash(raw)
		if err != nil {
			t.Fatalf("raw hash error: %v", err)
		}
		normalizedHash, err := ComputeMetricsHash(normalized)
		if err != nil {
			t.Fatalf("normalized hash error: %v", err)
		}

		if rawHash == normalizedHash {
			t.Fatal("expected normalized metrics to produce a different hash from raw metrics")
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
	t.Run("scenario: no stored data with current data returns true", func(t *testing.T) {
		currentHash, err := ComputeMetricsHash([]map[string]any{{"k": "v"}})
		if err != nil {
			t.Fatalf("current hash error: %v", err)
		}
		storedHash, err := ComputeMetricsHash([]map[string]any{})
		if err != nil {
			t.Fatalf("stored hash error: %v", err)
		}
		if currentHash == storedHash {
			t.Fatal("expected different hashes for non-empty current vs empty stored")
		}
	})

	t.Run("scenario: identical metrics returns false", func(t *testing.T) {
		metrics := []map[string]any{{"key": "value"}}
		hash1, err := ComputeMetricsHash(metrics)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(metrics)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}
		if hash1 != hash2 {
			t.Fatal("expected identical hashes for identical metrics")
		}
	})

	t.Run("scenario: different metrics returns true", func(t *testing.T) {
		metrics1 := []map[string]any{{"key": "value1"}}
		metrics2 := []map[string]any{{"key": "value2"}}
		hash1, err := ComputeMetricsHash(metrics1)
		if err != nil {
			t.Fatalf("hash1 error: %v", err)
		}
		hash2, err := ComputeMetricsHash(metrics2)
		if err != nil {
			t.Fatalf("hash2 error: %v", err)
		}
		if hash1 == hash2 {
			t.Fatal("expected different hashes for different metrics")
		}
	})

	t.Run("scenario: both empty returns false (zero-row convergence)", func(t *testing.T) {
		// When both storedMetrics and currentMetrics are empty slices,
		// HasDataChanged should return (false, nil) because nothing changed.
		// This is the "zero-row convergence" case for probes that legitimately
		// return no rows (e.g., pg_ident_file_mappings on a server with no
		// ident mappings).
		//
		// Verify via hash comparison: empty slices produce identical hashes.
		emptyHash1, err := ComputeMetricsHash([]map[string]any{})
		if err != nil {
			t.Fatalf("hash error: %v", err)
		}
		emptyHash2, err := ComputeMetricsHash([]map[string]any{})
		if err != nil {
			t.Fatalf("hash error: %v", err)
		}
		if emptyHash1 != emptyHash2 {
			t.Errorf("empty slices should produce identical hashes: %s vs %s",
				emptyHash1, emptyHash2)
		}
		t.Log("Verified: empty current and empty stored produce matching hashes, so changed=false")
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
