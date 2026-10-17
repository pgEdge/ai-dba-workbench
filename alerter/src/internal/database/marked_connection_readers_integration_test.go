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
	"context"
	"regexp"
	"strings"
	"testing"
)

// These tests cover the alerter's readers of connections.deleted_at: a
// connection the server has marked for deletion but not yet purged
// (issue #669) must not be evaluated, reported or notified about.

// liveConnectionJoin matches a registry join to connections that skips
// marked connections.
var liveConnectionJoin = regexp.MustCompile(
	`JOIN connections c ON c\.id = \w+\.connection_id AND c\.deleted_at IS NULL`)

// TestMetricRegistryJoinsSkipMarkedConnections pins the registry's
// connection joins. GetLatestMetricValues filters marked connections out
// of every latest query after the fact, but the historical queries feed
// baselines directly, so each must do so itself; and a latest query that
// joins connections must not join a marked one either.
func TestMetricRegistryJoinsSkipMarkedConnections(t *testing.T) {
	for _, name := range registryNames() {
		cfg := metricRegistry[name]
		for kind, sql := range map[string]string{"latestSQL": cfg.latestSQL, "historicalSQL": cfg.historicalSQL} {
			joins := strings.Count(sql, "JOIN connections")
			live := len(liveConnectionJoin.FindAllString(sql, -1))
			if joins != live {
				t.Errorf("%s %s joins connections %d times, %d of them skipping marked connections",
					name, kind, joins, live)
			}
		}
		if strings.TrimSpace(cfg.historicalSQL) != "" && !liveConnectionJoin.MatchString(cfg.historicalSQL) {
			t.Errorf("%s historicalSQL does not join live connections", name)
		}
	}
}

// TestGetHistoricalMetricValues_SkipsMarkedConnections exercises one
// historical query end to end: the marked connection's samples are not
// returned, so no baseline is calculated for it.
func TestGetHistoricalMetricValues_SkipsMarkedConnections(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	liveID := insertAuditConnection(t, pool, "historical-live")
	goneID := insertAuditConnection(t, pool, "historical-marked")
	for _, id := range []int{liveID, goneID} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.pg_stat_activity (connection_id, backend_type, collected_at)
			VALUES ($1, 'client backend', NOW() - INTERVAL '1 hour')`, id); err != nil {
			t.Fatalf("failed to insert pg_stat_activity row: %v", err)
		}
	}
	if _, err := pool.Exec(ctx,
		`UPDATE connections SET deleted_at = NOW() WHERE id = $1`, goneID); err != nil {
		t.Fatalf("failed to mark connection deleted: %v", err)
	}

	values, err := ds.GetHistoricalMetricValues(ctx, "pg_stat_activity.count", 1)
	if err != nil {
		t.Fatalf("GetHistoricalMetricValues failed: %v", err)
	}
	if len(values) != 1 || values[0].ConnectionID != liveID {
		t.Errorf("values = %+v, want one sample for connection %d", values, liveID)
	}
}

// TestConnectionReaders_SkipMarkedConnections covers the connection
// lookups. One marked connection still has is_monitored and enabled set,
// as a racing update might leave it, so only deleted_at can exclude it;
// the other is marked as DeleteConnection marks it, which would
// otherwise put it among the unmonitored connections.
func TestConnectionReaders_SkipMarkedConnections(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	live := insertTestConnection(t, pool, "live-conn")
	paused := insertTestConnection(t, pool, "paused-conn")
	racing := insertTestConnection(t, pool, "racing-marked")
	marked := insertTestConnection(t, pool, "marked-conn")
	for _, stmt := range []struct {
		sql string
		id  int
	}{
		{`UPDATE connections SET connection_error = 'refused' WHERE id IN ($1)`, live},
		{`UPDATE connections SET is_monitored = FALSE WHERE id = $1`, paused},
		{`UPDATE connections SET connection_error = 'refused', deleted_at = NOW() WHERE id = $1`, racing},
		{`UPDATE connections SET is_monitored = FALSE, enabled = FALSE, deleted_at = NOW() WHERE id = $1`, marked},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.id); err != nil {
			t.Fatalf("%s: %v", stmt.sql, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO probe_configs (name, collection_interval_seconds, scope)
		VALUES ('pg_stat_activity', 60, 'global')`); err != nil {
		t.Fatalf("failed to seed probe_configs: %v", err)
	}

	errs, err := ds.GetMonitoredConnectionErrors(ctx)
	if err != nil {
		t.Fatalf("GetMonitoredConnectionErrors: %v", err)
	}
	if len(errs) != 1 || errs[0].ConnectionID != live {
		t.Errorf("GetMonitoredConnectionErrors = %+v, want only connection %d", errs, live)
	}

	active, err := ds.GetActiveConnections(ctx)
	if err != nil {
		t.Fatalf("GetActiveConnections: %v", err)
	}
	if len(active) != 2 || active[0] != live || active[1] != paused {
		t.Errorf("GetActiveConnections = %v, want [%d %d]", active, live, paused)
	}

	unmonitored, err := ds.GetUnmonitoredConnections(ctx)
	if err != nil {
		t.Fatalf("GetUnmonitoredConnections: %v", err)
	}
	if len(unmonitored) != 1 || unmonitored[paused] != "paused-conn" {
		t.Errorf("GetUnmonitoredConnections = %v, want only connection %d", unmonitored, paused)
	}

	if name, interval, found, err := ds.GetProbeStalenessContext(ctx, live, "pg_stat_activity"); err != nil ||
		!found || name != "live-conn" || interval != 60 {
		t.Errorf("GetProbeStalenessContext(live) = %q, %d, %v, %v", name, interval, found, err)
	}
	for _, id := range []int{racing, marked} {
		if _, _, found, err := ds.GetProbeStalenessContext(ctx, id, "pg_stat_activity"); err != nil || found {
			t.Errorf("GetProbeStalenessContext(%d) found = %v, err = %v; want not found", id, found, err)
		}
		if _, _, _, err := ds.GetConnectionInfo(ctx, id); err == nil {
			t.Errorf("GetConnectionInfo(%d) returned a marked connection", id)
		}
	}
	if name, _, _, err := ds.GetConnectionInfo(ctx, live); err != nil || name != "live-conn" {
		t.Errorf("GetConnectionInfo(live) = %q, %v", name, err)
	}
}
