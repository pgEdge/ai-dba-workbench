/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import "testing"

// coldFrontRemappedColumns lists every probe column whose type a pgEdge
// ColdFront datastore stores differently, with the type information_schema
// reports for it on plain PostgreSQL and on ColdFront. It is taken from
// the column specification and mappedTypes in collector/src/coldfront on
// the collector's ColdFront branch: oid and xid become bigint, inet,
// "char" and name become text, and numeric becomes numeric(38,0), which
// information_schema reports as numeric either way.
var coldFrontRemappedColumns = []struct {
	probe, column, pgType, coldFrontType string
}{
	{"pg_database", "datdba", "oid", "bigint"},
	{"pg_database", "datlocprovider", `"char"`, "text"},
	{"pg_database", "datfrozenxid", "xid", "bigint"},
	{"pg_database", "datminmxid", "xid", "bigint"},
	{"pg_database", "dattablespace", "oid", "bigint"},
	{"pg_replication_slots", "retained_bytes", "numeric", "numeric"},
	{"pg_stat_activity", "datid", "oid", "bigint"},
	{"pg_stat_activity", "usesysid", "oid", "bigint"},
	{"pg_stat_activity", "client_addr", "inet", "text"},
	{"pg_stat_all_indexes", "indexrelid", "oid", "bigint"},
	{"pg_stat_all_indexes", "relid", "oid", "bigint"},
	{"pg_stat_database", "datid", "oid", "bigint"},
	{"pg_stat_database_conflicts", "datid", "oid", "bigint"},
	{"pg_stat_replication", "usesysid", "oid", "bigint"},
	{"pg_stat_replication", "client_addr", "inet", "text"},
	{"pg_stat_statements", "userid", "oid", "bigint"},
	{"pg_stat_statements", "dbid", "oid", "bigint"},
	{"pg_stat_subscription", "subid", "oid", "bigint"},
	{"pg_stat_subscription", "relid", "oid", "bigint"},
	{"pg_stat_user_functions", "funcid", "oid", "bigint"},
	{"pg_stat_wal", "wal_bytes", "numeric", "numeric"},
	{"pg_statio_all_sequences", "relid", "oid", "bigint"},
	{"spock_exception_log", "remote_origin", "oid", "bigint"},
	{"spock_exception_log", "local_origin", "oid", "bigint"},
	{"spock_resolutions", "node_name", "name", "text"},
}

// TestColumnClassificationSurvivesColdFrontTypes checks that every
// remapped column is a metric, a dimension and an entity key on ColdFront
// exactly as it is on plain PostgreSQL, so an identifier widened to
// bigint is not charted as a metric and an address stored as text does
// not split a session into one entity per address.
func TestColumnClassificationSurvivesColdFrontTypes(t *testing.T) {
	for _, c := range coldFrontRemappedColumns {
		pgMetric := IsMetricColumn(c.column, c.pgType)
		if got := IsMetricColumn(c.column, c.coldFrontType); got != pgMetric {
			t.Errorf("%s.%s: IsMetricColumn = %v as %s on ColdFront, %v as %s",
				c.probe, c.column, got, c.coldFrontType, pgMetric, c.pgType)
		}
		pgKey := IsEntityKeyColumn(c.column, c.pgType)
		if got := IsEntityKeyColumn(c.column, c.coldFrontType); got != pgKey {
			t.Errorf("%s.%s: IsEntityKeyColumn = %v as %s on ColdFront, %v as %s",
				c.probe, c.column, got, c.coldFrontType, pgKey, c.pgType)
		}
	}
}

// TestColdFrontIdentifierColumnsAreDimensions pins the expected outcome
// for the identifiers ColdFront widens, so the test above cannot pass by
// both types agreeing on the wrong answer.
func TestColdFrontIdentifierColumnsAreDimensions(t *testing.T) {
	for _, c := range coldFrontRemappedColumns {
		wantMetric := c.pgType == "numeric"
		if got := IsMetricColumn(c.column, c.coldFrontType); got != wantMetric {
			t.Errorf("%s.%s as %s: IsMetricColumn = %v, want %v",
				c.probe, c.column, c.coldFrontType, got, wantMetric)
		}
		wantKey := c.pgType == "name"
		if got := IsEntityKeyColumn(c.column, c.coldFrontType); got != wantKey {
			t.Errorf("%s.%s as %s: IsEntityKeyColumn = %v, want %v",
				c.probe, c.column, c.coldFrontType, got, wantKey)
		}
	}
}
