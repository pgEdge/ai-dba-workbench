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
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// validTimeRanges maps time_range parameter values to their duration.
var validTimeRanges = map[string]time.Duration{
	"1h":  1 * time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// PerfSummaryHandler handles GET /api/v1/metrics/performance-summary
type PerfSummaryHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore
}

// PerfSummaryResponse is the top-level JSON response.
type PerfSummaryResponse struct {
	TimeRange   string                   `json:"time_range"`
	Connections []PerfConnectionResponse `json:"connections"`
	Aggregate   *PerfAggregate           `json:"aggregate,omitempty"`
}

// PerfConnectionResponse holds performance data for a single connection.
type PerfConnectionResponse struct {
	ConnectionID   int               `json:"connection_id"`
	ConnectionName string            `json:"connection_name"`
	XIDAgeEntries  []XIDAgeEntry     `json:"xid_age"`
	CacheHitRatio  CacheHitRatioData `json:"cache_hit_ratio"`
	Transactions   TransactionData   `json:"transactions"`
	Checkpoints    CheckpointData    `json:"checkpoints"`
}

// XIDAgeEntry holds XID age information for a single database.
type XIDAgeEntry struct {
	DatabaseName string  `json:"database_name"`
	Age          int64   `json:"age"`
	Percent      float64 `json:"percent"`
}

// CacheHitRatioData holds cache hit ratio current value and time series.
type CacheHitRatioData struct {
	Current    float64              `json:"current"`
	TimeSeries []CacheHitRatioPoint `json:"time_series"`
}

// CacheHitRatioPoint is a single time-series data point for cache hit ratio.
type CacheHitRatioPoint struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

// TransactionData holds transaction throughput data.
type TransactionData struct {
	CommitsPerSec   float64            `json:"commits_per_sec"`
	RollbackPercent float64            `json:"rollback_percent"`
	TimeSeries      []TransactionPoint `json:"time_series"`
}

// TransactionPoint is a single time-series data point for transactions.
type TransactionPoint struct {
	Time            time.Time `json:"time"`
	CommitsPerSec   float64   `json:"commits_per_sec"`
	RollbackPercent float64   `json:"rollback_percent"`
}

// CheckpointData holds checkpoint activity data.
type CheckpointData struct {
	TimeSeries []CheckpointPoint `json:"time_series"`
}

// CheckpointPoint is a single time-series data point for checkpoints.
type CheckpointPoint struct {
	Time        time.Time `json:"time"`
	WriteTimeMs float64   `json:"write_time_ms"`
	SyncTimeMs  float64   `json:"sync_time_ms"`
}

// PerfAggregate holds aggregate metrics across multiple connections.
type PerfAggregate struct {
	CacheHitRatio float64 `json:"cache_hit_ratio"`
	CommitsPerSec float64 `json:"commits_per_sec"`
	RollbackPct   float64 `json:"rollback_percent"`
}

// DatabaseSummaryResponse is the response for the database summaries endpoint.
type DatabaseSummaryResponse struct {
	Databases []DatabaseSummary `json:"databases"`
}

// DatabaseSummary holds per-database summary metrics for a single connection.
type DatabaseSummary struct {
	DatabaseName      string            `json:"database_name"`
	SizeBytes         int64             `json:"size_bytes"`
	SizePretty        string            `json:"size_pretty"`
	CacheHitRatio     CacheHitRatioData `json:"cache_hit_ratio"`
	TransactionRate   float64           `json:"transaction_rate"`
	DeadTupleRatio    float64           `json:"dead_tuple_ratio"`
	ActiveConnections int               `json:"active_connections"`
}

// TopQueryRow holds a single row from pg_stat_statements.
type TopQueryRow struct {
	QueryID      string `json:"queryid"`
	DatabaseName string `json:"database_name"`
	// Username is the database role that ran the query, resolved from
	// pg_stat_statements.userid. It is an empty string when the role
	// could not be resolved; see buildTopQueriesSQL for why that can
	// happen.
	Username       string  `json:"username"`
	Query          string  `json:"query"`
	Calls          int64   `json:"calls"`
	TotalExecTime  float64 `json:"total_exec_time"`
	MeanExecTime   float64 `json:"mean_exec_time"`
	MinExecTime    float64 `json:"min_exec_time"`
	MaxExecTime    float64 `json:"max_exec_time"`
	Rows           int64   `json:"rows"`
	SharedBlksHit  int64   `json:"shared_blks_hit"`
	SharedBlksRead int64   `json:"shared_blks_read"`
}

// QueryStatsResponse is the response for the period-scoped query statistics
// endpoint. AvgExecTime is a pointer so that "no usable data" serializes as
// JSON null, which the client renders differently from a genuine zero.
type QueryStatsResponse struct {
	QueryID       string   `json:"queryid"`
	AvgExecTime   *float64 `json:"avg_exec_time"`
	Calls         int64    `json:"calls"`
	TotalExecTime float64  `json:"total_exec_time"`
}

// validTopQueryOrderColumns maps each accepted order_by request value to the
// literal column name that may be interpolated into an ORDER BY clause. The
// map value, never the request string, is what reaches the SQL text, so the
// generated statement can only ever contain one of these constants.
var validTopQueryOrderColumns = map[string]string{
	"total_exec_time":  "total_exec_time",
	"calls":            "calls",
	"mean_exec_time":   "mean_exec_time",
	"min_exec_time":    "min_exec_time",
	"max_exec_time":    "max_exec_time",
	"rows":             "rows",
	"shared_blks_hit":  "shared_blks_hit",
	"shared_blks_read": "shared_blks_read",
}

// topQueryOrderByError is the 400 message listing the accepted order_by
// values. It is derived from validTopQueryOrderColumns so the message can
// never drift from the whitelist it describes.
var topQueryOrderByError = "Invalid order_by: must be one of " +
	strings.Join(sortedKeys(validTopQueryOrderColumns), ", ")

// sortedKeys returns the keys of a string-keyed map in sorted order, so that
// generated messages are stable rather than following Go's randomized map
// iteration order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validTopQueryOrderDirections maps each accepted order request value to the
// literal sort direction interpolated into the ORDER BY clause, on the same
// principle as validTopQueryOrderColumns.
var validTopQueryOrderDirections = map[string]string{
	"asc":  "ASC",
	"desc": "DESC",
}

// NewPerfSummaryHandler creates a new performance summary handler.
func NewPerfSummaryHandler(
	datastore *database.Datastore,
	authStore *auth.AuthStore,
) *PerfSummaryHandler {
	return &PerfSummaryHandler{
		datastore: datastore,
		authStore: authStore,
	}
}

// RegisterRoutes registers the performance summary endpoint on the mux.
func (h *PerfSummaryHandler) RegisterRoutes(
	mux *http.ServeMux,
	authWrapper func(http.HandlerFunc) http.HandlerFunc,
) {
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Performance summary")
		mux.HandleFunc("/api/v1/metrics/performance-summary",
			authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/metrics/database-summaries",
			authWrapper(HandleNotConfigured("Database summaries")))
		mux.HandleFunc("/api/v1/metrics/top-queries",
			authWrapper(HandleNotConfigured("Top queries")))
		mux.HandleFunc("/api/v1/metrics/query-stats",
			authWrapper(HandleNotConfigured("Query statistics")))
		return
	}

	mux.HandleFunc("/api/v1/metrics/performance-summary",
		authWrapper(h.handlePerfSummary))
	mux.HandleFunc("/api/v1/metrics/database-summaries",
		authWrapper(h.handleDatabaseSummaries))
	mux.HandleFunc("/api/v1/metrics/top-queries",
		authWrapper(h.handleTopQueries))
	mux.HandleFunc("/api/v1/metrics/query-stats",
		authWrapper(h.handleQueryStats))
}

// handlePerfSummary handles GET /api/v1/metrics/performance-summary
func (h *PerfSummaryHandler) handlePerfSummary(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	// Parse connection IDs: either connection_id or connection_ids
	connectionIDs := h.parseConnectionIDs(w, r)
	if connectionIDs == nil {
		return // error already sent
	}

	// Check RBAC access for each connection
	rbacChecker := auth.NewRBACCheckerWithSharing(h.authStore, h.datastore.GetConnectionSharingInfo)
	for _, connID := range connectionIDs {
		canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connID)
		if !canAccess {
			RespondError(w, http.StatusForbidden,
				fmt.Sprintf("Permission denied: you do not have access to connection %d", connID))
			return
		}
	}

	// Parse time range
	timeRange := ParseQueryString(r, "time_range")
	if timeRange == "" {
		timeRange = "1h"
	}
	duration, ok := validTimeRanges[timeRange]
	if !ok {
		RespondError(w, http.StatusBadRequest,
			"Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d")
		return
	}

	// Calculate bucket interval: duration / 60, minimum 10 seconds
	bucketSeconds := int(duration.Seconds()) / 60
	if bucketSeconds < 10 {
		bucketSeconds = 10
	}
	bucketInterval := fmt.Sprintf("%d seconds", bucketSeconds)

	now := time.Now().UTC()
	startTime := now.Add(-duration)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()

	// Look up connection names
	connNames := h.getConnectionNames(ctx, pool, connectionIDs)

	// Execute all queries in a read-only transaction
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		log.Printf("[ERROR] Failed to begin read-only transaction: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to query performance metrics")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op

	response := PerfSummaryResponse{
		TimeRange:   timeRange,
		Connections: make([]PerfConnectionResponse, 0, len(connectionIDs)),
	}

	// Track aggregate values
	var totalBlksHit, totalBlksRead float64
	var totalCommitsPerSec float64
	var totalCommits, totalRollbacks float64

	for _, connID := range connectionIDs {
		connResp := PerfConnectionResponse{
			ConnectionID:   connID,
			ConnectionName: connNames[connID],
			XIDAgeEntries:  []XIDAgeEntry{},
			CacheHitRatio: CacheHitRatioData{
				TimeSeries: []CacheHitRatioPoint{},
			},
			Transactions: TransactionData{
				TimeSeries: []TransactionPoint{},
			},
			Checkpoints: CheckpointData{
				TimeSeries: []CheckpointPoint{},
			},
		}

		// Query 1: XID Age
		connResp.XIDAgeEntries = h.queryXIDAage(ctx, tx, connID)

		// Query 2: Cache Hit current
		blksHit, blksRead, ratio := h.queryCacheHitCurrent(ctx, tx, connID)
		connResp.CacheHitRatio.Current = ratio
		totalBlksHit += blksHit
		totalBlksRead += blksRead

		// Query 3: Cache Hit time series
		connResp.CacheHitRatio.TimeSeries = h.queryCacheHitTimeSeries(
			ctx, tx, connID, startTime, now, bucketInterval)

		// Query 4: Transaction throughput
		cps, rbPct, tsSeries := h.queryTransactions(
			ctx, tx, connID, startTime, now, bucketInterval)
		connResp.Transactions.CommitsPerSec = cps
		connResp.Transactions.RollbackPercent = rbPct
		connResp.Transactions.TimeSeries = tsSeries
		totalCommitsPerSec += cps

		// Extract total commits and rollbacks for aggregate weighted average
		if len(tsSeries) > 0 {
			for _, pt := range tsSeries {
				totalCommits += pt.CommitsPerSec
				totalRollbacks += pt.RollbackPercent * pt.CommitsPerSec / 100.0
			}
		}

		// Query 5: Checkpoint activity
		connResp.Checkpoints.TimeSeries = h.queryCheckpoints(
			ctx, tx, connID, startTime, now, bucketInterval)

		response.Connections = append(response.Connections, connResp)
	}

	// Commit the read-only transaction
	if err := tx.Commit(ctx); err != nil {
		log.Printf("[ERROR] Failed to commit read-only transaction: %v", err)
	}

	// Compute aggregate for multi-connection requests
	if len(connectionIDs) > 1 {
		agg := &PerfAggregate{}

		// Weighted average for cache hit ratio
		totalBlocks := totalBlksHit + totalBlksRead
		if totalBlocks > 0 {
			agg.CacheHitRatio = roundTo(totalBlksHit/totalBlocks*100.0, 1)
		}

		// Sum for commits/sec
		agg.CommitsPerSec = roundTo(totalCommitsPerSec, 1)

		// Weighted average for rollback percent
		var weightedRollbackSum, weightedRollbackDenom float64
		for i := range response.Connections {
			weightedRollbackSum += response.Connections[i].Transactions.RollbackPercent *
				response.Connections[i].Transactions.CommitsPerSec
			weightedRollbackDenom += response.Connections[i].Transactions.CommitsPerSec
		}
		if weightedRollbackDenom > 0 {
			agg.RollbackPct = roundTo(
				weightedRollbackSum/weightedRollbackDenom, 1)
		}

		response.Aggregate = agg
	}

	RespondJSON(w, http.StatusOK, response)
}

// parseConnectionIDs extracts connection IDs from query parameters.
// It supports both connection_id (single) and connection_ids (comma-separated).
func (h *PerfSummaryHandler) parseConnectionIDs(
	w http.ResponseWriter,
	r *http.Request,
) []int {
	// Try connection_ids first (comma-separated list)
	if ids, ok := ParseQueryIntList(w, r, "connection_ids"); ok {
		return ids
	}
	if r.URL.Query().Get("connection_ids") != "" {
		return nil // Error already sent by ParseQueryIntList
	}

	// Try single connection_id
	if id, ok := ParseQueryInt(w, r, "connection_id"); ok {
		return []int{id}
	}
	if r.URL.Query().Get("connection_id") != "" {
		return nil // Error already sent by ParseQueryInt
	}

	RespondError(w, http.StatusBadRequest,
		"Either connection_id or connection_ids is required")
	return nil
}

// getConnectionNames looks up connection names for the given IDs.
func (h *PerfSummaryHandler) getConnectionNames(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionIDs []int,
) map[int]string {
	names := make(map[int]string, len(connectionIDs))
	for _, id := range connectionIDs {
		conn, err := h.datastore.GetConnection(ctx, id)
		if err != nil {
			log.Printf("[DEBUG] Could not look up connection %d: %s", id, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: id is an integer and err is sanitized via logging.SanitizeForLog
			names[id] = ""
			continue
		}
		names[id] = conn.Name
	}
	return names
}

// queryXIDAage queries the latest XID age from metrics.pg_database.
func (h *PerfSummaryHandler) queryXIDAage(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
) []XIDAgeEntry {
	rows, err := tx.Query(ctx, `
        SELECT datname, age_datfrozenxid
        FROM metrics.pg_database
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_database
              WHERE connection_id = $1
          )
          AND datistemplate = false
          AND age_datfrozenxid IS NOT NULL
        ORDER BY age_datfrozenxid DESC
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No XID age data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return []XIDAgeEntry{}
	}
	defer rows.Close()

	var entries []XIDAgeEntry
	for rows.Next() {
		var name string
		var age int64
		if err := rows.Scan(&name, &age); err != nil {
			log.Printf("[DEBUG] Error scanning XID age: %v", err)
			continue
		}
		pct := float64(age) / 2147483647.0 * 100.0
		entries = append(entries, XIDAgeEntry{
			DatabaseName: name,
			Age:          age,
			Percent:      roundTo(pct, 2),
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating XID age rows: %v", err)
	}
	if entries == nil {
		entries = []XIDAgeEntry{}
	}
	return entries
}

// queryCacheHitCurrent returns the current cache hit ratio for a connection.
// Returns blks_hit, blks_read, and the ratio percentage.
func (h *PerfSummaryHandler) queryCacheHitCurrent(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
) (float64, float64, float64) {
	var blksHit, blksRead float64
	err := tx.QueryRow(ctx, `
        SELECT COALESCE(SUM(blks_hit), 0),
               COALESCE(SUM(blks_read), 0)
        FROM metrics.pg_stat_database
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_stat_database
              WHERE connection_id = $1
          )
    `, connectionID).Scan(&blksHit, &blksRead)
	if err != nil {
		log.Printf("[DEBUG] No cache hit data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return 0, 0, 0
	}

	total := blksHit + blksRead
	if total == 0 {
		return blksHit, blksRead, 0
	}
	return blksHit, blksRead, roundTo(blksHit/total*100.0, 2)
}

// queryCacheHitTimeSeries returns bucketed cache hit ratio over time.
func (h *PerfSummaryHandler) queryCacheHitTimeSeries(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	startTime, endTime time.Time,
	bucketInterval string,
) []CacheHitRatioPoint {
	rows, err := tx.Query(ctx, `
        SELECT date_bin($1::interval, collected_at, $2) AS bucket,
               CASE WHEN (SUM(blks_hit) + SUM(blks_read)) = 0 THEN 0
                    ELSE SUM(blks_hit)::float /
                         (SUM(blks_hit) + SUM(blks_read))::float * 100.0
               END AS ratio
        FROM metrics.pg_stat_database
        WHERE connection_id = $3
          AND collected_at >= $2
          AND collected_at <= $4
        GROUP BY bucket
        ORDER BY bucket
    `, bucketInterval, startTime, connectionID, endTime)
	if err != nil {
		log.Printf("[DEBUG] No cache hit time series for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return []CacheHitRatioPoint{}
	}
	defer rows.Close()

	var points []CacheHitRatioPoint
	for rows.Next() {
		var pt CacheHitRatioPoint
		if err := rows.Scan(&pt.Time, &pt.Value); err != nil {
			log.Printf("[DEBUG] Error scanning cache hit time series: %v", err)
			continue
		}
		pt.Value = roundTo(pt.Value, 2)
		points = append(points, pt)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating cache hit time series: %v", err)
	}
	if points == nil {
		points = []CacheHitRatioPoint{}
	}
	return points
}

// queryTransactions returns the latest transaction throughput and time series.
func (h *PerfSummaryHandler) queryTransactions(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	startTime, endTime time.Time,
	bucketInterval string,
) (float64, float64, []TransactionPoint) {
	rows, err := tx.Query(ctx, `
        WITH deltas AS (
            SELECT
                collected_at,
                SUM(xact_commit) AS total_commit,
                SUM(xact_rollback) AS total_rollback,
                LAG(SUM(xact_commit)) OVER (ORDER BY collected_at) AS prev_commit,
                LAG(SUM(xact_rollback)) OVER (ORDER BY collected_at) AS prev_rollback,
                EXTRACT(EPOCH FROM
                    collected_at - LAG(collected_at) OVER (ORDER BY collected_at)
                ) AS elapsed_sec
            FROM metrics.pg_stat_database
            WHERE connection_id = $3
              AND collected_at >= $2
              AND collected_at <= $4
            GROUP BY collected_at
        ),
        valid_deltas AS (
            SELECT
                collected_at,
                (total_commit - prev_commit) AS delta_commit,
                (total_rollback - prev_rollback) AS delta_rollback,
                elapsed_sec
            FROM deltas
            WHERE prev_commit IS NOT NULL
              AND elapsed_sec > 0
              AND (total_commit - prev_commit) >= 0
              AND (total_rollback - prev_rollback) >= 0
        )
        SELECT date_bin($1::interval, collected_at, $2) AS bucket,
               SUM(delta_commit) / SUM(elapsed_sec) AS commits_per_sec,
               CASE WHEN SUM(delta_commit + delta_rollback) = 0 THEN 0
                    ELSE SUM(delta_rollback)::float /
                         SUM(delta_commit + delta_rollback)::float * 100.0
               END AS rollback_percent
        FROM valid_deltas
        GROUP BY bucket
        ORDER BY bucket
    `, bucketInterval, startTime, connectionID, endTime)
	if err != nil {
		log.Printf("[DEBUG] No transaction data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return 0, 0, []TransactionPoint{}
	}
	defer rows.Close()

	var points []TransactionPoint
	for rows.Next() {
		var pt TransactionPoint
		if err := rows.Scan(&pt.Time, &pt.CommitsPerSec, &pt.RollbackPercent); err != nil {
			log.Printf("[DEBUG] Error scanning transaction data: %v", err)
			continue
		}
		pt.CommitsPerSec = roundTo(pt.CommitsPerSec, 1)
		pt.RollbackPercent = roundTo(pt.RollbackPercent, 1)
		points = append(points, pt)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating transaction data: %v", err)
	}
	if points == nil {
		points = []TransactionPoint{}
	}

	// Compute the latest bucket as the "current" values
	var cps, rbPct float64
	if len(points) > 0 {
		latest := points[len(points)-1]
		cps = latest.CommitsPerSec
		rbPct = latest.RollbackPercent
	}

	return cps, rbPct, points
}

// queryCheckpoints returns bucketed checkpoint write and sync time deltas.
func (h *PerfSummaryHandler) queryCheckpoints(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	startTime, endTime time.Time,
	bucketInterval string,
) []CheckpointPoint {
	rows, err := tx.Query(ctx, `
        WITH deltas AS (
            SELECT
                collected_at,
                write_time,
                sync_time,
                LAG(write_time) OVER (ORDER BY collected_at) AS prev_write_time,
                LAG(sync_time) OVER (ORDER BY collected_at) AS prev_sync_time
            FROM metrics.pg_stat_checkpointer
            WHERE connection_id = $3
              AND collected_at >= $2
              AND collected_at <= $4
        ),
        valid_deltas AS (
            SELECT
                collected_at,
                (write_time - prev_write_time) AS delta_write,
                (sync_time - prev_sync_time) AS delta_sync
            FROM deltas
            WHERE prev_write_time IS NOT NULL
              AND (write_time - prev_write_time) >= 0
              AND (sync_time - prev_sync_time) >= 0
        )
        SELECT date_bin($1::interval, collected_at, $2) AS bucket,
               SUM(delta_write) AS write_time_ms,
               SUM(delta_sync) AS sync_time_ms
        FROM valid_deltas
        GROUP BY bucket
        ORDER BY bucket
    `, bucketInterval, startTime, connectionID, endTime)
	if err != nil {
		log.Printf("[DEBUG] No checkpoint data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return []CheckpointPoint{}
	}
	defer rows.Close()

	var points []CheckpointPoint
	for rows.Next() {
		var pt CheckpointPoint
		if err := rows.Scan(&pt.Time, &pt.WriteTimeMs, &pt.SyncTimeMs); err != nil {
			log.Printf("[DEBUG] Error scanning checkpoint data: %v", err)
			continue
		}
		pt.WriteTimeMs = roundTo(pt.WriteTimeMs, 1)
		pt.SyncTimeMs = roundTo(pt.SyncTimeMs, 1)
		points = append(points, pt)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating checkpoint data: %v", err)
	}
	if points == nil {
		points = []CheckpointPoint{}
	}
	return points
}

// handleDatabaseSummaries handles GET /api/v1/metrics/database-summaries
func (h *PerfSummaryHandler) handleDatabaseSummaries(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	connectionIDs := h.parseConnectionIDs(w, r)
	if connectionIDs == nil {
		return
	}
	if len(connectionIDs) != 1 {
		RespondError(w, http.StatusBadRequest,
			"Exactly one connection_id is required")
		return
	}
	connID := connectionIDs[0]

	rbacChecker := auth.NewRBACCheckerWithSharing(h.authStore, h.datastore.GetConnectionSharingInfo)
	canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			fmt.Sprintf("Permission denied: you do not have access to connection %d", connID))
		return
	}

	timeRange := ParseQueryString(r, "time_range")
	if timeRange == "" {
		timeRange = "24h"
	}
	duration, ok := validTimeRanges[timeRange]
	if !ok {
		RespondError(w, http.StatusBadRequest,
			"Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d")
		return
	}

	bucketSeconds := int(duration.Seconds()) / 60
	if bucketSeconds < 10 {
		bucketSeconds = 10
	}
	bucketInterval := fmt.Sprintf("%d seconds", bucketSeconds)

	now := time.Now().UTC()
	startTime := now.Add(-duration)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		log.Printf("[ERROR] Failed to begin read-only transaction: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to query database summaries")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback after commit is a no-op

	dbMap := make(map[string]*DatabaseSummary)

	// Query 1: Database sizes from metrics.pg_database
	h.queryDatabaseSizes(ctx, tx, connID, dbMap)

	// Query 2: Stats from metrics.pg_stat_database
	h.queryDatabaseStats(ctx, tx, connID, dbMap)

	// Query 3: Dead tuple ratio from metrics.pg_stat_all_tables
	h.queryDeadTupleRatios(ctx, tx, connID, dbMap)

	// Query 4: Transaction rate (delta between latest two collections)
	h.queryTransactionRates(ctx, tx, connID, dbMap)

	// Query 5: Cache hit ratio time series per database
	h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID, startTime, now,
		bucketInterval, dbMap)

	if err := tx.Commit(ctx); err != nil {
		log.Printf("[ERROR] Failed to commit read-only transaction: %v", err)
	}

	databases := make([]DatabaseSummary, 0, len(dbMap))
	for _, db := range dbMap {
		if db.CacheHitRatio.TimeSeries == nil {
			db.CacheHitRatio.TimeSeries = []CacheHitRatioPoint{}
		}
		databases = append(databases, *db)
	}

	RespondJSON(w, http.StatusOK, DatabaseSummaryResponse{
		Databases: databases,
	})
}

// queryDatabaseSizes populates database size information from pg_database.
func (h *PerfSummaryHandler) queryDatabaseSizes(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	dbMap map[string]*DatabaseSummary,
) {
	rows, err := tx.Query(ctx, `
        SELECT datname, database_size_bytes
        FROM metrics.pg_database
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_database
              WHERE connection_id = $1
          )
          AND datistemplate = false
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No database size data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var sizeBytes int64
		if err := rows.Scan(&name, &sizeBytes); err != nil {
			log.Printf("[DEBUG] Error scanning database size: %v", err)
			continue
		}
		dbMap[name] = &DatabaseSummary{
			DatabaseName: name,
			SizeBytes:    sizeBytes,
			SizePretty:   formatBytes(sizeBytes),
			CacheHitRatio: CacheHitRatioData{
				TimeSeries: []CacheHitRatioPoint{},
			},
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating database size rows: %v", err)
	}
}

// queryDatabaseStats populates connection count and cache hit ratio from
// pg_stat_database.
func (h *PerfSummaryHandler) queryDatabaseStats(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	dbMap map[string]*DatabaseSummary,
) {
	rows, err := tx.Query(ctx, `
        SELECT datname, numbackends,
               COALESCE(blks_hit, 0), COALESCE(blks_read, 0)
        FROM metrics.pg_stat_database
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_stat_database
              WHERE connection_id = $1
          )
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No pg_stat_database data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var numBackends int
		var blksHit, blksRead float64
		if err := rows.Scan(&name, &numBackends, &blksHit, &blksRead); err != nil {
			log.Printf("[DEBUG] Error scanning database stats: %v", err)
			continue
		}
		// Only queryDatabaseSizes may create entries; the latest
		// pg_database snapshot is the source of truth for which
		// databases currently exist. Skip rows for databases absent
		// from that snapshot (e.g. recently dropped databases whose
		// historical rows still linger in pg_stat_database).
		db, exists := dbMap[name]
		if !exists {
			continue
		}
		db.ActiveConnections = numBackends
		total := blksHit + blksRead
		if total > 0 {
			db.CacheHitRatio.Current = roundTo(blksHit/total*100.0, 2)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating database stats rows: %v", err)
	}
}

// queryDeadTupleRatios populates dead tuple ratios from pg_stat_all_tables.
func (h *PerfSummaryHandler) queryDeadTupleRatios(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	dbMap map[string]*DatabaseSummary,
) {
	rows, err := tx.Query(ctx, `
        SELECT database_name,
               CASE WHEN SUM(n_live_tup) + SUM(n_dead_tup) = 0 THEN 0
                    ELSE SUM(n_dead_tup)::float /
                         (SUM(n_live_tup) + SUM(n_dead_tup))::float * 100.0
               END AS dead_tuple_ratio
        FROM metrics.pg_stat_all_tables
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_stat_all_tables
              WHERE connection_id = $1
          )
        GROUP BY database_name
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No dead tuple data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var ratio float64
		if err := rows.Scan(&name, &ratio); err != nil {
			log.Printf("[DEBUG] Error scanning dead tuple ratio: %v", err)
			continue
		}
		// Only queryDatabaseSizes may create entries; skip databases
		// absent from the latest pg_database snapshot.
		db, exists := dbMap[name]
		if !exists {
			continue
		}
		db.DeadTupleRatio = roundTo(ratio, 2)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating dead tuple rows: %v", err)
	}
}

// queryTransactionRates computes transaction rate per database as the delta
// between the latest two collections.
func (h *PerfSummaryHandler) queryTransactionRates(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	dbMap map[string]*DatabaseSummary,
) {
	rows, err := tx.Query(ctx, `
        WITH latest_two AS (
            SELECT DISTINCT collected_at
            FROM metrics.pg_stat_database
            WHERE connection_id = $1
            ORDER BY collected_at DESC
            LIMIT 2
        ),
        pivoted AS (
            SELECT datname,
                   collected_at,
                   xact_commit,
                   xact_rollback
            FROM metrics.pg_stat_database
            WHERE connection_id = $1
              AND collected_at IN (SELECT collected_at FROM latest_two)
        )
        SELECT p1.datname,
               CASE WHEN EXTRACT(EPOCH FROM p1.collected_at - p2.collected_at) > 0
                    THEN (p1.xact_commit - p2.xact_commit)::float /
                         EXTRACT(EPOCH FROM p1.collected_at - p2.collected_at)
                    ELSE 0
               END AS tx_rate
        FROM pivoted p1
        JOIN pivoted p2
          ON p1.datname = p2.datname
         AND p1.collected_at > p2.collected_at
        WHERE (p1.xact_commit - p2.xact_commit) >= 0
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No transaction rate data for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var rate float64
		if err := rows.Scan(&name, &rate); err != nil {
			log.Printf("[DEBUG] Error scanning transaction rate: %v", err)
			continue
		}
		// Only queryDatabaseSizes may create entries; skip databases
		// absent from the latest pg_database snapshot.
		db, exists := dbMap[name]
		if !exists {
			continue
		}
		db.TransactionRate = roundTo(rate, 1)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating transaction rate rows: %v", err)
	}
}

// queryDatabaseCacheHitTimeSeries populates per-database cache hit ratio
// time series using date_bin bucketing.
func (h *PerfSummaryHandler) queryDatabaseCacheHitTimeSeries(
	ctx context.Context,
	tx pgx.Tx,
	connectionID int,
	startTime, endTime time.Time,
	bucketInterval string,
	dbMap map[string]*DatabaseSummary,
) {
	rows, err := tx.Query(ctx, `
        SELECT datname,
               date_bin($1::interval, collected_at, $2) AS bucket,
               CASE WHEN (SUM(blks_hit) + SUM(blks_read)) = 0 THEN 0
                    ELSE SUM(blks_hit)::float /
                         (SUM(blks_hit) + SUM(blks_read))::float * 100.0
               END AS ratio
        FROM metrics.pg_stat_database
        WHERE connection_id = $3
          AND collected_at >= $2
          AND collected_at <= $4
        GROUP BY datname, bucket
        ORDER BY datname, bucket
    `, bucketInterval, startTime, connectionID, endTime)
	if err != nil {
		log.Printf("[DEBUG] No cache hit time series for connection %d: %s", connectionID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connectionID is an integer and err is sanitized via logging.SanitizeForLog
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var pt CacheHitRatioPoint
		if err := rows.Scan(&name, &pt.Time, &pt.Value); err != nil {
			log.Printf("[DEBUG] Error scanning db cache hit time series: %v",
				err)
			continue
		}
		pt.Value = roundTo(pt.Value, 2)
		// Only queryDatabaseSizes may create entries. This query scans
		// the entire requested time range, so it can still see rows for
		// a database that was dropped partway through the window. Skip
		// such databases so their historical samples do not resurrect a
		// ghost card for a database that no longer exists (issue #362).
		db, exists := dbMap[name]
		if !exists {
			continue
		}
		db.CacheHitRatio.TimeSeries = append(
			db.CacheHitRatio.TimeSeries, pt)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating db cache hit time series: %v",
			err)
	}
}

// buildTopQueriesSQL assembles the two statements behind the top-queries
// endpoint and the argument slices that go with them. It is the only place
// in this file where SQL text is composed, and it is deliberately pure so
// that the clause selection and the $N placeholder numbering can be tested
// without a database.
//
// The only values interpolated into the statement text are orderCol and
// orderDir, which callers must resolve through validTopQueryOrderColumns and
// validTopQueryOrderDirections respectively, plus the generated placeholder
// positions. Every caller-supplied value (connID, queryID, databaseName,
// limit, offset) is bound as a parameter and appears in the returned
// argument slices, never in the SQL.
//
// filterArgs serves the count statement; pageArgs is filterArgs followed by
// the limit and offset, so the two statements share identical placeholder
// numbering for the filters.
func buildTopQueriesSQL(
	connID int,
	queryID, databaseName string,
	excludeCollector bool,
	orderCol, orderDir string,
	limit, offset int,
) (countSQL, pageSQL string, filterArgs, pageArgs []any) {
	// Optional queryid filter, applied inside the CTE.
	queryIDClause := ""
	filterArgs = []any{connID}
	if queryID != "" {
		queryIDClause = fmt.Sprintf(
			"AND pss.queryid::text = $%d", len(filterArgs)+1)
		filterArgs = append(filterArgs, queryID)
	}

	// Optional clause to exclude collector probe queries. It contains no
	// caller-supplied data at all.
	excludeCollectorClause := ""
	if excludeCollector {
		excludeCollectorClause = "AND pss.query NOT LIKE '%ai_dba_wb_probe%'"
	}

	// The database filter is applied to the outer select so that it matches
	// the resolved database name (the COALESCE below), not the raw
	// pss.database_name column.
	databaseClause := ""
	if databaseName != "" {
		databaseClause = fmt.Sprintf(
			"WHERE database_name = $%d", len(filterArgs)+1)
		filterArgs = append(filterArgs, databaseName)
	}

	// db_names and user_names resolve the OIDs recorded in
	// pg_stat_statements to human-readable names using whatever
	// pg_stat_activity has observed for this connection. Both are
	// necessarily best-effort: an OID only appears in pg_stat_activity if
	// the collector sampled a backend for that database or role inside the
	// retention window, so a role that never had an active backend sampled
	// (or one whose samples have since been pruned) resolves to nothing and
	// the query is reported with an empty username. Both CTEs pick the most
	// recently observed name per OID, so a database or role renamed within
	// the window resolves to its current name rather than to whichever of
	// the two names the planner happened to reach first.
	cte := fmt.Sprintf(`
        WITH db_names AS (
            SELECT DISTINCT ON (datid) datid, datname
            FROM metrics.pg_stat_activity
            WHERE connection_id = $1
              AND datid IS NOT NULL
              AND datname IS NOT NULL
            ORDER BY datid, collected_at DESC
        ),
        user_names AS (
            SELECT DISTINCT ON (usesysid) usesysid, usename
            FROM metrics.pg_stat_activity
            WHERE connection_id = $1
              AND usesysid IS NOT NULL
              AND usename IS NOT NULL
            ORDER BY usesysid, collected_at DESC
        ),
        deduped AS (
            SELECT DISTINCT ON (pss.queryid)
                pss.queryid::text,
                COALESCE(dn.datname, pss.database_name) AS database_name,
                COALESCE(un.usename, '') AS username,
                pss.query, pss.calls, pss.total_exec_time,
                pss.mean_exec_time, pss.min_exec_time, pss.max_exec_time,
                pss.rows,
                pss.shared_blks_hit, pss.shared_blks_read
            FROM metrics.pg_stat_statements pss
            LEFT JOIN db_names dn ON pss.dbid = dn.datid
            LEFT JOIN user_names un ON pss.userid = un.usesysid
            WHERE pss.connection_id = $1
              AND pss.collected_at = (
                  SELECT MAX(collected_at)
                  FROM metrics.pg_stat_statements
                  WHERE connection_id = $1
              )
              %s
              %s
            ORDER BY pss.queryid
        )`, queryIDClause, excludeCollectorClause)

	// The total is obtained with a separate COUNT(*) over the same CTE
	// rather than a COUNT(*) OVER () window on the page query. A window
	// count would come back on the data rows themselves, so it would be
	// unavailable whenever the requested offset runs past the end of the
	// result set, which is exactly the case a paging client needs the total
	// for.
	countSQL = cte + "\n        SELECT COUNT(*) FROM deduped " + databaseClause

	pageArgs = make([]any, 0, len(filterArgs)+2)
	pageArgs = append(pageArgs, filterArgs...)
	limitPos := len(pageArgs) + 1
	pageArgs = append(pageArgs, limit, offset)

	// The ORDER BY carries a queryid tiebreaker so that the sort is a total
	// order rather than a partial one. Without it, rows tying on the
	// selected column could come back in any order, and successive pages of
	// the same result set could then repeat a row or skip one entirely. The
	// CTE dedupes with DISTINCT ON (pss.queryid), so queryid is unique
	// across the result set and is therefore sufficient on its own; the
	// cast in the CTE keeps the output column named queryid, which is what
	// this clause resolves against.
	pageSQL = fmt.Sprintf(`%s
        SELECT * FROM deduped
        %s
        ORDER BY %s %s, queryid
        LIMIT $%d OFFSET $%d
    `, cte, databaseClause, orderCol, orderDir, limitPos, limitPos+1)

	return countSQL, pageSQL, filterArgs, pageArgs
}

// handleTopQueries handles GET /api/v1/metrics/top-queries
func (h *PerfSummaryHandler) handleTopQueries(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	connectionIDs := h.parseConnectionIDs(w, r)
	if connectionIDs == nil {
		return
	}
	if len(connectionIDs) != 1 {
		RespondError(w, http.StatusBadRequest,
			"Exactly one connection_id is required")
		return
	}
	connID := connectionIDs[0]

	rbacChecker := auth.NewRBACCheckerWithSharing(h.authStore, h.datastore.GetConnectionSharingInfo)
	canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			fmt.Sprintf("Permission denied: you do not have access to connection %d", connID))
		return
	}

	// Parse limit (default 10, max 100)
	limit := 10
	if l, ok := ParseQueryInt(w, r, "limit"); ok {
		limit = l
	} else if r.URL.Query().Get("limit") != "" {
		return // ParseQueryInt already sent error
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	// Parse offset (default 0). Unlike limit, a negative offset is
	// rejected rather than clamped, because it almost always signals a
	// paging bug in the caller rather than a harmless over-request.
	// ParseOffsetWithDefault is not used here: it silently falls back to
	// the default for invalid input, whereas this endpoint reports bad
	// parameters in the same style as limit.
	offset := 0
	if o, ok := ParseQueryInt(w, r, "offset"); ok {
		offset = o
	} else if r.URL.Query().Get("offset") != "" {
		return // ParseQueryInt already sent error
	}
	if offset < 0 {
		RespondError(w, http.StatusBadRequest,
			"Invalid offset: must be zero or greater")
		return
	}

	// Parse and validate order_by. orderCol is the whitelisted constant
	// taken from the map, not the request string.
	orderBy := ParseQueryString(r, "order_by")
	if orderBy == "" {
		orderBy = "total_exec_time"
	}
	orderCol, ok := validTopQueryOrderColumns[orderBy]
	if !ok {
		RespondError(w, http.StatusBadRequest, topQueryOrderByError)
		return
	}

	// Parse and validate order direction, resolved to a constant in the
	// same way as the order column.
	order := strings.ToLower(ParseQueryString(r, "order"))
	if order == "" {
		order = "desc"
	}
	orderDir, ok := validTopQueryOrderDirections[order]
	if !ok {
		RespondError(w, http.StatusBadRequest,
			"Invalid order: must be asc or desc")
		return
	}

	// Parse optional queryid filter
	queryID := ParseQueryString(r, "queryid")

	// Parse optional exclude_collector filter
	excludeCollector := r.URL.Query().Get("exclude_collector") == "true"

	// Parse optional database_name filter. The value is always bound as a
	// parameter, never interpolated into the SQL text.
	databaseName := ParseQueryString(r, "database_name")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		log.Printf("[ERROR] Failed to begin read-only transaction: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to query top queries")
		return
	}
	// Roll back with a non-cancelable context so a canceled request
	// context cannot trigger the pgx v5 close-of-closed-channel panic
	// described in jackc/pgx#2470, which would leak the pooled
	// connection in an aborted-transaction state. This endpoint takes
	// several early returns between BEGIN and COMMIT, so the deferred
	// rollback is well exercised. The other handlers in this file
	// still pass the request context; converting them belongs in a
	// deliberate sweep of its own rather than here.
	defer tx.Rollback(context.Background()) //nolint:errcheck // Rollback is no-op if already committed

	countQuery, query, filterArgs, pageArgs := buildTopQueriesSQL(
		connID, queryID, databaseName, excludeCollector, orderCol, orderDir,
		limit, offset)

	var totalCount int64
	if err := tx.QueryRow(ctx, countQuery, filterArgs...).Scan(
		&totalCount); err != nil {
		log.Printf("[DEBUG] No top queries data for connection %d: %s", connID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connID is an integer and err is sanitized via logging.SanitizeForLog
		respondEmptyTopQueries(w)
		return
	}

	rows, err := tx.Query(ctx, query, pageArgs...)
	if err != nil {
		log.Printf("[DEBUG] No top queries data for connection %d: %s", connID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connID is an integer and err is sanitized via logging.SanitizeForLog
		respondEmptyTopQueries(w)
		return
	}
	defer rows.Close()

	results := make([]TopQueryRow, 0)
	for rows.Next() {
		var row TopQueryRow
		if err := rows.Scan(
			&row.QueryID, &row.DatabaseName, &row.Username, &row.Query,
			&row.Calls, &row.TotalExecTime, &row.MeanExecTime,
			&row.MinExecTime, &row.MaxExecTime, &row.Rows,
			&row.SharedBlksHit, &row.SharedBlksRead,
		); err != nil {
			log.Printf("[DEBUG] Error scanning top query row: %v", err)
			continue
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating top query rows: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("[ERROR] Failed to commit read-only transaction: %v", err)
	}

	// X-Total-Count reports the number of rows matching the filters,
	// ignoring limit and offset, so that a client can size its pager. The
	// body deliberately stays a bare JSON array of TopQueryRow, because the
	// OpenAPI spec and the web client both depend on that shape.
	w.Header().Set(headerTotalCount, strconv.FormatInt(totalCount, 10))
	RespondJSON(w, http.StatusOK, results)
}

// headerTotalCount is the response header carrying the unpaginated row
// count for paged collection endpoints.
const headerTotalCount = "X-Total-Count"

// respondEmptyTopQueries returns the empty top-queries result used when the
// underlying metrics tables are missing or the query fails. The endpoint
// treats missing metrics data as "no rows" rather than an error, so the
// total count is reported as zero.
func respondEmptyTopQueries(w http.ResponseWriter) {
	w.Header().Set(headerTotalCount, "0")
	RespondJSON(w, http.StatusOK, []TopQueryRow{})
}

// queryStatsSQL computes the period-scoped statistics for a single query.
//
// pg_stat_statements exposes cumulative counters, so the figures for a time
// range are the summed deltas between consecutive samples rather than the
// values of any single sample. One collection can hold several rows for the
// same queryid (the probe records one row per database, role, and toplevel
// flag), so each sample is first collapsed with SUM before the LAG.
//
// Sample pairs whose call or time delta is negative are discarded: a negative
// delta means the counters were reset by pg_stat_reset() or by a server
// restart, and the pre-reset totals cannot be compared with the post-reset
// ones. Dropping only the offending pair costs a single interval rather than
// poisoning the whole range, which matches the reset handling in
// metrics.BuildDerivedMetricsQuery. The first sample in the range has no
// predecessor and so contributes nothing, exactly as in the transaction
// throughput query.
//
// The final row reports the summed deltas plus the number of usable pairs, so
// the caller can distinguish "no usable data at all" from "data, but no calls
// in this period".
const queryStatsSQL = `
        WITH samples AS (
            SELECT
                collected_at,
                SUM(calls) AS total_calls,
                SUM(total_exec_time) AS total_time,
                LAG(SUM(calls)) OVER (ORDER BY collected_at) AS prev_calls,
                LAG(SUM(total_exec_time)) OVER (ORDER BY collected_at)
                    AS prev_time
            FROM metrics.pg_stat_statements
            WHERE connection_id = $1
              AND queryid::text = $2
              AND collected_at >= $3
              AND collected_at <= $4
            GROUP BY collected_at
        ),
        valid_deltas AS (
            SELECT
                (total_calls - prev_calls) AS delta_calls,
                (total_time - prev_time) AS delta_time
            FROM samples
            WHERE prev_calls IS NOT NULL
              AND (total_calls - prev_calls) >= 0
              AND (total_time - prev_time) >= 0
        )
        SELECT COUNT(*),
               COALESCE(SUM(delta_calls), 0),
               COALESCE(SUM(delta_time), 0)
        FROM valid_deltas
    `

// buildQueryStats turns the summed deltas into the response body. avg is
// reported as null (a nil pointer) when there were no usable sample pairs or
// no calls at all in the period, so that the client can tell "no data" apart
// from a genuine zero.
func buildQueryStats(
	queryID string,
	pairs, calls int64,
	totalTime float64,
) QueryStatsResponse {
	resp := QueryStatsResponse{
		QueryID:       queryID,
		Calls:         calls,
		TotalExecTime: roundTo(totalTime, 3),
	}
	if pairs > 0 && calls > 0 {
		avg := roundTo(totalTime/float64(calls), 3)
		resp.AvgExecTime = &avg
	}
	return resp
}

// handleQueryStats handles GET /api/v1/metrics/query-stats, returning the
// average execution time of a single query over the selected time range.
// Unlike the mean_exec_time column of pg_stat_statements, which is a
// cumulative lifetime average, this figure covers only the requested period.
func (h *PerfSummaryHandler) handleQueryStats(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !RequireGET(w, r) {
		return
	}

	connectionIDs := h.parseConnectionIDs(w, r)
	if connectionIDs == nil {
		return
	}
	if len(connectionIDs) != 1 {
		RespondError(w, http.StatusBadRequest,
			"Exactly one connection_id is required")
		return
	}
	connID := connectionIDs[0]

	rbacChecker := auth.NewRBACCheckerWithSharing(h.authStore, h.datastore.GetConnectionSharingInfo)
	canAccess, _ := rbacChecker.CanAccessConnection(r.Context(), connID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			fmt.Sprintf("Permission denied: you do not have access to connection %d", connID))
		return
	}

	queryID := ParseQueryString(r, "queryid")
	if queryID == "" {
		RespondError(w, http.StatusBadRequest, "queryid is required")
		return
	}

	timeRange := ParseQueryString(r, "time_range")
	if timeRange == "" {
		timeRange = "1h"
	}
	duration, ok := validTimeRanges[timeRange]
	if !ok {
		RespondError(w, http.StatusBadRequest,
			"Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d")
		return
	}

	now := time.Now().UTC()
	startTime := now.Add(-duration)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var pairs, calls int64
	var totalTime float64
	err := h.datastore.GetPool().QueryRow(ctx, queryStatsSQL,
		connID, queryID, startTime, now).Scan(&pairs, &calls, &totalTime)
	if err != nil {
		// Missing metrics tables are treated as "no data" rather than an
		// error, matching the top-queries endpoint: a workbench whose
		// collector has never run should render an empty panel, not a
		// failure.
		log.Printf("[DEBUG] No query stats for connection %d: %s", connID, logging.SanitizeForLog(err.Error())) //nolint:gosec // G706: connID is an integer and err is sanitized via logging.SanitizeForLog
		RespondJSON(w, http.StatusOK, buildQueryStats(queryID, 0, 0, 0))
		return
	}

	RespondJSON(w, http.StatusOK,
		buildQueryStats(queryID, pairs, calls, totalTime))
}

// roundTo rounds a float64 to the specified number of decimal places.
func roundTo(val float64, places int) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0
	}
	pow := math.Pow(10, float64(places))
	return math.Round(val*pow) / pow
}
