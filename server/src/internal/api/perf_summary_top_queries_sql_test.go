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
	"time"

	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// testTopQueriesWindow is the fixed window the builder tests pass in. The
// bounds are bound as parameters, so their values never reach the SQL text;
// they are constants here only so the expected argument slices can name
// them.
func testTopQueriesWindow() metrics.TimeWindow {
	return metrics.TimeWindow{
		Start: time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
	}
}

// topQueriesCTEHead is the invariant leading part of the generated CTE, in
// whitespace-normalised form: everything from the WITH keyword up to and
// including last_client's query_id IS NOT NULL predicate. The optional
// queryid predicate inside last_client follows it, then
// topQueriesCTEBody, then the optional sample filters, and
// topQueriesCTETail closes the statement.
const topQueriesCTEHead = "WITH latest AS ( " +
	"SELECT MAX(collected_at) AS collected_at " +
	"FROM metrics.pg_stat_statements " +
	"WHERE connection_id = $1 " +
	"), db_names AS ( " +
	"SELECT DISTINCT ON (datid) datid, datname " +
	"FROM ( " +
	"SELECT datid, datname, collected_at " +
	"FROM metrics.pg_stat_database " +
	"WHERE connection_id = $1 " +
	"AND collected_at >= (SELECT collected_at FROM latest) " +
	"- INTERVAL '1 hour' " +
	"AND datid IS NOT NULL " +
	"AND datname IS NOT NULL " +
	"UNION ALL " +
	"SELECT datid, datname, collected_at " +
	"FROM metrics.pg_stat_activity " +
	"WHERE connection_id = $1 " +
	"AND collected_at >= (SELECT collected_at FROM latest) " +
	"- INTERVAL '1 hour' " +
	"AND datid IS NOT NULL " +
	"AND datname IS NOT NULL " +
	") observed " +
	"ORDER BY datid, collected_at DESC, datname " +
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
// queryid predicate and the sample scan's own optional filters.
const topQueriesCTEBody = "ORDER BY query_id, datid, usesysid, " +
	"collected_at DESC " +
	"), readings AS ( " +
	"SELECT DISTINCT ON (pss.queryid, pss.userid, pss.dbid, " +
	"pss.toplevel, pss.collected_at) " +
	"pss.queryid, pss.userid, pss.dbid, pss.toplevel, " +
	"pss.collected_at, " +
	"pss.database_name AS sample_database_name, " +
	"COALESCE(dn.datname, pss.database_name) AS database_name, " +
	"pss.calls, pss.total_exec_time, pss.rows, " +
	"pss.shared_blks_hit, pss.shared_blks_read, " +
	"pss.min_exec_time, pss.max_exec_time " +
	"FROM metrics.pg_stat_statements pss " +
	"LEFT JOIN db_names dn ON pss.dbid = dn.datid " +
	"WHERE pss.connection_id = $1 " +
	"AND pss.collected_at >= $2 " +
	"AND pss.collected_at <= $3"

// topQueriesCTETail runs from the ORDER BY that the DISTINCT ON in
// readings keys on to the samples CTE's read of readings; topQueriesCTEWindow
// is the rest: the identity window that the LAG runs over, the per-queryid
// delta aggregation, the latest sample each statement was seen in, and the
// joins that resolve the role OID to a name and attach the last observed
// client. Splitting the golden copy lets the optional sample filters sit
// between the sample predicate and that ORDER BY, and the optional database
// predicate between samples' FROM and its WINDOW, which is where the
// builder puts them.
const topQueriesCTETail = "ORDER BY pss.queryid, pss.userid, pss.dbid, " +
	"pss.toplevel, " +
	"pss.collected_at, pss.database_name " +
	"), samples AS ( " +
	"SELECT " +
	"r.queryid, r.collected_at, " +
	"r.database_name, r.sample_database_name, r.dbid, r.userid, " +
	"r.min_exec_time, r.max_exec_time, " +
	"r.calls - LAG(r.calls) OVER identity AS delta_calls, " +
	"r.total_exec_time " +
	"- LAG(r.total_exec_time) OVER identity AS delta_time, " +
	"r.rows - LAG(r.rows) OVER identity AS delta_rows, " +
	"r.shared_blks_hit " +
	"- LAG(r.shared_blks_hit) OVER identity AS delta_hit, " +
	"r.shared_blks_read " +
	"- LAG(r.shared_blks_read) OVER identity AS delta_read " +
	"FROM readings r"

// topQueriesCTEWindow follows the optional database predicate, which sits
// between the readings scan in samples and its identity window.
const topQueriesCTEWindow = "WINDOW identity AS ( " +
	"PARTITION BY r.queryid, r.userid, r.dbid, r.toplevel " +
	"ORDER BY r.collected_at " +
	") " +
	"), totals AS MATERIALIZED ( " +
	"SELECT " +
	"queryid, " +
	"SUM(delta_calls)::bigint AS calls, " +
	"SUM(delta_time) AS total_exec_time, " +
	"SUM(GREATEST(delta_rows, 0))::bigint AS rows, " +
	"SUM(GREATEST(delta_hit, 0))::bigint AS shared_blks_hit, " +
	"SUM(GREATEST(delta_read, 0))::bigint AS shared_blks_read " +
	"FROM samples " +
	"WHERE delta_calls >= 0 " +
	"AND delta_time >= 0 " +
	"GROUP BY queryid " +
	"HAVING SUM(delta_calls) > 0 " +
	"), latest_sample AS MATERIALIZED ( " +
	"SELECT DISTINCT ON (queryid) " +
	"queryid, database_name, sample_database_name, dbid, userid, " +
	"min_exec_time, max_exec_time " +
	"FROM samples " +
	"ORDER BY queryid, collected_at DESC, dbid, userid " +
	"), deduped AS ( " +
	"SELECT " +
	"t.queryid::text, " +
	"t.queryid AS sample_queryid, " +
	"ls.sample_database_name, " +
	"ls.database_name, " +
	"COALESCE(un.usename, '') AS username, " +
	"t.calls, t.total_exec_time, " +
	"CASE WHEN t.calls > 0 " +
	"THEN t.total_exec_time / t.calls " +
	"ELSE 0 END AS mean_exec_time, " +
	"ls.min_exec_time, ls.max_exec_time, " +
	"t.rows, " +
	"t.shared_blks_hit, t.shared_blks_read, " +
	"lc.client_addr, lc.client_hostname, " +
	"lc.collected_at AS client_observed_at " +
	"FROM totals t " +
	"JOIN latest_sample ls ON ls.queryid = t.queryid " +
	"LEFT JOIN user_names un ON ls.userid = un.usesysid " +
	"LEFT JOIN last_client lc ON ls.queryid = lc.query_id " +
	"AND ls.dbid = lc.datid AND ls.userid = lc.usesysid )"

// topQueriesPageSelect is the projection the page statement wraps the
// paged CTE in, and topQueriesPageLateral is the lookup that resolves the
// query text for the rows on that page. The text is deliberately not
// carried through the aggregation, so the page statement is no longer a
// bare "SELECT * FROM deduped"; splitting the golden copy here lets the
// optional database clause, the ORDER BY and the LIMIT sit between them,
// which is where the builder puts them.
const topQueriesPageSelect = "SELECT " +
	"page.queryid, page.database_name, page.username, qtext.query, " +
	"page.calls, page.total_exec_time, page.mean_exec_time, " +
	"page.min_exec_time, page.max_exec_time, page.rows, " +
	"page.shared_blks_hit, page.shared_blks_read, " +
	"page.client_addr, page.client_hostname, page.client_observed_at " +
	"FROM ( SELECT * FROM deduped"

const topQueriesPageLateral = ") page " +
	"LEFT JOIN LATERAL ( " +
	"SELECT pss.query " +
	"FROM metrics.pg_stat_statements pss " +
	"WHERE pss.connection_id = $1 " +
	"AND pss.database_name = page.sample_database_name " +
	"AND pss.queryid = page.sample_queryid " +
	"AND pss.collected_at >= $2 " +
	"AND pss.collected_at <= $3 " +
	"ORDER BY pss.collected_at DESC " +
	"LIMIT 1 " +
	") qtext ON TRUE"

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

	window := testTopQueriesWindow()
	start, end := window.Start, window.End

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
		// wantDBClause is the optional database predicate. Since #387's
		// review it selects which counters are summed, and since #508 it
		// sits in samples, after readings keeps one copy per counter, so
		// that an unresolved dbid matches only one probing database.
		wantDBClause   string
		wantTail       string
		wantFilterArgs []any
		wantPageArgs   []any
	}{
		{
			name:           "no filters",
			wantTail:       "LIMIT $4 OFFSET $5",
			wantFilterArgs: []any{connID, start, end},
			wantPageArgs:   []any{connID, start, end, limit, offset},
		},
		{
			name:             "exclude collector only",
			excludeCollector: true,
			wantFilters:      excludeSQL,
			wantTail:         "LIMIT $4 OFFSET $5",
			wantFilterArgs:   []any{connID, start, end},
			wantPageArgs:     []any{connID, start, end, limit, offset},
		},
		{
			name:           "database name only",
			databaseName:   databaseName,
			wantDBClause:   "WHERE r.database_name = $4",
			wantTail:       "LIMIT $5 OFFSET $6",
			wantFilterArgs: []any{connID, start, end, databaseName},
			wantPageArgs: []any{
				connID, start, end, databaseName, limit, offset},
		},
		{
			name:             "database name and exclude collector",
			databaseName:     databaseName,
			excludeCollector: true,
			wantFilters:      excludeSQL,
			wantDBClause:     "WHERE r.database_name = $4",
			wantTail:         "LIMIT $5 OFFSET $6",
			wantFilterArgs:   []any{connID, start, end, databaseName},
			wantPageArgs: []any{
				connID, start, end, databaseName, limit, offset},
		},
		{
			name:           "queryid only",
			queryID:        &queryID,
			wantLastClient: "AND query_id = $4",
			wantFilters:    "AND pss.queryid = $4",
			wantTail:       "LIMIT $5 OFFSET $6",
			wantFilterArgs: []any{connID, start, end, queryID},
			wantPageArgs: []any{
				connID, start, end, queryID, limit, offset},
		},
		{
			name:             "queryid and exclude collector",
			queryID:          &queryID,
			excludeCollector: true,
			wantLastClient:   "AND query_id = $4",
			wantFilters:      "AND pss.queryid = $4 " + excludeSQL,
			wantTail:         "LIMIT $5 OFFSET $6",
			wantFilterArgs:   []any{connID, start, end, queryID},
			wantPageArgs: []any{
				connID, start, end, queryID, limit, offset},
		},
		{
			name:           "queryid and database name",
			queryID:        &queryID,
			databaseName:   databaseName,
			wantLastClient: "AND query_id = $4",
			wantFilters:    "AND pss.queryid = $4",
			wantDBClause:   "WHERE r.database_name = $5",
			wantTail:       "LIMIT $6 OFFSET $7",
			wantFilterArgs: []any{connID, start, end, queryID, databaseName},
			wantPageArgs: []any{
				connID, start, end, queryID, databaseName, limit, offset},
		},
		{
			name:             "queryid, database name and exclude collector",
			queryID:          &queryID,
			databaseName:     databaseName,
			excludeCollector: true,
			wantLastClient:   "AND query_id = $4",
			wantFilters:      "AND pss.queryid = $4 " + excludeSQL,
			wantDBClause:     "WHERE r.database_name = $5",
			wantTail:         "LIMIT $6 OFFSET $7",
			wantFilterArgs:   []any{connID, start, end, queryID, databaseName},
			wantPageArgs: []any{
				connID, start, end, queryID, databaseName, limit, offset},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			countSQL, pageSQL, filterArgs, pageArgs := buildTopQueriesSQL(
				connID, window, tc.queryID, tc.databaseName,
				tc.excludeCollector, "total_exec_time", "DESC", limit,
				offset)

			wantCTE := joinSQL(topQueriesCTEHead, tc.wantLastClient,
				topQueriesCTEBody, tc.wantFilters, topQueriesCTETail,
				tc.wantDBClause, topQueriesCTEWindow)
			wantCount := joinSQL(wantCTE, "SELECT COUNT(*) FROM deduped")
			wantPage := joinSQL(wantCTE, topQueriesPageSelect,
				"ORDER BY total_exec_time DESC, queryid",
				tc.wantTail, topQueriesPageLateral)

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
					1, testTopQueriesWindow(), nil, "", false, column,
					direction, 10, 0)
				want := "ORDER BY " + column + " " + direction +
					", queryid LIMIT $4 OFFSET $5 " + topQueriesPageLateral
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
		7, testTopQueriesWindow(), &queryID, "beta", true, "calls", "ASC",
		5, 10)

	if len(filterArgs) != 5 {
		t.Fatalf("filterArgs = %#v, want five entries", filterArgs)
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

	window := metrics.TimeWindow{
		Start: time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
		End:   time.Date(2026, 2, 4, 4, 5, 6, 0, time.UTC),
	}

	countSQL, pageSQL, filterArgs, pageArgs := buildTopQueriesSQL(
		31337, window, &evilQueryID, evilDBName, true, "rows", "ASC", 11, 22)

	for _, sql := range []string{countSQL, pageSQL} {
		for _, value := range []string{"8675309", evilDBName, "31337",
			"11", "22", "2026-02-03", "2026-02-04"} {
			if strings.Contains(sql, value) {
				t.Errorf("generated SQL contains user value %q:\n%s", value,
					sql)
			}
		}
	}

	wantFilters := []any{31337, window.Start, window.End, evilQueryID,
		evilDBName}
	if !reflect.DeepEqual(filterArgs, wantFilters) {
		t.Errorf("filterArgs = %#v, want %#v", filterArgs, wantFilters)
	}
	wantPage := []any{31337, window.Start, window.End, evilQueryID,
		evilDBName, 11, 22}
	if !reflect.DeepEqual(pageArgs, wantPage) {
		t.Errorf("pageArgs = %#v, want %#v", pageArgs, wantPage)
	}
}
