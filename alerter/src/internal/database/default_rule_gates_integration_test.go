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
)

// This file covers GitHub issue #616: four built-in rules fired on
// conditions nobody could act on, so their metric queries gained gates.
// Each test seeds the deadRuleSchema fixture with data on both sides of
// a gate and asserts which connections the metric still reports. A
// connection the gate excludes must emit no row at all, because each of
// these entries clears its alert when the row is absent.

// Seed statements for the tables only this file writes to.
const (
	insertGateNodeRoleSQL = `
        INSERT INTO metrics.pg_node_role
            (connection_id, is_in_recovery, collected_at)
        VALUES ($1, $2, $3)
    `

	insertGateStatDatabaseSQL = `
        INSERT INTO metrics.pg_stat_database
            (connection_id, database_name, datname, blks_hit, blks_read,
             collected_at)
        VALUES ($1, 'appdb', 'appdb', $2, $3, $4)
    `
)

// gateValuesByConnection returns the metric's value per connection.
func gateValuesByConnection(t *testing.T, ds *Datastore,
	metric string) map[int]float64 {
	t.Helper()
	values, err := ds.GetLatestMetricValues(context.Background(), metric)
	if err != nil {
		t.Fatalf("GetLatestMetricValues(%s) failed: %v", metric, err)
	}
	got := make(map[int]float64, len(values))
	for _, v := range values {
		if _, dup := got[v.ConnectionID]; dup {
			t.Fatalf("metric %s returned more than one row for connection %d",
				metric, v.ConnectionID)
		}
		got[v.ConnectionID] = v.Value
	}
	return got
}

// assertGateRows checks that exactly the wanted connections report.
func assertGateRows(t *testing.T, got map[int]float64,
	want map[int]bool, names map[int]string) {
	t.Helper()
	for id, name := range names {
		_, present := got[id]
		if present != want[id] {
			t.Errorf("%s: row present = %v, want %v (value %v)",
				name, present, want[id], got[id])
		}
	}
}

// TestDefaultRuleGatesCacheHitReadRate proves the read-rate gate on
// pg_stat_database.cache_hit_ratio: an interval must read at least 100
// blocks per second as well as move 10000 blocks in all. Samples are
// exactly 300 seconds apart, so the boundary is 30000 reads.
func TestDefaultRuleGatesCacheHitReadRate(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	base := time.Now().Truncate(time.Second)
	at := []time.Time{
		base.Add(-12 * time.Minute),
		base.Add(-7 * time.Minute),
		base.Add(-2 * time.Minute),
	}

	// seed writes cumulative counters growing by the given per-interval
	// hits and reads.
	seed := func(connID int, hits, reads []int64) {
		var h, r int64
		for i := range at {
			if i > 0 {
				h += hits[i-1]
				r += reads[i-1]
			}
			execDeadRuleSeed(t, pool, insertGateStatDatabaseSQL,
				connID, h, r, at[i])
		}
	}

	busy := insertDeadRuleConnection(t, pool, "busy-poor-cache")
	seed(busy, []int64{20_000, 20_000}, []int64{60_000, 60_000})

	// The same poor ratio on an idle database: 26000 blocks clears the
	// total floor, but 6000 reads in 300 seconds is 20 per second.
	idle := insertDeadRuleConnection(t, pool, "idle-poor-cache")
	seed(idle, []int64{20_000, 20_000}, []int64{6_000, 6_000})

	atRate := insertDeadRuleConnection(t, pool, "exactly-100-per-second")
	seed(atRate, []int64{10_000, 10_000}, []int64{30_000, 30_000})

	belowRate := insertDeadRuleConnection(t, pool, "just-under-100-per-second")
	seed(belowRate, []int64{10_000, 10_000}, []int64{29_999, 29_999})

	// Busy in the older interval only: the newest qualifying interval
	// still decides, so one quiet interval does not drop the database.
	quietened := insertDeadRuleConnection(t, pool, "busy-then-quiet")
	seed(quietened, []int64{20_000, 50_000}, []int64{60_000, 1_000})

	got := gateValuesByConnection(t, ds, "pg_stat_database.cache_hit_ratio")
	assertGateRows(t, got,
		map[int]bool{busy: true, atRate: true, quietened: true},
		map[int]string{
			busy:      "busy database",
			idle:      "idle database",
			atRate:    "100 reads per second",
			belowRate: "under 100 reads per second",
			quietened: "busy then quiet",
		})

	if want := 20_000.0 / 80_000.0 * 100; got[busy] != want {
		t.Errorf("busy database ratio = %v, want %v", got[busy], want)
	}
	if got[busy] >= 50 {
		t.Errorf("busy database ratio %v does not cross the new default of 50",
			got[busy])
	}
	if want := 20_000.0 / 80_000.0 * 100; got[quietened] != want {
		t.Errorf("busy then quiet ratio = %v, want the older busy "+
			"interval's %v", got[quietened], want)
	}
}

// TestDefaultRuleGatesCacheHitReadRateHistorical checks that the
// historical query, which seeds anomaly baselines, applies the same
// read-rate gate as the latest query.
func TestDefaultRuleGatesCacheHitReadRateHistorical(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	base := time.Now().Truncate(time.Second)
	idle := insertDeadRuleConnection(t, pool, "idle-history")
	busy := insertDeadRuleConnection(t, pool, "busy-history")
	for i, offset := range []time.Duration{-12, -7, -2} {
		at := base.Add(offset * time.Minute)
		execDeadRuleSeed(t, pool, insertGateStatDatabaseSQL,
			idle, int64(i*20_000), int64(i*6_000), at)
		execDeadRuleSeed(t, pool, insertGateStatDatabaseSQL,
			busy, int64(i*20_000), int64(i*60_000), at)
	}

	values, err := ds.GetHistoricalMetricValues(context.Background(),
		"pg_stat_database.cache_hit_ratio", 1)
	if err != nil {
		t.Fatalf("GetHistoricalMetricValues failed: %v", err)
	}
	counts := map[int]int{}
	for _, v := range values {
		counts[v.ConnectionID]++
	}
	if counts[idle] != 0 {
		t.Errorf("idle database contributed %d historical rows, want 0",
			counts[idle])
	}
	if counts[busy] != 2 {
		t.Errorf("busy database contributed %d historical rows, want 2",
			counts[busy])
	}
}

// TestDefaultRuleGatesDeadTupleFloor proves that dead_tuple_percent only
// considers tables with at least 10000 dead tuples, judged on each
// table's newest sample.
func TestDefaultRuleGatesDeadTupleFloor(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	recent := time.Now().Add(-2 * time.Minute)
	older := time.Now().Add(-10 * time.Minute)

	large := insertDeadRuleConnection(t, pool, "large-dead-count")
	execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, large, "appdb",
		"orders", int64(40_000), int64(60_000), nil, recent)

	// 80 percent dead, but only 8000 dead tuples.
	small := insertDeadRuleConnection(t, pool, "small-dead-count")
	execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, small, "appdb",
		"orders", int64(2_000), int64(8_000), nil, recent)

	atFloor := insertDeadRuleConnection(t, pool, "exactly-10000-dead")
	execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, atFloor, "appdb",
		"orders", int64(5_000), int64(10_000), nil, recent)

	// Over the floor ten minutes ago, vacuumed since: the newest sample
	// decides, so the table drops out at once.
	vacuumed := insertDeadRuleConnection(t, pool, "vacuumed")
	execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, vacuumed, "appdb",
		"orders", int64(40_000), int64(60_000), nil, older)
	execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, vacuumed, "appdb",
		"orders", int64(100_000), int64(100), nil, recent)

	got := gateValuesByConnection(t, ds,
		"pg_stat_all_tables.dead_tuple_percent")
	assertGateRows(t, got,
		map[int]bool{large: true, atFloor: true},
		map[int]string{
			large:    "60000 dead tuples",
			small:    "8000 dead tuples",
			atFloor:  "10000 dead tuples",
			vacuumed: "vacuumed table",
		})
	if got[large] != 60 {
		t.Errorf("large table dead percent = %v, want 60", got[large])
	}
	if got[large] <= 50 {
		t.Errorf("large table dead percent %v does not cross the new "+
			"default of 50", got[large])
	}
}

// TestDefaultRuleGatesAutovacuumSustained proves that
// table_last_autovacuum_hours only counts a table that has been past
// its autovacuum trigger in every sample for at least 30 minutes.
func TestDefaultRuleGatesAutovacuumSustained(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	dayOld := time.Now().Add(-24 * time.Hour)
	minutesAgo := func(m int) time.Time {
		return time.Now().Add(-time.Duration(m) * time.Minute)
	}
	// With the default settings the trigger for 1000 live tuples is
	// 50 + 0.2 * 1000 = 250 dead tuples.
	const past, under = int64(5_000), int64(100)

	seed := func(name string, samples map[int]int64) int {
		id := insertDeadRuleConnection(t, pool, name)
		for minute, dead := range samples {
			execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, id, "appdb",
				"orders", int64(1_000), dead, dayOld, minutesAgo(minute))
		}
		return id
	}

	sustained := seed("sustained", map[int]int64{40: past, 20: past, 1: past})
	crossed := seed("crossed-recently", map[int]int64{40: under, 20: past, 1: past})
	dipped := seed("dipped-in-between", map[int]int64{40: past, 20: under, 1: past})
	recovered := seed("recovered", map[int]int64{40: past, 20: past, 1: under})
	newHistory := seed("history-starts-late", map[int]int64{20: past, 1: past})
	stale := seed("no-recent-sample", map[int]int64{45: past, 35: past, 20: past})

	got := gateValuesByConnection(t, ds, "table_last_autovacuum_hours")
	assertGateRows(t, got,
		map[int]bool{sustained: true},
		map[int]string{
			sustained:  "past the trigger for 40 minutes",
			crossed:    "crossed the trigger 20 minutes ago",
			dipped:     "under the trigger 20 minutes ago",
			recovered:  "under the trigger now",
			newHistory: "no sample before the 30 minute mark",
			stale:      "no sample in the last 15 minutes",
		})
	if v := got[sustained]; v < 23.9 || v > 24.1 {
		t.Errorf("sustained table hours since autovacuum = %v, want about 24", v)
	}
}

// TestDefaultRuleGatesCheckpointRecovery proves that
// checkpoints_req_delta skips connections whose newest recovery state
// in the last hour is in recovery, and still evaluates primaries and
// connections with no recovery state at all.
func TestDefaultRuleGatesCheckpointRecovery(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	now := time.Now()
	minutesAgo := func(m int) time.Time {
		return now.Add(-time.Duration(m) * time.Minute)
	}

	// Each connection requests 30 checkpoints over the hour, well over
	// the default of 12.
	seed := func(name string, roles map[int]bool) int {
		id := insertDeadRuleConnection(t, pool, name)
		for i, requested := range []int64{1000, 1006, 1012, 1018, 1024, 1030} {
			execDeadRuleSeed(t, pool, insertDeadRuleCheckpointerSQL, id,
				requested, int64(10), minutesAgo(50-i*10))
		}
		for minute, inRecovery := range roles {
			execDeadRuleSeed(t, pool, insertGateNodeRoleSQL, id,
				inRecovery, minutesAgo(minute))
		}
		return id
	}

	primary := seed("primary", map[int]bool{30: false, 2: false})
	noRole := seed("no-role-sample", nil)
	standby := seed("standby", map[int]bool{30: true, 2: true})
	promoted := seed("promoted", map[int]bool{30: true, 2: false})
	demoted := seed("demoted", map[int]bool{30: false, 2: true})
	// A recovery state older than the hour is not trusted.
	staleStandby := seed("stale-standby-sample", map[int]bool{90: true})

	got := gateValuesByConnection(t, ds,
		"pg_stat_checkpointer.checkpoints_req_delta")
	assertGateRows(t, got,
		map[int]bool{primary: true, noRole: true, promoted: true,
			staleStandby: true},
		map[int]string{
			primary:      "primary",
			noRole:       "no recovery state",
			standby:      "standby",
			promoted:     "promoted standby",
			demoted:      "demoted primary",
			staleStandby: "recovery state over an hour old",
		})
	for _, id := range []int{primary, noRole, promoted} {
		if got[id] != 30 {
			t.Errorf("connection %d requested checkpoints = %v, want 30",
				id, got[id])
		}
	}
}
