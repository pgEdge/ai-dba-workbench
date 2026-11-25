/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"fmt"
	"time"
)

// Request deadlines for datastore work on plain PostgreSQL. Handlers pass
// one of these (or their own figure) to Datastore.QueryTimeout, which
// raises it on a datastore whose queries are known to be slower.
const (
	// ShortQueryTimeout bounds a request that reads or writes a handful
	// of indexed rows: a connection, a cluster, a group.
	ShortQueryTimeout = 10 * time.Second

	// LongQueryTimeout bounds a request that aggregates metrics or
	// gathers an estate snapshot or timeline.
	LongQueryTimeout = 30 * time.Second
)

// ColdFrontQueryTimeout is the least deadline a request gets on a pgEdge
// ColdFront datastore when database.query_timeout is unset (issue #891).
// ColdFront's metrics views have no indexes and run in DuckDB, and the
// first statement over one in a session attaches the Iceberg catalog,
// which was measured at about 29 seconds; later statements take a few
// seconds each. Listing connections reads four of those views in one
// request, and nothing guarantees every pooled connection has paid the
// attach already, so neither ShortQueryTimeout nor LongQueryTimeout
// survives a cold pool. Two minutes covers several cold attaches in one
// request whilst staying well inside MaxQueryTimeout.
const ColdFrontQueryTimeout = 2 * time.Minute

// MaxQueryTimeout is the largest database.query_timeout accepted. It is
// ConnectionDeleteTimeout, the longest budget the server already grants
// a request, chosen so the response can still be written within the
// HTTP server's 300 second WriteTimeout.
const MaxQueryTimeout = ConnectionDeleteTimeout

// resolveQueryTimeout parses the configured database.query_timeout. An
// empty value returns zero, which selects the per-datastore default in
// Datastore.QueryTimeout. Anything else must be a positive Go duration
// no larger than MaxQueryTimeout; failing here names the configuration
// key rather than leaving every request to time out.
func resolveQueryTimeout(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid query_timeout: %w", err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("invalid query_timeout: %s is not positive", value)
	}
	if timeout > MaxQueryTimeout {
		return 0, fmt.Errorf("invalid query_timeout: %s exceeds the maximum of %s",
			value, MaxQueryTimeout)
	}
	return timeout, nil
}

// effectiveQueryTimeoutFloor is the least deadline any datastore
// request gets: the configured query_timeout when set,
// ColdFrontQueryTimeout on a ColdFront datastore, and zero (no floor)
// otherwise.
func effectiveQueryTimeoutFloor(configured time.Duration, coldFront bool) time.Duration {
	if configured > 0 {
		return configured
	}
	if coldFront {
		return ColdFrontQueryTimeout
	}
	return 0
}

// QueryTimeout returns the deadline for a request whose budget on plain
// PostgreSQL is base: base itself, raised to the configured
// database.query_timeout or, when that is unset, to ColdFrontQueryTimeout
// on a ColdFront datastore. It never lowers base. A nil Datastore returns
// base, so handlers built without one in tests keep their own budget.
func (d *Datastore) QueryTimeout(base time.Duration) time.Duration {
	if d == nil {
		return base
	}
	return max(base, d.queryTimeoutFloor)
}

// WithQueryTimeout derives a context from parent whose deadline is
// QueryTimeout(base).
func (d *Datastore) WithQueryTimeout(parent context.Context, base time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d.QueryTimeout(base))
}
