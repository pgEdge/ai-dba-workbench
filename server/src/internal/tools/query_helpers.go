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

// queryResult holds the output of executeReadOnlyQuery.
type queryResult struct {
	ColumnNames  []string
	Rows         [][]any
	WasTruncated bool
	ResultsTSV   string
}

// Stages of executeQueryWith at which a statement can fail.
const (
	queryStageExecute = "execute"
	queryStageRow     = "row"
	queryStageIterate = "iterate"
	queryStageCommit  = "commit"
)

// queryErrorFormatter turns an error raised at one stage of running a
// statement into the message the tool returns.
type queryErrorFormatter func(stage string, err error) string

// defaultQueryErrorFormatter gives the messages executeReadOnlyQuery has always
// returned.
func defaultQueryErrorFormatter(errorPrefix string) queryErrorFormatter {
	return func(stage string, err error) string {
		switch stage {
		case queryStageRow:
			return fmt.Sprintf("Error reading row: %v", err)
		case queryStageIterate:
			return fmt.Sprintf("Error iterating rows: %v", err)
		case queryStageCommit:
			return fmt.Sprintf("Failed to commit transaction: %v", err)
		default:
			return safeToolQueryError(errorPrefix, err)
		}
	}
}

// executeQueryWith is executeReadOnlyQuery with the error messages
// chosen by formatErr. Errors starting the transaction keep their
// usual messages.
func executeQueryWith(
	ctx context.Context,
	pool *pgxpool.Pool,
	sqlQuery string,
	limit int,
	formatErr queryErrorFormatter,
	queryArgs ...any,
) (*queryResult, *mcp.ToolResponse) {
	mt, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
	if errResp != nil {
		return nil, errResp
	}
	defer cleanup()

	rows, err := mt.Tx.Query(ctx, sqlQuery, queryArgs...)
	if err != nil {
		resp, _ := mcp.NewToolError(formatErr(queryStageExecute, err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp
	}
	defer rows.Close()

	// Collect column names
	fieldDescriptions := rows.FieldDescriptions()
	var columnNames []string
	for _, fd := range fieldDescriptions {
		columnNames = append(columnNames, string(fd.Name))
	}

	// Collect row data, stopping once a row beyond limit shows that more
	// exist.
	var results [][]any
	wasTruncated := false
	for rows.Next() {
		if limit > 0 && len(results) >= limit {
			wasTruncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			resp, _ := mcp.NewToolError(formatErr(queryStageRow, err)) //nolint:errcheck // NewToolError always succeeds
			return nil, &resp
		}
		results = append(results, values)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		resp, _ := mcp.NewToolError(formatErr(queryStageIterate, err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp
	}

	resultsTSV := tsv.FormatResults(columnNames, results)

	if err := mt.Commit(); err != nil {
		resp, _ := mcp.NewToolError(formatErr(queryStageCommit, err)) //nolint:errcheck // NewToolError always succeeds
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
// pool, returns at most limit rows (0 means no cap), and formats the
// result as TSV. The cap is enforced while reading rather than relying
// on the LIMIT injectLimitOffset adds, which is absent when the caller
// wrote their own and is lost behind a trailing comment; reading stops
// at the first row beyond the cap, which marks the result truncated.
// queryArgs are bound to the statement's placeholders.
func executeReadOnlyQuery(
	ctx context.Context,
	pool *pgxpool.Pool,
	sqlQuery string,
	limit int,
	errorPrefix string,
	queryArgs ...any,
) (*queryResult, *mcp.ToolResponse) {
	return executeQueryWith(ctx, pool, sqlQuery, limit,
		defaultQueryErrorFormatter(errorPrefix), queryArgs...)
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
