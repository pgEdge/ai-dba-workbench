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
	"errors"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// maxAggregationTimeSpan caps the window the metrics aggregation
// endpoints will roll up. It is deliberately tighter than
// metrics.MaxCustomTimeSpan, which stays at 366 days for /metrics/query,
// where the bucket width is derived from the span and so the work stays
// bounded however long the window is. Nothing damps the cost on the
// endpoints this cap governs: their aggregations are linear in the window,
// several of them run more than one scan per request (top-queries runs the
// whole CTE twice, once for the count and once for the page, and
// performance-summary runs five sub-queries per requested connection), and
// they all share a datastore pool with only a handful of connections. The
// pool's statement_timeout (database.DefaultDatastoreStatementTimeout,
// 30 seconds unless configured) is the backstop that stops a runaway
// statement holding a connection indefinitely; this cap is what keeps an
// ordinary request well inside it, so that an unbounded window is not a
// cheap authenticated denial of service.
//
// Thirty days is the largest preset in metrics.ValidTimeRanges, so it is
// the longest window the web client can ask for, and it is the shape
// idx_pg_stat_statements_identity_time (collector migration 15) was
// benchmarked against. Anyone raising this figure needs to re-measure the
// aggregations at the new span, including the top-queries
// exclude_collector=true case, which cannot use the index-only scan and so
// carries the query text through a sort.
const maxAggregationTimeSpan = 30 * 24 * time.Hour

// aggregationTimeSpanError is the 400 message returned when a custom window
// exceeds maxAggregationTimeSpan. The wording follows the span error from
// metrics.ResolveCustomWindow so that the two read alike.
const aggregationTimeSpanError = "invalid time range: span must not exceed 30 days"

// checkAggregationTimeSpan reports whether an already-resolved window is
// short enough for an aggregation endpoint to roll up. It is applied per
// endpoint, immediately after metrics.ResolveTimeWindow returns, rather
// than by tightening the shared constant, because the other endpoints that
// resolve a custom window legitimately need the full 366 days.
func checkAggregationTimeSpan(window metrics.TimeWindow) error {
	if window.End.Sub(window.Start) > maxAggregationTimeSpan {
		return errors.New(aggregationTimeSpanError)
	}
	return nil
}
