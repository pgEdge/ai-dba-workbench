/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// These tests cover the two classes of active anomaly alert that the
// first cut of recovery (GitHub issue #611) could not reach: alerts on an
// absence-driven metric, whose recovery is the metric returning no row,
// and alerts recovery can never re-score at all, such as those raised on
// a per-database metric before detection was scoped by database.

// absentRecoveryMetric is a baselineable clearWhenAbsent metric: its
// latest query emits a row only while a client backend waits on a lock.
const absentRecoveryMetric = "pg_stat_activity.blocked_count"

const (
	absentRecoverySchema = `
        DROP TABLE IF EXISTS probe_availability CASCADE;
        DROP TABLE IF EXISTS probe_configs CASCADE;

        CREATE TABLE probe_availability (
            connection_id INTEGER NOT NULL,
            probe_name TEXT NOT NULL,
            is_available BOOLEAN NOT NULL DEFAULT TRUE,
            last_collected TIMESTAMPTZ,
            unavailable_reason TEXT,
            PRIMARY KEY (connection_id, probe_name)
        );

        CREATE TABLE probe_configs (
            id BIGSERIAL PRIMARY KEY,
            name TEXT NOT NULL,
            connection_id INTEGER,
            is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
            collection_interval_seconds INTEGER NOT NULL DEFAULT 60
        );

        CREATE TABLE metrics.pg_stat_activity (
            connection_id INTEGER NOT NULL,
            backend_type TEXT,
            state TEXT,
            wait_event_type TEXT,
            xact_start TIMESTAMPTZ,
            query_start TIMESTAMPTZ,
            collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
        );
    `

	absentRecoveryTeardown = `
        DROP TABLE IF EXISTS probe_availability CASCADE;
        DROP TABLE IF EXISTS probe_configs CASCADE;
    `

	// collectAbsentProbeSQL records a pg_stat_activity collection the
	// given interval ago, as the collector does on every run.
	collectAbsentProbeSQL = `
        INSERT INTO probe_availability
            (connection_id, probe_name, is_available, last_collected)
        VALUES ($1, 'pg_stat_activity', TRUE, NOW() - $2::interval)
        ON CONFLICT (connection_id, probe_name) DO UPDATE
           SET is_available = TRUE, last_collected = EXCLUDED.last_collected
    `

	insertBlockedBackendSQL = `
        INSERT INTO metrics.pg_stat_activity
            (connection_id, backend_type, state, wait_event_type, collected_at)
        VALUES ($1, 'client backend', 'active', 'Lock', NOW())
    `

	selectAlertDescriptionSQL = `SELECT description FROM alerts WHERE id = $1`

	setAlertDescriptionSQL = `UPDATE alerts SET description = $2 WHERE id = $1`

	insertMetricAlertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, database_name, metric_name, severity,
             title, description, status)
        VALUES ('anomaly', $1, $2, $3, 'warning', $3, 'seeded', $4)
        RETURNING id
    `
)

// newAbsentRecoveryEnv is newRecoveryEnv with the probe staleness tables
// and the pg_stat_activity metrics table, and the probe registered.
func newAbsentRecoveryEnv(t *testing.T) *recoveryEnv {
	t.Helper()
	env := newRecoveryEnv(t)
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, absentRecoverySchema); err != nil {
		t.Fatalf("failed to create probe tables: %v", err)
	}
	t.Cleanup(func() {
		if _, err := env.pool.Exec(context.Background(), absentRecoveryTeardown); err != nil {
			t.Logf("probe table teardown failed: %v", err)
		}
	})
	if _, err := env.pool.Exec(ctx, probeConfigSeedSQL, "pg_stat_activity"); err != nil {
		t.Fatalf("failed to seed probe config: %v", err)
	}
	return env
}

// seedMetricAlert inserts an anomaly alert on any metric.
func (env *recoveryEnv) seedMetricAlert(t *testing.T, metric string, db *string, status string) int64 {
	t.Helper()
	var id int64
	if err := env.pool.QueryRow(context.Background(), insertMetricAlertSQL,
		env.connID, db, metric, status).Scan(&id); err != nil {
		t.Fatalf("failed to seed alert: %v", err)
	}
	return id
}

// collect records a pg_stat_activity collection age ago.
func (env *recoveryEnv) collect(t *testing.T, age string) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(), collectAbsentProbeSQL, env.connID, age); err != nil {
		t.Fatalf("failed to record probe collection: %v", err)
	}
}

func (env *recoveryEnv) description(t *testing.T, id int64) string {
	t.Helper()
	var d string
	if err := env.pool.QueryRow(context.Background(), selectAlertDescriptionSQL, id).Scan(&d); err != nil {
		t.Fatalf("failed to read description: %v", err)
	}
	return d
}

// TestAnomalyRecoveryAbsentMetricClears checks that an alert on an
// absence-driven metric clears once its probe has collected clear_count
// times with no row for the connection, counting each collection once
// however many passes see it, and queues one clear notification.
func TestAnomalyRecoveryAbsentMetricClears(t *testing.T) {
	env := newAbsentRecoveryEnv(t)
	capture := installStalenessNotificationCapture(t, env.engine)
	id := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")

	// Another connection still has a blocked backend, so the query
	// returns rows, just none for this alert's connection.
	var otherConn int
	if err := env.pool.QueryRow(context.Background(), insertAnomalyConnectionSQL,
		"other-conn").Scan(&otherConn); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	if _, err := env.pool.Exec(context.Background(), insertBlockedBackendSQL, otherConn); err != nil {
		t.Fatalf("failed to insert blocked backend: %v", err)
	}

	env.collect(t, "90 seconds")
	env.engine.detectAnomalies(context.Background())
	env.engine.detectAnomalies(context.Background())
	if got := env.engine.anomalyStreak(id); got != 1 {
		t.Fatalf("streak after two passes over one collection = %d, want 1", got)
	}

	env.collect(t, "60 seconds")
	env.engine.detectAnomalies(context.Background())
	wantStatus(t, env.alert(t, id), "active")
	if got := env.engine.anomalyStreak(id); got != 2 {
		t.Fatalf("streak after two collections = %d, want 2", got)
	}

	env.collect(t, "30 seconds")
	env.engine.detectAnomalies(context.Background())
	got := env.alert(t, id)
	wantStatus(t, got, "cleared")
	if !got.cleared {
		t.Error("expected cleared_at to be set")
	}
	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertClear] != 1 || len(counts) != 1 {
		t.Errorf("notifications = %v, want one alert_clear", counts)
	}
}

// TestAnomalyRecoveryAbsentMetricHolds checks every condition under which
// an absent row is not evidence of recovery holds the alert open.
func TestAnomalyRecoveryAbsentMetricHolds(t *testing.T) {
	ctx := context.Background()

	t.Run("probe not collecting", func(t *testing.T) {
		env := newAbsentRecoveryEnv(t)
		id := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")
		env.collect(t, "90 seconds")
		env.engine.detectAnomalies(ctx)

		// The last collection is now older than the query's five
		// minute window, so the empty result proves nothing; the count
		// is kept, not reset.
		env.collect(t, "10 minutes")
		for i := 0; i < 4; i++ {
			env.engine.detectAnomalies(ctx)
		}
		wantStatus(t, env.alert(t, id), "active")
		if got := env.engine.anomalyStreak(id); got != 1 {
			t.Errorf("streak whilst the probe is stale = %d, want 1", got)
		}
	})

	t.Run("staleness read fails", func(t *testing.T) {
		env := newAbsentRecoveryEnv(t)
		id := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")
		if _, err := env.pool.Exec(ctx, `DROP TABLE probe_availability`); err != nil {
			t.Fatalf("failed to drop probe_availability: %v", err)
		}
		for i := 0; i < 4; i++ {
			env.engine.detectAnomalies(ctx)
		}
		wantStatus(t, env.alert(t, id), "active")
		if got := env.engine.anomalyStreak(id); got != 0 {
			t.Errorf("streak without a staleness view = %d, want 0", got)
		}
	})

	t.Run("blackout", func(t *testing.T) {
		env := newAbsentRecoveryEnv(t)
		id := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")
		env.collect(t, "90 seconds")
		env.engine.detectAnomalies(ctx)

		// A blackout that starts after detectAnomalies resolved the
		// connection set is caught by the series check in recovery.
		pass := env.engine.loadAnomalyRecovery(ctx)
		now := time.Now()
		if _, err := env.pool.Exec(ctx, insertRecoveryDBBlackoutSQL, env.connID, nil,
			now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
			t.Fatalf("failed to insert blackout: %v", err)
		}
		env.collect(t, "30 seconds")
		env.engine.recoverAbsentAnomalyAlerts(ctx, pass, absentRecoveryMetric, nil,
			map[int]bool{env.connID: true}, env.engine.getConfig())
		wantStatus(t, env.alert(t, id), "active")
		if got := pass.next[id].count; got != 0 {
			t.Errorf("streak during a blackout = %d, want 0", got)
		}
	})

	t.Run("blackout check fails", func(t *testing.T) {
		env := newAbsentRecoveryEnv(t)
		id := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")
		env.collect(t, "90 seconds")
		env.engine.detectAnomalies(ctx)

		// The connection-wide check in detectAnomalies only logs a
		// failure, so the series check inside recovery is the one that
		// must hold the alert.
		pass := env.engine.loadAnomalyRecovery(ctx)
		if _, err := env.pool.Exec(ctx, `DROP TABLE blackouts CASCADE`); err != nil {
			t.Fatalf("failed to drop blackouts: %v", err)
		}
		env.collect(t, "30 seconds")
		env.engine.recoverAbsentAnomalyAlerts(ctx, pass, absentRecoveryMetric, nil,
			map[int]bool{env.connID: true}, env.engine.getConfig())
		if got := pass.next[id].count; got != 0 {
			t.Errorf("streak after a failed blackout check = %d, want 0", got)
		}
		wantStatus(t, env.alert(t, id), "active")
	})

	t.Run("connection not evaluated", func(t *testing.T) {
		env := newAbsentRecoveryEnv(t)
		id := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")
		env.collect(t, "30 seconds")
		pass := env.engine.loadAnomalyRecovery(ctx)
		env.engine.recoverAbsentAnomalyAlerts(ctx, pass, absentRecoveryMetric, nil,
			map[int]bool{}, env.engine.getConfig())
		if _, ok := pass.next[id]; ok {
			t.Error("expected an alert on an unevaluated connection to be left alone")
		}
	})

	t.Run("row still reported", func(t *testing.T) {
		env := newAbsentRecoveryEnv(t)
		id := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")
		env.collect(t, "30 seconds")
		if _, err := env.pool.Exec(ctx, insertBlockedBackendSQL, env.connID); err != nil {
			t.Fatalf("failed to insert blocked backend: %v", err)
		}
		for i := 0; i < 4; i++ {
			env.engine.detectAnomalies(ctx)
		}
		wantStatus(t, env.alert(t, id), "active")
	})
}

// TestAnomalyRecoveryAbsentMetricIgnoresOtherMetrics checks that the
// absence path leaves alerts on a metric that is not absence-driven, and
// alerts on other metrics, to the scoring path.
func TestAnomalyRecoveryAbsentMetricIgnoresOtherMetrics(t *testing.T) {
	ctx := context.Background()
	env := newAbsentRecoveryEnv(t)
	loadID := env.seedAlert(t, nil, "warning", "active")
	blockedID := env.seedMetricAlert(t, absentRecoveryMetric, nil, "active")
	env.collect(t, "30 seconds")

	pass := env.engine.loadAnomalyRecovery(ctx)
	evaluable := map[int]bool{env.connID: true}
	env.engine.recoverAbsentAnomalyAlerts(ctx, pass, tierSkipMetric, nil, evaluable,
		env.engine.getConfig())
	if _, ok := pass.next[loadID]; ok {
		t.Error("expected a metric that is not absence-driven to be left alone")
	}
	if _, ok := pass.next[blockedID]; ok {
		t.Error("expected another metric's alert to be left alone")
	}

	env.engine.recoverAbsentAnomalyAlerts(ctx, nil, absentRecoveryMetric, nil, evaluable,
		env.engine.getConfig())
}

// TestProbeLastCollected covers the lookup the absence path keys its
// samples on, including a probe the entries do not hold.
func TestProbeLastCollected(t *testing.T) {
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	entries := []database.ProbeStaleness{
		{ConnectionID: 1, ProbeName: "other", LastCollected: at.Add(time.Hour)},
		{ConnectionID: 2, ProbeName: "pg_stat_activity", LastCollected: at.Add(time.Hour)},
		{ConnectionID: 1, ProbeName: "pg_stat_activity", LastCollected: at},
	}
	if got := probeLastCollected(entries, 1, "pg_stat_activity"); !got.Equal(at) {
		t.Errorf("probeLastCollected = %v, want %v", got, at)
	}
	if got := probeLastCollected(entries, 3, "pg_stat_activity"); !got.IsZero() {
		t.Errorf("probeLastCollected for a missing probe = %v, want zero", got)
	}
}

// TestAnomalyRecoveryRetiresUnreachableAlerts checks that an active alert
// recovery can never re-score is closed on the first pass, without a
// notification and with its description saying why, whilst a reachable
// alert and an acknowledged one are left alone.
func TestAnomalyRecoveryRetiresUnreachableAlerts(t *testing.T) {
	ctx := context.Background()
	env := newRecoveryEnv(t)
	capture := installStalenessNotificationCapture(t, env.engine)
	db := "appdb"

	legacy := env.seedMetricAlert(t, "pg_stat_database.cache_hit_ratio", nil, "active")
	unsupported := env.seedMetricAlert(t, "pg_stat_replication.lag_bytes", nil, "active")
	scoped := env.seedMetricAlert(t, "pg_stat_database.cache_hit_ratio", &db, "active")
	reachable := env.seedMetricAlert(t, tierSkipMetric, nil, "active")
	acknowledged := env.seedMetricAlert(t, "pg_stat_database.cache_hit_ratio", nil, "acknowledged")
	retiredBefore := env.seedMetricAlert(t, "pg_stat_database.deadlocks_delta", nil, "active")
	already := unreachableAnomalyDescriptionPrefix + "earlier"
	if _, err := env.pool.Exec(ctx, setAlertDescriptionSQL, retiredBefore, already); err != nil {
		t.Fatalf("failed to set description: %v", err)
	}

	env.passes(t, val(inBandValue))

	for name, id := range map[string]int64{
		"legacy NULL database": legacy,
		"unsupported metric":   unsupported,
	} {
		wantStatus(t, env.alert(t, id), "cleared")
		desc := env.description(t, id)
		if !strings.HasPrefix(desc, unreachableAnomalyDescriptionPrefix) ||
			!strings.HasSuffix(desc, "The alert said: seeded") {
			t.Errorf("%s: description = %q", name, desc)
		}
	}
	wantStatus(t, env.alert(t, scoped), "active")
	wantStatus(t, env.alert(t, reachable), "active")
	if got := env.engine.anomalyStreak(reachable); got != 1 {
		t.Errorf("reachable alert streak = %d, want 1", got)
	}
	wantStatus(t, env.alert(t, acknowledged), "acknowledged")
	wantStatus(t, env.alert(t, retiredBefore), "cleared")
	if got := env.description(t, retiredBefore); got != already {
		t.Errorf("description rewritten twice: %q", got)
	}
	if counts := capture.drain(t); len(counts) != 0 {
		t.Errorf("notifications = %v, want none", counts)
	}
}

// TestRetireUnreachableAnomalyAlertFailures covers the write failures: a
// failed clear leaves the alert active for the next pass, and an alert
// no longer active is neither cleared nor rewritten.
func TestRetireUnreachableAnomalyAlertFailures(t *testing.T) {
	ctx := context.Background()
	env := newRecoveryEnv(t)
	id := env.seedMetricAlert(t, "pg_stat_database.cache_hit_ratio", nil, "active")
	alert := &database.Alert{ID: id, Description: "seeded"}

	if _, err := env.pool.Exec(ctx, failRecoveryUpdatesSQL); err != nil {
		t.Fatalf("failed to install trigger: %v", err)
	}
	env.engine.retireUnreachableAnomalyAlert(ctx, alert, "reason")
	if _, err := env.pool.Exec(ctx, dropFailRecoveryUpdatesSQL); err != nil {
		t.Fatalf("failed to drop trigger: %v", err)
	}
	wantStatus(t, env.alert(t, id), "active")

	if _, err := env.pool.Exec(ctx, acknowledgeRecoveryAlertSQL, id); err != nil {
		t.Fatalf("failed to acknowledge: %v", err)
	}
	env.engine.retireUnreachableAnomalyAlert(ctx, alert, "reason")
	wantStatus(t, env.alert(t, id), "acknowledged")
	if got := env.description(t, id); got != "seeded" {
		t.Errorf("description of an acknowledged alert = %q, want unchanged", got)
	}
}

// TestUnreachableAnomalyReason pins the classification without a
// database.
func TestUnreachableAnomalyReason(t *testing.T) {
	db := "appdb"
	name := func(s string) *string { return &s }
	tests := []struct {
		metric      string
		db          *string
		unreachable bool
	}{
		{"pg_stat_database.cache_hit_ratio", &db, false},
		{"pg_stat_database.cache_hit_ratio", nil, true},
		{tierSkipMetric, nil, false},
		{tierSkipMetric, &db, false},
		{absentRecoveryMetric, nil, false},
		{"pg_stat_replication.lag_bytes", nil, true},
		{"metric_staleness", nil, true},
	}
	for _, tc := range tests {
		alert := &database.Alert{MetricName: name(tc.metric), DatabaseName: tc.db}
		if got := unreachableAnomalyReason(alert) != ""; got != tc.unreachable {
			t.Errorf("unreachableAnomalyReason(%s, db %v) unreachable = %v, want %v",
				tc.metric, tc.db != nil, got, tc.unreachable)
		}
	}
}
