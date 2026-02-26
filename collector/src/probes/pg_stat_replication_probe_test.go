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

func TestPgStatReplicationProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatReplication,
	}
	probe := NewPgStatReplicationProbe(config)

	if probe.GetName() != ProbeNamePgStatReplication {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgStatReplication)
	}
}

func TestPgStatReplicationProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatReplication,
	}
	probe := NewPgStatReplicationProbe(config)

	if probe.GetTableName() != ProbeNamePgStatReplication {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgStatReplication)
	}
}

func TestPgStatReplicationProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatReplication,
	}
	probe := NewPgStatReplicationProbe(config)

	if probe.IsDatabaseScoped() != false {
		t.Errorf("IsDatabaseScoped() = %v, want %v", probe.IsDatabaseScoped(), false)
	}
}

func TestPgStatReplicationProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatReplication,
	}
	probe := NewPgStatReplicationProbe(config)

	// pg_stat_replication uses multiple queries, so GetQuery returns empty string
	query := probe.GetQuery()
	if query != "" {
		t.Errorf("GetQuery() = %v, want empty string", query)
	}
}

func TestPgStatReplicationProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgStatReplication,
		Description:               "Test description",
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	}
	probe := NewPgStatReplicationProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig == nil {
		t.Fatal("GetConfig() returned nil")
	}

	if returnedConfig.Name != ProbeNamePgStatReplication {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgStatReplication)
	}

	if returnedConfig.CollectionIntervalSeconds != 300 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want %v", returnedConfig.CollectionIntervalSeconds, 300)
	}

	if returnedConfig.RetentionDays != 30 {
		t.Errorf("GetConfig().RetentionDays = %v, want %v", returnedConfig.RetentionDays, 30)
	}
}
