/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// cgTestStart and cgTestEnd are the fixed window bounds used by the pure SQL
// builder tests, so that the assertions never depend on the wall clock.
var (
	cgTestStart = time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	cgTestEnd   = time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
)

// TestBuildConnectionGroupsSQL_GroupingExpressions verifies that each
// whitelisted group_by value selects its own label expression and hostname
// aggregate, and that the fragments belonging to the other groupings do not
// leak into the query.
func TestBuildConnectionGroupsSQL_GroupingExpressions(t *testing.T) {
	tests := []struct {
		name        string
		groupBy     string
		wantLabel   string
		wantHost    string
		absentLabel []string
	}{
		{
			name:      "user",
			groupBy:   "user",
			wantLabel: `COALESCE(NULLIF(usename, ''), '(unknown)') AS group_label`,
			wantHost:  `NULL::text AS client_hostname`,
			absentLabel: []string{
				`COALESCE(host(client_addr), 'local')`,
				`COALESCE(NULLIF(datname, ''), '(none)')`,
				`MIN(client_hostname)`,
			},
		},
		{
			name:      "client",
			groupBy:   "client",
			wantLabel: `COALESCE(host(client_addr), 'local') AS group_label`,
			wantHost:  `MIN(client_hostname) AS client_hostname`,
			absentLabel: []string{
				`COALESCE(NULLIF(usename, ''), '(unknown)')`,
				`COALESCE(NULLIF(datname, ''), '(none)')`,
				`NULL::text AS client_hostname`,
			},
		},
		{
			name:      "database",
			groupBy:   "database",
			wantLabel: `COALESCE(NULLIF(datname, ''), '(none)') AS group_label`,
			wantHost:  `NULL::text AS client_hostname`,
			absentLabel: []string{
				`COALESCE(NULLIF(usename, ''), '(unknown)')`,
				`COALESCE(host(client_addr), 'local')`,
				`MIN(client_hostname)`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query, args := buildConnectionGroupsSQL(tc.groupBy, 7,
				cgTestStart, cgTestEnd)

			if !strings.Contains(query, tc.wantLabel) {
				t.Errorf("query missing label expression %q\nquery: %s",
					tc.wantLabel, query)
			}
			if !strings.Contains(query, tc.wantHost) {
				t.Errorf("query missing hostname expression %q\nquery: %s",
					tc.wantHost, query)
			}
			for _, absent := range tc.absentLabel {
				if strings.Contains(query, absent) {
					t.Errorf("query unexpectedly contains %q\nquery: %s",
						absent, query)
				}
			}

			want := []any{7, cgTestStart, cgTestEnd}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("args = %#v, want %#v", args, want)
			}
		})
	}
}

// TestBuildConnectionGroupsSQL_UnknownGroupByFallsBackToDefault verifies the
// defensive fallback: an unrecognized group_by produces exactly the default
// grouping's query rather than interpolating the caller's value or panicking.
func TestBuildConnectionGroupsSQL_UnknownGroupByFallsBackToDefault(t *testing.T) {
	fallback, fallbackArgs := buildConnectionGroupsSQL("not-a-grouping", 3,
		cgTestStart, cgTestEnd)
	def, defArgs := buildConnectionGroupsSQL(defaultConnectionGroupBy, 3,
		cgTestStart, cgTestEnd)

	if fallback != def {
		t.Errorf("unknown group_by must produce the default query\n"+
			"got:  %s\nwant: %s", fallback, def)
	}
	if !reflect.DeepEqual(fallbackArgs, defArgs) {
		t.Errorf("args = %#v, want %#v", fallbackArgs, defArgs)
	}
}

// TestBuildConnectionGroupsSQL_NoUserValuesInSQL proves that no caller-supplied
// value reaches the query text: the connection ID and both window bounds are
// bound as $1, $2 and $3, and a hostile group_by string never appears.
func TestBuildConnectionGroupsSQL_NoUserValuesInSQL(t *testing.T) {
	const hostile = "user'; DROP TABLE metrics.pg_stat_activity; --"
	const connID = 987654

	query, args := buildConnectionGroupsSQL(hostile, connID, cgTestStart,
		cgTestEnd)

	for _, fragment := range []string{
		hostile, "DROP TABLE", "987654", "2026-07-28", "2026-07-29",
	} {
		if strings.Contains(query, fragment) {
			t.Errorf("query must not contain user value %q\nquery: %s",
				fragment, query)
		}
	}

	for _, placeholder := range []string{"$1", "$2", "$3"} {
		if !strings.Contains(query, placeholder) {
			t.Errorf("query missing bind placeholder %s\nquery: %s",
				placeholder, query)
		}
	}
	if strings.Contains(query, "$4") {
		t.Errorf("query must bind exactly three parameters\nquery: %s", query)
	}
	if len(args) != 3 {
		t.Fatalf("len(args) = %d, want 3 (%#v)", len(args), args)
	}
	if args[0] != connID {
		t.Errorf("args[0] = %v, want %d", args[0], connID)
	}
}

// TestBuildConnectionGroupsSQL_StructuralInvariants locks in the parts of the
// query that the endpoint's documented semantics depend on: the latest-snapshot
// selection, the client-backend filter, the state buckets and the ordering.
func TestBuildConnectionGroupsSQL_StructuralInvariants(t *testing.T) {
	query, _ := buildConnectionGroupsSQL("user", 1, cgTestStart, cgTestEnd)

	required := []string{
		"MAX(collected_at) AS collected_at",
		"metrics.pg_stat_activity",
		"psa.backend_type = 'client backend'",
		"JOIN latest l ON psa.collected_at = l.collected_at",
		// The snapshot CTE must bound the partition key directly, or the
		// planner cannot prune metrics.pg_stat_activity's partitions.
		"AND psa.collected_at >= $2",
		"AND psa.collected_at <= $3",
		"LIMIT 200",
		"COUNT(*) FILTER (WHERE state = 'active') AS active",
		"COUNT(*) FILTER (WHERE state = 'idle') AS idle",
		"state LIKE 'idle in transaction%'",
		"state NOT LIKE 'idle in transaction%'",
		"GROUP BY group_label",
		"ORDER BY total DESC, group_label ASC",
	}
	for _, fragment := range required {
		if !strings.Contains(query, fragment) {
			t.Errorf("query missing required fragment %q\nquery: %s",
				fragment, query)
		}
	}

	// The LIKE patterns must use a single literal percent sign; a stray
	// doubled percent would be a fmt.Sprintf escaping bug.
	if strings.Contains(query, "%%") {
		t.Errorf("query contains an unescaped doubled percent\nquery: %s",
			query)
	}
}

// TestBuildConnectionGroupsSQL_PartitionPruningBounds asserts, for every
// grouping, that the snapshot CTE bounds collected_at directly rather than
// relying solely on the join to latest. metrics.pg_stat_activity is range
// partitioned on collected_at, so without those predicates the planner keeps
// every retained partition in the plan.
func TestBuildConnectionGroupsSQL_PartitionPruningBounds(t *testing.T) {
	for groupBy := range connectionGroupByColumns {
		t.Run(groupBy, func(t *testing.T) {
			query, _ := buildConnectionGroupsSQL(groupBy, 1, cgTestStart,
				cgTestEnd)

			// Both the latest CTE and the snapshot CTE must bound the
			// partition key, hence two occurrences of each bound.
			for _, bound := range []string{
				"collected_at >= $2", "collected_at <= $3",
			} {
				if got := strings.Count(query, bound); got != 2 {
					t.Errorf("query has %d occurrences of %q, want 2 "+
						"(one per CTE)\nquery: %s", got, bound, query)
				}
			}
		})
	}
}

// TestBuildConnectionGroupsSQL_GroupLimit verifies the defensive bound on group
// cardinality is applied, and that it matches the documented constant.
func TestBuildConnectionGroupsSQL_GroupLimit(t *testing.T) {
	if maxConnectionGroups != 200 {
		t.Errorf("maxConnectionGroups = %d, want 200; the OpenAPI "+
			"description and user guide quote this value",
			maxConnectionGroups)
	}

	for groupBy := range connectionGroupByColumns {
		query, _ := buildConnectionGroupsSQL(groupBy, 1, cgTestStart,
			cgTestEnd)
		want := fmt.Sprintf("LIMIT %d", maxConnectionGroups)
		if !strings.Contains(query, want) {
			t.Errorf("%s grouping missing %q\nquery: %s", groupBy, want,
				query)
		}
	}
}

// TestSortedConnectionGroupByValues verifies the accepted values are listed in
// a stable alphabetical order, and that the 400 message names all of them.
func TestSortedConnectionGroupByValues(t *testing.T) {
	got := sortedConnectionGroupByValues()
	want := []string{"client", "database", "user"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedConnectionGroupByValues() = %#v, want %#v", got, want)
	}

	msg := connectionGroupByError()
	if msg != "Invalid group_by: must be one of client, database, user" {
		t.Errorf("connectionGroupByError() = %q", msg)
	}
}

// TestDefaultConnectionGroupByIsWhitelisted guards against the default value
// drifting out of the whitelist, which would make an omitted group_by
// unusable.
func TestDefaultConnectionGroupByIsWhitelisted(t *testing.T) {
	if _, ok := connectionGroupByColumns[defaultConnectionGroupBy]; !ok {
		t.Fatalf("default group_by %q is not in the whitelist",
			defaultConnectionGroupBy)
	}
}
