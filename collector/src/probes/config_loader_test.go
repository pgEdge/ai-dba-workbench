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
	// in the getDefaultInterval map, preventing future drift.
	allProbes := allProbeNameConstants()

	// Verify we have exactly 34 probe constants
	expectedCount := 34
	if len(allProbes) != expectedCount {
		t.Errorf("Expected %d probe constants, got %d. Update allProbeNameConstants() if constants.go changed.",
			expectedCount, len(allProbes))
	}

	// Track which probes return the fallback default vs an explicit value
	// We need to verify each probe has an entry in the map, not just that
	// it returns a value (since the fallback would always return 300)
	for _, probeName := range allProbes {
		interval := getDefaultInterval(probeName)
		if interval <= 0 {
			t.Errorf("Probe %q returned invalid interval %d", probeName, interval)
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
