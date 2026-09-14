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
