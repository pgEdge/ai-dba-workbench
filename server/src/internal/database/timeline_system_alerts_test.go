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
	"strings"
	"testing"
)

// TestAppendTimelineCondition covers both shapes of WHERE clause.
func TestAppendTimelineCondition(t *testing.T) {
	if got := appendTimelineCondition("", "x = 1"); got != "WHERE x = 1" {
		t.Errorf("empty clause: got %q", got)
	}
	if got := appendTimelineCondition("WHERE y = 2", "x = 1"); got != "WHERE y = 2 AND x = 1" {
		t.Errorf("existing clause: got %q", got)
	}
}

// TestTimelineAlertCountQueries_ExcludeSystemAlerts proves every alert
// count query leaves out system alerts (GitHub issue #582), matching
// the event queries, which join connections, whatever filter precedes.
func TestTimelineAlertCountQueries_ExcludeSystemAlerts(t *testing.T) {
	builders := map[string]struct {
		build func(string) string
		want  string
	}{
		"fired":        {buildAlertFiredCountQuery, "connection_id IS NOT NULL"},
		"cleared":      {buildAlertClearedCountQuery, "connection_id IS NOT NULL"},
		"acknowledged": {buildAlertAcknowledgedCountQuery, "a.connection_id IS NOT NULL"},
	}
	for name, b := range builders {
		for _, where := range []string{"", "WHERE connection_id = $1 AND event_time >= $2"} {
			got := b.build(where)
			if !strings.Contains(got, b.want) {
				t.Errorf("%s (where %q): missing %q in %s", name, where, b.want, got)
			}
			if strings.Count(got, "WHERE") != 1 {
				t.Errorf("%s (where %q): want one WHERE in %s", name, where, got)
			}
		}
	}
	ack := buildAlertAcknowledgedCountQuery("WHERE connection_id = $1")
	if strings.Contains(ack, "a.a.connection_id") {
		t.Errorf("acknowledged query double-qualified: %s", ack)
	}
}
