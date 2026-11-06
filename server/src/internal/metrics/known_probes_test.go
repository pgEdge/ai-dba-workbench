/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"sort"
	"testing"
)

// registerFixtureProbeForTest makes name a known probe until the test
// ends, so a fixture table under a test-only name can be read through
// ProbeExists, which serves known probes only. The package's fixtures
// carry their own names so they cannot collide with a real probe's
// table, which other packages' tests create in the same database.
func registerFixtureProbeForTest(t testing.TB, name string) {
	t.Helper()
	knownProbesMu.Lock()
	_, had := knownProbes[name]
	knownProbes[name] = struct{}{}
	knownProbesMu.Unlock()
	if had {
		return
	}
	t.Cleanup(func() {
		knownProbesMu.Lock()
		defer knownProbesMu.Unlock()
		delete(knownProbes, name)
	})
}

// TestKnownProbesCoverMaskedAndKeyedProbes checks that every probe the
// server masks or keys by name is a known probe, so a ColdFront view of
// it is served.
func TestKnownProbesCoverMaskedAndKeyedProbes(t *testing.T) {
	for name := range ProbeRedactions {
		if !IsKnownProbe(name) {
			t.Errorf("ProbeRedactions masks %q, which is not a known probe", name)
		}
	}
	for name := range probeEntityKeyFallback {
		if !IsKnownProbe(name) {
			t.Errorf("probeEntityKeyFallback keys %q, which is not a known probe", name)
		}
	}
}

// metricsProbeTablesQuery lists every probe table in the metrics schema:
// partitioned parents and plain tables, not partitions.
const metricsProbeTablesQuery = `
        SELECT c.relname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'metrics'
            AND c.relkind IN ('r', 'p')
            AND NOT c.relispartition
        ORDER BY c.relname
    `

// TestKnownProbesMatchCollectorSchema is the drift guard for knownProbes:
// against the schema the collector really creates, every metrics table
// must be a known probe and every known probe a metrics table.
func TestKnownProbesMatchCollectorSchema(t *testing.T) {
	pool := newMigratedMetricsPool(t)
	rows, err := pool.Query(context.Background(), metricsProbeTablesQuery)
	if err != nil {
		t.Fatalf("list probe tables: %v", err)
	}
	seen := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("read probe tables: %v", err)
		}
		seen[name] = true
		if !IsKnownProbe(name) {
			t.Errorf("the collector creates metrics.%s, which knownProbes does not hold", name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read probe tables: %v", err)
	}
	var stale []string
	for name := range knownProbes {
		if !seen[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("knownProbes holds probes the collector schema has no table for: %q", stale)
	}
}
