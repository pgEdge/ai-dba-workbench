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
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/pkg/rollback"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

// TestQueryTool creates the test_query tool for validating SQL query correctness
func TestQueryTool(dbClient *database.Client, resolver *ConnectionResolver) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "test_query",
			Description: `Validate a SQL query for correctness without executing it.

<database_context>
Specify connection_id to target a particular monitored database.
Use list_connections to discover available connection IDs and
their default databases. Optionally provide database_name to
override the default database for the connection. If
connection_id is omitted, the tool uses the currently selected
connection.
</database_context>

<usecase>
Use when:
- You have generated a SQL query and need to verify it is valid before
  presenting it to the user
- You want to check that table names, column names, and types are correct
- You need to validate multi-statement SQL scripts
</usecase>

<what_it_returns>
Returns one of two results:
- A success message confirming the query is valid
- The PostgreSQL error message explaining why the query is invalid
</what_it_returns>

<when_not_to_use>
DO NOT use for:
- Performance analysis (use execute_explain instead)
- Actually running the query (use query_database instead)
</when_not_to_use>

<important>
- This tool validates ANY SQL statement type (SELECT, INSERT, UPDATE, DELETE, DDL)
- The query is never executed; EXPLAIN is used inside a read-only transaction
- Multi-statement queries are supported; each statement is validated individually
- Always use this tool to validate SQL you generate before presenting it to the user
</important>`,
			CompactDescription: `Validate SQL query correctness without executing it. Specify connection_id to target a database; use list_connections to discover IDs. Uses EXPLAIN in a read-only transaction.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "ID of the monitored database connection to use. Use list_connections to discover available IDs. If omitted, uses the currently selected connection.",
					},
					"database_name": map[string]any{
						"type":        "string",
						"description": "Database name to connect to. If omitted, uses the connection's default database.",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "The SQL query to validate",
					},
				},
				Required: []string{"query"},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			// Extract and validate parameters
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return mcp.NewToolError("Parameter 'query' is required and must be a non-empty string")
			}

			if mayRunSideEffects(query) {
				return mcp.NewToolError("The query calls a function with side " +
					"effects (for example pg_terminate_backend, pg_reload_conf, " +
					"set_config or nextval) and does not start with a statement " +
					"EXPLAIN only plans, such as SELECT, so validating it could " +
					"run it. Remove any leading ANALYZE, VERBOSE or option list.")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Resolve connection (explicit connection_id or fallback)
			resolved, errResp := resolver.Resolve(ctx, deniedActionConnectionQueryValidate, args, dbClient)
			if errResp != nil {
				return *errResp, nil
			}
			pool := resolved.Pool

			// Open a read-only transaction
			rot, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
			if errResp != nil {
				return *errResp, nil
			}
			defer cleanup()

			// A query carrying $N placeholders cannot be planned by
			// the extended protocol, which has no values to infer the
			// parameter types from and fails with "could not determine
			// data type of parameter $1". PostgreSQL 16 added EXPLAIN
			// (GENERIC_PLAN) for exactly this case, so use it where the
			// server supports it and fall through to the plain EXPLAIN
			// path, and its error, where it does not.
			if containsParamPlaceholder(query) {
				generic, verErr := supportsGenericPlan(ctx, rot.Tx)
				if verErr != nil {
					return mcp.NewToolError(fmt.Sprintf(
						"Query validation failed: %v", verErr))
				}
				if generic {
					return validateGenericPlan(ctx, rot.Tx, query)
				}
			}

			// Try EXPLAIN on the full query first. A query of several
			// statements fails as one prepared statement, and that
			// failure aborts the transaction, so take a savepoint to
			// unwind to before the per-statement retry.
			if _, spErr := rot.Tx.Exec(ctx,
				"SAVEPOINT "+testQuerySavepoint); spErr != nil {
				return mcp.NewToolError(fmt.Sprintf(
					"Query validation failed: %v", spErr))
			}
			explainQuery := sqllex.PlainExplainPrefix + query
			rows, err := rot.Tx.Query(ctx, explainQuery)
			if err != nil {
				// Check if the error is about multiple statements
				if isMultipleStatementError(err) {
					if rbErr := rollback.ToSavepoint[pgconn.CommandTag](
						ctx, rot.Tx, testQuerySavepoint); rbErr != nil {
						return mcp.NewToolError(fmt.Sprintf(
							"Query validation failed: %v", rbErr))
					}

					// Split and validate each statement individually
					statements := splitStatements(query)
					if len(statements) == 0 {
						return mcp.NewToolError("Query contains no valid SQL statements")
					}

					for i, stmt := range statements {
						stmtExplain := sqllex.PlainExplainPrefix + stmt
						stmtRows, stmtErr := rot.Tx.Query(ctx, stmtExplain)
						if stmtErr != nil {
							return mcp.NewToolError(fmt.Sprintf(
								"Statement %d is invalid: %v\n\nStatement: %s",
								i+1, stmtErr, stmt))
						}
						stmtRows.Close()
					}
				} else {
					return mcp.NewToolError(fmt.Sprintf("Query validation failed: %v", err))
				}
			} else {
				rows.Close()
			}

			// Log the validation
			logging.Info("test_query_executed",
				"query_length", len(query),
				"result", "valid",
			)

			return mcp.NewToolSuccess("Query is valid.")
		},
	}
}

// testQuerySavepoint is the savepoint the whole-query EXPLAIN runs
// under, so that its failure does not leave the transaction aborted
// for the per-statement retry.
const testQuerySavepoint = "workbench_test_query"

// mayRunSideEffects reports whether validating query could run a
// side-effecting function. The tool validates by putting
// sqllex.PlainExplainPrefix in front of the query, or of each of its
// statements, whose closed option list already makes a leading ANALYZE
// a syntax error rather than an EXPLAIN ANALYZE. A read-only
// transaction does not stop a function such as pg_terminate_backend
// (issue #646), so as a second guard a query calling such a function is
// validated only when the query and each of its statements, split
// under either reading of a plain '...' literal, begin with a statement
// EXPLAIN merely plans.
func mayRunSideEffects(query string) bool {
	if !sqllex.CallsSideEffectingFunctionUnderAnyReading(query) {
		return false
	}
	if !explainOnlyPlans(query) {
		return true
	}
	for _, reading := range sqllex.StringReadings {
		for _, stmt := range sqllex.SplitStatements(query, reading) {
			if !explainOnlyPlans(stmt) {
				return true
			}
		}
	}
	return false
}

// genericPlanMinVersionNum is the server_version_num of the first
// release with EXPLAIN (GENERIC_PLAN), which is PostgreSQL 16.
const genericPlanMinVersionNum = 160000

// validateGenericPlan validates a query whose statements carry $N
// parameter placeholders, planning each statement with EXPLAIN
// (GENERIC_PLAN). The query is split first, and each statement is
// planned on its own (see explainGenericPlan).
func validateGenericPlan(ctx context.Context, tx pgx.Tx, query string) (mcp.ToolResponse, error) {
	statements := splitStatements(query)
	if len(statements) == 0 {
		return mcp.NewToolError("Query contains no valid SQL statements")
	}

	for i, stmt := range statements {
		if err := explainGenericPlan(ctx, tx, stmt); err != nil {
			return mcp.NewToolError(fmt.Sprintf(
				"Statement %d is invalid: %v\n\nStatement: %s",
				i+1, err, stmt))
		}
	}

	logging.Info("test_query_executed",
		"query_length", len(query),
		"result", "valid",
	)

	return mcp.NewToolSuccess("Query is valid.")
}

// explainGenericPlan plans one statement with EXPLAIN (GENERIC_PLAN)
// on the transaction's own connection. It goes through pgconn rather
// than pgx, which would otherwise read the $N placeholders as bind
// parameters it has no values for, and uses the extended query
// protocol with every placeholder bound to NULL, which GENERIC_PLAN
// ignores. The extended protocol refuses to prepare more than one
// command, so SQL the splitter failed to divide is rejected rather
// than run, as the simple protocol would run it, including a COMMIT
// that ends the read-only transaction.
func explainGenericPlan(ctx context.Context, tx pgx.Tx, stmt string) error {
	pgConn := tx.Conn().PgConn()
	sd, err := pgConn.Prepare(ctx, "", "EXPLAIN (GENERIC_PLAN) "+stmt, nil)
	if err != nil {
		return err
	}
	params := make([][]byte, len(sd.ParamOIDs))
	return pgConn.ExecPrepared(ctx, "", params, nil, nil).Read().Err
}

// supportsGenericPlan reports whether the server is new enough for
// EXPLAIN (GENERIC_PLAN).
func supportsGenericPlan(ctx context.Context, tx pgx.Tx) (bool, error) {
	var versionNum int
	err := tx.QueryRow(ctx,
		"SELECT current_setting('server_version_num')::int").Scan(&versionNum)
	if err != nil {
		return false, err
	}
	return versionNum >= genericPlanMinVersionNum, nil
}

// containsParamPlaceholder reports whether the SQL contains a $N
// parameter placeholder such as $1.
func containsParamPlaceholder(sql string) bool {
	for i := 0; i+1 < len(sql); i++ {
		if sql[i] == '$' && sql[i+1] >= '1' && sql[i+1] <= '9' {
			return true
		}
	}
	return false
}

// isMultipleStatementError checks if a PostgreSQL error indicates that
// multiple commands were provided where only one was expected.
func isMultipleStatementError(err error) bool {
	var pgErr *pgconn.PgError
	if ok := isPgError(err, &pgErr); ok {
		// 42601 is syntax_error; PostgreSQL uses this code for
		// "cannot insert multiple commands into a prepared statement"
		if pgErr.Code == "42601" && strings.Contains(pgErr.Message, "multiple commands") {
			return true
		}
	}
	// Fallback: check the error message string
	return strings.Contains(err.Error(), "multiple commands") ||
		strings.Contains(err.Error(), "cannot insert multiple commands")
}

// isPgError attempts to extract a *pgconn.PgError from the given error.
func isPgError(err error, target **pgconn.PgError) bool {
	var pgErr *pgconn.PgError
	if ok := errors.As(err, &pgErr); ok { //nolint:errorlint // using errors.As
		*target = pgErr
		return true
	}
	return false
}

// splitStatements splits a SQL string into its statements with the
// shared lexer, reading a plain '...' literal as a standard string (see
// sqllex.SplitStatements).
func splitStatements(sql string) []string {
	return sqllex.SplitStatements(sql, sqllex.StandardStrings)
}
