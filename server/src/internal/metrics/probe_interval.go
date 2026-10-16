/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultProbeInterval is the collection interval assumed for a probe that
// has no probe_configs row at all. It matches the collector's own fallback
// when a probe is missing from its configuration.
const DefaultProbeInterval = 300 * time.Second

// ResolveProbeInterval returns the collection interval of probeName for
// the given connections, as the collector configures it in probe_configs.
//
// The collector creates a server-scope row per connection and probe
// on first run, copying any cluster or group override into it, so the
// server-scope rows are the closest thing to the interval the collector
// actually runs at. The resolution is therefore: the server-scope rows for
// the given connections; then, for any connection without one, the global
// row; then DefaultProbeInterval. With several connections the largest
// interval wins, because one bucket width has to serve every series in the
// response and the coarsest probe decides how fine it can be.
func ResolveProbeInterval(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	connectionIDs []int,
) (time.Duration, error) {
	const serverQuery = `
        SELECT MAX(collection_interval_seconds), COUNT(DISTINCT connection_id)
        FROM probe_configs
        WHERE name = $1 AND scope = 'server' AND connection_id = ANY($2)`

	var serverMax *int
	var covered int
	err := pool.QueryRow(ctx, serverQuery, probeName, connectionIDs).
		Scan(&serverMax, &covered)
	if err != nil {
		return 0, fmt.Errorf(
			"failed to resolve interval for probe %q: %w", probeName, err)
	}

	best := 0
	if serverMax != nil {
		best = *serverMax
	}

	// Every requested connection has a server row: nothing else competes.
	if covered >= len(connectionIDs) && best > 0 {
		return time.Duration(best) * time.Second, nil
	}

	const globalQuery = `
        SELECT collection_interval_seconds
        FROM probe_configs
        WHERE name = $1 AND scope = 'global'
        ORDER BY id
        LIMIT 1`

	var global int
	err = pool.QueryRow(ctx, globalQuery, probeName).Scan(&global)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No global row: the server rows, if any, decide.
	case err != nil:
		return 0, fmt.Errorf(
			"failed to resolve global interval for probe %q: %w", probeName, err)
	case global > best:
		best = global
	}

	if best <= 0 {
		return DefaultProbeInterval, nil
	}
	return time.Duration(best) * time.Second, nil
}

// ObserveSampleSpacing returns the tightest spacing actually present in the
// stored samples of probeName for the given connections inside window: per
// connection the smallest positive difference between consecutive distinct
// collection times, and across connections the largest of those, matching
// ResolveProbeInterval's rule that the coarsest probe decides. It returns
// zero when the window holds fewer than two samples for every connection,
// which is no evidence either way.
//
// The point is that probe_configs describes how the collector runs now,
// whilst the samples being judged were taken under whatever configuration
// was in force when they were collected. Tightening a probe's interval
// must not retroactively reclassify older, wider-spaced samples as
// collection gaps, so callers take the larger of the two (see
// ResolveEffectiveInterval).
//
// The smallest spacing is deliberate rather than an average or a median: a
// genuine outage is a single wide interval among tighter ones and leaves
// the minimum alone, so the bound only widens when every sample in the
// window is spaced more widely than the configuration claims.
func ObserveSampleSpacing(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	connectionIDs []int,
	window TimeWindow,
) (time.Duration, error) {
	if !IsValidIdentifier(probeName) {
		return 0, fmt.Errorf("invalid probe name %q", probeName)
	}

	query := `
        WITH samples AS (
            SELECT
                connection_id,
                collected_at,
                LAG(collected_at) OVER (
                    PARTITION BY connection_id ORDER BY collected_at
                ) AS prev_collected_at
            FROM (
                SELECT DISTINCT connection_id, collected_at
                FROM metrics.` + QuoteIdentifier(probeName) + `
                WHERE connection_id = ANY($1)
                    AND collected_at >= $2
                    AND collected_at <= $3
            ) distinct_samples
        ),
        per_connection AS (
            SELECT MIN(EXTRACT(EPOCH FROM collected_at - prev_collected_at))
                AS spacing_sec
            FROM samples
            WHERE prev_collected_at IS NOT NULL
                AND collected_at > prev_collected_at
            GROUP BY connection_id
        )
        SELECT MAX(spacing_sec) FROM per_connection`

	var spacing *float64
	err := pool.QueryRow(ctx, query, connectionIDs, window.Start, window.End).
		Scan(&spacing)
	if err != nil {
		return 0, fmt.Errorf(
			"failed to observe sample spacing for probe %q: %w", probeName, err)
	}
	if spacing == nil || *spacing <= 0 {
		return 0, nil
	}
	return time.Duration(*spacing * float64(time.Second)), nil
}

// ResolveEffectiveInterval returns the interval the stored samples should
// be judged against: the configured collection interval, widened to the
// observed sample spacing whenever the samples in window are spaced more
// widely than the configuration claims.
//
// Bounds derived from it (the counter gap bound and the gauge carry) then
// only narrow where the configuration agrees with the samples it is
// judging, so tightening a probe's interval cannot blank the history
// collected at the old, wider one.
func ResolveEffectiveInterval(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	connectionIDs []int,
	window TimeWindow,
) (configured, effective time.Duration, err error) {
	configured, err = ResolveProbeInterval(ctx, pool, probeName, connectionIDs)
	if err != nil {
		return 0, 0, err
	}
	observed, err := ObserveSampleSpacing(
		ctx, pool, probeName, connectionIDs, window)
	if err != nil {
		return 0, 0, err
	}
	effective = configured
	if observed > effective {
		effective = observed
	}
	return configured, effective, nil
}
