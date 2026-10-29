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

// TestWithBaselineQueries checks that withBaselineQueries copies the
// shared query onto a baselined metric's entry, leaves an entry without
// a shared definition unbaselined, and skips a shared definition that
// has no entry rather than adding one.
func TestWithBaselineQueries(t *testing.T) {
	defs := anomaly.BaselineMetrics()
	if len(defs) < 2 {
		t.Fatalf("pkg/anomaly defines %d baselined metrics, want at least 2", len(defs))
	}
	kept, missing := defs[0], defs[1]
	const plain = "test.not_baselined"

	got := withBaselineQueries(map[string]metricQueryConfig{
		kept.Name: {probeName: kept.ProbeName},
		plain:     {probeName: "test"},
	})

	if len(got) != 2 {
		t.Errorf("registry has %d entries, want 2", len(got))
	}
	if cfg := got[kept.Name]; cfg.historicalSQL != kept.HistoricalSQL ||
		cfg.historicalScan != kept.HistoricalScan || cfg.probeName != kept.ProbeName {
		t.Errorf("%s = %+v, want the shared query copied in", kept.Name, cfg)
	}
	if cfg := got[plain]; cfg.historicalSQL != "" {
		t.Errorf("%s gained a historical query: %q", plain, cfg.historicalSQL)
	}
	if _, ok := got[missing.Name]; ok {
		t.Errorf("%s was added, want a definition without an entry skipped", missing.Name)
	}
}
