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

// topQueriesCTEHead is the invariant leading part of the generated CTE, in
// whitespace-normalised form: everything from the WITH keyword up to and
// including last_client's query_id IS NOT NULL predicate. The optional
// queryid predicate inside last_client follows it, then
// topQueriesCTEBody, then the optional deduped filters and the trailing
// ORDER BY.
const topQueriesCTEHead = "WITH latest AS ( " +
	"SELECT MAX(collected_at) AS collected_at " +
	"FROM metrics.pg_stat_statements " +
	"WHERE connection_id = $1 " +
	"), db_names AS ( " +
	"SELECT DISTINCT ON (datid) datid, datname " +
	"FROM metrics.pg_stat_activity " +
	"WHERE connection_id = $1 " +
	"AND collected_at >= (SELECT collected_at FROM latest) " +
	"- INTERVAL '1 hour' " +
	"AND datid IS NOT NULL " +
	"AND datname IS NOT NULL " +
	"ORDER BY datid, collected_at DESC " +
	"), user_names AS ( " +
	"SELECT DISTINCT ON (usesysid) usesysid, usename " +
	"FROM metrics.pg_stat_activity " +
	"WHERE connection_id = $1 " +
	"AND collected_at >= (SELECT collected_at FROM latest) " +
	"- INTERVAL '1 hour' " +
	"AND usesysid IS NOT NULL " +
	"AND usename IS NOT NULL " +
	"ORDER BY usesysid, collected_at DESC " +
	"), last_client AS ( " +
	"SELECT DISTINCT ON (query_id, datid, usesysid) " +
	"query_id, datid, usesysid, " +
	"COALESCE(host(client_addr), 'local') AS client_addr, " +
	"client_hostname, collected_at " +
	"FROM metrics.pg_stat_activity " +
	"WHERE connection_id = $1 " +
	"AND collected_at >= (SELECT collected_at FROM latest) " +
	"- INTERVAL '1 hour' " +
	"AND query_id IS NOT NULL"

// topQueriesCTEBody is the part of the CTE between last_client's optional
// queryid predicate and deduped's own optional filters.
const topQueriesCTEBody = "ORDER BY query_id, datid, usesysid, " +
	"collected_at DESC " +
	"), deduped AS ( " +
	"SELECT DISTINCT ON (pss.queryid) " +
	"pss.queryid::text, " +
	"COALESCE(dn.datname, pss.database_name) AS database_name, " +
	"COALESCE(un.usename, '') AS username, " +
	"pss.query, pss.calls, pss.total_exec_time, " +
	"pss.mean_exec_time, pss.min_exec_time, pss.max_exec_time, " +
	"pss.rows, " +
	"pss.shared_blks_hit, pss.shared_blks_read, " +
	"lc.client_addr, lc.client_hostname, " +
	"lc.collected_at AS client_observed_at " +
	"FROM metrics.pg_stat_statements pss " +
	"LEFT JOIN db_names dn ON pss.dbid = dn.datid " +
	"LEFT JOIN user_names un ON pss.userid = un.usesysid " +
	"LEFT JOIN last_client lc ON pss.queryid = lc.query_id " +
	"AND pss.dbid = lc.datid AND pss.userid = lc.usesysid " +
	"WHERE pss.connection_id = $1 " +
	"AND pss.collected_at = (SELECT collected_at FROM latest)"

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
		databaseName = "alpha"
		limit        = 25
		offset       = 50
		// The exclusion clause is shared with the rest of the API and
		// matches both markers literally; see
		// excludeWorkbenchQueriesClause.
		excludeSQL = excludeWorkbenchQueriesClause
	)

	// queryID is a variable rather than a constant so that its address
	// can be taken: the builder receives the parsed identifier as *int64.
	var queryID int64 = 1234567890

	tests := []struct {
		name             string
		queryID          *int64
		databaseName     string
		excludeCollector bool
		// wantLastClient is the optional queryid predicate pushed into
		// the last_client CTE. It shares its placeholder with
		// wantFilters' pss.queryid predicate rather than binding a
		// second parameter, which is what the argument assertions below
		// pin down.
		wantLastClient string
		wantFilters    string
		wantDBClause   string
		wantTail       string
		wantFilterArgs []any
		wantPageArgs   []any
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
			queryID:        &queryID,
			wantLastClient: "AND query_id = $2",
			wantFilters:    "AND pss.queryid = $2",
			wantTail:       "LIMIT $3 OFFSET $4",
			wantFilterArgs: []any{connID, queryID},
			wantPageArgs:   []any{connID, queryID, limit, offset},
		},
		{
			name:             "queryid and exclude collector",
			queryID:          &queryID,
			excludeCollector: true,
			wantLastClient:   "AND query_id = $2",
			wantFilters:      "AND pss.queryid = $2 " + excludeSQL,
			wantTail:         "LIMIT $3 OFFSET $4",
			wantFilterArgs:   []any{connID, queryID},
			wantPageArgs:     []any{connID, queryID, limit, offset},
		},
		{
			name:           "queryid and database name",
			queryID:        &queryID,
			databaseName:   databaseName,
			wantLastClient: "AND query_id = $2",
			wantFilters:    "AND pss.queryid = $2",
			wantDBClause:   "WHERE database_name = $3",
			wantTail:       "LIMIT $4 OFFSET $5",
			wantFilterArgs: []any{connID, queryID, databaseName},
			wantPageArgs: []any{
				connID, queryID, databaseName, limit, offset},
		},
		{
			name:             "queryid, database name and exclude collector",
			queryID:          &queryID,
			databaseName:     databaseName,
			excludeCollector: true,
			wantLastClient:   "AND query_id = $2",
			wantFilters:      "AND pss.queryid = $2 " + excludeSQL,
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

			wantCTE := joinSQL(topQueriesCTEHead, tc.wantLastClient,
				topQueriesCTEBody, tc.wantFilters,
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
					1, nil, "", false, column, direction, 10, 0)
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
	var queryID int64 = 99
	_, _, filterArgs, pageArgs := buildTopQueriesSQL(
		7, &queryID, "beta", true, "calls", "ASC", 5, 10)

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
// only in the argument slices. The queryid is an int64 by the time it
// reaches the builder, so the handler's parser is what keeps SQL text out
// of it; here it is checked for the same reason as the numeric limit and
// offset, namely that its decimal form is never interpolated.
func TestBuildTopQueriesSQL_NoUserValuesInSQL(t *testing.T) {
	const evilDBName = "alpha'; DROP TABLE metrics.pg_stat_statements; --"
	var evilQueryID int64 = 8675309

	countSQL, pageSQL, filterArgs, pageArgs := buildTopQueriesSQL(
		31337, &evilQueryID, evilDBName, true, "rows", "ASC", 11, 22)

	for _, sql := range []string{countSQL, pageSQL} {
		for _, value := range []string{"8675309", evilDBName, "31337",
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
