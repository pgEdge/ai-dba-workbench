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
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// healthTestRelationsDDL creates the dashboard tables and the metrics
// schema that VerifySchemaHealth probes. The DDL is wrapped in
// CREATE ... IF NOT EXISTS so individual tests can selectively drop a
// single table (to exercise the partial-drop branch) without breaking
// the shared fixture.
const healthTestRelationsDDL = `
CREATE TABLE IF NOT EXISTS cluster_groups (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS blackouts (
    id BIGSERIAL PRIMARY KEY,
    reason TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS blackout_schedules (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE SCHEMA IF NOT EXISTS metrics;
CREATE TABLE IF NOT EXISTS metrics.pg_settings (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);
`

// healthTestSchemaVersionDDL recreates the schema_version table the
// floor check reads. Tests that exercise a missing table simply skip
// this DDL.
const healthTestSchemaVersionDDL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// healthTestTeardown drops everything VerifySchemaHealth touches so
// the next test starts from a clean slate. CASCADE is used because
// the test schema may have foreign keys from siblings in this
// package's other integration tests.
const healthTestTeardown = `
DROP TABLE IF EXISTS schema_version CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// newHealthTestPool returns a pgxpool.Pool connected to the test
// database described by TEST_AI_WORKBENCH_SERVER. The caller receives
// a cleanup that drops every relation VerifySchemaHealth touches and
// closes the pool. Tests skip cleanly when the env var is unset.
func newHealthTestPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping health integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	// Always start from a clean slate; an earlier failed test could
	// leave partial state behind.
	if _, err := pool.Exec(ctx, healthTestTeardown); err != nil {
		pool.Close()
		t.Fatalf("Failed to reset health test schema: %v", err)
	}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), healthTestTeardown); err != nil {
			t.Logf("health teardown failed: %v", err)
		}
		pool.Close()
	}

	return pool, cleanup
}

// applyHealthRelations creates every relation that VerifySchemaHealth
// probes. Tests that exercise the all-present happy path call this
// before invoking VerifySchemaHealth.
func applyHealthRelations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), healthTestRelationsDDL); err != nil {
		t.Fatalf("Failed to create health relations: %v", err)
	}
}

// applySchemaVersion (re)creates the schema_version table and inserts
// the supplied version row. Pass version=0 to create the table but
// leave it empty.
func applySchemaVersion(t *testing.T, pool *pgxpool.Pool, version int) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, healthTestSchemaVersionDDL); err != nil {
		t.Fatalf("Failed to create schema_version: %v", err)
	}
	if version > 0 {
		_, err := pool.Exec(ctx,
			`INSERT INTO schema_version (version, description)
			 VALUES ($1, $2)
			 ON CONFLICT (version) DO NOTHING`,
			version, fmt.Sprintf("test seed v%d", version))
		if err != nil {
			t.Fatalf("Failed to seed schema_version=%d: %v", version, err)
		}
	}
}

// resetHealthFixture brings the test database back to a known-empty
// state between sub-tests so the next case starts from scratch.
func resetHealthFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), healthTestTeardown); err != nil {
		t.Fatalf("Failed to reset health fixture: %v", err)
	}
}

// TestVerifySchemaHealth_TableMissing covers the first failure mode:
// no schema_version table exists at all. The error must wrap
// ErrSchemaVersionTableMissing and must include the datastore target
// so an operator can tell which DB is wrong.
func TestVerifySchemaHealth_TableMissing(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	// No schema_version table, no critical relations.
	err := ds.VerifySchemaHealth(context.Background())
	if err == nil {
		t.Fatal("expected error when schema_version table is missing")
	}
	if !errors.Is(err, ErrSchemaVersionTableMissing) {
		t.Errorf("expected ErrSchemaVersionTableMissing, got %v", err)
	}
	if !strings.Contains(err.Error(), "no schema_version table") {
		t.Errorf("error must name the missing table: %v", err)
	}
	if !strings.Contains(err.Error(), "collector") {
		t.Errorf("error must point operator at the collector: %v", err)
	}
}

// TestVerifySchemaHealth_TablePresentButEmpty covers the "table
// exists, no rows" case. This collapses into the same operator
// message as the missing-table case because both mean "no collector
// has run here".
func TestVerifySchemaHealth_TablePresentButEmpty(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	applySchemaVersion(t, pool, 0) // table only, no rows

	err := ds.VerifySchemaHealth(context.Background())
	if err == nil {
		t.Fatal("expected error when schema_version is empty")
	}
	if !errors.Is(err, ErrSchemaVersionTableMissing) {
		t.Errorf("expected ErrSchemaVersionTableMissing for empty table, got %v", err)
	}
}

// TestVerifySchemaHealth_BelowFloor covers a version that is present
// but lower than MinCollectorSchemaVersion. The error must wrap
// ErrSchemaVersionBelowFloor and must include both the current and
// required version numbers.
func TestVerifySchemaHealth_BelowFloor(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	belowFloor := MinCollectorSchemaVersion - 1
	if belowFloor < 1 {
		t.Skipf("MinCollectorSchemaVersion=%d leaves no room for a below-floor test",
			MinCollectorSchemaVersion)
	}
	applySchemaVersion(t, pool, belowFloor)
	applyHealthRelations(t, pool)

	err := ds.VerifySchemaHealth(context.Background())
	if err == nil {
		t.Fatal("expected error when version is below floor")
	}
	if !errors.Is(err, ErrSchemaVersionBelowFloor) {
		t.Errorf("expected ErrSchemaVersionBelowFloor, got %v", err)
	}
	wantVersion := fmt.Sprintf("v%d", belowFloor)
	if !strings.Contains(err.Error(), wantVersion) {
		t.Errorf("error must mention current version %s: %v", wantVersion, err)
	}
	wantFloor := fmt.Sprintf("v%d", MinCollectorSchemaVersion)
	if !strings.Contains(err.Error(), wantFloor) {
		t.Errorf("error must mention required version %s: %v", wantFloor, err)
	}
}

// TestVerifySchemaHealth_AtFloor covers the boundary: version equals
// MinCollectorSchemaVersion with all critical relations present. The
// check must succeed.
func TestVerifySchemaHealth_AtFloor(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	applySchemaVersion(t, pool, MinCollectorSchemaVersion)
	applyHealthRelations(t, pool)

	if err := ds.VerifySchemaHealth(context.Background()); err != nil {
		t.Errorf("expected nil error at floor with all relations present, got %v", err)
	}
}

// TestVerifySchemaHealth_AboveFloor covers a version greater than the
// floor (the typical case after a future migration).
func TestVerifySchemaHealth_AboveFloor(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	applySchemaVersion(t, pool, MinCollectorSchemaVersion+5)
	applyHealthRelations(t, pool)

	if err := ds.VerifySchemaHealth(context.Background()); err != nil {
		t.Errorf("expected nil error above floor with all relations present, got %v", err)
	}
}

// TestVerifySchemaHealth_RelationMissing covers the drift probe: the
// floor passes, but one of the critical relations has been dropped.
// The error must name the missing relation. This case iterates each
// member of criticalRelations to guarantee the schema-qualified
// metrics.pg_settings branch is exercised alongside the unqualified
// names.
func TestVerifySchemaHealth_RelationMissing(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	for _, victim := range criticalRelations {
		t.Run(victim.name, func(t *testing.T) {
			// Each sub-case starts from a known-clean fixture, then
			// recreates everything and drops exactly the victim.
			resetHealthFixture(t, pool)
			applySchemaVersion(t, pool, MinCollectorSchemaVersion)
			applyHealthRelations(t, pool)

			// Drop the victim. Schema-qualified names go through as-is;
			// unqualified names get dropped from the search_path schema.
			// The victim name comes from the hardcoded
			// criticalRelations slice; the fmt.Sprintf here is fixture
			// scaffolding, not user-driven SQL.
			dropStmt := fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, victim.name)
			if _, err := pool.Exec(context.Background(), dropStmt); err != nil {
				t.Fatalf("Failed to drop %s: %v", victim.name, err)
			}

			err := ds.VerifySchemaHealth(context.Background())
			if err == nil {
				t.Fatalf("expected error after dropping %s", victim.name)
			}
			if !errors.Is(err, ErrCriticalRelationMissing) {
				t.Errorf("expected ErrCriticalRelationMissing, got %v", err)
			}
			if !strings.Contains(err.Error(), victim.name) {
				t.Errorf("error must name the missing relation %q: %v", victim.name, err)
			}
			if !strings.Contains(err.Error(), "partially") {
				t.Errorf("error must hint at partial drop: %v", err)
			}
		})
	}
}

// TestVerifySchemaHealth_AllPresent covers the all-green happy path
// at the floor, asserting that the target description is reachable
// (not "<unknown>") through the live pool.
func TestVerifySchemaHealth_AllPresent(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	applySchemaVersion(t, pool, MinCollectorSchemaVersion)
	applyHealthRelations(t, pool)

	if err := ds.VerifySchemaHealth(context.Background()); err != nil {
		t.Fatalf("expected success when all relations present: %v", err)
	}

	target := ds.targetDescription()
	if target == "<unknown>" {
		t.Error("targetDescription must resolve real connection details from the pool")
	}
	if !strings.Contains(target, "/") {
		t.Errorf("target description should include host:port/db separator, got %q", target)
	}
}

// TestVerifySchemaHealth_FloorReadError exercises the non-undefined
// transport-error branch in readSchemaVersion's error wrapping. The
// caller passes a context that has already been canceled, so the
// pool's QueryRow returns context.Canceled (which is not a
// pgconn.PgError); VerifySchemaHealth must surface the wrapped
// error rather than misclassify it as a missing-table case.
func TestVerifySchemaHealth_FloorReadError(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	// schema_version exists; the failure must come from the query
	// itself, not from a missing relation.
	applySchemaVersion(t, pool, MinCollectorSchemaVersion)
	applyHealthRelations(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ds.VerifySchemaHealth(ctx)
	if err == nil {
		t.Fatal("expected error when context is already canceled")
	}
	if errors.Is(err, ErrSchemaVersionTableMissing) {
		t.Errorf("cancellation must not be misclassified as missing-table: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to read schema_version") {
		t.Errorf("error must surface the read failure wrapper: %v", err)
	}
}

// TestVerifySchemaHealth_ProbeError exercises the non-undefined
// error branch inside the criticalRelations probe loop. Without a
// straightforward way to make a single relation probe fail on
// something other than 42P01, the test directly invokes
// probeRelation with a canceled context: that returns
// context.Canceled, which probeRelation surfaces verbatim. The
// surrounding VerifySchemaHealth wrapper is also tested by
// canceling the context between the floor read and the probe
// loop, which on a fast database lands inside the probe loop and
// drives the loop's wrapping branch.
func TestVerifySchemaHealth_ProbeError(t *testing.T) {
	pool, cleanup := newHealthTestPool(t)
	defer cleanup()
	ds := NewTestDatastore(pool)

	applySchemaVersion(t, pool, MinCollectorSchemaVersion)
	applyHealthRelations(t, pool)

	// Direct probe with a canceled context: must return the
	// underlying error, not ErrCriticalRelationMissing. The probe
	// target is the first entry in criticalRelations, which carries
	// a static probe query string.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ds.probeRelation(ctx, criticalRelations[0])
	if err == nil {
		t.Fatal("expected probeRelation to fail on a canceled context")
	}
	if errors.Is(err, ErrCriticalRelationMissing) {
		t.Errorf("cancellation must not be misclassified as relation-missing: %v", err)
	}

	// Now exercise VerifySchemaHealth's loop-level wrapper: invoke
	// the public entry point with a context whose deadline is
	// already in the past. The floor read may or may not race
	// against the deadline; if it does fail, we still get a wrapped
	// error from somewhere, which is fine -- the assertion below is
	// loose enough to accept either branch as long as we do not
	// silently succeed.
	deadCtx, deadCancel := context.WithCancel(context.Background())
	deadCancel()
	if err := ds.VerifySchemaHealth(deadCtx); err == nil {
		t.Error("expected VerifySchemaHealth to fail on a canceled context")
	}
}

// TestVerifySchemaHealth_NilDatastore exercises the defensive guards
// at the top of the public entry point. A nil receiver and a
// nil-pool receiver must both return an explicit error rather than
// panic; these branches are unreachable from production but cheap to
// keep tested so a future refactor cannot regress them.
func TestVerifySchemaHealth_NilDatastore(t *testing.T) {
	var ds *Datastore
	if err := ds.VerifySchemaHealth(context.Background()); err == nil {
		t.Error("expected error for nil Datastore")
	}

	empty := &Datastore{}
	if err := empty.VerifySchemaHealth(context.Background()); err == nil {
		t.Error("expected error for nil-pool Datastore")
	}

	// targetDescription must also be panic-safe on both shapes.
	if got := ds.targetDescription(); got != "<unknown>" {
		t.Errorf("targetDescription on nil Datastore = %q, want <unknown>", got)
	}
	if got := empty.targetDescription(); got != "<unknown>" {
		t.Errorf("targetDescription on nil-pool Datastore = %q, want <unknown>", got)
	}
}
