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
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// activityRow is one synthetic metrics.pg_stat_activity row. Offsets are
// subtracted from the sample's collected_at to give xact_start and
// query_start; a zero offset leaves the column NULL.
type activityRow struct {
	backendType   *string
	state         *string
	waitEventType *string
	xactAge       time.Duration
	queryAge      time.Duration
}

// strPtr returns a pointer to s, for the nullable activity columns.
func strPtr(s string) *string { return &s }

// clientBackend and the background process types below are the
// backend_type values PostgreSQL reports; only clientBackend may count
// towards the pg_stat_activity metrics.
var (
	clientBackend     = strPtr("client backend")
	checkpointer      = strPtr("checkpointer")
	walWriter         = strPtr("walwriter")
	backgroundWriter  = strPtr("background writer")
	autovacuumWorker  = strPtr("autovacuum worker")
	autovacuumLaunch  = strPtr("autovacuum launcher")
	walSender         = strPtr("walsender")
	logicalRepLaunch  = strPtr("logical replication launcher")
	parallelWorker    = strPtr("parallel worker")
	backgroundWorkerT = strPtr("background worker")
)

// insertActivitySnapshot writes every row as one pg_stat_activity sample
// collected at the given instant.
func insertActivitySnapshot(t *testing.T, pool *pgxpool.Pool, connID int,
	at time.Time, rows []activityRow) {
	t.Helper()

	for i, r := range rows {
		var xactStart, queryStart *time.Time
		if r.xactAge > 0 {
			v := at.Add(-r.xactAge)
			xactStart = &v
		}
		if r.queryAge > 0 {
			v := at.Add(-r.queryAge)
			queryStart = &v
		}
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO metrics.pg_stat_activity
			    (connection_id, backend_type, state, wait_event_type,
			     xact_start, query_start, collected_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, connID, r.backendType, r.state, r.waitEventType,
			xactStart, queryStart, at); err != nil {
			t.Fatalf("failed to insert pg_stat_activity row %d: %v", i, err)
		}
	}
}

// insertMaxConnections writes a max_connections pg_settings snapshot.
func insertMaxConnections(t *testing.T, pool *pgxpool.Pool, connID int,
	setting string, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.pg_settings (connection_id, name, setting, collected_at)
		VALUES ($1, 'max_connections', $2, $3)
	`, connID, setting, at); err != nil {
		t.Fatalf("failed to insert max_connections setting: %v", err)
	}
}

// historicalValuesFor returns the metric's historical rows for the
// connection over a one day lookback, keyed by collected_at in UTC.
func historicalValuesFor(t *testing.T, ds *Datastore, metric string,
	connID int) map[time.Time]float64 {
	t.Helper()

	values, err := ds.GetHistoricalMetricValues(context.Background(), metric, 1)
	if err != nil {
		t.Fatalf("GetHistoricalMetricValues(%s) failed: %v", metric, err)
	}
	found := make(map[time.Time]float64)
	for _, v := range values {
		if v.ConnectionID == connID {
			found[v.CollectedAt.UTC()] = v.Value
		}
	}
	return found
}

// assertClose fails when got differs from want by more than a rounding
// error; the percentages are float division results.
func assertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// sampleInstant returns a collected_at the given distance in the past,
// truncated to the microsecond precision timestamptz stores so it can be
// used as a map key against the scanned rows.
func sampleInstant(ago time.Duration) time.Time {
	return time.Now().UTC().Add(-ago).Truncate(time.Microsecond)
}

// TestConnectionUtilizationPercent_LatestMatchesHistorical is the
// regression test for GitHub issue #567. The historical query counted
// every pg_stat_activity row, background processes included, whilst the
// latest query counted client backends only, so the baseline sat above
// any value a live sample could reach. For one fixed snapshot both
// queries must report the same client-only percentage.
func TestConnectionUtilizationPercent_LatestMatchesHistorical(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "conn-util-567")
	insertMaxConnections(t, pool, connID, "100", sampleInstant(2*time.Hour))

	at := sampleInstant(2 * time.Minute)
	insertActivitySnapshot(t, pool, connID, at, []activityRow{
		{backendType: clientBackend, state: strPtr("active")},
		{backendType: clientBackend, state: strPtr("idle")},
		{backendType: clientBackend, state: strPtr("idle in transaction")},
		{backendType: checkpointer},
		{backendType: walWriter},
		{backendType: backgroundWriter},
		{backendType: autovacuumLaunch},
		{backendType: logicalRepLaunch},
		{backendType: nil},
	})

	const want = 3.0 // three client backends of max_connections = 100

	latest := latestValueFor(t, ds, "connection_utilization_percent", connID)
	assertClose(t, "latest", latest, want)

	historical := historicalValuesFor(t, ds, "connection_utilization_percent", connID)
	got, ok := historical[at]
	if !ok {
		t.Fatalf("historical: no row for the snapshot at %v, got %+v", at, historical)
	}
	assertClose(t, "historical", got, want)
	assertClose(t, "historical minus latest", got-latest, 0)
}

// TestConnectionUtilizationPercent_BackgroundOnlySnapshot pins that a
// snapshot holding no client backends contributes no baseline sample,
// just as the latest query reports no row for it; counting the
// background processes there would give the baseline a value the live
// metric never produces.
func TestConnectionUtilizationPercent_BackgroundOnlySnapshot(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "conn-util-567-bg")
	insertMaxConnections(t, pool, connID, "100", sampleInstant(2*time.Hour))

	at := sampleInstant(2 * time.Minute)
	insertActivitySnapshot(t, pool, connID, at, []activityRow{
		{backendType: checkpointer},
		{backendType: walWriter},
		{backendType: autovacuumLaunch},
	})

	// The only connection has no qualifying row, so the query returns an
	// empty result, which GetLatestMetricValues reports as ErrNoMetricData.
	values, err := ds.GetLatestMetricValues(context.Background(),
		"connection_utilization_percent")
	if err != nil && !errors.Is(err, ErrNoMetricData) {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	for _, v := range values {
		if v.ConnectionID == connID {
			t.Errorf("latest: expected no row for a background-only snapshot, got %+v", v)
		}
	}
	if historical := historicalValuesFor(t, ds,
		"connection_utilization_percent", connID); len(historical) != 0 {
		t.Errorf("historical: expected no rows for a background-only snapshot, got %+v",
			historical)
	}
}

// TestConnectionUtilizationPercent_HistoricalUsesSettingInForce pins the
// denominator. The latest query divides the newest sample by the
// max_connections in force when that sample was taken; the historical
// query must do the same for every sample rather than rescale the whole
// history by today's setting. A sample collected
// before the first pg_settings snapshot takes the earliest setting, so
// samples written just ahead of the settings probe at onboarding are not
// dropped from the baseline.
func TestConnectionUtilizationPercent_HistoricalUsesSettingInForce(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "conn-util-567-settings")
	insertMaxConnections(t, pool, connID, "100", sampleInstant(3*time.Hour))
	insertMaxConnections(t, pool, connID, "200", sampleInstant(1*time.Hour))

	fourClients := []activityRow{
		{backendType: clientBackend},
		{backendType: clientBackend},
		{backendType: clientBackend},
		{backendType: clientBackend},
		{backendType: checkpointer},
	}
	beforeFirst := sampleInstant(4 * time.Hour)
	underFirst := sampleInstant(2 * time.Hour)
	underSecond := sampleInstant(2 * time.Minute)
	for _, at := range []time.Time{beforeFirst, underFirst, underSecond} {
		insertActivitySnapshot(t, pool, connID, at, fourClients)
	}

	latest := latestValueFor(t, ds, "connection_utilization_percent", connID)
	assertClose(t, "latest", latest, 2)

	historical := historicalValuesFor(t, ds, "connection_utilization_percent", connID)
	if len(historical) != 3 {
		t.Fatalf("historical: expected one row per snapshot (3), got %+v", historical)
	}
	cases := []struct {
		label string
		at    time.Time
		want  float64
	}{
		{"before the first setting", beforeFirst, 4},
		{"under max_connections = 100", underFirst, 4},
		{"under max_connections = 200", underSecond, 2},
	}
	for _, c := range cases {
		got, ok := historical[c.at]
		if !ok {
			t.Errorf("historical %s: no row at %v", c.label, c.at)
			continue
		}
		assertClose(t, "historical "+c.label, got, c.want)
	}
	assertClose(t, "newest historical minus latest", historical[underSecond]-latest, 0)
}

// TestConnectionUtilizationPercent_LatestIgnoresNewerSetting is the
// regression test for GitHub issue #596. A pg_settings sample written
// after the newest activity sample, as just after a restart that changed
// max_connections, must not become the latest query's denominator, or the
// live value disagrees with the baseline for the same snapshot.
func TestConnectionUtilizationPercent_LatestIgnoresNewerSetting(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "conn-util-596")
	insertMaxConnections(t, pool, connID, "100", sampleInstant(2*time.Hour))

	at := sampleInstant(3 * time.Minute)
	insertActivitySnapshot(t, pool, connID, at, []activityRow{
		{backendType: clientBackend},
		{backendType: clientBackend},
		{backendType: clientBackend},
		{backendType: clientBackend},
		{backendType: checkpointer},
	})
	insertMaxConnections(t, pool, connID, "400", sampleInstant(1*time.Minute))

	const want = 4.0 // four client backends of the max_connections = 100 in force

	latest := latestValueFor(t, ds, "connection_utilization_percent", connID)
	assertClose(t, "latest", latest, want)

	historical := historicalValuesFor(t, ds, "connection_utilization_percent", connID)
	got, ok := historical[at]
	if !ok {
		t.Fatalf("historical: no row for the snapshot at %v, got %+v", at, historical)
	}
	assertClose(t, "historical", got, want)
	assertClose(t, "historical minus latest", got-latest, 0)
}

// TestConnectionUtilizationPercent_LatestBeforeFirstSetting pins the
// onboarding case for the latest query: an activity sample that predates
// every pg_settings write takes the earliest setting, as the historical
// query does, rather than a later one.
func TestConnectionUtilizationPercent_LatestBeforeFirstSetting(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "conn-util-596-onboard")

	at := sampleInstant(3 * time.Minute)
	insertActivitySnapshot(t, pool, connID, at, []activityRow{
		{backendType: clientBackend},
		{backendType: clientBackend},
	})
	insertMaxConnections(t, pool, connID, "50", sampleInstant(2*time.Minute))
	insertMaxConnections(t, pool, connID, "200", sampleInstant(1*time.Minute))

	const want = 4.0 // two client backends of the earliest max_connections = 50

	latest := latestValueFor(t, ds, "connection_utilization_percent", connID)
	assertClose(t, "latest", latest, want)

	historical := historicalValuesFor(t, ds, "connection_utilization_percent", connID)
	got, ok := historical[at]
	if !ok {
		t.Fatalf("historical: no row for the snapshot at %v, got %+v", at, historical)
	}
	assertClose(t, "historical", got, want)
}

// TestPgStatActivityMetrics_HistoricalMatchesLatest extends the #567
// check to the other pg_stat_activity entries whose historical query
// omitted the latest query's backend_type filter. Each snapshot carries
// client backends plus background processes that satisfy the metric's
// other predicates with a larger value, so an unfiltered historical query
// would report the background figure and disagree with the latest one.
func TestPgStatActivityMetrics_HistoricalMatchesLatest(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	lock := strPtr("Lock")
	active := strPtr("active")
	idleInXact := strPtr("idle in transaction")

	cases := []struct {
		metric string
		rows   []activityRow
		want   float64
	}{
		{
			metric: "pg_stat_activity.count",
			rows: []activityRow{
				{backendType: clientBackend},
				{backendType: clientBackend},
				{backendType: checkpointer},
				{backendType: walWriter},
			},
			want: 2,
		},
		{
			metric: "pg_stat_activity.blocked_count",
			rows: []activityRow{
				{backendType: clientBackend, waitEventType: lock},
				{backendType: clientBackend},
				{backendType: autovacuumWorker, waitEventType: lock},
				{backendType: backgroundWorkerT, waitEventType: lock},
			},
			want: 1,
		},
		{
			metric: "pg_stat_activity.idle_in_transaction_seconds",
			rows: []activityRow{
				{backendType: clientBackend, state: idleInXact, xactAge: 30 * time.Second},
				{backendType: parallelWorker, state: idleInXact, xactAge: 600 * time.Second},
			},
			want: 30,
		},
		{
			metric: "pg_stat_activity.max_query_duration_seconds",
			rows: []activityRow{
				{backendType: clientBackend, state: active, queryAge: 10 * time.Second},
				{backendType: walSender, state: active, queryAge: 3600 * time.Second},
			},
			want: 10,
		},
		{
			metric: "pg_stat_activity.max_xact_duration_seconds",
			rows: []activityRow{
				{backendType: clientBackend, xactAge: 45 * time.Second},
				{backendType: autovacuumWorker, xactAge: 900 * time.Second},
			},
			want: 45,
		},
	}

	for _, c := range cases {
		t.Run(c.metric, func(t *testing.T) {
			resetMetricsTables(t, pool)

			connID := insertConnection(t, pool, "activity-"+c.metric)
			at := sampleInstant(2 * time.Minute)
			insertActivitySnapshot(t, pool, connID, at, c.rows)

			latest := latestValueFor(t, ds, c.metric, connID)
			assertClose(t, "latest", latest, c.want)

			historical := historicalValuesFor(t, ds, c.metric, connID)
			got, ok := historical[at]
			if !ok {
				t.Fatalf("historical: no row for the snapshot at %v, got %+v",
					at, historical)
			}
			assertClose(t, "historical", got, c.want)
		})
	}
}
