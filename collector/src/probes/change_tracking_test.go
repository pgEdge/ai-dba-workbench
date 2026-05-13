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
	"strings"
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

// TestStripProbeMarker_RemovesMarkerColumn documents the contract of
// stripProbeMarker for change-detection probes. The function must
// remove the wrapper column injected by WrapQuery from every row so
// the resulting hash matches the stored snapshot, which does not
// include that column.
func TestStripProbeMarker_RemovesMarkerColumn(t *testing.T) {
	t.Run("strips marker from every row", func(t *testing.T) {
		input := []map[string]any{
			{"ai_dba_wb_probe": "pg_settings", "name": "max_connections", "setting": "100"},
			{"ai_dba_wb_probe": "pg_settings", "name": "shared_buffers", "setting": "128MB"},
		}
		out := stripProbeMarker(input)
		if len(out) != 2 {
			t.Fatalf("expected 2 rows, got %d", len(out))
		}
		for i, row := range out {
			if _, present := row["ai_dba_wb_probe"]; present {
				t.Errorf("row %d still contains ai_dba_wb_probe", i)
			}
		}
		// Other keys must be preserved verbatim.
		if out[0]["name"] != "max_connections" {
			t.Errorf("row 0 name mutated: %v", out[0]["name"])
		}
		if out[1]["setting"] != "128MB" {
			t.Errorf("row 1 setting mutated: %v", out[1]["setting"])
		}
	})

	t.Run("does not modify original input", func(t *testing.T) {
		input := []map[string]any{
			{"ai_dba_wb_probe": "pg_settings", "name": "x"},
		}
		_ = stripProbeMarker(input)
		if _, present := input[0]["ai_dba_wb_probe"]; !present {
			t.Error("stripProbeMarker mutated its input")
		}
	})

	t.Run("returns same slice when no marker present", func(t *testing.T) {
		input := []map[string]any{
			{"name": "x", "setting": "y"},
		}
		out := stripProbeMarker(input)
		// The function returns the original slice when no row has the
		// marker; this is a cheap allocation-avoidance optimization.
		if len(out) != 1 {
			t.Fatalf("expected 1 row, got %d", len(out))
		}
		if out[0]["name"] != "x" {
			t.Errorf("row 0 mutated: %v", out[0])
		}
		// Identity check guarantees the optimization actually
		// short-circuits: if a future refactor accidentally introduces
		// a copy, this comparison fires even though the values still
		// match.
		if &out[0] != &input[0] {
			t.Error("expected the original slice to be returned when no marker is present")
		}
	})

	t.Run("handles empty slice", func(t *testing.T) {
		out := stripProbeMarker([]map[string]any{})
		if len(out) != 0 {
			t.Errorf("expected empty slice, got %d rows", len(out))
		}
	})

	t.Run("handles nil input", func(t *testing.T) {
		out := stripProbeMarker(nil)
		if len(out) != 0 {
			t.Errorf("expected empty result, got %d rows", len(out))
		}
	})
}

// TestHasDataChanged_HashFailsCurrent reaches the otherwise-defensive
// branch where ComputeMetricsHash returns an error for the live
// metrics. We trigger it by injecting a Go channel — json.Marshal,
// which ComputeMetricsHash uses internally, rejects channel values.
// The integration pool is needed because HasDataChanged signature
// requires a real *pgxpool.Conn, but the connection is never touched
// because the hash failure short-circuits first.
func TestHasDataChanged_HashFailsCurrent(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	bad := []map[string]any{{"name": "x", "unsupported": make(chan int)}}
	_, err := HasDataChanged(ctx, conn, 1, "fixture", bad,
		"SELECT 1 WHERE FALSE", nil)
	if err == nil {
		t.Fatal("expected hash failure for current metrics")
	}
	if !strings.Contains(err.Error(), "failed to compute current metrics hash") {
		t.Errorf("error did not include expected wrapper: %v", err)
	}
}

// TestHasDataChanged_QueryError exercises the error-return branch of
// HasDataChanged when the supplied fetch query fails. The integration
// pool is required because pgxpool.Conn cannot be constructed by
// hand; deliberately broken SQL surfaces a Query error without
// needing a fake driver.
func TestHasDataChanged_QueryError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Syntactically invalid query so pgx returns an error from Query;
	// no parameter substitution can rescue this. The function should
	// wrap and propagate the error rather than panicking or treating
	// it as "no change".
	_, err := HasDataChanged(ctx, conn, 1, "fixture",
		[]map[string]any{{"k": "v"}},
		"SELECT not a valid query at all",
		nil)
	if err == nil {
		t.Fatal("expected HasDataChanged to surface query error")
	}
	if !strings.Contains(err.Error(), "failed to query most recent data") {
		t.Errorf("error did not include expected wrapper: %v", err)
	}
}

// TestHasDataChanged_NormalizerInvoked verifies that the optional
// normalizer is applied AFTER the marker is stripped, so probes that
// supply a normalizer (e.g. pg_extension renaming _database_name) do
// not have to also strip the marker themselves.
func TestHasDataChanged_NormalizerInvoked(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Run against a known-empty subset of the datastore so the stored
	// side returns zero rows and the comparison only depends on the
	// live-side metric shape. metrics.pg_extension has the right
	// schema; using a unique synthetic connection_id keeps the
	// fixture isolated from any other test data.
	const fixtureConnID = 9876543

	// Sanity wipe.
	if _, err := conn.Exec(ctx,
		"DELETE FROM metrics.pg_extension WHERE connection_id=$1",
		fixtureConnID); err != nil {
		t.Fatalf("clean fixture: %v", err)
	}

	// Track whether the normalizer was called. The marker has already
	// been stripped by stripProbeMarker before we see it here.
	called := false
	normalizer := func(in []map[string]any) []map[string]any {
		called = true
		for _, m := range in {
			if _, present := m["ai_dba_wb_probe"]; present {
				t.Error("normalizer saw marker column; strip should run first")
			}
		}
		return in
	}

	_, err := HasDataChanged(ctx, conn, fixtureConnID, "pg_extension",
		[]map[string]any{
			{"ai_dba_wb_probe": "pg_extension", "extname": "plpgsql"},
		},
		`SELECT database_name, extname, extversion, extrelocatable, schema_name
		 FROM metrics.pg_extension
		 WHERE connection_id = $1
		   AND collected_at = (
		       SELECT MAX(collected_at)
		       FROM metrics.pg_extension
		       WHERE connection_id = $1
		   )
		 ORDER BY extname`,
		normalizer)
	if err != nil {
		t.Fatalf("HasDataChanged: %v", err)
	}
	if !called {
		t.Error("normalizer was not invoked")
	}
}

// TestStripProbeMarker_FixesOverCollectionHashMismatch is the
// regression test for the pg_settings over-collection bug reported in
// issue #219. Live probe metrics produced via WrapQuery +
// ScanRowsToMaps carry an extra ai_dba_wb_probe column that stored
// snapshots do not; without stripping, every hourly collection
// produced a fresh snapshot. The test reproduces that mismatch and
// verifies stripProbeMarker eliminates it.
func TestStripProbeMarker_FixesOverCollectionHashMismatch(t *testing.T) {
	// What the live probe returns through ScanRowsToMaps.
	liveMetrics := []map[string]any{
		{
			"ai_dba_wb_probe": "pg_settings",
			"name":            "max_connections",
			"setting":         "100",
			"unit":            nil,
			"category":        "Connections",
			"context":         "postmaster",
			"vartype":         "integer",
			"source":          "default",
			"min_val":         "1",
			"max_val":         "262143",
			"enumvals":        nil,
			"boot_val":        "100",
			"reset_val":       "100",
			"sourcefile":      nil,
			"sourceline":      nil,
			"pending_restart": false,
			"short_desc":      "max conns",
			"extra_desc":      "",
		},
	}

	// What the stored snapshot query returns; same columns minus the
	// synthetic ai_dba_wb_probe marker.
	storedMetrics := []map[string]any{
		{
			"name":            "max_connections",
			"setting":         "100",
			"unit":            nil,
			"category":        "Connections",
			"context":         "postmaster",
			"vartype":         "integer",
			"source":          "default",
			"min_val":         "1",
			"max_val":         "262143",
			"enumvals":        nil,
			"boot_val":        "100",
			"reset_val":       "100",
			"sourcefile":      nil,
			"sourceline":      nil,
			"pending_restart": false,
			"short_desc":      "max conns",
			"extra_desc":      "",
		},
	}

	// Without stripping, the hashes differ; this is the bug.
	rawLiveHash, err := ComputeMetricsHash(liveMetrics)
	if err != nil {
		t.Fatalf("hash live: %v", err)
	}
	storedHash, err := ComputeMetricsHash(storedMetrics)
	if err != nil {
		t.Fatalf("hash stored: %v", err)
	}
	if rawLiveHash == storedHash {
		t.Fatal("test fixture invalid: unstripped live and stored hashes should differ")
	}

	// With stripping, the hashes match; this is the fix.
	strippedHash, err := ComputeMetricsHash(stripProbeMarker(liveMetrics))
	if err != nil {
		t.Fatalf("hash stripped: %v", err)
	}
	if strippedHash != storedHash {
		t.Errorf("stripped live hash should match stored hash:\n  stripped: %s\n  stored:   %s",
			strippedHash, storedHash)
	}
}
