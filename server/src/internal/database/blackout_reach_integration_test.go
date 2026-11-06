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
	"sort"
	"testing"
)

// TestBlackoutsReachingConnections checks that a ConnectionIDs filter,
// and so the blackouts in a scoped overview snapshot, keeps only the
// blackouts that reach one of the given connections. A blackout on a
// server, cluster or group outside them must not appear, since the
// snapshot is summarized for callers who cannot see those resources.
func TestBlackoutsReachingConnections(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	// A second group, cluster and connection that the caller cannot
	// see, alongside the fixture's visible ones.
	var hiddenGroup, hiddenCluster, hiddenConn int
	if err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ('grp-hidden') RETURNING id`).Scan(&hiddenGroup); err != nil {
		t.Fatalf("seed hidden group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, 'cls-hidden') RETURNING id`,
		hiddenGroup).Scan(&hiddenCluster); err != nil {
		t.Fatalf("seed hidden cluster: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES ($1, 'conn-hidden') RETURNING id`,
		hiddenCluster).Scan(&hiddenConn); err != nil {
		t.Fatalf("seed hidden connection: %v", err)
	}

	insert := func(scope, reason string, groupID, clusterID, connID any) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
            INSERT INTO blackouts (scope, group_id, cluster_id, connection_id,
                                   reason, start_time, end_time, created_by)
            VALUES ($1, $2, $3, $4, $5, NOW() - INTERVAL '1 hour',
                    NOW() + INTERVAL '1 hour', 'tester')`,
			scope, groupID, clusterID, connID, reason); err != nil {
			t.Fatalf("insert %s blackout: %v", reason, err)
		}
	}
	insert("estate", "estate", nil, nil, nil)
	insert("server", "visible-server", nil, nil, fx.ConnID)
	insert("server", "hidden-server", nil, nil, hiddenConn)
	insert("cluster", "visible-cluster", nil, fx.ClusterID, nil)
	insert("cluster", "hidden-cluster", nil, hiddenCluster, nil)
	insert("group", "visible-group", fx.GroupID, nil, nil)
	insert("group", "hidden-group", hiddenGroup, nil, nil)

	want := []string{"estate", "visible-cluster", "visible-group", "visible-server"}

	result, err := ds.ListBlackouts(ctx, BlackoutFilter{ConnectionIDs: []int{fx.ConnID}})
	if err != nil {
		t.Fatalf("ListBlackouts: %v", err)
	}
	if result.TotalCount != len(want) {
		t.Errorf("TotalCount = %d, want %d", result.TotalCount, len(want))
	}
	got := make([]string, 0, len(result.Blackouts))
	for i := range result.Blackouts {
		got = append(got, result.Blackouts[i].Reason)
	}
	assertReasons(t, "ListBlackouts", got, want)

	snapshot := &EstateSnapshot{}
	ds.gatherBlackoutData(ctx, snapshot, []int{fx.ConnID})
	got = got[:0]
	for _, b := range snapshot.ActiveBlackouts {
		got = append(got, b.Reason)
	}
	assertReasons(t, "gatherBlackoutData", got, want)

	// The estate snapshot still lists every blackout.
	estate := &EstateSnapshot{}
	ds.gatherEstateBlackoutData(ctx, estate)
	if len(estate.ActiveBlackouts) != 7 {
		t.Errorf("estate blackouts = %d, want 7", len(estate.ActiveBlackouts))
	}
}

func assertReasons(t *testing.T, label string, got, want []string) {
	t.Helper()
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	if len(sorted) != len(want) {
		t.Fatalf("%s reasons = %v, want %v", label, sorted, want)
	}
	for i := range want {
		if sorted[i] != want[i] {
			t.Fatalf("%s reasons = %v, want %v", label, sorted, want)
		}
	}
}
