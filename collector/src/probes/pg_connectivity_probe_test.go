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

func TestPgConnectivityProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgConnectivity,
	}
	probe := NewPgConnectivityProbe(config)

	if probe.GetName() != ProbeNamePgConnectivity {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgConnectivity)
	}
}

func TestPgConnectivityProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgConnectivity,
	}
	probe := NewPgConnectivityProbe(config)

	if probe.IsDatabaseScoped() != false {
		t.Errorf("IsDatabaseScoped() = %v, want %v", probe.IsDatabaseScoped(), false)
	}
}

func TestPgConnectivityProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgConnectivity,
	}
	probe := NewPgConnectivityProbe(config)

	if probe.GetTableName() != ProbeNamePgConnectivity {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgConnectivity)
	}
}

func TestPgConnectivityProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgConnectivity,
	}
	probe := NewPgConnectivityProbe(config)

	query := probe.GetQuery()
	if query != "" {
		t.Errorf("GetQuery() = %v, want empty string", query)
	}
}

func TestPgConnectivityProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgConnectivity,
		Description:               "Monitors database connectivity and response time",
		CollectionIntervalSeconds: 30,
		RetentionDays:             7,
		IsEnabled:                 true,
	}
	probe := NewPgConnectivityProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig == nil {
		t.Fatal("GetConfig() returned nil")
	}

	if returnedConfig.Name != ProbeNamePgConnectivity {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgConnectivity)
	}

	if returnedConfig.CollectionIntervalSeconds != 30 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want %v", returnedConfig.CollectionIntervalSeconds, 30)
	}

	if returnedConfig.RetentionDays != 7 {
		t.Errorf("GetConfig().RetentionDays = %v, want %v", returnedConfig.RetentionDays, 7)
	}
}
