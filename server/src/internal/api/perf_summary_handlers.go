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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// validTimeRanges maps time_range parameter values to their duration.
var validTimeRanges = map[string]time.Duration{
	"1h":  1 * time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
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
		return
	}

	mux.HandleFunc("/api/v1/metrics/performance-summary",
		authWrapper(h.handlePerfSummary))
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
	rbacChecker := auth.NewRBACChecker(h.authStore, true)
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
			"Invalid time_range: must be one of 1h, 6h, 24h, 7d")
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
			log.Printf("[DEBUG] Could not look up connection %d: %v", id, err)
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
		log.Printf("[DEBUG] No XID age data for connection %d: %v",
			connectionID, err)
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
		log.Printf("[DEBUG] No cache hit data for connection %d: %v",
			connectionID, err)
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
		log.Printf("[DEBUG] No cache hit time series for connection %d: %v",
			connectionID, err)
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
		log.Printf("[DEBUG] No transaction data for connection %d: %v",
			connectionID, err)
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
		log.Printf("[DEBUG] No checkpoint data for connection %d: %v",
			connectionID, err)
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

// roundTo rounds a float64 to the specified number of decimal places.
func roundTo(val float64, places int) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0
	}
	pow := math.Pow(10, float64(places))
	return math.Round(val*pow) / pow
}
