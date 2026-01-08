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

func TestPgServerInfoProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgServerInfo,
	}
	probe := NewPgServerInfoProbe(config)

	if probe.GetName() != ProbeNamePgServerInfo {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgServerInfo)
	}
}

func TestPgServerInfoProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgServerInfo,
	}
	probe := NewPgServerInfoProbe(config)

	if probe.GetTableName() != ProbeNamePgServerInfo {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgServerInfo)
	}
}

func TestPgServerInfoProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgServerInfo,
	}
	probe := NewPgServerInfoProbe(config)

	if probe.IsDatabaseScoped() != false {
		t.Errorf("IsDatabaseScoped() = %v, want %v", probe.IsDatabaseScoped(), false)
	}
}

func TestPgServerInfoProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgServerInfo,
	}
	probe := NewPgServerInfoProbe(config)

	query := probe.GetQuery()
	if query == "" {
		t.Error("GetQuery() returned empty string")
	}

	// Check that the query contains expected elements
	expectedElements := []string{
		"server_version",
		"server_version_num",
		"system_identifier",
		"cluster_name",
		"data_directory",
		"max_connections",
		"max_wal_senders",
		"max_replication_slots",
		"installed_extensions",
	}

	for _, element := range expectedElements {
		if !contains(query, element) {
			t.Errorf("GetQuery() does not contain expected element: %s", element)
		}
	}
}

func TestPgServerInfoProbe_ComputeMetricsHash(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgServerInfo,
	}
	probe := NewPgServerInfoProbe(config)

	metrics := []map[string]interface{}{
		{
			"server_version":        "17.0",
			"server_version_num":    170000,
			"system_identifier":     int64(1234567890),
			"cluster_name":          "test-cluster",
			"data_directory":        "/var/lib/postgresql/17/main",
			"max_connections":       100,
			"max_wal_senders":       10,
			"max_replication_slots": 10,
			"installed_extensions":  []string{"plpgsql", "pg_stat_statements"},
		},
	}

	hash1, err := probe.computeMetricsHash(metrics)
	if err != nil {
		t.Fatalf("computeMetricsHash() returned error: %v", err)
	}

	if hash1 == "" {
		t.Error("computeMetricsHash() returned empty hash")
	}

	// Computing the hash again should return the same value
	hash2, err := probe.computeMetricsHash(metrics)
	if err != nil {
		t.Fatalf("computeMetricsHash() returned error on second call: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("computeMetricsHash() returned different hashes for same input: %s != %s", hash1, hash2)
	}

	// Different metrics should produce different hash
	metricsModified := []map[string]interface{}{
		{
			"server_version":        "17.1",
			"server_version_num":    170100,
			"system_identifier":     int64(1234567890),
			"cluster_name":          "test-cluster",
			"data_directory":        "/var/lib/postgresql/17/main",
			"max_connections":       100,
			"max_wal_senders":       10,
			"max_replication_slots": 10,
			"installed_extensions":  []string{"plpgsql", "pg_stat_statements"},
		},
	}

	hash3, err := probe.computeMetricsHash(metricsModified)
	if err != nil {
		t.Fatalf("computeMetricsHash() returned error for modified metrics: %v", err)
	}

	if hash1 == hash3 {
		t.Error("computeMetricsHash() returned same hash for different input")
	}
}

func TestPgServerInfoProbe_ComputeMetricsHash_EmptyMetrics(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgServerInfo,
	}
	probe := NewPgServerInfoProbe(config)

	metrics := []map[string]interface{}{}

	hash, err := probe.computeMetricsHash(metrics)
	if err != nil {
		t.Fatalf("computeMetricsHash() returned error for empty metrics: %v", err)
	}

	if hash == "" {
		t.Error("computeMetricsHash() returned empty hash for empty metrics")
	}
}

func TestPgServerInfoProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgServerInfo,
		Description:               "Test description",
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
		IsEnabled:                 true,
	}
	probe := NewPgServerInfoProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig == nil {
		t.Fatal("GetConfig() returned nil")
	}

	if returnedConfig.Name != ProbeNamePgServerInfo {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgServerInfo)
	}

	if returnedConfig.CollectionIntervalSeconds != 3600 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want %v", returnedConfig.CollectionIntervalSeconds, 3600)
	}

	if returnedConfig.RetentionDays != 365 {
		t.Errorf("GetConfig().RetentionDays = %v, want %v", returnedConfig.RetentionDays, 365)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
