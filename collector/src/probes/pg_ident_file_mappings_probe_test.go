/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"testing"
)

func TestPgIdentFileMappingsProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	}
	probe := NewPgIdentFileMappingsProbe(config)

	if probe.GetName() != ProbeNamePgIdentFileMappings {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgIdentFileMappings)
	}
}

func TestPgIdentFileMappingsProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	}
	probe := NewPgIdentFileMappingsProbe(config)

	if probe.GetTableName() != ProbeNamePgIdentFileMappings {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgIdentFileMappings)
	}
}

func TestPgIdentFileMappingsProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	}
	probe := NewPgIdentFileMappingsProbe(config)

	if probe.IsDatabaseScoped() {
		t.Error("IsDatabaseScoped() = true, want false (pg_ident_file_mappings is server-scoped)")
	}
}

func TestPgIdentFileMappingsProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	}
	probe := NewPgIdentFileMappingsProbe(config)

	query := probe.GetQuery()
	if query == "" {
		t.Error("GetQuery() returned empty string")
	}

	// Verify the query includes key columns from pg_ident_file_mappings
	expectedColumns := []string{
		"map_number",
		"file_name",
		"line_number",
		"map_name",
		"sys_name",
		"pg_username",
		"error",
	}

	for _, column := range expectedColumns {
		if !containsColumn(query, column) {
			t.Errorf("GetQuery() missing expected column: %s", column)
		}
	}
}

func TestPgIdentFileMappingsProbe_ComputeMetricsHash(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	}
	probe := NewPgIdentFileMappingsProbe(config)

	// Test with identical metrics
	metrics1 := []map[string]interface{}{
		{
			"map_number":  1,
			"map_name":    "omicron",
			"sys_name":    "bryanh",
			"pg_username": "bryanh",
		},
		{
			"map_number":  2,
			"map_name":    "omicron",
			"sys_name":    "ann",
			"pg_username": "ann",
		},
	}

	metrics2 := []map[string]interface{}{
		{
			"map_number":  1,
			"map_name":    "omicron",
			"sys_name":    "bryanh",
			"pg_username": "bryanh",
		},
		{
			"map_number":  2,
			"map_name":    "omicron",
			"sys_name":    "ann",
			"pg_username": "ann",
		},
	}

	hash1, err := probe.computeMetricsHash(metrics1)
	if err != nil {
		t.Fatalf("computeMetricsHash(metrics1) error = %v", err)
	}

	hash2, err := probe.computeMetricsHash(metrics2)
	if err != nil {
		t.Fatalf("computeMetricsHash(metrics2) error = %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("Identical metrics produced different hashes: %s vs %s", hash1, hash2)
	}

	// Test with different metrics
	metrics3 := []map[string]interface{}{
		{
			"map_number":  1,
			"map_name":    "omicron",
			"sys_name":    "robert", // Different value
			"pg_username": "bob",
		},
		{
			"map_number":  2,
			"map_name":    "omicron",
			"sys_name":    "ann",
			"pg_username": "ann",
		},
	}

	hash3, err := probe.computeMetricsHash(metrics3)
	if err != nil {
		t.Fatalf("computeMetricsHash(metrics3) error = %v", err)
	}

	if hash1 == hash3 {
		t.Errorf("Different metrics produced same hash: %s", hash1)
	}
}

func TestPgIdentFileMappingsProbe_ComputeMetricsHash_EmptyMetrics(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	}
	probe := NewPgIdentFileMappingsProbe(config)

	metrics := []map[string]interface{}{}

	hash, err := probe.computeMetricsHash(metrics)
	if err != nil {
		t.Fatalf("computeMetricsHash(empty) error = %v", err)
	}

	if hash == "" {
		t.Error("computeMetricsHash(empty) returned empty hash")
	}
}

func TestPgIdentFileMappingsProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgIdentFileMappings,
		Description:               "Test probe",
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
		IsEnabled:                 true,
	}
	probe := NewPgIdentFileMappingsProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig != config {
		t.Error("GetConfig() did not return the same config instance")
	}

	if returnedConfig.Name != ProbeNamePgIdentFileMappings {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgIdentFileMappings)
	}

	if returnedConfig.CollectionIntervalSeconds != 3600 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want 3600", returnedConfig.CollectionIntervalSeconds)
	}

	if returnedConfig.RetentionDays != 365 {
		t.Errorf("GetConfig().RetentionDays = %v, want 365", returnedConfig.RetentionDays)
	}
}
