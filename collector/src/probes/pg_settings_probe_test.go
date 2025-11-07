/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"testing"
)

func TestPgSettingsProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgSettings,
	}
	probe := NewPgSettingsProbe(config)

	if probe.GetName() != ProbeNamePgSettings {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgSettings)
	}
}

func TestPgSettingsProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgSettings,
	}
	probe := NewPgSettingsProbe(config)

	if probe.GetTableName() != ProbeNamePgSettings {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgSettings)
	}
}

func TestPgSettingsProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgSettings,
	}
	probe := NewPgSettingsProbe(config)

	if probe.IsDatabaseScoped() {
		t.Error("IsDatabaseScoped() = true, want false (pg_settings is server-scoped)")
	}
}

func TestPgSettingsProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgSettings,
	}
	probe := NewPgSettingsProbe(config)

	query := probe.GetQuery()
	if query == "" {
		t.Error("GetQuery() returned empty string")
	}

	// Verify the query includes key columns from pg_settings
	expectedColumns := []string{
		"name",
		"setting",
		"unit",
		"category",
		"context",
		"vartype",
		"source",
		"min_val",
		"max_val",
		"enumvals",
		"boot_val",
		"reset_val",
		"sourcefile",
		"sourceline",
		"pending_restart",
	}

	for _, column := range expectedColumns {
		if !containsColumn(query, column) {
			t.Errorf("GetQuery() missing expected column: %s", column)
		}
	}
}

func TestPgSettingsProbe_ComputeMetricsHash(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgSettings,
	}
	probe := NewPgSettingsProbe(config)

	// Test with identical metrics
	metrics1 := []map[string]interface{}{
		{
			"name":    "max_connections",
			"setting": "100",
			"unit":    nil,
		},
		{
			"name":    "shared_buffers",
			"setting": "128MB",
			"unit":    "8kB",
		},
	}

	metrics2 := []map[string]interface{}{
		{
			"name":    "max_connections",
			"setting": "100",
			"unit":    nil,
		},
		{
			"name":    "shared_buffers",
			"setting": "128MB",
			"unit":    "8kB",
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
			"name":    "max_connections",
			"setting": "200", // Different value
			"unit":    nil,
		},
		{
			"name":    "shared_buffers",
			"setting": "128MB",
			"unit":    "8kB",
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

func TestPgSettingsProbe_ComputeMetricsHash_EmptyMetrics(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgSettings,
	}
	probe := NewPgSettingsProbe(config)

	metrics := []map[string]interface{}{}

	hash, err := probe.computeMetricsHash(metrics)
	if err != nil {
		t.Fatalf("computeMetricsHash(empty) error = %v", err)
	}

	if hash == "" {
		t.Error("computeMetricsHash(empty) returned empty hash")
	}
}

func TestPgSettingsProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgSettings,
		Description:               "Test probe",
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
		IsEnabled:                 true,
	}
	probe := NewPgSettingsProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig != config {
		t.Error("GetConfig() did not return the same config instance")
	}

	if returnedConfig.Name != ProbeNamePgSettings {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgSettings)
	}

	if returnedConfig.CollectionIntervalSeconds != 3600 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want 3600", returnedConfig.CollectionIntervalSeconds)
	}

	if returnedConfig.RetentionDays != 365 {
		t.Errorf("GetConfig().RetentionDays = %v, want 365", returnedConfig.RetentionDays)
	}
}

// Helper function to check if a query contains a column name
func containsColumn(query string, column string) bool {
	// Check if the column name appears in the query
	// Use strings package for the check
	for i := 0; i < len(query)-len(column); i++ {
		if query[i:i+len(column)] == column {
			return true
		}
	}
	return false
}
