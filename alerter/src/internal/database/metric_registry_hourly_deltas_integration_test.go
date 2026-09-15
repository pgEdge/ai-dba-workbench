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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// hourlyDeltaMetrics lists the two registry entries that report the sum
// of positive per-sample counter deltas over a fixed one hour window
// (GitHub issue #409). Both read metrics.pg_stat_database and share a
// query shape, so every test below runs against each of them.
var hourlyDeltaMetrics = []string{
	"pg_stat_database.deadlocks_delta",
	"pg_stat_database.temp_files_delta",
}

// insertCounterSamples writes one metrics.pg_stat_database row per
// counter value for the given connection and database. The deadlocks and
// temp_files columns receive the same value so the same fixture drives
// both hourly delta metrics. Sample i is stamped at base + i*step.
func insertCounterSamples(t *testing.T, pool *pgxpool.Pool, connID int,
	dbName string, base time.Time, step time.Duration, counters []int64) {
	t.Helper()

	for i, c := range counters {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO metrics.pg_stat_database
			    (connection_id, database_name, datname, deadlocks, temp_files, collected_at)
			VALUES ($1, $2, $2, $3, $3, $4)
		`, connID, dbName, c, base.Add(time.Duration(i)*step)); err != nil {
			t.Fatalf("failed to insert pg_stat_database sample %d: %v", i, err)
		}
	}
}

// latestValueFor returns the latest value the registry reports for the
// given metric and connection, failing the test when the connection is
// missing or reported more than once.
func latestValueFor(t *testing.T, ds *Datastore, metric string, connID int) float64 {
	t.Helper()

	values, err := ds.GetLatestMetricValues(context.Background(), metric)
	if err != nil {
		t.Fatalf("GetLatestMetricValues(%s) failed: %v", metric, err)
	}
	var found []MetricValue
	for _, v := range values {
		if v.ConnectionID == connID {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s: expected exactly one row for connection %d, got %d: %+v",
			metric, connID, len(found), found)
	}
	return found[0].Value
}

// TestMetricRegistry_HourlyDeltas_IntervalIndependent verifies that the
// hourly sum does not depend on how often the probe sampled the counter:
// three samples 300 s apart rising by 3 then 2 report 5, and seven
// samples 100 s apart whose increases also total 5 report 5.
func TestMetricRegistry_HourlyDeltas_IntervalIndependent(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	now := time.Now().UTC()
	slow := insertConnection(t, pool, "hourly-delta-slow-probe")
	fast := insertConnection(t, pool, "hourly-delta-fast-probe")

	// Two per-sample deltas of +3 and +2 over 600 s.
	insertCounterSamples(t, pool, slow, "appdb", now.Add(-10*time.Minute),
		5*time.Minute, []int64{10, 13, 15})
	// Six per-sample deltas (1, 1, 0, 2, 0, 1) over 600 s.
	insertCounterSamples(t, pool, fast, "appdb", now.Add(-10*time.Minute),
		100*time.Second, []int64{10, 11, 12, 12, 14, 14, 15})

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			if got := latestValueFor(t, ds, metric, slow); got != 5 {
				t.Errorf("slow probe value = %v, want 5", got)
			}
			if got := latestValueFor(t, ds, metric, fast); got != 5 {
				t.Errorf("fast probe value = %v, want 5", got)
			}
		})
	}
}

// TestMetricRegistry_HourlyDeltas_CounterResetContributesZero verifies
// that a counter falling between samples (a stats reset) adds nothing to
// the hourly sum rather than a negative amount.
func TestMetricRegistry_HourlyDeltas_CounterResetContributesZero(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	now := time.Now().UTC()
	connID := insertConnection(t, pool, "hourly-delta-reset")
	// Deltas: +5, -13 (reset, counts 0), +2 = 7.
	insertCounterSamples(t, pool, connID, "appdb", now.Add(-15*time.Minute),
		5*time.Minute, []int64{10, 15, 2, 4})

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			if got := latestValueFor(t, ds, metric, connID); got != 7 {
				t.Errorf("value = %v, want 7", got)
			}
		})
	}
}

// TestMetricRegistry_HourlyDeltas_SingleSampleReportsZero verifies that a
// connection with one sample inside the window is still reported, with a
// value of 0, rather than dropping out of the result set. The second
// case has an older sample that falls outside the hour, which must not
// be paired with the in-window sample.
func TestMetricRegistry_HourlyDeltas_SingleSampleReportsZero(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	now := time.Now().UTC()
	lone := insertConnection(t, pool, "hourly-delta-single")
	insertCounterSamples(t, pool, lone, "appdb", now.Add(-1*time.Minute),
		time.Minute, []int64{42})

	stale := insertConnection(t, pool, "hourly-delta-stale-prev")
	insertCounterSamples(t, pool, stale, "appdb", now.Add(-90*time.Minute),
		time.Minute, []int64{0})
	insertCounterSamples(t, pool, stale, "appdb", now.Add(-1*time.Minute),
		time.Minute, []int64{100})

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			if got := latestValueFor(t, ds, metric, lone); got != 0 {
				t.Errorf("single-sample value = %v, want 0", got)
			}
			if got := latestValueFor(t, ds, metric, stale); got != 0 {
				t.Errorf("stale-previous value = %v, want 0", got)
			}
		})
	}
}

// TestMetricRegistry_HourlyDeltas_PerDatabase verifies that the sum is
// kept separate per database on the same connection and that template
// databases are excluded.
func TestMetricRegistry_HourlyDeltas_PerDatabase(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	now := time.Now().UTC()
	connID := insertConnection(t, pool, "hourly-delta-per-db")
	insertCounterSamples(t, pool, connID, "alpha", now.Add(-10*time.Minute),
		5*time.Minute, []int64{0, 4, 9})
	insertCounterSamples(t, pool, connID, "beta", now.Add(-10*time.Minute),
		5*time.Minute, []int64{0, 1, 1})
	insertCounterSamples(t, pool, connID, "template1", now.Add(-10*time.Minute),
		5*time.Minute, []int64{0, 50, 100})

	want := map[string]float64{"alpha": 9, "beta": 1}
	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			values, err := ds.GetLatestMetricValues(context.Background(), metric)
			if err != nil {
				t.Fatalf("GetLatestMetricValues failed: %v", err)
			}
			got := map[string]float64{}
			for _, v := range values {
				if v.ConnectionID != connID || v.DatabaseName == nil {
					continue
				}
				got[*v.DatabaseName] = v.Value
			}
			if len(got) != len(want) {
				t.Fatalf("databases reported = %v, want %v", got, want)
			}
			for db, w := range want {
				if got[db] != w {
					t.Errorf("%s value = %v, want %v", db, got[db], w)
				}
			}
		})
	}
}

// TestMetricRegistry_HourlyDeltas_HistoricalHourlyBuckets verifies the
// baseline feed: one row per connection, database and hour bucket, whose
// value is the sum of positive per-sample deltas inside that hour and
// whose collected_at is the bucket start. The delta is computed before
// bucketing, so the first sample of an hour is paired with the last
// sample of the previous hour, and a reset inside a bucket adds zero.
func TestMetricRegistry_HourlyDeltas_HistoricalHourlyBuckets(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "hourly-delta-historical")
	h0 := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	h1 := h0.Add(time.Hour)

	// Hour h0: 10 -> 12 -> 15 gives 2 + 3 = 5 (the first sample has no
	// predecessor and contributes nothing).
	insertCounterSamples(t, pool, connID, "appdb", h0, 10*time.Minute,
		[]int64{10, 12, 15})
	// Hour h1: 15 -> 16 is +1 across the hour boundary, then 16 -> 10 is
	// a reset that counts 0, so the bucket totals 1.
	insertCounterSamples(t, pool, connID, "appdb", h1.Add(5*time.Minute),
		35*time.Minute, []int64{16, 10})

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			rows, err := ds.GetHistoricalMetricValues(context.Background(), metric, 1)
			if err != nil {
				t.Fatalf("GetHistoricalMetricValues failed: %v", err)
			}
			var got []HistoricalMetricValue
			for _, r := range rows {
				if r.ConnectionID == connID {
					got = append(got, r)
				}
			}
			if len(got) != 2 {
				t.Fatalf("expected 2 hourly buckets, got %d: %+v", len(got), got)
			}
			want := []struct {
				bucket  time.Time
				value   float64
				samples int64
			}{{h0, 5, 3}, {h1, 1, 2}}
			for i, w := range want {
				if !got[i].CollectedAt.Equal(w.bucket) {
					t.Errorf("bucket[%d] collected_at = %s, want %s",
						i, got[i].CollectedAt.UTC(), w.bucket)
				}
				if got[i].Value != w.value {
					t.Errorf("bucket[%d] value = %v, want %v", i, got[i].Value, w.value)
				}
				if got[i].SampleCount != w.samples {
					t.Errorf("bucket[%d] sample count = %d, want %d",
						i, got[i].SampleCount, w.samples)
				}
				if got[i].DatabaseName == nil || *got[i].DatabaseName != "appdb" {
					t.Errorf("bucket[%d] database = %v, want appdb", i, got[i].DatabaseName)
				}
			}
		})
	}
}

// historicalRowsFor returns the historical rows the registry reports for
// the given metric and connection, in query order.
func historicalRowsFor(t *testing.T, ds *Datastore, metric string, connID int) []HistoricalMetricValue {
	t.Helper()

	rows, err := ds.GetHistoricalMetricValues(context.Background(), metric, 1)
	if err != nil {
		t.Fatalf("GetHistoricalMetricValues(%s) failed: %v", metric, err)
	}
	var got []HistoricalMetricValue
	for _, r := range rows {
		if r.ConnectionID == connID {
			got = append(got, r)
		}
	}
	return got
}

// TestMetricRegistry_HourlyDeltas_HistoricalKeepsZeroBucket verifies that
// the historical query filters inside the aggregate, as the latest query
// does, so a bucket whose only sample has no predecessor still produces a
// row with a value of 0. A newly onboarded connection with a single
// sample is the case that bites: filtering in a WHERE clause instead
// removed the row and the connection contributed no baseline data at all.
func TestMetricRegistry_HourlyDeltas_HistoricalKeepsZeroBucket(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "hourly-delta-zero-bucket")
	h0 := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	insertCounterSamples(t, pool, connID, "appdb", h0.Add(5*time.Minute),
		time.Minute, []int64{42})

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			got := historicalRowsFor(t, ds, metric, connID)
			if len(got) != 1 {
				t.Fatalf("expected 1 bucket for a single-sample connection, got %d: %+v",
					len(got), got)
			}
			if !got[0].CollectedAt.Equal(h0) {
				t.Errorf("bucket collected_at = %s, want %s", got[0].CollectedAt.UTC(), h0)
			}
			if got[0].Value != 0 {
				t.Errorf("bucket value = %v, want 0", got[0].Value)
			}
			if got[0].SampleCount != 1 {
				t.Errorf("bucket sample count = %d, want 1", got[0].SampleCount)
			}
		})
	}
}

// TestMetricRegistry_HourlyDeltas_HistoricalBucketsAreUTC verifies that
// the hour buckets are cut on the UTC value of collected_at rather than
// in the session's TimeZone. Nothing pins the alerter's pool to UTC, so
// on a server whose timezone GUC has a fractional offset a plain
// date_trunc('hour', collected_at) would cut the hour at half past and
// split one UTC hour across two buckets. Asia/Kolkata is +05:30 all year,
// which makes that failure deterministic.
func TestMetricRegistry_HourlyDeltas_HistoricalBucketsAreUTC(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastoreInTimeZone(t, "Asia/Kolkata")
	defer cleanup()

	connID := insertConnection(t, pool, "hourly-delta-session-tz")
	h0 := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)

	// Three samples inside one UTC hour, straddling the half hour where
	// a +05:30 session would cut a bucket boundary.
	insertCounterSamples(t, pool, connID, "appdb", h0.Add(5*time.Minute),
		0, []int64{10})
	insertCounterSamples(t, pool, connID, "appdb", h0.Add(20*time.Minute),
		0, []int64{12})
	insertCounterSamples(t, pool, connID, "appdb", h0.Add(50*time.Minute),
		0, []int64{15})

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			got := historicalRowsFor(t, ds, metric, connID)
			if len(got) != 1 {
				t.Fatalf("expected 1 UTC hour bucket, got %d: %+v", len(got), got)
			}
			if !got[0].CollectedAt.Equal(h0) {
				t.Errorf("bucket collected_at = %s, want the UTC hour %s",
					got[0].CollectedAt.UTC(), h0)
			}
			if got[0].Value != 5 {
				t.Errorf("bucket value = %v, want 5", got[0].Value)
			}
			if got[0].SampleCount != 3 {
				t.Errorf("bucket sample count = %d, want 3", got[0].SampleCount)
			}
		})
	}
}

// TestMetricRegistry_HourlyDeltas_HistoricalQueryError verifies that a
// failure to run the bucketed historical query is reported rather than
// swallowed as an empty baseline feed.
func TestMetricRegistry_HourlyDeltas_HistoricalQueryError(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		`DROP TABLE metrics.pg_stat_database`); err != nil {
		t.Fatalf("failed to drop metrics.pg_stat_database: %v", err)
	}

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			if _, err := ds.GetHistoricalMetricValues(
				context.Background(), metric, 1); err == nil {
				t.Error("expected an error with the metrics table dropped, got none")
			}
		})
	}
}

// TestMetricRegistry_HourlyDeltas_HistoricalScanError verifies that a row
// the scanner cannot read is reported as an error. database_name is
// scanned into a plain string, so a row whose database_name is NULL
// while datname is set (which the query's filters allow) fails the scan.
func TestMetricRegistry_HourlyDeltas_HistoricalScanError(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	connID := insertConnection(t, pool, "hourly-delta-scan-error")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.pg_stat_database
		    (connection_id, database_name, datname, deadlocks, temp_files, collected_at)
		VALUES ($1, NULL, 'appdb', 1, 1, NOW() - INTERVAL '10 minutes')
	`, connID); err != nil {
		t.Fatalf("failed to insert a row with a null database_name: %v", err)
	}

	for _, metric := range hourlyDeltaMetrics {
		t.Run(metric, func(t *testing.T) {
			if _, err := ds.GetHistoricalMetricValues(
				context.Background(), metric, 1); err == nil {
				t.Error("expected a scan error for a null database_name, got none")
			}
		})
	}
}
