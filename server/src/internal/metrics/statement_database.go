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

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NameLookupWindowSQL is the interval literal, spliced into the server's
// Top Queries SQL and StatementDatabaseLookupSQL as a constant rather
// than bound as a parameter, that bounds how far before the latest
// pg_stat_statements snapshot the db_names, user_names and last_client
// CTEs look for samples. It is a Go constant with no caller-supplied
// content, so interpolating it into the statement text is safe.
const NameLookupWindowSQL = "1 hour"

// StatementDatabaseLookupSQL is the pair of CTEs, latest and db_names,
// that the server's Top Queries and query-stats SQL, the metrics
// endpoint's statement_database_name filter and the query_metrics MCP
// tool use to resolve a
// pg_stat_statements dbid to the name of the database the statement ran
// in. It is written without the leading WITH so that each statement can
// place it first in its own CTE list, and it expects the connection ID as
// $1. It contains no caller-supplied content.
//
// The database_name column recorded on a pg_stat_statements row is the
// database the probe connected through, not the statement's database: the
// view is cluster-wide, and collectors before GitHub issue #762 read it in
// every database with the extension, so a counter can be stored once per
// probing database. The statement's own database is its dbid, which db_names maps
// to a name from two sources. metrics.pg_stat_database is the primary one:
// the collector records it in every database it monitors at every
// collection, so it knows the OID of every monitored database whether or
// not a backend happened to be caught in it. metrics.pg_stat_activity is
// read as well because it is sampled server-wide, so it can also name a
// database the collector never connects to, such as one the monitoring
// role holds no CONNECT privilege on, provided a backend was seen there.
// Issue #508 is the case this closes: before it, only pg_stat_activity was
// consulted, and a statement whose database had no backend sampled in the
// hour fell back to the probing database_name.
//
// Both reads are bounded to the NameLookupWindowSQL leading up to the
// latest pg_stat_statements snapshot, because neither table has an index on
// the OID and DISTINCT ON would otherwise sort every row in retention. The
// upper bound also keeps a name observed after statement collection stopped
// from renaming the statements already recorded. The
// most recent observation of an OID wins whichever table it came from, so
// a database renamed within the window resolves to its current name; the
// trailing datname only makes a tie at one instant stable.
//
// An OID neither source knows still resolves to nothing, and the callers
// fall back to the probing database_name. They apply that fallback after
// keeping one copy of each counter per collection, so the counter is
// reported under a single name rather than under every probing database.
const StatementDatabaseLookupSQL = `latest AS (
            SELECT MAX(collected_at) AS collected_at
            FROM metrics.pg_stat_statements
            WHERE connection_id = $1
        ),
        db_names AS (
            SELECT DISTINCT ON (datid) datid, datname
            FROM (
                SELECT datid, datname, collected_at
                FROM metrics.pg_stat_database
                WHERE connection_id = $1
                  AND collected_at >= (SELECT collected_at FROM latest)
                      - INTERVAL '` + NameLookupWindowSQL + `'
                  AND collected_at <= (SELECT collected_at FROM latest)
                  AND datid IS NOT NULL
                  AND datname IS NOT NULL
                UNION ALL
                SELECT datid, datname, collected_at
                FROM metrics.pg_stat_activity
                WHERE connection_id = $1
                  AND collected_at >= (SELECT collected_at FROM latest)
                      - INTERVAL '` + NameLookupWindowSQL + `'
                  AND collected_at <= (SELECT collected_at FROM latest)
                  AND datid IS NOT NULL
                  AND datname IS NOT NULL
            ) observed
            ORDER BY datid, collected_at DESC, datname
        )`

// statementDatabaseIDsSQL lists every dbid StatementDatabaseLookupSQL
// resolves, with its name, so a chart resolves a dbid exactly as the Top
// Queries list does. datid is oid on plain PostgreSQL and bigint on a
// ColdFront datastore, so it is cast to bigint for both.
const statementDatabaseIDsSQL = `WITH ` + StatementDatabaseLookupSQL + `
        SELECT datid::bigint, datname FROM db_names`

// LookupStatementDatabases returns every dbid the name lookup resolves
// for one connection, keyed by dbid, with its name. The connection ID is
// the only value bound, as $1, and the caller must already have checked
// that the caller may read that connection.
func LookupStatementDatabases(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
) (map[int64]string, error) {
	rows, err := pool.Query(ctx, statementDatabaseIDsSQL, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make(map[int64]string)
	for rows.Next() {
		var datid int64
		var datname string
		if err := rows.Scan(&datid, &datname); err != nil {
			return nil, err
		}
		names[datid] = datname
	}
	return names, rows.Err()
}

// NewStatementDatabaseFilter builds the filter for the statements of the
// database called name, from the dbid names LookupStatementDatabases
// returned. Both dbid lists are sorted so the bound arguments are stable.
func NewStatementDatabaseFilter(name string, names map[int64]string) *StatementDatabaseFilter {
	f := &StatementDatabaseFilter{
		Name:          name,
		DBIDs:         []int64{},
		ResolvedDBIDs: make([]int64, 0, len(names)),
	}
	for datid, datname := range names {
		f.ResolvedDBIDs = append(f.ResolvedDBIDs, datid)
		if datname == name {
			f.DBIDs = append(f.DBIDs, datid)
		}
	}
	slices.Sort(f.DBIDs)
	slices.Sort(f.ResolvedDBIDs)
	return f
}
