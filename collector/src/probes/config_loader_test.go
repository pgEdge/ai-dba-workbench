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
	"strings"
	"testing"
)

// allProbeNameConstants returns all probe name constants defined in constants.go.
// This list must be kept in sync with constants.go to ensure completeness testing works.
func allProbeNameConstants() []string {
	return []string{
		// Server-wide probes
		ProbeNamePgStatActivity,
		ProbeNamePgStatReplication,
		ProbeNamePgReplicationSlots,
		ProbeNamePgStatRecoveryPrefetch,
		ProbeNamePgStatSubscription,
		ProbeNamePgStatConnectionSecurity,
		ProbeNamePgStatIO,
		ProbeNamePgStatCheckpointer,
		ProbeNamePgStatWAL,
		ProbeNamePgSettings,
		ProbeNamePgHbaFileRules,
		ProbeNamePgIdentFileMappings,
		ProbeNamePgServerInfo,
		ProbeNamePgNodeRole,
		ProbeNamePgConnectivity,
		ProbeNamePgDatabase,

		// Database-scoped probes
		ProbeNamePgStatDatabase,
		ProbeNamePgStatDatabaseConflicts,
		ProbeNamePgStatAllTables,
		ProbeNamePgStatAllIndexes,
		ProbeNamePgStatioAllSequences,
		ProbeNamePgStatUserFunctions,
		ProbeNamePgStatStatements,
		ProbeNamePgExtension,

		// System stats probes
		ProbeNamePgSysOsInfo,
		ProbeNamePgSysCPUInfo,
		ProbeNamePgSysCPUUsageInfo,
		ProbeNamePgSysMemoryInfo,
		ProbeNamePgSysIoAnalysisInfo,
		ProbeNamePgSysDiskInfo,
		ProbeNamePgSysLoadAvgInfo,
		ProbeNamePgSysProcessInfo,
		ProbeNamePgSysNetworkInfo,
		ProbeNamePgSysCPUMemoryByProcess,
	}
}

func TestGetDefaultInterval_AllProbesHaveExplicitInterval(t *testing.T) {
	// This test ensures every probe name constant has an explicit entry
	// in the getDefaultInterval map by verifying against a complete
	// expected-intervals table. This catches both missing entries and
	// incorrect values, even when the intended interval equals the fallback.
	expectedIntervals := map[string]int{
		ProbeNamePgStatActivity:           60,
		ProbeNamePgStatReplication:        30,
		ProbeNamePgReplicationSlots:       300,
		ProbeNamePgStatRecoveryPrefetch:   600,
		ProbeNamePgStatSubscription:       300,
		ProbeNamePgStatConnectionSecurity: 300,
		ProbeNamePgStatIO:                 900,
		ProbeNamePgStatCheckpointer:       600,
		ProbeNamePgStatWAL:                600,
		ProbeNamePgSettings:               3600,
		ProbeNamePgHbaFileRules:           3600,
		ProbeNamePgIdentFileMappings:      3600,
		ProbeNamePgServerInfo:             3600,
		ProbeNamePgNodeRole:               300,
		ProbeNamePgConnectivity:           30,
		ProbeNamePgDatabase:               300,
		ProbeNamePgStatDatabase:           300,
		ProbeNamePgStatDatabaseConflicts:  300,
		ProbeNamePgStatAllTables:          300,
		ProbeNamePgStatAllIndexes:         300,
		ProbeNamePgStatioAllSequences:     300,
		ProbeNamePgStatUserFunctions:      300,
		ProbeNamePgStatStatements:         300,
		ProbeNamePgExtension:              3600,
		ProbeNamePgSysOsInfo:              3600,
		ProbeNamePgSysCPUInfo:             3600,
		ProbeNamePgSysCPUUsageInfo:        60,
		ProbeNamePgSysMemoryInfo:          300,
		ProbeNamePgSysIoAnalysisInfo:      300,
		ProbeNamePgSysDiskInfo:            300,
		ProbeNamePgSysLoadAvgInfo:         60,
		ProbeNamePgSysProcessInfo:         300,
		ProbeNamePgSysNetworkInfo:         300,
		ProbeNamePgSysCPUMemoryByProcess:  300,
	}

	allProbes := allProbeNameConstants()
	if len(expectedIntervals) != len(allProbes) {
		t.Fatalf("expectedIntervals has %d entries but allProbeNameConstants has %d; keep them in sync",
			len(expectedIntervals), len(allProbes))
	}

	for _, probeName := range allProbes {
		expected, ok := expectedIntervals[probeName]
		if !ok {
			t.Errorf("Probe %q missing from expectedIntervals map", probeName)
			continue
		}
		got := getDefaultInterval(probeName)
		if got != expected {
			t.Errorf("Probe %q: expected interval %d, got %d", probeName, expected, got)
		}
	}
}

func TestGetDefaultInterval_UnknownProbeReturnsFallback(t *testing.T) {
	// Unknown probes should return the fallback default of 300 seconds
	interval := getDefaultInterval("unknown_probe_that_does_not_exist")
	if interval != 300 {
		t.Errorf("Expected fallback interval 300 for unknown probe, got %d", interval)
	}
}

func TestGetDefaultInterval_SpecificValues(t *testing.T) {
	// Test specific interval values for key probes
	tests := []struct {
		probeName        string
		expectedInterval int
	}{
		// Fast-changing probes (30-60 second intervals)
		{ProbeNamePgStatReplication, 30},
		{ProbeNamePgConnectivity, 30},
		{ProbeNamePgStatActivity, 60},
		{ProbeNamePgSysCPUUsageInfo, 60},
		{ProbeNamePgSysLoadAvgInfo, 60},

		// Medium-frequency probes (300 second / 5 minute intervals)
		{ProbeNamePgReplicationSlots, 300},
		{ProbeNamePgStatSubscription, 300},
		{ProbeNamePgNodeRole, 300},
		{ProbeNamePgDatabase, 300},
		{ProbeNamePgStatDatabase, 300},
		{ProbeNamePgStatAllTables, 300},
		{ProbeNamePgStatStatements, 300},

		// Slow-changing probes (600 second / 10 minute intervals)
		{ProbeNamePgStatRecoveryPrefetch, 600},
		{ProbeNamePgStatCheckpointer, 600},
		{ProbeNamePgStatWAL, 600},

		// Very slow probes (900 second / 15 minute intervals)
		{ProbeNamePgStatIO, 900},

		// Configuration probes (3600 second / hourly intervals)
		{ProbeNamePgSettings, 3600},
		{ProbeNamePgHbaFileRules, 3600},
		{ProbeNamePgIdentFileMappings, 3600},
		{ProbeNamePgServerInfo, 3600},
		{ProbeNamePgExtension, 3600},
		{ProbeNamePgSysOsInfo, 3600},
		{ProbeNamePgSysCPUInfo, 3600},
	}

	for _, tc := range tests {
		t.Run(tc.probeName, func(t *testing.T) {
			interval := getDefaultInterval(tc.probeName)
			if interval != tc.expectedInterval {
				t.Errorf("Probe %q: expected interval %d, got %d",
					tc.probeName, tc.expectedInterval, interval)
			}
		})
	}
}

func TestAllProbeNameConstants_NoDuplicates(t *testing.T) {
	// Ensure there are no duplicate probe names in the list
	allProbes := allProbeNameConstants()
	seen := make(map[string]bool)

	for _, probeName := range allProbes {
		if seen[probeName] {
			t.Errorf("Duplicate probe name in allProbeNameConstants: %q", probeName)
		}
		seen[probeName] = true
	}
}

func TestAllProbeNameConstants_NoEmptyStrings(t *testing.T) {
	// Ensure no probe names are empty strings
	allProbes := allProbeNameConstants()

	for i, probeName := range allProbes {
		if probeName == "" {
			t.Errorf("Empty probe name at index %d in allProbeNameConstants", i)
		}
	}
}

// intPtr returns a pointer to the given int. It is a small helper used
// by the resolveProbeConfigDefaults tests below to populate the
// parent ProbeConfig's ConnectionID field, which is *int.
func intPtr(v int) *int {
	return &v
}

// TestLoadProbeConfigsQuery_FiltersByScope is a regression test for
// issue #151. Cluster- and group-scoped probe_configs rows have
// connection_id IS NULL just like global rows; without an explicit
// scope filter they collapse into the connection_id = 0 bucket and
// get applied as if they were global defaults. The query must
// restrict the result set to scope IN ('global', 'server').
//
// This test pins the SQL string. Removing the scope filter (the
// pre-fix behavior) makes the test fail.
func TestLoadProbeConfigsQuery_FiltersByScope(t *testing.T) {
	if !strings.Contains(loadProbeConfigsQuery, "scope IN ('global', 'server')") {
		t.Errorf("loadProbeConfigsQuery must restrict scope to global/server\n"+
			"to prevent cluster- and group-scoped rows (which also have\n"+
			"connection_id IS NULL) from collapsing into the global bucket.\n"+
			"Got query:\n%s", loadProbeConfigsQuery)
	}

	// Sanity: the query must still require the row to be enabled and
	// must select from probe_configs. These are not the bug fix but
	// they pin the contract the test is asserting.
	if !strings.Contains(loadProbeConfigsQuery, "is_enabled = TRUE") {
		t.Errorf("loadProbeConfigsQuery must filter on is_enabled = TRUE; got:\n%s",
			loadProbeConfigsQuery)
	}
	if !strings.Contains(loadProbeConfigsQuery, "FROM probe_configs") {
		t.Errorf("loadProbeConfigsQuery must select FROM probe_configs; got:\n%s",
			loadProbeConfigsQuery)
	}
}

// TestLoadProbeConfigsQuery_OrdersByConnectionThenName pins the
// ORDER BY clause that callers depend on: rows are grouped per
// connection_id (with NULL coalesced to 0 so global defaults sort
// first) and then alphabetised by probe name. Changing the ordering
// would silently reshuffle the per-connection slices in
// LoadProbeConfigs's returned map.
func TestLoadProbeConfigsQuery_OrdersByConnectionThenName(t *testing.T) {
	if !strings.Contains(loadProbeConfigsQuery, "ORDER BY COALESCE(connection_id, 0), name") {
		t.Errorf("loadProbeConfigsQuery must order by COALESCE(connection_id, 0), name; got:\n%s",
			loadProbeConfigsQuery)
	}
}

// TestResolveProbeConfigDefaults_InheritsDisabledParent is the core
// regression test for the second half of issue #151. When a parent
// (cluster, group, or global) probe_config has is_enabled = FALSE,
// the materialized server-level row must inherit that disabled flag
// rather than being silently re-enabled. The pre-fix INSERT
// hard-coded VALUES (..., TRUE, ...), which is exactly what this
// test guards against.
func TestResolveProbeConfigDefaults_InheritsDisabledParent(t *testing.T) {
	parent := &ProbeConfig{
		ID:                        42,
		Name:                      ProbeNamePgStatActivity,
		Description:               "cluster override - paused for maintenance",
		CollectionIntervalSeconds: 120,
		RetentionDays:             7,
		IsEnabled:                 false, // parent explicitly disabled
		ConnectionID:              nil,   // cluster/group/global rows have NULL
	}

	interval, retention, description, isEnabled := resolveProbeConfigDefaults(
		ProbeNamePgStatActivity, parent, true)

	if interval != 120 {
		t.Errorf("interval: got %d, want 120 (inherited from parent)", interval)
	}
	if retention != 7 {
		t.Errorf("retention: got %d, want 7 (inherited from parent)", retention)
	}
	if description != "cluster override - paused for maintenance" {
		t.Errorf("description: got %q, want parent's description", description)
	}
	if isEnabled {
		t.Errorf("isEnabled: got true, want false (must inherit parent's disabled flag)")
	}
}

// TestResolveProbeConfigDefaults_InheritsEnabledParent verifies that
// when a parent config is enabled, the helper inherits all four
// fields including IsEnabled = true. This complements the disabled
// case to confirm the helper truly forwards the parent's flag rather
// than always returning a fixed value.
func TestResolveProbeConfigDefaults_InheritsEnabledParent(t *testing.T) {
	parent := &ProbeConfig{
		ID:                        7,
		Name:                      ProbeNamePgStatReplication,
		Description:               "global default for replication probe",
		CollectionIntervalSeconds: 45,
		RetentionDays:             30,
		IsEnabled:                 true,
		ConnectionID:              intPtr(99), // server-scoped parent
	}

	interval, retention, description, isEnabled := resolveProbeConfigDefaults(
		ProbeNamePgStatReplication, parent, true)

	if interval != 45 {
		t.Errorf("interval: got %d, want 45", interval)
	}
	if retention != 30 {
		t.Errorf("retention: got %d, want 30", retention)
	}
	if description != "global default for replication probe" {
		t.Errorf("description: got %q, want parent's description", description)
	}
	if !isEnabled {
		t.Errorf("isEnabled: got false, want true (parent was enabled)")
	}
}

// TestResolveProbeConfigDefaults_NoParentUsesHardcodedDefaults
// covers the fallback branch: when no parent config is found, the
// helper must synthesise sensible defaults including isEnabled=true,
// the per-probe default interval, 28 days of retention, and a
// generated description. found=false is the discriminator.
func TestResolveProbeConfigDefaults_NoParentUsesHardcodedDefaults(t *testing.T) {
	interval, retention, description, isEnabled := resolveProbeConfigDefaults(
		ProbeNamePgStatActivity, nil, false)

	wantInterval := getDefaultInterval(ProbeNamePgStatActivity)
	if interval != wantInterval {
		t.Errorf("interval: got %d, want %d (probe default)", interval, wantInterval)
	}
	if retention != 28 {
		t.Errorf("retention: got %d, want 28 (hardcoded default)", retention)
	}
	wantDescription := "Configuration for " + ProbeNamePgStatActivity + " probe"
	if description != wantDescription {
		t.Errorf("description: got %q, want %q", description, wantDescription)
	}
	if !isEnabled {
		t.Errorf("isEnabled: got false, want true (default for synthesised configs)")
	}
}

// TestResolveProbeConfigDefaults_NoParentUnknownProbe covers the
// fallback branch for a probe name that has no entry in
// getDefaultInterval. The fallback interval is 300 seconds (5
// minutes) and retention/isEnabled remain at their hardcoded
// defaults.
func TestResolveProbeConfigDefaults_NoParentUnknownProbe(t *testing.T) {
	interval, retention, description, isEnabled := resolveProbeConfigDefaults(
		"some_brand_new_probe_name", nil, false)

	if interval != 300 {
		t.Errorf("interval: got %d, want 300 (fallback default for unknown probes)",
			interval)
	}
	if retention != 28 {
		t.Errorf("retention: got %d, want 28", retention)
	}
	if description != "Configuration for some_brand_new_probe_name probe" {
		t.Errorf("description: got %q", description)
	}
	if !isEnabled {
		t.Errorf("isEnabled: got false, want true")
	}
}

// TestResolveProbeConfigDefaults_FoundFalseIgnoresParent guards the
// helper's contract: when found is false the parentConfig argument
// is ignored, even if non-nil. Without this guarantee callers in
// EnsureProbeConfig that pass &parentConfig (the zero value) when
// no parent matched would accidentally inherit zero-valued fields.
func TestResolveProbeConfigDefaults_FoundFalseIgnoresParent(t *testing.T) {
	// A non-nil but zero-valued parent must NOT leak into the result.
	zeroParent := &ProbeConfig{}

	interval, retention, description, isEnabled := resolveProbeConfigDefaults(
		ProbeNamePgStatActivity, zeroParent, false)

	if interval != getDefaultInterval(ProbeNamePgStatActivity) {
		t.Errorf("interval should come from getDefaultInterval, not the zero parent; got %d",
			interval)
	}
	if retention != 28 {
		t.Errorf("retention should be 28 (default), not parent's zero; got %d", retention)
	}
	if description == "" {
		t.Errorf("description should be the synthesised default, not the parent's empty string")
	}
	if !isEnabled {
		t.Errorf("isEnabled should default to true when no parent is matched")
	}
}

// TestResolveProbeConfigDefaults_FoundTrueButNilParent guards the
// nil-pointer branch in the conditional: if a caller incorrectly
// passes (true, nil) the helper must fall through to the defaults
// rather than dereferencing nil and panicking.
func TestResolveProbeConfigDefaults_FoundTrueButNilParent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("resolveProbeConfigDefaults panicked on nil parent: %v", r)
		}
	}()

	interval, retention, _, isEnabled := resolveProbeConfigDefaults(
		ProbeNamePgStatActivity, nil, true)

	if interval != getDefaultInterval(ProbeNamePgStatActivity) {
		t.Errorf("interval: got %d, want probe default", interval)
	}
	if retention != 28 {
		t.Errorf("retention: got %d, want 28", retention)
	}
	if !isEnabled {
		t.Errorf("isEnabled: got false, want true (default fallback)")
	}
}
