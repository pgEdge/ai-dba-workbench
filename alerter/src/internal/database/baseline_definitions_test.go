/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"testing"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// TestRegistryMatchesSharedBaselineDefinitions pins the alerter registry
// to pkg/anomaly, which is the one home of every historical query: each
// registry metric that SupportsBaselines has a shared definition with
// the same SQL, scan kind and probe, and every shared definition names a
// registry metric, so none is silently left out of baselining.
func TestRegistryMatchesSharedBaselineDefinitions(t *testing.T) {
	for name, cfg := range metricRegistry {
		def, ok := anomaly.Metric(name)
		if !SupportsBaselines(name) {
			if ok {
				t.Errorf("%s has a shared definition but the registry does not baseline it", name)
			}
			continue
		}
		if !ok {
			t.Errorf("%s is baselined but has no pkg/anomaly definition", name)
			continue
		}
		if cfg.historicalSQL != def.HistoricalSQL {
			t.Errorf("%s historicalSQL differs from the pkg/anomaly definition", name)
		}
		if cfg.historicalScan != def.HistoricalScan {
			t.Errorf("%s historicalScan = %d, pkg/anomaly has %d", name, cfg.historicalScan, def.HistoricalScan)
		}
		if cfg.probeName != def.ProbeName {
			t.Errorf("%s probeName = %q, pkg/anomaly has %q", name, cfg.probeName, def.ProbeName)
		}
	}

	defs := anomaly.BaselineMetrics()
	supported := BaselineSupportedMetrics()
	if len(defs) != len(supported) {
		t.Fatalf("pkg/anomaly defines %d baselined metrics, the registry baselines %d", len(defs), len(supported))
	}
	for i, def := range defs {
		if def.Name != supported[i] {
			t.Errorf("baselined metric [%d] = %q in pkg/anomaly, %q in the registry", i, def.Name, supported[i])
		}
	}
}

// TestSharedSQLFragments checks that the fragments the latest queries
// embed are the pkg/anomaly ones the historical queries embed, so the
// two cannot drift apart.
func TestSharedSQLFragments(t *testing.T) {
	if cpuBusyPercentExpr != anomaly.CPUBusyPercentExpr {
		t.Error("cpuBusyPercentExpr differs from anomaly.CPUBusyPercentExpr")
	}
	if pseudoFilesystemExclusion != anomaly.PseudoFilesystemExclusion {
		t.Error("pseudoFilesystemExclusion differs from anomaly.PseudoFilesystemExclusion")
	}
}
