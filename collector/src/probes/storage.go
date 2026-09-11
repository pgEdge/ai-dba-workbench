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
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// buildMetricsInsert builds one multi-row INSERT for a batch of metric
// rows, returning the statement and its bind arguments. Column and
// table identifiers are quoted by the caller and by pgx.Identifier, and
// every value is passed as a bind parameter, so the statement carries
// no interpolated data.
//
// The statement is tagged as Workbench-internal so the server's Top
// Queries panel can filter out the collector's own write traffic
// against the datastore; see sqlmarker.Tag for why the marker has to
// sit immediately after the leading keyword. This is the single
// chokepoint through which every probe's metric writes pass, which is
// why the tagging lives here rather than in the individual probes.
func buildMetricsInsert(fullTableName string, columns []string, batch [][]any) (string, []any) {
	// Build column list with quoted identifiers
	columnList := ""
	for idx, col := range columns {
		if idx > 0 {
			columnList += ", "
		}
		columnList += pgx.Identifier{col}.Sanitize()
	}

	// Build VALUES clause with placeholders
	valuesClause := ""
	args := make([]any, 0, len(batch)*len(columns))
	for rowIdx, row := range batch {
		if rowIdx > 0 {
			valuesClause += ", "
		}
		valuesClause += "("
		for colIdx := range columns {
			if colIdx > 0 {
				valuesClause += ", "
			}
			placeholderNum := rowIdx*len(columns) + colIdx + 1
			valuesClause += fmt.Sprintf("$%d", placeholderNum)
			args = append(args, row[colIdx])
		}
		valuesClause += ")"
	}

	query := sqlmarker.Tag(fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s",
		fullTableName, columnList, valuesClause))

	return query, args
}

// StoreMetrics stores metrics using batched INSERT statements.
func StoreMetrics(ctx context.Context, conn *pgxpool.Conn, tableName string, columns []string, values [][]any) error {
	if len(values) == 0 {
		return nil // Nothing to store
	}

	fullTableName := pgx.Identifier{"metrics", tableName}.Sanitize()

	// Begin transaction
	txn, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if rerr := txn.Rollback(ctx); rerr != nil {
				logger.Errorf("Error rolling back transaction: %v", rerr)
			}
		}
	}()

	// Build multi-value INSERT statement
	// INSERT INTO table (col1, col2, ...) VALUES ($1, $2, ...), ($N+1, $N+2, ...), ...
	const batchSize = 100 // Insert up to 100 rows per statement

	for i := 0; i < len(values); i += batchSize {
		end := i + batchSize
		if end > len(values) {
			end = len(values)
		}
		batch := values[i:end]

		query, args := buildMetricsInsert(fullTableName, columns, batch)
		if _, err := txn.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to execute INSERT: %w", err)
		}
	}

	// Commit transaction
	if err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
