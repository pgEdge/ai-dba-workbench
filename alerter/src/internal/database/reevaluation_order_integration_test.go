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
)

// The queries behind the re-evaluation prompt break ties on their sort
// key by id, so rows sharing a timestamp or a name always come back in the
// same order and cannot change the prompt's fingerprint (GitHub issue
// #575). Each test below inserts rows in id order, which is also their
// order on disk, and then expects an order a plain sort on the key alone
// would not give.

// tiedAlertTime is the timestamp every tied row is given.
const tiedAlertTime = "2026-01-01 00:00:00+00"

// TestAlertQueriesBreakTriggeredAtTiesByID checks that alerts sharing a
// triggered_at come back newest id first from GetAlertsByConnection and
// GetAlertsByCluster.
func TestAlertQueriesBreakTriggeredAtTiesByID(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	_, clusterID := insertGroupAndCluster(t, pool, "g-tie", "c-tie")
	own := insertTestConnectionInCluster(t, pool, "tie-own", clusterID)
	peer := insertTestConnectionInCluster(t, pool, "tie-peer", clusterID)
	ruleID := insertTestRule(t, pool, "tie_rule", "x", ">", 1, "warning", true)

	ownLow := insertActiveThresholdAlert(t, pool, own, ruleID, nil)
	ownHigh := insertActiveThresholdAlert(t, pool, own, ruleID, nil)
	peerLow := insertActiveThresholdAlert(t, pool, peer, ruleID, nil)
	peerHigh := insertActiveThresholdAlert(t, pool, peer, ruleID, nil)
	if _, err := pool.Exec(ctx, `UPDATE alerts SET triggered_at = $1`, tiedAlertTime); err != nil {
		t.Fatalf("tie triggered_at: %v", err)
	}

	byConn, err := ds.GetAlertsByConnection(ctx, own)
	if err != nil {
		t.Fatalf("GetAlertsByConnection: %v", err)
	}
	if len(byConn) != 2 || byConn[0].ID != ownHigh || byConn[1].ID != ownLow {
		t.Errorf("GetAlertsByConnection ids = %v, want [%d %d]", alertIDs(byConn), ownHigh, ownLow)
	}

	byCluster, err := ds.GetAlertsByCluster(ctx, own)
	if err != nil {
		t.Fatalf("GetAlertsByCluster: %v", err)
	}
	if len(byCluster) != 2 || byCluster[0].ID != peerHigh || byCluster[1].ID != peerLow {
		t.Errorf("GetAlertsByCluster ids = %v, want [%d %d]", alertIDs(byCluster), peerHigh, peerLow)
	}
}

// TestAcknowledgementQueriesBreakAcknowledgedAtTiesByID checks that
// acknowledgements sharing an acknowledged_at are ordered newest id first,
// both for the latest acknowledgement GetAcknowledgedAnomalyAlerts shows
// and for GetAcknowledgmentHistoryForMetric.
func TestAcknowledgementQueriesBreakAcknowledgedAtTiesByID(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "tie-ack-conn")

	insertAlert := func() int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO alerts (alert_type, connection_id, severity, title, description,
			    status, metric_name)
			VALUES ('anomaly', $1, 'warning', 't', 'd', 'acknowledged', 'm_tie')
			RETURNING id
		`, connID).Scan(&id); err != nil {
			t.Fatalf("insert alert: %v", err)
		}
		return id
	}
	insertAck := func(alertID int64, message string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO alert_acknowledgments (alert_id, acknowledged_by,
			    acknowledged_at, acknowledge_type, message)
			VALUES ($1, 'tester', $2, 'acknowledge', $3)
		`, alertID, tiedAlertTime, message); err != nil {
			t.Fatalf("insert acknowledgement: %v", err)
		}
	}

	// Two acknowledgements of one alert at the same moment: the later
	// one is the one shown.
	current := insertAlert()
	insertAck(current, "first")
	insertAck(current, "second")

	due, err := ds.GetAcknowledgedAnomalyAlerts(ctx, 60, 10)
	if err != nil {
		t.Fatalf("GetAcknowledgedAnomalyAlerts: %v", err)
	}
	if len(due) != 1 || due[0].AckMessage == nil || *due[0].AckMessage != "second" {
		var got *string
		if len(due) > 0 {
			got = due[0].AckMessage
		}
		t.Errorf("%d due alerts, latest acknowledgement %v; want 1 with message %q",
			len(due), derefString(got), "second")
	}

	// Two earlier alerts on the metric acknowledged at the same moment:
	// the history lists the later acknowledgement first.
	older := insertAlert()
	insertAck(older, "older")
	newer := insertAlert()
	insertAck(newer, "newer")

	history, err := ds.GetAcknowledgmentHistoryForMetric(ctx, "m_tie", connID, current, 10)
	if err != nil {
		t.Fatalf("GetAcknowledgmentHistoryForMetric: %v", err)
	}
	if len(history) != 2 || history[0].ID != newer || history[1].ID != older {
		ids := make([]int64, 0, len(history))
		for _, h := range history {
			ids = append(ids, h.ID)
		}
		t.Errorf("history ids = %v, want [%d %d]", ids, newer, older)
	}
}

// TestGetClusterPeersBreaksNameTiesByID checks that peers sharing a name
// are ordered by id. The lower id is renamed last, which moves its row
// after the other on disk, so a sort on the name alone would list the
// higher id first.
func TestGetClusterPeersBreaksNameTiesByID(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	_, clusterID := insertGroupAndCluster(t, pool, "g-peer-tie", "c-peer-tie")
	self := insertTestConnectionInCluster(t, pool, "self", clusterID)
	low := insertTestConnectionInCluster(t, pool, "renamed", clusterID)
	high := insertTestConnectionInCluster(t, pool, "replica", clusterID)
	if _, err := pool.Exec(ctx, `UPDATE connections SET name = 'replica' WHERE id = $1`, low); err != nil {
		t.Fatalf("rename peer: %v", err)
	}

	peers, err := ds.GetClusterPeers(ctx, self)
	if err != nil {
		t.Fatalf("GetClusterPeers: %v", err)
	}
	if len(peers) != 2 || peers[0].ConnectionID != low || peers[1].ConnectionID != high {
		ids := make([]int, 0, len(peers))
		for _, p := range peers {
			ids = append(ids, p.ConnectionID)
		}
		t.Errorf("peer ids = %v, want [%d %d]", ids, low, high)
	}
}

// derefString returns *s, or "<nil>" when s is nil.
func derefString(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// alertIDs returns the IDs of the given alerts, in order.
func alertIDs(alerts []*Alert) []int64 {
	ids := make([]int64, 0, len(alerts))
	for _, a := range alerts {
		ids = append(ids, a.ID)
	}
	return ids
}
