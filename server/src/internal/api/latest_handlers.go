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
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// LatestSnapshotHandler handles GET /api/v1/metrics/latest, returning
// the most recent collected row for each unique entity in a probe table.
type LatestSnapshotHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore
}

// LatestSnapshotResponse holds the paginated result of a latest-snapshot
// query.
type LatestSnapshotResponse struct {
	Rows       []map[string]any `json:"rows"`
	TotalCount int              `json:"total_count"`
}

// internalColumns lists column names excluded from the response because
// they are internal bookkeeping fields.
var internalColumns = map[string]bool{
	"connection_id": true,
	"collected_at":  true,
	"inserted_at":   true,
}

// columnCache holds cached column discovery results for a probe table.
type columnCache struct {
	allColumns []string
	colTypes   map[string]string
	cachedAt   time.Time
}

// tableColumnCache stores column metadata per probe table with TTL.
var tableColumnCache sync.Map // map[string]*columnCache

// columnCacheTTL is the duration before cached column metadata expires.
const columnCacheTTL = 5 * time.Minute

// dbColCache holds cached database column resolution results.
type dbColCache struct {
	dbCol    string
	cachedAt time.Time
}

// tableDBColCache stores database column resolution per probe table.
var tableDBColCache sync.Map // map[string]*dbColCache

// NewLatestSnapshotHandler creates a new LatestSnapshotHandler.
func NewLatestSnapshotHandler(
	datastore *database.Datastore,
	authStore *auth.AuthStore,
) *LatestSnapshotHandler {
	return &LatestSnapshotHandler{
		datastore: datastore,
		authStore: authStore,
	}
}

// RegisterRoutes registers the latest-snapshot endpoint on the mux.
func (h *LatestSnapshotHandler) RegisterRoutes(
	mux *http.ServeMux,
	authWrapper func(http.HandlerFunc) http.HandlerFunc,
) {
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Latest snapshot")
		mux.HandleFunc("/api/v1/metrics/latest",
			authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/metrics/latest",
		authWrapper(h.handleLatestSnapshot))
}

// handleLatestSnapshot handles GET /api/v1/metrics/latest.
func (h *LatestSnapshotHandler) handleLatestSnapshot(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	// Parse required connection_id
	connIDStr := ParseQueryString(r, "connection_id")
	if connIDStr == "" {
		RespondError(w, http.StatusBadRequest,
			"connection_id is required")
		return
	}
	connectionID, err := strconv.Atoi(connIDStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest,
			"Invalid connection_id")
		return
	}

	// RBAC check
	rbacChecker := auth.NewRBACChecker(h.authStore)
	canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connectionID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			fmt.Sprintf("Permission denied: you do not have access to connection %d", connectionID))
		return
	}

	// Parse required probe_name
	probeName := ParseQueryString(r, "probe_name")
	if probeName == "" {
		RespondError(w, http.StatusBadRequest,
			"probe_name is required")
		return
	}
	if !metrics.IsValidIdentifier(probeName) {
		RespondError(w, http.StatusBadRequest,
			"Invalid probe_name: must contain only letters, numbers, and underscores")
		return
	}

	// Parse optional database_name filter
	databaseName := ParseQueryString(r, "database_name")

	// Parse optional exclude_schemas filter. By default no schemas
	// are excluded. Pass a comma-separated list of schema names to
	// exclude specific schemas.
	var excludeSchemas []string
	excludeSchemasStr := ParseQueryString(r, "exclude_schemas")
	if excludeSchemasStr == "none" || excludeSchemasStr == "" {
		excludeSchemas = nil
	} else {
		for _, s := range strings.Split(excludeSchemasStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				if !metrics.IsValidIdentifier(s) {
					RespondError(w, http.StatusBadRequest,
						fmt.Sprintf("Invalid schema name in exclude_schemas: %q", s))
					return
				}
				excludeSchemas = append(excludeSchemas, s)
			}
		}
	}

	// Parse optional order_by; default is resolved after column
	// discovery so that we can fall back to the first dimension column.
	orderBy := ParseQueryString(r, "order_by")
	if orderBy != "" && !metrics.IsValidIdentifier(orderBy) {
		RespondError(w, http.StatusBadRequest,
			"Invalid order_by: must contain only letters, numbers, and underscores")
		return
	}

	// Parse optional order direction (default "desc")
	order := strings.ToLower(ParseQueryString(r, "order"))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		RespondError(w, http.StatusBadRequest,
			"Invalid order: must be asc or desc")
		return
	}

	// Parse optional limit (default 20, max 100)
	limit := 20
	if limitStr := ParseQueryString(r, "limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err != nil || l < 1 {
			RespondError(w, http.StatusBadRequest,
				"Invalid limit: must be a positive integer")
			return
		}
		limit = l
	}
	if limit > 100 {
		limit = 100
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()

	// Verify the probe table exists
	var tableCount int
	existsQuery := `
        SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema = 'metrics'
            AND table_name = $1
            AND table_type = 'BASE TABLE'
    `
	if err := pool.QueryRow(ctx, existsQuery, probeName).Scan(&tableCount); err != nil {
		RespondError(w, http.StatusInternalServerError,
			"Failed to verify probe table")
		return
	}
	if tableCount == 0 {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("Probe %q not found", probeName))
		return
	}

	// Discover all columns and their types (cached)
	allColumns, colTypes, err := discoverAllColumnsCached(ctx, pool, probeName)
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			"Failed to discover table columns")
		return
	}

	// Determine dimension (entity key) columns for DISTINCT ON
	dimensionCols := BuildDimensionColumns(allColumns, colTypes)

	// Resolve the default order_by after column discovery. The
	// DISTINCT ON query already selects the latest row, so ordering
	// by collected_at is meaningless (and collected_at is excluded
	// from the output). Default to the first dimension column.
	if orderBy == "" || (orderBy == "collected_at" && internalColumns[orderBy]) {
		if len(dimensionCols) > 0 {
			orderBy = dimensionCols[0]
		}
	}

	// Build the set of return columns for validation
	returnColSet := make(map[string]bool, len(allColumns))
	for _, col := range allColumns {
		if !internalColumns[col] {
			returnColSet[col] = true
		}
	}

	// Validate order_by column exists in the output columns
	if !returnColSet[orderBy] {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid order_by: column %q does not exist in probe %q output",
				orderBy, probeName))
		return
	}

	// Resolve the database column name (cached)
	dbCol, err := resolveDatabaseColumnCached(ctx, pool, probeName)
	if err != nil {
		RespondError(w, http.StatusInternalServerError,
			"Failed to resolve database column")
		return
	}

	// Determine whether the probe table has a schemaname column so
	// we can apply the exclude_schemas filter.
	hasSchemaCol := false
	for _, col := range dimensionCols {
		if col == "schemaname" {
			hasSchemaCol = true
			break
		}
	}
	if !hasSchemaCol {
		excludeSchemas = nil
	}

	// Build and execute the query
	result, totalCount, err := queryLatestSnapshot(
		ctx, pool, probeName, connectionID, databaseName, dbCol,
		dimensionCols, allColumns, colTypes, orderBy, order, limit,
		excludeSchemas,
	)
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	RespondJSON(w, http.StatusOK, LatestSnapshotResponse{
		Rows:       result,
		TotalCount: totalCount,
	})
}

// discoverAllColumns queries information_schema.columns for the given
// probe table and returns all column names in ordinal order plus a map
// of column name to data type.
func discoverAllColumns(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
) ([]string, map[string]string, error) {
	query := `
        SELECT column_name, data_type
        FROM information_schema.columns
        WHERE table_schema = 'metrics'
            AND table_name = $1
        ORDER BY ordinal_position
    `

	rows, err := pool.Query(ctx, query, probeName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	var allCols []string
	colTypes := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, nil, fmt.Errorf("failed to scan column: %w", err)
		}
		allCols = append(allCols, name)
		colTypes[name] = dataType
	}

	return allCols, colTypes, rows.Err()
}

// discoverAllColumnsCached returns cached column metadata for the
// given probe table, refreshing the cache when it exceeds the TTL.
func discoverAllColumnsCached(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
) ([]string, map[string]string, error) {
	if cached, ok := tableColumnCache.Load(probeName); ok {
		cc, valid := cached.(*columnCache)
		if valid && time.Since(cc.cachedAt) < columnCacheTTL {
			return cc.allColumns, cc.colTypes, nil
		}
	}

	allColumns, colTypes, err := discoverAllColumns(ctx, pool, probeName)
	if err != nil {
		return nil, nil, err
	}

	tableColumnCache.Store(probeName, &columnCache{
		allColumns: allColumns,
		colTypes:   colTypes,
		cachedAt:   time.Now(),
	})

	return allColumns, colTypes, nil
}

// resolveDatabaseColumnCached returns a cached database column name
// for the given probe table, refreshing on TTL expiry.
func resolveDatabaseColumnCached(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
) (string, error) {
	if cached, ok := tableDBColCache.Load(probeName); ok {
		dc, valid := cached.(*dbColCache)
		if valid && time.Since(dc.cachedAt) < columnCacheTTL {
			return dc.dbCol, nil
		}
	}

	dbCol, err := metrics.ResolveDatabaseColumn(ctx, pool, probeName)
	if err != nil {
		return "", err
	}

	tableDBColCache.Store(probeName, &dbColCache{
		dbCol:    dbCol,
		cachedAt: time.Now(),
	})

	return dbCol, nil
}

// BuildDimensionColumns identifies entity key columns suitable for the
// DISTINCT ON clause. These are text/name-type columns that are not
// internal bookkeeping columns.
func BuildDimensionColumns(
	allColumns []string,
	colTypes map[string]string,
) []string {
	var dims []string
	for _, col := range allColumns {
		if internalColumns[col] {
			continue
		}
		dt := colTypes[col]
		if dt == "text" || dt == "character varying" || dt == "name" {
			dims = append(dims, col)
		}
	}
	return dims
}

// queryLatestSnapshot builds and executes the DISTINCT ON query that
// returns the latest row for each unique entity, then applies sorting
// and pagination. It uses a single query with COUNT(*) OVER() to
// retrieve both rows and the total count, and moves schema exclusion
// to the outer query for better index utilization.
func queryLatestSnapshot(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	connectionID int,
	databaseName string,
	dbCol string,
	dimensionCols []string,
	allColumns []string,
	colTypes map[string]string,
	orderBy string,
	order string,
	limit int,
	excludeSchemas []string,
) ([]map[string]any, int, error) {
	// Build the list of columns to return (exclude internal columns)
	var returnCols []string
	for _, col := range allColumns {
		if !internalColumns[col] {
			returnCols = append(returnCols, col)
		}
	}

	if len(dimensionCols) == 0 {
		return nil, 0, fmt.Errorf(
			"no dimension columns found in probe %q", probeName)
	}

	// Build DISTINCT ON column list
	var distinctOnParts []string
	for _, col := range dimensionCols {
		distinctOnParts = append(distinctOnParts,
			metrics.QuoteIdentifier(col))
	}
	distinctOnClause := strings.Join(distinctOnParts, ", ")

	// Build ORDER BY for DISTINCT ON: dimension cols + collected_at DESC
	var distinctOrderParts []string
	for _, col := range dimensionCols {
		distinctOrderParts = append(distinctOrderParts,
			metrics.QuoteIdentifier(col))
	}
	distinctOrderParts = append(distinctOrderParts, "collected_at DESC")
	distinctOrderClause := strings.Join(distinctOrderParts, ", ")

	// Build SELECT column list for the inner query
	var selectParts []string
	for _, col := range returnCols {
		selectParts = append(selectParts, metrics.QuoteIdentifier(col))
	}
	selectClause := strings.Join(selectParts, ", ")

	// Build inner WHERE clause: connection_id, database_name, and
	// collected_at time filter for partition pruning.
	var innerWhereParts []string
	var args []any
	argNum := 1

	innerWhereParts = append(innerWhereParts,
		fmt.Sprintf("connection_id = $%d", argNum))
	args = append(args, connectionID)
	argNum++

	// Time filter to enable partition pruning on weekly partitions
	innerWhereParts = append(innerWhereParts,
		"collected_at >= NOW() - INTERVAL '1 hour'")

	if databaseName != "" && dbCol != "" {
		innerWhereParts = append(innerWhereParts,
			fmt.Sprintf("%s = $%d",
				metrics.QuoteIdentifier(dbCol), argNum))
		args = append(args, databaseName)
		argNum++
	}

	innerWhereClause := strings.Join(innerWhereParts, " AND ")

	// Build outer WHERE clause for schema exclusion. Moving this
	// filter out of the inner DISTINCT ON query allows the inner
	// query to fully leverage the index scan.
	var outerWhereParts []string
	if len(excludeSchemas) > 0 {
		var placeholders []string
		for _, schema := range excludeSchemas {
			placeholders = append(placeholders,
				fmt.Sprintf("$%d", argNum))
			args = append(args, schema)
			argNum++
		}
		outerWhereParts = append(outerWhereParts,
			fmt.Sprintf("\"schemaname\" NOT IN (%s)",
				strings.Join(placeholders, ", ")))
	}

	outerWhereClause := ""
	if len(outerWhereParts) > 0 {
		outerWhereClause = "WHERE " + strings.Join(outerWhereParts, " AND ")
	}

	// Single combined query using COUNT(*) OVER() to get the total
	// count alongside the data rows, avoiding a separate count query.
	dataQuery := fmt.Sprintf(`
        SELECT *, COUNT(*) OVER() AS _total_count FROM (
            SELECT DISTINCT ON (%s) %s
            FROM metrics.%s
            WHERE %s
            ORDER BY %s
        ) latest
        %s
        ORDER BY %s %s
        LIMIT $%d
    `,
		distinctOnClause,
		selectClause,
		metrics.QuoteIdentifier(probeName),
		innerWhereClause,
		distinctOrderClause,
		outerWhereClause,
		metrics.QuoteIdentifier(orderBy),
		order,
		argNum,
	)
	args = append(args, limit)

	rows, err := pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query latest snapshot: %w", err)
	}
	defer rows.Close()

	var result []map[string]any
	totalCount := 0
	scanLen := len(returnCols) + 1 // +1 for _total_count

	for rows.Next() {
		values := make([]any, scanLen)
		valuePtrs := make([]any, scanLen)
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, 0, fmt.Errorf("failed to scan row: %w", err)
		}

		// Extract _total_count from the first row
		if len(result) == 0 {
			if tc, ok := values[scanLen-1].(int64); ok {
				totalCount = int(tc)
			}
		}

		row := make(map[string]any, len(returnCols))
		for i, col := range returnCols {
			row[col] = normalizeValue(values[i])
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	if result == nil {
		result = []map[string]any{}
	}

	return result, totalCount, nil
}

// normalizeValue converts database values to JSON-friendly types.
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case int64:
		return val
	case int32:
		return int64(val)
	case int16:
		return int64(val)
	case float64:
		return val
	case float32:
		return float64(val)
	case string:
		return val
	case []byte:
		return string(val)
	case bool:
		return val
	case time.Time:
		return val.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", val)
	}
}
