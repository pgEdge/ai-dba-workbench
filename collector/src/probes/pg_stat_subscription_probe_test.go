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

func TestPgStatSubscriptionProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatSubscription,
	}
	probe := NewPgStatSubscriptionProbe(config)

	if probe.GetName() != ProbeNamePgStatSubscription {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgStatSubscription)
	}
}

func TestPgStatSubscriptionProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatSubscription,
	}
	probe := NewPgStatSubscriptionProbe(config)

	if probe.GetTableName() != ProbeNamePgStatSubscription {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgStatSubscription)
	}
}

func TestPgStatSubscriptionProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatSubscription,
	}
	probe := NewPgStatSubscriptionProbe(config)

	if probe.IsDatabaseScoped() != false {
		t.Errorf("IsDatabaseScoped() = %v, want %v", probe.IsDatabaseScoped(), false)
	}
}

func TestPgStatSubscriptionProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgStatSubscription,
	}
	probe := NewPgStatSubscriptionProbe(config)

	// pg_stat_subscription uses dynamic queries, so GetQuery returns empty string
	query := probe.GetQuery()
	if query != "" {
		t.Errorf("GetQuery() = %v, want empty string", query)
	}
}

func TestPgStatSubscriptionProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgStatSubscription,
		Description:               "Test description",
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	}
	probe := NewPgStatSubscriptionProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig == nil {
		t.Fatal("GetConfig() returned nil")
	}

	if returnedConfig.Name != ProbeNamePgStatSubscription {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgStatSubscription)
	}

	if returnedConfig.CollectionIntervalSeconds != 300 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want %v", returnedConfig.CollectionIntervalSeconds, 300)
	}

	if returnedConfig.RetentionDays != 30 {
		t.Errorf("GetConfig().RetentionDays = %v, want %v", returnedConfig.RetentionDays, 30)
	}
}
