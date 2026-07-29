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
	"reflect"
	"strings"
	"testing"
)

// topQueriesCTEHead is the invariant part of the generated CTE, in
// whitespace-normalised form: everything from the WITH keyword up to and
// including the closing parenthesis of the latest-snapshot subquery. The
// optional filter clauses and the trailing ORDER BY follow it.
const topQueriesCTEHead = "WITH db_names AS ( " +
	"SELECT DISTINCT datid, datname " +
	"FROM metrics.pg_stat_activity " +
	"WHERE connection_id = $1 " +
	"AND datid IS NOT NULL " +
	"AND datname IS NOT NULL " +
	"), deduped AS ( " +
	"SELECT DISTINCT ON (pss.queryid) " +
	"pss.queryid::text, " +
	"COALESCE(dn.datname, pss.database_name) AS database_name, " +
	"pss.query, pss.calls, pss.total_exec_time, " +
	"pss.mean_exec_time, pss.rows, " +
	"pss.shared_blks_hit, pss.shared_blks_read " +
	"FROM metrics.pg_stat_statements pss " +
	"LEFT JOIN db_names dn ON pss.dbid = dn.datid " +
	"WHERE pss.connection_id = $1 " +
	"AND pss.collected_at = ( " +
	"SELECT MAX(collected_at) " +
	"FROM metrics.pg_stat_statements " +
	"WHERE connection_id = $1 )"

// normaliseSQL collapses every run of whitespace to a single space and trims
// the result, so that generated statements can be compared exactly without
// the assertions depending on the source layout of the query text.
func normaliseSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// joinSQL concatenates the non-empty parts with a single separating space.
func joinSQL(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

// TestBuildTopQueriesSQL_ClauseCombinations asserts the exact generated SQL
// and argument slices for all eight combinations of (queryid present or
// absent) x (exclude_collector on or off) x (database_name present or
// absent). The placeholder numbering is the security-relevant part: every
// user-supplied value must arrive as a $N parameter, and the count and page
// statements must number the shared filters identically.
func TestBuildTopQueriesSQL_ClauseCombinations(t *testing.T) {
	const (
		connID       = 42
		queryID      = "1234567890"
		databaseName = "alpha"
		limit        = 25
		offset       = 50
		excludeSQL   = "AND pss.query NOT LIKE '%ai_dba_wb_probe%'"
	)

	tests := []struct {
		name             string
		queryID          string
		databaseName     string
		excludeCollector bool
		wantFilters      string
		wantDBClause     string
		wantTail         string
		wantFilterArgs   []any
		wantPageArgs     []any
	}{
		{
			name:           "no filters",
			wantTail:       "LIMIT $2 OFFSET $3",
			wantFilterArgs: []any{connID},
			wantPageArgs:   []any{connID, limit, offset},
		},
		{
			name:             "exclude collector only",
			excludeCollector: true,
			wantFilters:      excludeSQL,
			wantTail:         "LIMIT $2 OFFSET $3",
			wantFilterArgs:   []any{connID},
			wantPageArgs:     []any{connID, limit, offset},
		},
		{
			name:           "database name only",
			databaseName:   databaseName,
			wantDBClause:   "WHERE database_name = $2",
			wantTail:       "LIMIT $3 OFFSET $4",
			wantFilterArgs: []any{connID, databaseName},
			wantPageArgs:   []any{connID, databaseName, limit, offset},
		},
		{
			name:             "database name and exclude collector",
			databaseName:     databaseName,
			excludeCollector: true,
			wantFilters:      excludeSQL,
			wantDBClause:     "WHERE database_name = $2",
			wantTail:         "LIMIT $3 OFFSET $4",
			wantFilterArgs:   []any{connID, databaseName},
			wantPageArgs:     []any{connID, databaseName, limit, offset},
		},
		{
			name:           "queryid only",
			queryID:        queryID,
			wantFilters:    "AND pss.queryid::text = $2",
			wantTail:       "LIMIT $3 OFFSET $4",
			wantFilterArgs: []any{connID, queryID},
			wantPageArgs:   []any{connID, queryID, limit, offset},
		},
		{
			name:             "queryid and exclude collector",
			queryID:          queryID,
			excludeCollector: true,
			wantFilters:      "AND pss.queryid::text = $2 " + excludeSQL,
			wantTail:         "LIMIT $3 OFFSET $4",
			wantFilterArgs:   []any{connID, queryID},
			wantPageArgs:     []any{connID, queryID, limit, offset},
		},
		{
			name:           "queryid and database name",
			queryID:        queryID,
			databaseName:   databaseName,
			wantFilters:    "AND pss.queryid::text = $2",
			wantDBClause:   "WHERE database_name = $3",
			wantTail:       "LIMIT $4 OFFSET $5",
			wantFilterArgs: []any{connID, queryID, databaseName},
			wantPageArgs: []any{
				connID, queryID, databaseName, limit, offset},
		},
		{
			name:             "queryid, database name and exclude collector",
			queryID:          queryID,
			databaseName:     databaseName,
			excludeCollector: true,
			wantFilters:      "AND pss.queryid::text = $2 " + excludeSQL,
			wantDBClause:     "WHERE database_name = $3",
			wantTail:         "LIMIT $4 OFFSET $5",
			wantFilterArgs:   []any{connID, queryID, databaseName},
			wantPageArgs: []any{
				connID, queryID, databaseName, limit, offset},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			countSQL, pageSQL, filterArgs, pageArgs := buildTopQueriesSQL(
				connID, tc.queryID, tc.databaseName, tc.excludeCollector,
				"total_exec_time", "DESC", limit, offset)

			wantCTE := joinSQL(topQueriesCTEHead, tc.wantFilters,
				"ORDER BY pss.queryid )")
			wantCount := joinSQL(wantCTE, "SELECT COUNT(*) FROM deduped",
				tc.wantDBClause)
			wantPage := joinSQL(wantCTE, "SELECT * FROM deduped",
				tc.wantDBClause, "ORDER BY total_exec_time DESC, queryid",
				tc.wantTail)

			if got := normaliseSQL(countSQL); got != wantCount {
				t.Errorf("count SQL:\n got: %s\nwant: %s", got, wantCount)
			}
			if got := normaliseSQL(pageSQL); got != wantPage {
				t.Errorf("page SQL:\n got: %s\nwant: %s", got, wantPage)
			}
			if !reflect.DeepEqual(filterArgs, tc.wantFilterArgs) {
				t.Errorf("filterArgs = %#v, want %#v", filterArgs,
					tc.wantFilterArgs)
			}
			if !reflect.DeepEqual(pageArgs, tc.wantPageArgs) {
				t.Errorf("pageArgs = %#v, want %#v", pageArgs,
					tc.wantPageArgs)
			}
		})
	}
}

// TestBuildTopQueriesSQL_OrderClause confirms that the ORDER BY clause is
// built from the constants supplied by the caller, for every column in the
// whitelist and both sort directions, and that the queryid tiebreaker that
// makes paging stable is always present.
func TestBuildTopQueriesSQL_OrderClause(t *testing.T) {
	for token, column := range validTopQueryOrderColumns {
		for dirToken, direction := range validTopQueryOrderDirections {
			t.Run(token+"_"+dirToken, func(t *testing.T) {
				_, pageSQL, _, _ := buildTopQueriesSQL(
					1, "", "", false, column, direction, 10, 0)
				want := "ORDER BY " + column + " " + direction +
					", queryid LIMIT $2 OFFSET $3"
				if got := normaliseSQL(pageSQL); !strings.HasSuffix(got,
					want) {
					t.Errorf("page SQL does not end with %q:\n%s", want, got)
				}
			})
		}
	}
}

// TestBuildTopQueriesSQL_ArgumentsAreIndependent confirms the page argument
// slice is a copy rather than an alias of the filter arguments, so appending
// the limit and offset cannot disturb the count query's arguments.
func TestBuildTopQueriesSQL_ArgumentsAreIndependent(t *testing.T) {
	_, _, filterArgs, pageArgs := buildTopQueriesSQL(
		7, "99", "beta", true, "calls", "ASC", 5, 10)

	if len(filterArgs) != 3 {
		t.Fatalf("filterArgs = %#v, want three entries", filterArgs)
	}
	pageArgs[0] = "mutated"
	if filterArgs[0] != 7 {
		t.Errorf("filterArgs[0] = %#v after mutating pageArgs, want 7",
			filterArgs[0])
	}
}

// TestBuildTopQueriesSQL_NoUserValuesInSQL is a belt-and-braces check that
// no caller-supplied value reaches the statement text; each one must appear
// only in the argument slices.
func TestBuildTopQueriesSQL_NoUserValuesInSQL(t *testing.T) {
	const (
		evilQueryID = "1 OR 1=1"
		evilDBName  = "alpha'; DROP TABLE metrics.pg_stat_statements; --"
	)

	countSQL, pageSQL, filterArgs, pageArgs := buildTopQueriesSQL(
		31337, evilQueryID, evilDBName, true, "rows", "ASC", 11, 22)

	for _, sql := range []string{countSQL, pageSQL} {
		for _, value := range []string{evilQueryID, evilDBName, "31337",
			"11", "22"} {
			if strings.Contains(sql, value) {
				t.Errorf("generated SQL contains user value %q:\n%s", value,
					sql)
			}
		}
	}

	wantFilters := []any{31337, evilQueryID, evilDBName}
	if !reflect.DeepEqual(filterArgs, wantFilters) {
		t.Errorf("filterArgs = %#v, want %#v", filterArgs, wantFilters)
	}
	wantPage := []any{31337, evilQueryID, evilDBName, 11, 22}
	if !reflect.DeepEqual(pageArgs, wantPage) {
		t.Errorf("pageArgs = %#v, want %#v", pageArgs, wantPage)
	}
}
