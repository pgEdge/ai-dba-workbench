/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package anomaly

import (
	"testing"

	"github.com/pgedge/ai-workbench/pkg/coldfrontsql"
)

// TestHistoricalSQLColdFrontShapes fails when a baselined metric's
// HistoricalSQL, or a shared fragment the alerter's latest-value queries
// embed, reintroduces a shape DuckDB cannot run on a ColdFront datastore:
// an uncast bound parameter, an array parameter or ANY(), an uncast
// avg(), a PostgreSQL-only type or function such as pg_lsn, or an
// infinite timestamp. The alerter and the server both run these queries,
// so the guard sits beside them rather than in either consumer. GitHub
// issue #755.
func TestHistoricalSQLColdFrontShapes(t *testing.T) {
	defs := BaselineMetrics()
	if len(defs) == 0 {
		t.Fatal("BaselineMetrics() is empty")
	}
	for _, def := range defs {
		for _, p := range coldfrontsql.Violations(def.HistoricalSQL) {
			t.Errorf("%s HistoricalSQL: %s", def.Name, p)
		}
	}

	fragments := map[string]string{
		"CPUBusyPercentExpr":        CPUBusyPercentExpr,
		"PseudoFilesystemExclusion": PseudoFilesystemExclusion,
		"ReplicationLagBytesExpr":   ReplicationLagBytesExpr,
	}
	for name, sql := range fragments {
		for _, p := range coldfrontsql.Violations(sql) {
			t.Errorf("%s: %s", name, p)
		}
	}
}
