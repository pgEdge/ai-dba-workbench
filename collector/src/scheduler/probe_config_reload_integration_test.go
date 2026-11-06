/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package scheduler

import (
	"context"
	"testing"

	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// These tests cover how loadConfigs applies probe configuration changes
// to a connection that is already monitored: edits to Probe Defaults
// and to cluster or group overrides reach the copied server-level rows
// (issue #810), and a disabled configuration stops the probe (issue
// #811).

// execDatastore runs one statement against the fixture's datastore.
func execDatastore(t *testing.T, f *integrationFixture, query string, args ...any) {
	t.Helper()
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer f.ds.ReturnConnection(conn)
	if _, err := conn.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// serverRow reads the fixture connection's server-level row for a probe.
func serverRow(t *testing.T, f *integrationFixture, name string) (enabled bool, interval, retention int) {
	t.Helper()
	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer f.ds.ReturnConnection(conn)
	if err := conn.QueryRow(context.Background(), `
		SELECT is_enabled, collection_interval_seconds, retention_days
		FROM probe_configs
		WHERE scope = 'server' AND name = $1 AND connection_id = $2
	`, name, f.connID).Scan(&enabled, &interval, &retention); err != nil {
		t.Fatalf("read server row for %s: %v", name, err)
	}
	return enabled, interval, retention
}

// registeredProbe returns the probe loadConfigs registered for the
// fixture connection, or nil.
func registeredProbe(ps *ProbeScheduler, connID int, name string) probes.MetricsProbe {
	ps.probesMutex.RLock()
	defer ps.probesMutex.RUnlock()
	return ps.probesByConn[connID][name]
}

// enableConnectivityGlobally enables the connectivity probe's global row
// at a long interval, so its goroutine never ticks during a test, and
// restores the fixture's all-disabled state when the test ends.
func enableConnectivityGlobally(t *testing.T, f *integrationFixture) {
	t.Helper()
	execDatastore(t, f, `
		UPDATE probe_configs
		SET is_enabled = TRUE, collection_interval_seconds = 3600, retention_days = 28
		WHERE name = $1 AND scope = 'global'
	`, probes.ProbeNamePgConnectivity)
	t.Cleanup(func() {
		execDatastore(t, f, "DELETE FROM probe_configs WHERE scope IN ('server', 'cluster', 'group')")
		execDatastore(t, f, `
			UPDATE probe_configs SET is_enabled = FALSE, collection_interval_seconds = 30
			WHERE name = $1 AND scope = 'global'
		`, probes.ProbeNamePgConnectivity)
	})
}

func loadConfigsOrFail(t *testing.T, ps *ProbeScheduler) {
	t.Helper()
	if err := ps.loadConfigs(context.Background()); err != nil {
		t.Fatalf("loadConfigs: %v", err)
	}
}

// TestSchedulerLoadConfigs_ProbeDefaultsReachMonitoredServer is the
// regression test for issue #810: a Probe Defaults edit made after the
// server-level row was copied must reach that row and the running
// probe, whilst an explicit server override keeps its own settings.
func TestSchedulerLoadConfigs_ProbeDefaultsReachMonitoredServer(t *testing.T) {
	f := setupIntegration(t)
	enableConnectivityGlobally(t, f)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	loadConfigsOrFail(t, ps)
	if _, interval, _ := serverRow(t, f, probes.ProbeNamePgConnectivity); interval != 3600 {
		t.Fatalf("copied interval = %d, want 3600", interval)
	}
	first := registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity)
	if first == nil {
		t.Fatal("connectivity probe was not started")
	}

	// Change Probe Defaults: the copied row and the probe follow it.
	execDatastore(t, f, `
		UPDATE probe_configs SET collection_interval_seconds = 7200, retention_days = 14
		WHERE name = $1 AND scope = 'global'
	`, probes.ProbeNamePgConnectivity)
	loadConfigsOrFail(t, ps)
	if enabled, interval, retention := serverRow(t, f, probes.ProbeNamePgConnectivity); !enabled || interval != 7200 || retention != 14 {
		t.Errorf("server row = enabled %v interval %d retention %d, want true 7200 14",
			enabled, interval, retention)
	}
	second := registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity)
	if second == nil || second == first || second.GetConfig().CollectionIntervalSeconds != 7200 {
		t.Errorf("probe was not replaced at the new interval: %+v", second)
	}

	// An explicit server override is left alone by later default edits.
	execDatastore(t, f, `
		UPDATE probe_configs SET collection_interval_seconds = 1800, user_modified = TRUE
		WHERE name = $1 AND scope = 'server' AND connection_id = $2
	`, probes.ProbeNamePgConnectivity, f.connID)
	execDatastore(t, f, `
		UPDATE probe_configs SET collection_interval_seconds = 3000
		WHERE name = $1 AND scope = 'global'
	`, probes.ProbeNamePgConnectivity)
	loadConfigsOrFail(t, ps)
	if _, interval, _ := serverRow(t, f, probes.ProbeNamePgConnectivity); interval != 1800 {
		t.Errorf("overridden interval = %d, want 1800", interval)
	}
	if p := registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity); p == nil ||
		p.GetConfig().CollectionIntervalSeconds != 1800 {
		t.Errorf("probe does not run at the overridden interval: %+v", p)
	}
}

// TestSchedulerLoadConfigs_ClusterAndGroupOverridesReachMonitoredServer
// checks the sync resolves parents in the same order EnsureProbeConfig
// does: a cluster override beats a group override, which beats the
// global row, and removing an override falls back to the next parent.
func TestSchedulerLoadConfigs_ClusterAndGroupOverridesReachMonitoredServer(t *testing.T) {
	f := setupIntegration(t)
	enableConnectivityGlobally(t, f)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	conn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	var groupID, clusterID int
	err = conn.QueryRow(context.Background(),
		`INSERT INTO cluster_groups (name) VALUES ('reload-test-group') RETURNING id`).Scan(&groupID)
	if err == nil {
		err = conn.QueryRow(context.Background(),
			`INSERT INTO clusters (name, group_id) VALUES ('reload-test-cluster', $1) RETURNING id`,
			groupID).Scan(&clusterID)
	}
	f.ds.ReturnConnection(conn)
	if err != nil {
		t.Fatalf("seed cluster hierarchy: %v", err)
	}
	execDatastore(t, f, "UPDATE connections SET cluster_id = $1 WHERE id = $2", clusterID, f.connID)
	t.Cleanup(func() {
		execDatastore(t, f, "UPDATE connections SET cluster_id = NULL WHERE id = $1", f.connID)
		execDatastore(t, f, "DELETE FROM clusters WHERE id = $1", clusterID)
		execDatastore(t, f, "DELETE FROM cluster_groups WHERE id = $1", groupID)
	})

	loadConfigsOrFail(t, ps)

	execDatastore(t, f, `
		INSERT INTO probe_configs (name, description, scope, group_id, is_enabled,
		                           collection_interval_seconds, retention_days, user_modified)
		VALUES ($1, 'group', 'group', $2, TRUE, 2400, 10, TRUE)
	`, probes.ProbeNamePgConnectivity, groupID)
	loadConfigsOrFail(t, ps)
	if _, interval, retention := serverRow(t, f, probes.ProbeNamePgConnectivity); interval != 2400 || retention != 10 {
		t.Errorf("with a group override: interval %d retention %d, want 2400 10", interval, retention)
	}

	execDatastore(t, f, `
		INSERT INTO probe_configs (name, description, scope, cluster_id, is_enabled,
		                           collection_interval_seconds, retention_days, user_modified)
		VALUES ($1, 'cluster', 'cluster', $2, TRUE, 1200, 5, TRUE)
	`, probes.ProbeNamePgConnectivity, clusterID)
	loadConfigsOrFail(t, ps)
	if _, interval, retention := serverRow(t, f, probes.ProbeNamePgConnectivity); interval != 1200 || retention != 5 {
		t.Errorf("with a cluster override: interval %d retention %d, want 1200 5", interval, retention)
	}

	execDatastore(t, f, "DELETE FROM probe_configs WHERE scope IN ('cluster', 'group')")
	loadConfigsOrFail(t, ps)
	if _, interval, retention := serverRow(t, f, probes.ProbeNamePgConnectivity); interval != 3600 || retention != 28 {
		t.Errorf("after removing the overrides: interval %d retention %d, want 3600 28", interval, retention)
	}
}

// TestSchedulerLoadConfigs_DisabledConfigStopsProbe is the regression
// test for issue #811: disabling a running probe's server-level row
// removes it from the scheduler, and so does disabling the probe in
// Probe Defaults for a connection that inherits it.
func TestSchedulerLoadConfigs_DisabledConfigStopsProbe(t *testing.T) {
	f := setupIntegration(t)
	enableConnectivityGlobally(t, f)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	loadConfigsOrFail(t, ps)
	if registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity) == nil {
		t.Fatal("connectivity probe was not started")
	}

	// Disable the server row, as PUT /api/v1/probe-configs/{id} does.
	execDatastore(t, f, `
		UPDATE probe_configs SET is_enabled = FALSE, user_modified = TRUE
		WHERE name = $1 AND scope = 'server' AND connection_id = $2
	`, probes.ProbeNamePgConnectivity, f.connID)
	loadConfigsOrFail(t, ps)
	if p := registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity); p != nil {
		t.Error("probe still registered after its server row was disabled")
	}

	// A disabled override stays stopped across reloads.
	loadConfigsOrFail(t, ps)
	if p := registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity); p != nil {
		t.Error("disabled probe was restarted by a later reload")
	}

	// Re-enabling the override starts it again.
	execDatastore(t, f, `
		UPDATE probe_configs SET is_enabled = TRUE
		WHERE name = $1 AND scope = 'server' AND connection_id = $2
	`, probes.ProbeNamePgConnectivity, f.connID)
	loadConfigsOrFail(t, ps)
	if registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity) == nil {
		t.Fatal("probe was not restarted after its server row was re-enabled")
	}

	// Drop the override, so the row inherits again, then disable the
	// probe in Probe Defaults.
	execDatastore(t, f, `
		UPDATE probe_configs SET user_modified = FALSE
		WHERE name = $1 AND scope = 'server' AND connection_id = $2
	`, probes.ProbeNamePgConnectivity, f.connID)
	execDatastore(t, f, `
		UPDATE probe_configs SET is_enabled = FALSE
		WHERE name = $1 AND scope = 'global'
	`, probes.ProbeNamePgConnectivity)
	loadConfigsOrFail(t, ps)
	if p := registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity); p != nil {
		t.Error("probe still registered after it was disabled in Probe Defaults")
	}
	if enabled, _, _ := serverRow(t, f, probes.ProbeNamePgConnectivity); enabled {
		t.Error("inherited server row is still enabled after Probe Defaults disabled it")
	}
}

// TestSchedulerLoadConfigs_OverrideEnablesGloballyDisabledProbe checks
// that a server override enabling a probe wins over a disabled global
// row, which before issue #811 hid the probe from the scheduler.
func TestSchedulerLoadConfigs_OverrideEnablesGloballyDisabledProbe(t *testing.T) {
	f := setupIntegration(t)
	t.Cleanup(func() {
		execDatastore(t, f, "DELETE FROM probe_configs WHERE scope = 'server'")
	})
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	execDatastore(t, f, `
		INSERT INTO probe_configs (name, description, scope, connection_id, is_enabled,
		                           collection_interval_seconds, retention_days, user_modified)
		VALUES ($1, 'override', 'server', $2, TRUE, 3600, 28, TRUE)
	`, probes.ProbeNamePgConnectivity, f.connID)
	loadConfigsOrFail(t, ps)
	if registeredProbe(ps, f.connID, probes.ProbeNamePgConnectivity) == nil {
		t.Error("an enabled server override did not start a globally disabled probe")
	}
}
