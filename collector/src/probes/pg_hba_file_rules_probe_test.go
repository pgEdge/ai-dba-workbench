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

func TestPgHbaFileRulesProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	if probe.GetName() != ProbeNamePgHbaFileRules {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgHbaFileRules)
	}
}

func TestPgHbaFileRulesProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	if probe.GetTableName() != ProbeNamePgHbaFileRules {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgHbaFileRules)
	}
}

func TestPgHbaFileRulesProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	if probe.IsDatabaseScoped() {
		t.Error("IsDatabaseScoped() = true, want false (pg_hba_file_rules is server-scoped)")
	}
}

func TestPgHbaFileRulesProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	query := probe.GetQuery()
	if query == "" {
		t.Error("GetQuery() returned empty string")
	}

	// Verify the query includes key columns from pg_hba_file_rules
	expectedColumns := []string{
		"rule_number",
		"file_name",
		"line_number",
		"type",
		"database",
		"user_name",
		"address",
		"netmask",
		"auth_method",
		"options",
		"error",
	}

	for _, column := range expectedColumns {
		if !containsColumn(query, column) {
			t.Errorf("GetQuery() missing expected column: %s", column)
		}
	}
}

func TestPgHbaFileRulesProbe_ComputeMetricsHash(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	// Test with identical metrics
	metrics1 := []map[string]interface{}{
		{
			"rule_number": 1,
			"auth_method": "trust",
			"database":    []string{"all"},
		},
		{
			"rule_number": 2,
			"auth_method": "md5",
			"database":    []string{"postgres"},
		},
	}

	metrics2 := []map[string]interface{}{
		{
			"rule_number": 1,
			"auth_method": "trust",
			"database":    []string{"all"},
		},
		{
			"rule_number": 2,
			"auth_method": "md5",
			"database":    []string{"postgres"},
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
			"rule_number": 1,
			"auth_method": "scram-sha-256", // Different value
			"database":    []string{"all"},
		},
		{
			"rule_number": 2,
			"auth_method": "md5",
			"database":    []string{"postgres"},
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

func TestPgHbaFileRulesProbe_ComputeMetricsHash_EmptyMetrics(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	metrics := []map[string]interface{}{}

	hash, err := probe.computeMetricsHash(metrics)
	if err != nil {
		t.Fatalf("computeMetricsHash(empty) error = %v", err)
	}

	if hash == "" {
		t.Error("computeMetricsHash(empty) returned empty hash")
	}
}

func TestPgHbaFileRulesProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgHbaFileRules,
		Description:               "Test probe",
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
		IsEnabled:                 true,
	}
	probe := NewPgHbaFileRulesProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig != config {
		t.Error("GetConfig() did not return the same config instance")
	}

	if returnedConfig.Name != ProbeNamePgHbaFileRules {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgHbaFileRules)
	}

	if returnedConfig.CollectionIntervalSeconds != 3600 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want 3600", returnedConfig.CollectionIntervalSeconds)
	}

	if returnedConfig.RetentionDays != 365 {
		t.Errorf("GetConfig().RetentionDays = %v, want 365", returnedConfig.RetentionDays)
	}
}
