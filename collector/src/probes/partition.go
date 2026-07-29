/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// weeklyPartitionBounds returns the partition name suffix and the
// [from, to) instants for the Monday-aligned week that contains t.
// All math is performed in UTC so the partition naming and range
// boundaries always refer to the same instant regardless of the
// caller's local timezone.
func weeklyPartitionBounds(t time.Time) (nameSuffix string, from, to time.Time) {
	utc := t.UTC()
	daysFromMonday := int(utc.Weekday())
	if utc.Weekday() == time.Sunday {
		daysFromMonday = 6
	} else {
		daysFromMonday--
	}

	year, month, day := utc.Date()
	from = time.Date(year, month, day-daysFromMonday, 0, 0, 0, 0, time.UTC)
	to = from.AddDate(0, 0, 7)
	nameSuffix = from.Format("20060102")
	return nameSuffix, from, to
}

// partitionBoundLiteral formats a time for use as a Postgres range
// boundary literal with an explicit UTC offset so the datastore
// session's TimeZone setting cannot reinterpret it.
const partitionBoundLayout = "2006-01-02 15:04:05Z07:00"

// DatastoreQuerier is the subset of *pgxpool.Conn that the partition
// maintenance helpers use. Accepting the interface rather than the
// concrete connection type keeps the helpers testable: unit tests can
// inject a fake that both records the SQL actually issued (so the
// Workbench-internal marker can be asserted without a database) and
// returns errors from Query, Scan, and Exec.
type DatastoreQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// partitionExistsQuery checks whether a partition of the given name has
// already been created. Tagged as Workbench-internal so partition
// maintenance does not show up in the server's Top Queries panel when
// monitoring queries are hidden; see sqlmarker.Tag for why the marker
// sits immediately after the leading keyword.
var partitionExistsQuery = sqlmarker.Tag(`
        SELECT EXISTS (
            SELECT 1 FROM pg_tables
            WHERE schemaname = 'metrics'
            AND tablename = $1
        )
    `)

// createPartitionSQL builds the tagged DDL that attaches a weekly
// partition to a metrics table. Both relation names are quoted with
// pgx.Identifier, and the range bounds are formatted from time.Time
// values, so no caller-supplied text reaches the statement verbatim.
//
// #nosec G201 - the table name comes from a probe definition, both
// identifiers are quoted by pgx.Identifier, and DDL cannot bind an
// identifier as a placeholder.
func createPartitionSQL(partitionName, tableName string, weekStart, weekEnd time.Time) string {
	return sqlmarker.Tag(fmt.Sprintf(`
        CREATE TABLE IF NOT EXISTS %s
        PARTITION OF %s
        FOR VALUES FROM ('%s') TO ('%s')
    `, pgx.Identifier{"metrics", partitionName}.Sanitize(),
		pgx.Identifier{"metrics", tableName}.Sanitize(),
		weekStart.Format(partitionBoundLayout),
		weekEnd.Format(partitionBoundLayout)))
}

// dropPartitionSQL builds the tagged DDL that drops an expired
// partition. The partition name is quoted with pgx.Identifier.
func dropPartitionSQL(partitionName string) string {
	return sqlmarker.Tag(fmt.Sprintf("DROP TABLE IF EXISTS %s",
		pgx.Identifier{"metrics", partitionName}.Sanitize()))
}

// protectedPartitionsQuery builds the tagged query that finds, for a
// change-tracked probe, the partitions holding the most recent sample
// per connection. The relation name is quoted with pgx.Identifier
// because a table cannot be named by a bind parameter.
//
// #nosec G201 - the table name comes from a probe definition and is
// quoted by pgx.Identifier; a relation cannot be bound as a
// placeholder.
func protectedPartitionsQuery(tableName string) string {
	table := pgx.Identifier{"metrics", tableName}.Sanitize()
	return sqlmarker.Tag(fmt.Sprintf(`
        SELECT DISTINCT
            c.relname AS partition_name
        FROM (
            SELECT connection_id, MAX(collected_at) as max_collected_at
            FROM %[1]s
            GROUP BY connection_id
        ) latest
        JOIN %[1]s tbl ON tbl.connection_id = latest.connection_id
            AND tbl.collected_at = latest.max_collected_at
        JOIN pg_class c ON c.oid = tbl.tableoid
    `, table))
}

// partitionCandidatesQuery is the tagged catalog query listing every
// partition of a parent table together with its bound expression. The
// parent name is compared as a value rather than interpolated as an
// identifier, so it binds as $1.
var partitionCandidatesQuery = sqlmarker.Tag(`
        SELECT
            c.relname AS partition_name,
            pg_get_expr(c.relpartbound, c.oid) AS partition_bound
        FROM pg_class c
        JOIN pg_namespace n ON c.relnamespace = n.oid
        JOIN pg_inherits i ON c.oid = i.inhrelid
        JOIN pg_class p ON i.inhparent = p.oid
        WHERE n.nspname = 'metrics'
        AND p.relname = $1
        AND c.relkind = 'r'
        ORDER BY c.relname
    `)

// EnsurePartition creates a partition for the given week if it doesn't exist
func EnsurePartition(ctx context.Context, conn DatastoreQuerier, tableName string, timestamp time.Time) error {
	nameSuffix, weekStart, weekEnd := weeklyPartitionBounds(timestamp)

	partitionName := fmt.Sprintf("%s_%s", tableName, nameSuffix)

	// Check if partition already exists
	var exists bool
	// partitionExistsQuery is a fixed SQL literal that is only non-constant
	// because sqlmarker.Tag inserts the internal marker comment into it;
	// the partition name is bound as $1 and never interpolated.
	//nosemgrep: go_sql_rule-concat-sqli -- fixed literal tagged by sqlmarker.Tag; partition name bound as $1
	err := conn.QueryRow(ctx, partitionExistsQuery, partitionName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if partition exists: %w", err)
	}

	if exists {
		return nil
	}

	// Create the partition
	createSQL := createPartitionSQL(
		partitionName, tableName, weekStart, weekEnd)

	// createPartitionSQL interpolates only an internal probe table name,
	// quoted with pgx.Identifier, and range bounds formatted from
	// time.Time; CREATE TABLE cannot name a relation with a placeholder.
	//nosemgrep: go_sql_rule-concat-sqli -- DDL identifiers come from probe definitions and are sanitized by pgx.Identifier
	_, err = conn.Exec(ctx, createSQL)
	if err != nil {
		// Check if this is a "relation already exists" error (42P07)
		// This can happen due to race conditions when multiple goroutines
		// try to create the same partition simultaneously
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P07" {
			// Partition was created by another goroutine, that's fine
			return nil
		}
		return fmt.Errorf("failed to create partition %s: %w", partitionName, err)
	}

	logger.Infof("Created partition %s for table %s", partitionName, tableName)
	return nil
}

// DropExpiredPartitions drops partitions that contain only expired data.
//
// Implementation note: the pgx protocol does not permit issuing a new
// command (for example Exec) on a connection that still has an open
// Rows result set. Doing so surfaces as a "conn busy" error. The two
// catalog queries used here are therefore fully drained and closed
// before any DROP TABLE is issued against the same connection.
func DropExpiredPartitions(ctx context.Context, conn DatastoreQuerier, tableName string, retentionDays int) (int, error) {
	// Calculate the cutoff timestamp
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// For change-tracked probes (pg_settings, pg_hba_file_rules,
	// pg_ident_file_mappings, pg_server_info) find the most recent
	// partition with data for each connection. These partitions must
	// never be dropped, regardless of age.
	protectedPartitions, err := loadProtectedPartitions(ctx, conn, tableName)
	if err != nil {
		return 0, err
	}

	// Collect candidate partitions fully into memory before any DROP
	// is issued, so the underlying connection is not holding an open
	// Rows result when we later call Exec on it.
	candidates, err := loadPartitionCandidates(ctx, conn, tableName)
	if err != nil {
		return 0, err
	}

	var droppedCount int
	for _, c := range candidates {
		if protectedPartitions[c.name] {
			logger.Infof("Skipping protected partition %s (contains most recent data for %s)", c.name, tableName)
			continue
		}

		endTimestamp, ok := parsePartitionEnd(c.name, c.bound)
		if !ok {
			continue
		}

		if !endTimestamp.Before(cutoff) {
			continue
		}

		// c.name is a partition name read back from pg_class, and
		// dropPartitionSQL quotes it with pgx.Identifier; DROP TABLE
		// cannot name its relation with a bind parameter.
		//nosemgrep: go_sql_rule-concat-sqli -- partition name comes from pg_class and is sanitized by pgx.Identifier
		if _, err := conn.Exec(ctx, dropPartitionSQL(c.name)); err != nil {
			logger.Errorf("Warning: failed to drop partition %s: %v", c.name, err)
			continue
		}
		logger.Infof("Dropped expired partition %s (end: %s, cutoff: %s)",
			c.name, endTimestamp.Format("2006-01-02"), cutoff.Format("2006-01-02"))
		droppedCount++
	}

	if droppedCount > 0 {
		logger.Infof("Dropped %d expired partition(s) for table %s", droppedCount, tableName)
	}

	return droppedCount, nil
}

// partitionCandidate is a partition row returned by
// loadPartitionCandidates, captured in memory so its parent Rows can
// be closed before any further command is issued on the connection.
type partitionCandidate struct {
	name  string
	bound string
}

// loadProtectedPartitions returns the set of partition names that
// must be preserved because they hold the most recent sample for a
// change-tracked probe. The returned map is empty for probes that
// are not change-tracked. The query's Rows is fully drained and
// closed before the function returns so the caller can safely reuse
// the same connection for subsequent commands.
func loadProtectedPartitions(ctx context.Context, conn DatastoreQuerier, tableName string) (map[string]bool, error) {
	protected := make(map[string]bool)

	switch tableName {
	case "pg_settings", "pg_hba_file_rules", "pg_ident_file_mappings", "pg_server_info":
		// change-tracked; proceed
	default:
		return protected, nil
	}

	// tableName is one of the four change-tracked probe names matched by
	// the switch above, and protectedPartitionsQuery quotes it with
	// pgx.Identifier; a relation cannot be bound as a placeholder.
	//nosemgrep: go_sql_rule-concat-sqli -- table name is one of four internal probe names, sanitized by pgx.Identifier
	rows, err := conn.Query(ctx, protectedPartitionsQuery(tableName))
	if err != nil {
		return nil, fmt.Errorf("failed to query protected partitions for %s: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var partitionName string
		if err := rows.Scan(&partitionName); err != nil {
			return nil, fmt.Errorf("failed to scan protected partition name: %w", err)
		}
		protected[partitionName] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating protected partitions for %s: %w", tableName, err)
	}

	if len(protected) > 0 {
		logger.Infof("Protected %d partition(s) for %s containing most recent data per connection", len(protected), tableName)
	}
	return protected, nil
}

// loadPartitionCandidates returns every partition of tableName along
// with its bound expression, read fully into memory so the Rows
// result is closed before the caller issues DROP TABLE on the same
// connection. This is required because pgx reports "conn busy" when
// a command is sent on a connection that still has open rows.
func loadPartitionCandidates(ctx context.Context, conn DatastoreQuerier, tableName string) ([]partitionCandidate, error) {
	// partitionCandidatesQuery is a fixed SQL literal that is only
	// non-constant because sqlmarker.Tag inserts the internal marker
	// comment into it; the parent table name is bound as $1.
	//nosemgrep: go_sql_rule-concat-sqli -- fixed literal tagged by sqlmarker.Tag; parent table name bound as $1
	rows, err := conn.Query(ctx, partitionCandidatesQuery, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query partitions: %w", err)
	}
	defer rows.Close()

	var out []partitionCandidate
	for rows.Next() {
		var c partitionCandidate
		if err := rows.Scan(&c.name, &c.bound); err != nil {
			return nil, fmt.Errorf("failed to scan partition info: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating partitions: %w", err)
	}
	return out, nil
}

// parsePartitionEnd extracts the upper-bound timestamp from a
// partition bound expression of the form
// FOR VALUES FROM ('...') TO ('...'). It returns the parsed time
// and true on success; on failure it logs a warning and returns
// the zero time and false so the caller can skip the partition
// without aborting the full GC pass.
func parsePartitionEnd(partitionName, partitionBound string) (time.Time, bool) {
	toIdx := strings.Index(partitionBound, "TO ('")
	if toIdx == -1 {
		logger.Errorf("Warning: failed to find TO clause in partition bound for %s: %s", partitionName, partitionBound)
		return time.Time{}, false
	}

	timestampStart := toIdx + 5 // len("TO ('")
	timestampEnd := strings.Index(partitionBound[timestampStart:], "'")
	if timestampEnd == -1 {
		logger.Errorf("Warning: failed to find end quote in partition bound for %s: %s", partitionName, partitionBound)
		return time.Time{}, false
	}

	timestampStr := partitionBound[timestampStart : timestampStart+timestampEnd]

	// Try timezone formats: with minutes (+05:30), without minutes (+05), then legacy (no tz)
	tzFormats := []string{
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05",
	}
	for _, layout := range tzFormats {
		t, err := time.Parse(layout, timestampStr)
		if err == nil {
			return t, true
		}
	}
	logger.Errorf("Warning: failed to parse timestamp in partition bound for %s: %q", partitionName, timestampStr)
	return time.Time{}, false
}
