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

import "testing"

func TestCountServerStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		server EstateServerSummary
		want   [3]int // online, warning, offline
	}{
		{"offline with alerts", EstateServerSummary{Status: "offline", ActiveAlertCount: 2}, [3]int{0, 0, 1}},
		{"stale with no alerts", EstateServerSummary{Status: "warning"}, [3]int{0, 1, 0}},
		{"online with alerts", EstateServerSummary{Status: "online", ActiveAlertCount: 1}, [3]int{0, 1, 0}},
		{"online with no alerts", EstateServerSummary{Status: "online"}, [3]int{1, 0, 0}},
		{"unknown status", EstateServerSummary{Status: "unknown"}, [3]int{1, 0, 0}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var snap EstateSnapshot
			countServerStatus(&snap, &tc.server)
			got := [3]int{snap.ServerOnline, snap.ServerWarning, snap.ServerOffline}
			if got != tc.want {
				t.Errorf("online/warning/offline = %v, want %v", got, tc.want)
			}
		})
	}
}
