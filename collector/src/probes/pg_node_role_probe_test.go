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

func TestPgNodeRoleProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	if probe.GetName() != ProbeNamePgNodeRole {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgNodeRole)
	}
}

func TestPgNodeRoleProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	if probe.GetTableName() != ProbeNamePgNodeRole {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgNodeRole)
	}
}

func TestPgNodeRoleProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	if probe.IsDatabaseScoped() != false {
		t.Errorf("IsDatabaseScoped() = %v, want %v", probe.IsDatabaseScoped(), false)
	}
}

func TestPgNodeRoleProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	// pg_node_role uses multiple queries, so GetQuery returns empty string
	query := probe.GetQuery()
	if query != "" {
		t.Errorf("GetQuery() = %v, want empty string", query)
	}
}

func TestPgNodeRoleProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgNodeRole,
		Description:               "Test description",
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	}
	probe := NewPgNodeRoleProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig == nil {
		t.Fatal("GetConfig() returned nil")
	}

	if returnedConfig.Name != ProbeNamePgNodeRole {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgNodeRole)
	}

	if returnedConfig.CollectionIntervalSeconds != 300 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want %v", returnedConfig.CollectionIntervalSeconds, 300)
	}

	if returnedConfig.RetentionDays != 30 {
		t.Errorf("GetConfig().RetentionDays = %v, want %v", returnedConfig.RetentionDays, 30)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_Standalone(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	info := &NodeRoleInfo{
		IsInRecovery:      false,
		HasBinaryStandbys: false,
		PublicationCount:  0,
		SubscriptionCount: 0,
		HasSpock:          false,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleStandalone {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleStandalone)
	}

	if len(flags) != 0 {
		t.Errorf("determineNodeRole() flags = %v, want empty", flags)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_BinaryPrimary(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	info := &NodeRoleInfo{
		IsInRecovery:       false,
		HasBinaryStandbys:  true,
		BinaryStandbyCount: 2,
		PublicationCount:   0,
		SubscriptionCount:  0,
		HasSpock:           false,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleBinaryPrimary {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleBinaryPrimary)
	}

	if !containsString(flags, FlagBinaryPrimary) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagBinaryPrimary)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_BinaryStandby(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	info := &NodeRoleInfo{
		IsInRecovery:       true,
		IsStreamingStandby: true,
		HasBinaryStandbys:  false,
		PublicationCount:   0,
		SubscriptionCount:  0,
		HasSpock:           false,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleBinaryStandby {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleBinaryStandby)
	}

	if !containsString(flags, FlagBinaryStandby) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagBinaryStandby)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_BinaryCascading(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	info := &NodeRoleInfo{
		IsInRecovery:       true,
		IsStreamingStandby: true,
		HasBinaryStandbys:  true,
		BinaryStandbyCount: 1,
		PublicationCount:   0,
		SubscriptionCount:  0,
		HasSpock:           false,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleBinaryCascading {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleBinaryCascading)
	}

	if !containsString(flags, FlagBinaryPrimary) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagBinaryPrimary)
	}
	if !containsString(flags, FlagBinaryStandby) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagBinaryStandby)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_LogicalPublisher(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	// A server with publications AND active logical replication slots (subscribers connected)
	info := &NodeRoleInfo{
		IsInRecovery:          false,
		HasBinaryStandbys:     false,
		PublicationCount:      3,
		SubscriptionCount:     0,
		HasActiveLogicalSlots: true, // Active subscribers are connected
		HasSpock:              false,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleLogicalPublisher {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleLogicalPublisher)
	}

	if !containsString(flags, FlagLogicalPublisher) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagLogicalPublisher)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_LogicalSubscriber(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	info := &NodeRoleInfo{
		IsInRecovery:      false,
		HasBinaryStandbys: false,
		PublicationCount:  0,
		SubscriptionCount: 2,
		HasSpock:          false,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleLogicalSubscriber {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleLogicalSubscriber)
	}

	if !containsString(flags, FlagLogicalSubscriber) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagLogicalSubscriber)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_LogicalBidirectional(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	// A server with both publications and subscriptions, and active logical slots
	info := &NodeRoleInfo{
		IsInRecovery:          false,
		HasBinaryStandbys:     false,
		PublicationCount:      2,
		SubscriptionCount:     2,
		HasActiveLogicalSlots: true, // Active subscribers are connected
		HasSpock:              false,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleLogicalBidirectional {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleLogicalBidirectional)
	}

	if !containsString(flags, FlagLogicalPublisher) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagLogicalPublisher)
	}
	if !containsString(flags, FlagLogicalSubscriber) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagLogicalSubscriber)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_SpockNode(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	nodeName := "node1"
	info := &NodeRoleInfo{
		IsInRecovery:      false,
		HasBinaryStandbys: false,
		PublicationCount:  0,
		SubscriptionCount: 0,
		HasSpock:          true,
		SpockNodeName:     &nodeName,
	}

	role, flags := probe.determineNodeRole(info)

	if role != RoleSpockNode {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleSpockNode)
	}

	if !containsString(flags, FlagSpockNode) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagSpockNode)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_SpockStandby(t *testing.T) {
	// A streaming standby of a Spock node is NOT a Spock cluster member.
	// It has Spock extension and tables (replicated from primary) but is just
	// a binary standby for HA purposes. Only the primary is a true Spock node.
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	nodeName := "node1"
	info := &NodeRoleInfo{
		IsInRecovery:       true,
		IsStreamingStandby: true,
		HasBinaryStandbys:  false,
		PublicationCount:   0,
		SubscriptionCount:  0,
		HasSpock:           true,
		SpockNodeName:      &nodeName, // Has Spock tables from replication, but not a true Spock member
	}

	role, flags := probe.determineNodeRole(info)

	// Should be binary_standby, NOT spock_standby
	if role != RoleBinaryStandby {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleBinaryStandby)
	}

	// Should NOT have spock_node flag since it's in recovery
	if containsString(flags, FlagSpockNode) {
		t.Errorf("determineNodeRole() flags should NOT contain %v for a standby", FlagSpockNode)
	}
	if !containsString(flags, FlagBinaryStandby) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagBinaryStandby)
	}
}

func TestPgNodeRoleProbe_DetermineNodeRole_CombinedRoles(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	// A primary with standbys AND logical publications with active subscribers
	info := &NodeRoleInfo{
		IsInRecovery:          false,
		HasBinaryStandbys:     true,
		BinaryStandbyCount:    2,
		PublicationCount:      3,
		SubscriptionCount:     0,
		HasActiveLogicalSlots: true, // Has active subscribers connected
		HasSpock:              false,
	}

	role, flags := probe.determineNodeRole(info)

	// Primary role should be binary_primary (more specific)
	if role != RoleBinaryPrimary {
		t.Errorf("determineNodeRole() role = %v, want %v", role, RoleBinaryPrimary)
	}

	// Flags should include both binary_primary and logical_publisher
	if !containsString(flags, FlagBinaryPrimary) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagBinaryPrimary)
	}
	if !containsString(flags, FlagLogicalPublisher) {
		t.Errorf("determineNodeRole() flags should contain %v", FlagLogicalPublisher)
	}
}

func TestPgNodeRoleProbe_InfoToMap(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgNodeRole,
	}
	probe := NewPgNodeRoleProbe(config)

	timelineID := 1
	info := &NodeRoleInfo{
		IsInRecovery:            false,
		TimelineID:              &timelineID,
		HasBinaryStandbys:       true,
		BinaryStandbyCount:      2,
		IsStreamingStandby:      false,
		PublicationCount:        3,
		SubscriptionCount:       1,
		ActiveSubscriptionCount: 1,
		HasSpock:                false,
		PrimaryRole:             RoleBinaryPrimary,
		RoleFlags:               []string{FlagBinaryPrimary, FlagLogicalPublisher, FlagLogicalSubscriber},
		RoleDetails:             make(map[string]interface{}),
	}

	result := probe.infoToMap(info)

	if result["is_in_recovery"] != false {
		t.Errorf("infoToMap() is_in_recovery = %v, want false", result["is_in_recovery"])
	}
	if result["has_binary_standbys"] != true {
		t.Errorf("infoToMap() has_binary_standbys = %v, want true", result["has_binary_standbys"])
	}
	if result["binary_standby_count"] != 2 {
		t.Errorf("infoToMap() binary_standby_count = %v, want 2", result["binary_standby_count"])
	}
	if result["primary_role"] != RoleBinaryPrimary {
		t.Errorf("infoToMap() primary_role = %v, want %v", result["primary_role"], RoleBinaryPrimary)
	}
}

// Helper function to check if a slice contains a string
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
