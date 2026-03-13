/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// parseLimitOffset extracts limit and offset parameters from the tool
// arguments, clamping limit to the range [1, 1000] with a default of
// 100 and offset to a minimum of 0 with a default of 0.
func parseLimitOffset(args map[string]any) (limit int, offset int) {
	limit = 100
	if limitVal, ok := args["limit"]; ok {
		switch v := limitVal.(type) {
		case float64:
			limit = int(v)
		case int:
			limit = v
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	offset = 0
	if offsetVal, ok := args["offset"]; ok {
		switch v := offsetVal.(type) {
		case float64:
			offset = int(v)
		case int:
			offset = v
		}
	}
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}

// injectLimitOffset appends LIMIT and OFFSET clauses to sqlQuery when
// the query is a SELECT-like statement and does not already contain
// them. Non-SELECT queries (INSERT, UPDATE, DELETE, DDL, etc.) are
// returned unchanged because LIMIT/OFFSET is not valid SQL for those
// statement types. The injected LIMIT is limit+1 so the caller can
// detect whether additional rows exist.
func injectLimitOffset(sqlQuery string, limit, offset int) (modified string, hadLimit, hadOffset bool) {
	// Only inject LIMIT/OFFSET for SELECT-like statements.
	if !isSelectQuery(sqlQuery) {
		return sqlQuery, false, false
	}

	hadLimit = hasLimitClause(sqlQuery)
	hadOffset = hasOffsetClause(sqlQuery)

	modified = sqlQuery
	if limit > 0 && !hadLimit {
		modified = fmt.Sprintf("%s LIMIT %d", modified, limit+1)
	}
	if offset > 0 && !hadOffset {
		modified = fmt.Sprintf("%s OFFSET %d", modified, offset)
	}

	return modified, hadLimit, hadOffset
}

// queryResult holds the output of executeQuery / executeReadOnlyQuery.
type queryResult struct {
	ColumnNames  []string
	Rows         [][]any
	WasTruncated bool
	ResultsTSV   string
}

// executeQuery runs sqlQuery inside a transaction on pool, collects
// rows, detects truncation against limit, and formats the result as
// TSV. When readOnly is true a read-only transaction is used;
// otherwise a regular read-write transaction is started. The
// hadExistingLimit flag indicates whether the original query already
// contained a LIMIT clause (in which case truncation detection is
// skipped).
func executeQuery(
	ctx context.Context,
	pool *pgxpool.Pool,
	sqlQuery string,
	limit int,
	hadExistingLimit bool,
	errorPrefix string,
	readOnly bool,
) (*queryResult, *mcp.ToolResponse) {
	var mt *ManagedTx
	var errResp *mcp.ToolResponse
	var cleanup func()

	if readOnly {
		mt, errResp, cleanup = BeginReadOnlyTx(ctx, pool)
	} else {
		mt, errResp, cleanup = BeginTx(ctx, pool)
	}
	if errResp != nil {
		return nil, errResp
	}
	defer cleanup()

	rows, err := mt.Tx.Query(ctx, sqlQuery)
	if err != nil {
		resp, _ := mcp.NewToolError(safeToolQueryError(errorPrefix, err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp
	}
	defer rows.Close()

	// Collect column names
	fieldDescriptions := rows.FieldDescriptions()
	var columnNames []string
	for _, fd := range fieldDescriptions {
		columnNames = append(columnNames, string(fd.Name))
	}

	// Collect row data
	var results [][]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			resp, _ := mcp.NewToolError(fmt.Sprintf("Error reading row: %v", err)) //nolint:errcheck // NewToolError always succeeds
			return nil, &resp
		}
		results = append(results, values)
	}

	if err := rows.Err(); err != nil {
		resp, _ := mcp.NewToolError(fmt.Sprintf("Error iterating rows: %v", err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp
	}

	// Detect truncation (we fetched limit+1 to check)
	wasTruncated := false
	if !hadExistingLimit && limit > 0 && len(results) > limit {
		wasTruncated = true
		results = results[:limit]
	}

	resultsTSV := tsv.FormatResults(columnNames, results)

	if err := mt.Commit(); err != nil {
		resp, _ := mcp.NewToolError(fmt.Sprintf("Failed to commit transaction: %v", err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp
	}

	return &queryResult{
		ColumnNames:  columnNames,
		Rows:         results,
		WasTruncated: wasTruncated,
		ResultsTSV:   resultsTSV,
	}, nil
}

// executeReadOnlyQuery runs sqlQuery inside a read-only transaction on
// pool. This is a convenience wrapper around executeQuery with
// readOnly=true, kept for backward compatibility with other tools.
func executeReadOnlyQuery(
	ctx context.Context,
	pool *pgxpool.Pool,
	sqlQuery string,
	limit int,
	hadExistingLimit bool,
	errorPrefix string,
) (*queryResult, *mcp.ToolResponse) {
	return executeQuery(ctx, pool, sqlQuery, limit, hadExistingLimit, errorPrefix, true)
}

// formatPaginatedResults writes the results header with pagination
// information into a string builder. The truncationHint is appended
// to the truncation message (e.g. "or count_rows for total" vs
// "or increase limit").
func formatPaginatedResults(
	sb *strings.Builder,
	qr *queryResult,
	sqlQuery string,
	limit int,
	offset int,
	truncationHint string,
) {
	fmt.Fprintf(sb, "SQL Query:\n%s\n\n", sqlQuery)

	if offset > 0 {
		startRow := offset + 1
		endRow := offset + len(qr.Rows)
		if qr.WasTruncated {
			fmt.Fprintf(sb, "Results (rows %d-%d, more available - use offset=%d for next page):\n%s",
				startRow, endRow, offset+limit, qr.ResultsTSV)
		} else {
			fmt.Fprintf(sb, "Results (rows %d-%d):\n%s",
				startRow, endRow, qr.ResultsTSV)
		}
	} else if qr.WasTruncated {
		fmt.Fprintf(sb, "Results (%d rows shown, more available - use offset=%d for next page%s):\n%s",
			len(qr.Rows), limit, truncationHint, qr.ResultsTSV)
	} else {
		fmt.Fprintf(sb, "Results (%d rows):\n%s",
			len(qr.Rows), qr.ResultsTSV)
	}
}

// safeToolQueryError sanitizes a query execution error for MCP tool
// responses. For recognized PostgreSQL errors it returns the server
// message; for known transient conditions it returns a descriptive
// message. All other errors are logged and replaced with a generic
// message to avoid leaking internal details.
func safeToolQueryError(prefix string, err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Detail != "" {
			return fmt.Sprintf("%s%s (%s)", prefix, pgErr.Message, pgErr.Detail)
		}
		return fmt.Sprintf("%s%s", prefix, pgErr.Message)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("%squery timed out", prefix)
	}

	if errors.Is(err, context.Canceled) {
		return fmt.Sprintf("%squery was canceled", prefix)
	}

	msg := err.Error()
	lower := strings.ToLower(msg)
	connectionPatterns := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"no such host",
		"i/o timeout",
		"broken pipe",
		"closed network connection",
		"failed to connect",
	}
	for _, p := range connectionPatterns {
		if strings.Contains(lower, p) {
			return fmt.Sprintf("%sconnection error; the database may be unreachable", prefix)
		}
	}

	if (strings.Contains(lower, "expected") && strings.Contains(lower, "arguments")) ||
		strings.Contains(lower, "arguments, got") {
		return fmt.Sprintf(
			"%squery contains parameter placeholders ($1, $2, ...) "+
				"that require values; these cannot be executed directly", prefix)
	}

	log.Printf("MCP tool query error (non-PgError): %v", err)
	return fmt.Sprintf("%san internal error occurred", prefix)
}
