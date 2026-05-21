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
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MinCollectorSchemaVersion is the minimum collector-owned schema
// version the server requires to operate correctly.
//
// The collector at /collector/src/database/schema.go owns the dashboard
// schema (cluster_groups, alerts, blackouts, metrics.*, etc.) and tracks
// applied migrations in the `schema_version` table. The server is a
// consumer of that schema and never runs the migrations itself, so it
// must verify that the datastore is at least at this version at startup
// or refuse to come up.
//
// Bump this constant whenever the server begins to depend on a feature
// introduced by a new collector migration. The corresponding migration
// must be merged to the collector first; otherwise an upgraded server
// will refuse to start against a still-current collector schema.
const MinCollectorSchemaVersion = 4

// pgErrCodeUndefinedTable is the SQLSTATE that PostgreSQL returns when
// a relation referenced in a query does not exist.
const pgErrCodeUndefinedTable = "42P01"

// criticalRelations enumerates a small canonical set of dashboard
// tables the server must be able to read. The set is intentionally
// minimal: covering one relation per major feature area is enough to
// detect partial drops, and probing more relations would only slow
// startup without adding signal.
//
// The list mixes unqualified names (resolved through the search_path)
// with one schema-qualified name (metrics.pg_settings); probeRelation
// handles the quoting accordingly.
var criticalRelations = []string{
	"cluster_groups",
	"alerts",
	"blackouts",
	"blackout_schedules",
	"metrics.pg_settings",
}

// Sentinel errors for schema health verification. Callers may inspect
// these with errors.Is to differentiate failure modes (for example, a
// test that wants to assert specifically that the floor check fired
// rather than the drift probe).
var (
	// ErrSchemaVersionTableMissing is returned when the schema_version
	// table itself does not exist; this almost always means the
	// collector has never been run against the configured database.
	ErrSchemaVersionTableMissing = errors.New("schema_version table not found")

	// ErrSchemaVersionBelowFloor is returned when the recorded
	// collector schema version is lower than MinCollectorSchemaVersion.
	ErrSchemaVersionBelowFloor = errors.New("collector schema below minimum required version")

	// ErrCriticalRelationMissing is returned when the schema_version
	// check passed but one of the criticalRelations probes failed; the
	// most likely cause is a manual DROP that the migration framework
	// cannot detect.
	ErrCriticalRelationMissing = errors.New("critical relation missing from datastore")
)

// VerifySchemaHealth runs two layered checks against the datastore and
// returns nil only when the server can safely serve requests:
//
//  1. Floor check. Reads MAX(version) from schema_version. If the table
//     is missing, the value is zero, or the value is below
//     MinCollectorSchemaVersion, an operator-actionable error is
//     returned naming the datastore target and the required action.
//
//  2. Drift probe. For each relation in criticalRelations runs
//     SELECT 1 FROM <rel> LIMIT 0. A 42P01 (undefined_table) from any
//     probe surfaces as ErrCriticalRelationMissing with the offending
//     relation name embedded; any other error is returned wrapped.
//
// Both checks fail closed: the caller (server main) is expected to log
// the message and exit non-zero rather than continuing to serve from a
// broken datastore.
func (d *Datastore) VerifySchemaHealth(ctx context.Context) error {
	if d == nil || d.pool == nil {
		return errors.New("datastore pool is not initialized")
	}

	target := d.targetDescription()

	version, err := d.readSchemaVersion(ctx)
	if err != nil {
		if errors.Is(err, ErrSchemaVersionTableMissing) {
			return fmt.Errorf(
				"datastore at %s has no schema_version table -- "+
					"has the collector been run against this database? "+
					"The dashboard schema is owned by the collector; "+
					"the server is a consumer of it: %w",
				target, ErrSchemaVersionTableMissing,
			)
		}
		return fmt.Errorf(
			"datastore at %s: failed to read schema_version: %w",
			target, err,
		)
	}

	if version == 0 {
		return fmt.Errorf(
			"datastore at %s has an empty schema_version table -- "+
				"has the collector been run against this database? "+
				"The dashboard schema is owned by the collector; "+
				"the server is a consumer of it: %w",
			target, ErrSchemaVersionTableMissing,
		)
	}

	if version < MinCollectorSchemaVersion {
		return fmt.Errorf(
			"datastore at %s: collector schema is at v%d, server "+
				"requires at least v%d; upgrade the collector and "+
				"restart: %w",
			target, version, MinCollectorSchemaVersion,
			ErrSchemaVersionBelowFloor,
		)
	}

	for _, rel := range criticalRelations {
		if err := d.probeRelation(ctx, rel); err != nil {
			if errors.Is(err, ErrCriticalRelationMissing) {
				return fmt.Errorf(
					"datastore at %s: critical relation %q is "+
						"missing; the datastore looks partially "+
						"dropped -- re-run the collector to "+
						"restore the dashboard schema: %w",
					target, rel, ErrCriticalRelationMissing,
				)
			}
			return fmt.Errorf(
				"datastore at %s: probe of %q failed: %w",
				target, rel, err,
			)
		}
	}

	return nil
}

// readSchemaVersion returns the maximum recorded migration version in
// the collector's schema_version table. A missing table is reported as
// ErrSchemaVersionTableMissing; an empty table returns 0 without
// error (the COALESCE in the query collapses an empty aggregate to
// zero) so the caller can apply a uniform message for both flavors
// of "no collector has run here yet".
func (d *Datastore) readSchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := d.pool.QueryRow(
		ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_version`,
	).Scan(&version)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeUndefinedTable {
			return 0, ErrSchemaVersionTableMissing
		}
		return 0, err
	}
	return version, nil
}

// probeRelation executes a zero-row probe against the given relation.
// The relation may be schema-qualified (for example "metrics.pg_settings")
// or unqualified ("alerts"); the function quotes each segment with
// pgx.Identifier.Sanitize so a dotted name does not get rendered as a
// single identifier.
//
// On a successful probe the returned error is nil. A 42P01 from
// PostgreSQL is normalised to ErrCriticalRelationMissing so callers can
// distinguish "table missing" from arbitrary transport errors with
// errors.Is.
func (d *Datastore) probeRelation(ctx context.Context, rel string) error {
	parts := strings.Split(rel, ".")
	quoted := pgx.Identifier(parts).Sanitize()

	// LIMIT 0 keeps the probe metadata-only: PostgreSQL still
	// resolves the relation (so a missing table surfaces immediately
	// with 42P01) but no rows are read or transferred. Exec rather
	// than Query so any error from the round-trip is returned
	// directly without a separate rows.Err() second-chance check.
	_, err := d.pool.Exec(ctx,
		fmt.Sprintf(`SELECT 1 FROM %s LIMIT 0`, quoted))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeUndefinedTable {
			return ErrCriticalRelationMissing
		}
		return err
	}
	return nil
}

// targetDescription returns a "user@host:port/database" string for the
// configured datastore, suitable for inclusion in operator-facing
// error messages. The detail is read from the pool's config rather
// than from a separate field on Datastore so this helper remains
// correct regardless of how the pool was constructed.
//
// If the pool is nil (for example, a zero-value Datastore constructed
// by a test that did not wire up a pool), the function returns a
// "<unknown>" sentinel. pgxpool.Pool.Config and the embedded
// ConnConfig are guaranteed non-nil for any live pool, so no further
// defensive checks are warranted here.
func (d *Datastore) targetDescription() string {
	if d == nil || d.pool == nil {
		return "<unknown>"
	}
	cc := d.pool.Config().ConnConfig
	return fmt.Sprintf("%s@%s:%d/%s", cc.User, cc.Host, cc.Port, cc.Database)
}
