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
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// statementDatabaseParam names the GET /api/v1/metrics/query parameter
// that restricts pg_stat_statements rows to the statements of one
// database. database_name cannot do this: on that probe it is the
// database the statistics were collected through, and the statement list
// is server-wide, so one probing database carries every database's
// counters for a queryid (collectors before GitHub issue #762 stored that
// list once per probing database).
const statementDatabaseParam = "statement_database_name"

// statementDatabaseProbe is the only probe whose rows carry a dbid.
const statementDatabaseProbe = "pg_stat_statements"

// statementDatabaseLookupFunc returns every dbid the name lookup
// resolves for a connection, keyed by dbid, with its name.
type statementDatabaseLookupFunc func(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
) (map[int64]string, error)

// lookupStatementDatabases is the production lookup, shared with the
// query_metrics MCP tool.
var lookupStatementDatabases statementDatabaseLookupFunc = metrics.LookupStatementDatabases

// applyStatementDatabaseFilter validates the optional
// statement_database_name parameter and, when it is present, resolves it
// into filters.StatementDatabase. The name is caller-supplied, so it is
// checked with database.ValidateDatabaseName and only ever bound as a
// parameter. The filter needs the probe's dbid column and one
// connection's name lookup, so it is refused for any other probe and for
// more than one connection. connectionIDs have already passed the RBAC
// check, and the lookup reads only that connection's rows. It returns
// false after sending an error response.
func (h *MetricsHandler) applyStatementDatabaseFilter(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	probeName string,
	connectionIDs []int,
	filters *metrics.MetricFilters,
) bool {
	if !r.URL.Query().Has(statementDatabaseParam) {
		return true
	}
	name := ParseQueryString(r, statementDatabaseParam)
	if err := database.ValidateDatabaseName(name); err != nil {
		RespondError(w, http.StatusBadRequest,
			"Invalid "+statementDatabaseParam+": "+err.Error())
		return false
	}
	if probeName != statementDatabaseProbe {
		RespondError(w, http.StatusBadRequest,
			statementDatabaseParam+" applies only to probe_name "+statementDatabaseProbe)
		return false
	}
	if len(connectionIDs) != 1 {
		RespondError(w, http.StatusBadRequest,
			statementDatabaseParam+" requires exactly one connection_id")
		return false
	}

	lookup := h.statementDatabaseLookupFn
	if lookup == nil {
		lookup = lookupStatementDatabases
	}
	names, err := lookup(ctx, h.datastore.GetPool(), connectionIDs[0])
	if err != nil {
		log.Printf("[ERROR] Failed to resolve statement databases for connection %d: %s", connectionIDs[0], logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: the connection ID is an integer and err is sanitized via logging.SanitizeForLog
		RespondError(w, http.StatusInternalServerError,
			fmt.Sprintf("Failed to resolve %s", statementDatabaseParam))
		return false
	}

	filters.StatementDatabase = metrics.NewStatementDatabaseFilter(name, names)
	return true
}
