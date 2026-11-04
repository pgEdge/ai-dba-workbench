/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package coldfrontcheck

import "testing"

func TestShapeProblems(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		bad  bool
	}{
		{"array parameter", "WHERE connection_id = ANY($1)", true},
		{"cast array parameter", "WHERE connection_id = ANY($2::int[])", true},
		{"formatted array parameter", "WHERE connection_id = ANY($%d)", true},
		{"text list", "WHERE connection_id = ANY(string_to_array($1, ',')::integer[])", false},
		{"untyped IN list", "WHERE name IN ($2, $3)", true},
		{"integer IN list", "WHERE connection_id IN ($1::int, $2::int)", true},
		{"text IN list", "WHERE name IN ($2::text, $3::text)", false},
		{"single typed IN", "WHERE connection_id IN ($1::integer)", false},
		{"subquery IN", "WHERE dbid IN (SELECT datid FROM db_names)", false},
		{"bare host", "COALESCE(host(client_addr), 'local')", true},
		{"inet host", "COALESCE(host(client_addr::inet), 'local')", false},
		{"parameter minus interval", "collected_at >= $3 - INTERVAL '1 hour'", true},
		{"interval times parameter", "INTERVAL '1 day' * $1", true},
		{"interval times int parameter", "NOW() - INTERVAL '1 day' * $1::int", false},
		{"bigint parameter times interval", "NOW() - $1::bigint * INTERVAL '1 day'", false},
		{"interval times float parameter", "INTERVAL '1 day' * $1::float8", true},
		{"int parameter minus one", "connection_id > $2::int - 1", true},
		{"cast timestamp minus interval", "collected_at > $2::timestamptz - INTERVAL '1 hour'", true},
		{"pg_lsn", "(sent_lsn::pg_lsn - replay_lsn::pg_lsn)::float", true},
		{"infinity literal", "collected_at <= 'infinity'::timestamptz", true},
		{"minus infinity literal", "THEN '-infinity'::timestamptz", true},
		{"array literal", "connection_id = ANY('{1,2}'::int[])", true},
		{"untyped array literal", "state = ANY('{active,idle}')", true},
		{"array constructor", "connection_id = ANY(ARRAY[1, 2])", false},
		{"parameter is null", "($2 IS NULL OR collected_at <= $2)", true},
		{"cast parameter is not null", "$2::timestamptz IS NOT NULL", true},
		{"formatted parameter is null", "$%d::text IS NULL", true},
		{"column is null", "m.valid_from IS NULL OR a.collected_at >= m.valid_from", false},
		{"coalesced open bound", "collected_at <= COALESCE($2::timestamptz, collected_at)", false},
		{"not distinct from parameter", "database_name IS NOT DISTINCT FROM $4::text", false},
		{"jsonb object literal", "COALESCE(details, '{}'::jsonb)", false},
		{"json object literal", `'{"a": 1}'::json ->> 'a'`, false},
		{"now minus parameter", "collected_at > now() - $2::interval", true},
		{"column plus parameter", "bucket_time + $1::interval", false},
		{"comparison", "collected_at >= $3 AND collected_at <= $4", false},
		{"uncast avg", "SELECT avg(xact_commit) FROM metrics.x", true},
		{"cast avg", "SELECT avg(xact_commit)::float8 FROM metrics.x", false},
		{"uncast epoch", "MIN(EXTRACT(EPOCH FROM a - b)) AS s", true},
		{"float8 epoch", "MIN(EXTRACT(EPOCH FROM a - b)::float8) AS s", false},
		{"bigint epoch", "GREATEST(EXTRACT(EPOCH FROM (a - b))::BIGINT, 0)", false},
		{"open call", "SELECT avg(", false},
		{"qualified table", "FROM public.connections c LEFT JOIN metrics.pg_connectivity l", true},
		{"qualified join", `JOIN "public"."alerts" a ON true`, true},
		{"unqualified table", "FROM connections c LEFT JOIN metrics.pg_connectivity l", false},
		{"metrics table", `FROM metrics."pg_stat_database" t`, false},
		{"distinct from a column", "x IS DISTINCT FROM changes.prev_setting", false},
		{"extract from a column", "EXTRACT(EPOCH FROM p1.collected_at - p2.collected_at)::float8", false},
		{"untyped JSON value", "jsonb_build_object('name', n, 'change_type', 'modified'\n)", true},
		{"typed JSON value", "jsonb_build_object('name', n, 'change_type', 'modified'::text)", false},
		{"JSON keys only", "jsonb_build_object('name', n, 'value', v)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShapeProblems(tt.sql)
			if (len(got) > 0) != tt.bad {
				t.Errorf("ShapeProblems(%q) = %q, want bad = %v", tt.sql, got, tt.bad)
			}
		})
	}
	listTests := []struct {
		name     string
		literals []string
		bad      bool
	}{
		{"untyped run-time list", []string{"$%d", "name IN (%s)"}, true},
		{"typed run-time list", []string{"$%d::text", "name NOT IN (%s)"}, false},
		{"integer run-time list", []string{"$%d::int", "id IN (%s)"}, true},
		{"one typed list beside an untyped one",
			[]string{"$%d::text", "a IN (%s)", "$%d", "b IN (%s)"}, true},
		{"run-time list with no placeholder literal", []string{"name IN (%s)"}, true},
		{"untyped placeholder without a list", []string{"$%d", "LIMIT %s"}, false},
		{"no list", []string{"name = $1"}, false},
	}
	for _, tt := range listTests {
		if got := UnitProblems(tt.literals); (len(got) > 0) != tt.bad {
			t.Errorf("%s: unitShapeProblems = %q, want bad = %v", tt.name, got, tt.bad)
		}
	}
	if matchingParen("((x)", 0) != -1 {
		t.Error("matchingParen should report an unclosed parenthesis")
	}
}
