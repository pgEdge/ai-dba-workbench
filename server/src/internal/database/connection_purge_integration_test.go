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
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connPurgeTestSchema is the minimum the two-step delete (issue #669)
// touches: a connections table, a cascading dependent outside the
// metrics schema (purge_test_alerts), a SET NULL dependent that the mark
// step must leave alone (purge_test_history), a partitioned metrics
// table with two partitions, and a metrics table with no foreign key to
// connections at all.
const connPurgeTestSchema = `
DROP TABLE IF EXISTS purge_test_alerts CASCADE;
DROP TABLE IF EXISTS purge_test_history CASCADE;
DROP TABLE IF EXISTS metrics.purge_test_part CASCADE;
DROP TABLE IF EXISTS metrics.purge_test_nofk CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;

CREATE SCHEMA IF NOT EXISTS metrics;

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    auto_cluster_key VARCHAR(255),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    deleted_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE purge_test_alerts (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE
);

CREATE TABLE purge_test_history (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE SET NULL
);

CREATE TABLE metrics.purge_test_part (
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    n INTEGER NOT NULL,
    PRIMARY KEY (connection_id, n)
) PARTITION BY RANGE (n);
CREATE TABLE metrics.purge_test_part_lo PARTITION OF metrics.purge_test_part
    FOR VALUES FROM (0) TO (100);
CREATE TABLE metrics.purge_test_part_hi PARTITION OF metrics.purge_test_part
    FOR VALUES FROM (100) TO (200);

CREATE TABLE metrics.purge_test_nofk (
    connection_id INTEGER NOT NULL,
    n INTEGER NOT NULL,
    PRIMARY KEY (connection_id, n)
);
`

const connPurgeTestTeardown = `
DROP TABLE IF EXISTS purge_test_alerts CASCADE;
DROP TABLE IF EXISTS purge_test_history CASCADE;
DROP TABLE IF EXISTS metrics.purge_test_part CASCADE;
DROP TABLE IF EXISTS metrics.purge_test_nofk CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
`

// newConnPurgeTestDatastore builds the purge schema on the
// TEST_AI_WORKBENCH_SERVER instance and returns a datastore over it.
func newConnPurgeTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping connection purge integration test")
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
	if _, err := pool.Exec(ctx, connPurgeTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create connection purge test schema: %v", err)
	}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), connPurgeTestTeardown); err != nil {
			t.Logf("connection purge teardown failed: %v", err)
		}
		pool.Close()
	}
	return NewTestDatastore(pool), pool, cleanup
}

// seedPurgeConnection inserts a connection with metricsRows rows spread
// over both partitions and the no-foreign-key table, plus one cascading
// and one SET NULL dependent row, and returns its id.
func seedPurgeConnection(t *testing.T, pool *pgxpool.Pool, name string, metricsRows int) int {
	t.Helper()
	ctx := context.Background()
	var id int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name) VALUES ($1) RETURNING id`, name,
	).Scan(&id); err != nil {
		t.Fatalf("insert connection %q: %v", name, err)
	}
	stmts := []string{
		// Odd n land in the low partition, even n in the high one.
		`INSERT INTO metrics.purge_test_part (connection_id, n)
         SELECT $1, CASE WHEN g % 2 = 0 THEN 100 + g ELSE g END
         FROM generate_series(1, $2) g`,
		`INSERT INTO metrics.purge_test_nofk (connection_id, n)
         SELECT $1, g FROM generate_series(1, $2) g`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, id, metricsRows); err != nil {
			t.Fatalf("seed metrics for %q: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO purge_test_alerts (connection_id) VALUES ($1)`, id); err != nil {
		t.Fatalf("seed alert for %q: %v", name, err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO purge_test_history (connection_id) VALUES ($1)`, id); err != nil {
		t.Fatalf("seed history for %q: %v", name, err)
	}
	return id
}

// countPurgeRows counts the rows referencing connection id in table.
// table is a fixed name from the tests below.
func countPurgeRows(t *testing.T, pool *pgxpool.Pool, table string, id int) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM "+table+" WHERE connection_id = $1", id,
	).Scan(&n); err != nil {
		t.Fatalf("count %s rows: %v", table, err)
	}
	return n
}

// TestConnectionPurge_EndToEnd walks a connection through both steps:
// the mark deletes the cascading dependents and leaves the metrics and
// the SET NULL dependent, and the purge then removes the metrics in
// batches from every partition and the table without a foreign key,
// and finally the row. A second connection is untouched throughout.
func TestConnectionPurge_EndToEnd(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	// 23 rows with a batch size of 5 needs several full batches and a
	// short final one in each table.
	victim := seedPurgeConnection(t, pool, "victim", 23)
	keeper := seedPurgeConnection(t, pool, "keeper", 7)

	if err := ds.DeleteConnection(ctx, victim); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}

	if got := countPurgeRows(t, pool, "purge_test_alerts", victim); got != 0 {
		t.Fatalf("cascading dependent rows after mark = %d, want 0", got)
	}
	if got := countPurgeRows(t, pool, "purge_test_history", victim); got != 1 {
		t.Fatalf("SET NULL dependent rows after mark = %d, want 1", got)
	}
	if got := countPurgeRows(t, pool, "metrics.purge_test_part", victim); got != 23 {
		t.Fatalf("metrics rows after mark = %d, want 23 (purge has not run)", got)
	}

	ids, err := ds.ListDeletedConnectionIDs(ctx)
	if err != nil {
		t.Fatalf("ListDeletedConnectionIDs: %v", err)
	}
	if !slices.Equal(ids, []int{victim}) {
		t.Fatalf("ListDeletedConnectionIDs = %v, want [%d]", ids, victim)
	}

	if err := ds.PurgeConnection(ctx, victim, 5); err != nil {
		t.Fatalf("PurgeConnection: %v", err)
	}

	for _, table := range []string{
		"metrics.purge_test_part_lo", "metrics.purge_test_part_hi",
		"metrics.purge_test_nofk",
	} {
		if got := countPurgeRows(t, pool, table, victim); got != 0 {
			t.Errorf("%s rows of the purged connection = %d, want 0", table, got)
		}
	}
	if got := countPurgeRows(t, pool, "metrics.purge_test_part", keeper); got != 7 {
		t.Errorf("keeper partitioned rows = %d, want 7", got)
	}
	if got := countPurgeRows(t, pool, "metrics.purge_test_nofk", keeper); got != 7 {
		t.Errorf("keeper no-foreign-key rows = %d, want 7", got)
	}
	if got := countPurgeRows(t, pool, "purge_test_alerts", keeper); got != 1 {
		t.Errorf("keeper alert rows = %d, want 1", got)
	}

	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM connections WHERE id = $1`, victim,
	).Scan(&rows); err != nil {
		t.Fatalf("count connection rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("purged connection still has %d rows", rows)
	}
	var nulled int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM purge_test_history WHERE connection_id IS NULL`,
	).Scan(&nulled); err != nil {
		t.Fatalf("count nulled history rows: %v", err)
	}
	if nulled != 1 {
		t.Fatalf("history rows set to NULL by the final delete = %d, want 1", nulled)
	}

	ids, err = ds.ListDeletedConnectionIDs(ctx)
	if err != nil {
		t.Fatalf("ListDeletedConnectionIDs after purge: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("ListDeletedConnectionIDs after purge = %v, want none", ids)
	}
}

// TestPurgeConnection_UnmarkedIsNoOp verifies that a live connection's
// metrics can never be purged, whatever id the caller passes.
func TestPurgeConnection_UnmarkedIsNoOp(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()

	live := seedPurgeConnection(t, pool, "live", 4)
	if err := ds.PurgeConnection(context.Background(), live, 2); err != nil {
		t.Fatalf("PurgeConnection on a live connection: %v", err)
	}
	if got := countPurgeRows(t, pool, "metrics.purge_test_part", live); got != 4 {
		t.Fatalf("live connection metrics rows = %d, want 4", got)
	}
	if got := countPurgeRows(t, pool, "purge_test_alerts", live); got != 1 {
		t.Fatalf("live connection alert rows = %d, want 1", got)
	}
}

// TestPurgeConnection_RejectsNonPositiveBatch covers the batch size
// guard, which would otherwise loop forever on a zero LIMIT.
func TestPurgeConnection_RejectsNonPositiveBatch(t *testing.T) {
	ds := NewTestDatastore(nil)
	for _, size := range []int{0, -1} {
		err := ds.PurgeConnection(context.Background(), 1, size)
		if err == nil || !strings.Contains(err.Error(), "batch size must be positive") {
			t.Errorf("PurgeConnection(batch %d) error = %v, want a batch size error", size, err)
		}
	}
}

// TestPurgeConnection_ResumesAfterFailure is the restart-safety test: a
// batch that fails leaves the connection marked with its remaining
// metrics, and a later pass finishes the job.
func TestPurgeConnection_ResumesAfterFailure(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	victim := seedPurgeConnection(t, pool, "victim", 6)
	if err := ds.DeleteConnection(ctx, victim); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}

	if _, err := pool.Exec(ctx, `
        CREATE OR REPLACE FUNCTION purge_test_block()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'purge blocked by test trigger';
        END;
        $$;
        CREATE TRIGGER purge_test_block
        BEFORE DELETE ON metrics.purge_test_nofk
        FOR EACH ROW EXECUTE FUNCTION purge_test_block();
    `); err != nil {
		t.Fatalf("install blocking trigger: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS purge_test_block() CASCADE`)
	}()

	err := ds.PurgeConnection(ctx, victim, 4)
	if err == nil || !strings.Contains(err.Error(), "purge_test_nofk") {
		t.Fatalf("PurgeConnection error = %v, want a purge_test_nofk failure", err)
	}
	if !connectionMarkedDeleted(t, pool, victim) {
		t.Fatal("connection lost its mark after a failed purge")
	}
	if got := countPurgeRows(t, pool, "metrics.purge_test_nofk", victim); got != 6 {
		t.Fatalf("no-foreign-key rows after failed purge = %d, want 6", got)
	}

	if _, err := pool.Exec(ctx,
		`DROP TRIGGER purge_test_block ON metrics.purge_test_nofk`); err != nil {
		t.Fatalf("drop blocking trigger: %v", err)
	}
	if err := ds.PurgeConnection(ctx, victim, 4); err != nil {
		t.Fatalf("PurgeConnection retry: %v", err)
	}
	if got := countPurgeRows(t, pool, "metrics.purge_test_nofk", victim); got != 0 {
		t.Fatalf("no-foreign-key rows after retry = %d, want 0", got)
	}
}

// TestPurgeConnection_RemoveRowFails covers the final DELETE failing:
// the metrics are gone, but the row stays marked for the next pass.
func TestPurgeConnection_RemoveRowFails(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	victim := seedPurgeConnection(t, pool, "victim", 3)
	if err := ds.DeleteConnection(ctx, victim); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        CREATE OR REPLACE FUNCTION purge_test_block_row()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'row delete blocked by test trigger';
        END;
        $$;
        CREATE TRIGGER purge_test_block_row
        BEFORE DELETE ON connections
        FOR EACH ROW EXECUTE FUNCTION purge_test_block_row();
    `); err != nil {
		t.Fatalf("install blocking trigger: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS purge_test_block_row() CASCADE`)
	}()

	err := ds.PurgeConnection(ctx, victim, 10)
	if err == nil || !strings.Contains(err.Error(), "failed to remove deleted connection") {
		t.Fatalf("PurgeConnection error = %v, want a row removal failure", err)
	}
	if !connectionMarkedDeleted(t, pool, victim) {
		t.Fatal("connection lost its mark after a failed row removal")
	}
}

// TestPurgeConnection_CommitFails covers the commit of the final DELETE
// failing, through a deferred constraint trigger.
func TestPurgeConnection_CommitFails(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	victim := seedPurgeConnection(t, pool, "victim", 1)
	if err := ds.DeleteConnection(ctx, victim); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        CREATE OR REPLACE FUNCTION purge_test_block_commit()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'purge commit blocked by test trigger';
        END;
        $$;
        CREATE CONSTRAINT TRIGGER purge_test_block_commit
        AFTER DELETE ON connections
        DEFERRABLE INITIALLY DEFERRED
        FOR EACH ROW EXECUTE FUNCTION purge_test_block_commit();
    `); err != nil {
		t.Fatalf("install commit-blocking trigger: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS purge_test_block_commit() CASCADE`)
	}()

	err := ds.PurgeConnection(ctx, victim, 10)
	if err == nil || !strings.Contains(err.Error(), "failed to commit purge") {
		t.Fatalf("PurgeConnection error = %v, want a commit failure", err)
	}
	if !connectionMarkedDeleted(t, pool, victim) {
		t.Fatal("connection lost its mark after a failed commit")
	}
}

// TestDeleteConnection_DependentDeleteFails covers a cascading
// dependent that cannot be deleted at mark time: the whole mark rolls
// back.
func TestDeleteConnection_DependentDeleteFails(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	victim := seedPurgeConnection(t, pool, "victim", 1)
	if _, err := pool.Exec(ctx, `
        CREATE OR REPLACE FUNCTION purge_test_block_dep()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'dependent delete blocked by test trigger';
        END;
        $$;
        CREATE TRIGGER purge_test_block_dep
        BEFORE DELETE ON purge_test_alerts
        FOR EACH ROW EXECUTE FUNCTION purge_test_block_dep();
    `); err != nil {
		t.Fatalf("install blocking trigger: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS purge_test_block_dep() CASCADE`)
	}()

	err := ds.DeleteConnection(ctx, victim)
	if err == nil || !strings.Contains(err.Error(), "purge_test_alerts") {
		t.Fatalf("DeleteConnection error = %v, want a purge_test_alerts failure", err)
	}
	if errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection returned ErrConnectionNotFound: %v", err)
	}
	if connectionMarkedDeleted(t, pool, victim) {
		t.Fatal("connection was marked despite the failed dependent delete")
	}
}

// TestConnectionPurge_ClosedPool covers the query failures of the purge
// entry points against a pool that can no longer connect.
func TestConnectionPurge_ClosedPool(t *testing.T) {
	_, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()

	scratchPool, err := pgxpool.New(context.Background(), pool.Config().ConnString())
	if err != nil {
		t.Fatalf("scratch pool creation failed: %v", err)
	}
	scratch := NewTestDatastore(scratchPool)
	scratchPool.Close()

	if _, err := scratch.ListDeletedConnectionIDs(context.Background()); err == nil {
		t.Error("ListDeletedConnectionIDs against a closed pool returned nil error")
	}
	if err := scratch.PurgeConnection(context.Background(), 1, 10); err == nil {
		t.Error("PurgeConnection against a closed pool returned nil error")
	}
	if err := scratch.removeDeletedConnection(context.Background(), 1); err == nil {
		t.Error("removeDeletedConnection against a closed pool returned nil error")
	}
}

// TestDeletedConnectionHiddenFromReaders verifies that a connection
// marked deleted is invisible to the datastore's connection readers and
// cannot be updated, exactly as if it had gone (issue #669).
func TestDeletedConnectionHiddenFromReaders(t *testing.T) {
	ds, pool, cleanup := newConnUpdatePasswordTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	var liveID, goneID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name) VALUES ('live') RETURNING id`,
	).Scan(&liveID); err != nil {
		t.Fatalf("insert live connection: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name, deleted_at) VALUES ('gone', now()) RETURNING id`,
	).Scan(&goneID); err != nil {
		t.Fatalf("insert marked connection: %v", err)
	}

	list, err := ds.GetAllConnections(ctx)
	if err != nil {
		t.Fatalf("GetAllConnections: %v", err)
	}
	if len(list) != 1 || list[0].ID != liveID {
		t.Fatalf("GetAllConnections = %+v, want only connection %d", list, liveID)
	}

	if _, err := ds.GetConnection(ctx, goneID); err == nil {
		t.Error("GetConnection returned a connection marked deleted")
	}
	if _, _, err := ds.GetConnectionSharingInfo(ctx, goneID); err == nil {
		t.Error("GetConnectionSharingInfo returned a connection marked deleted")
	}
	if _, err := ds.UpdateConnectionName(ctx, goneID, "revived"); err == nil {
		t.Error("UpdateConnectionName updated a connection marked deleted")
	}
	name := "revived"
	if _, err := ds.UpdateConnectionFull(ctx, goneID, ConnectionUpdateParams{Name: &name}); err == nil {
		t.Error("UpdateConnectionFull updated a connection marked deleted")
	}
	if err := ds.DeleteConnection(ctx, goneID); !errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("DeleteConnection of a marked connection error = %v, want ErrConnectionNotFound", err)
	}

	if _, err := ds.GetConnection(ctx, liveID); err != nil {
		t.Errorf("GetConnection(live): %v", err)
	}
}

// TestPurgeConnection_SweepsLateRowsWithoutForeignKey covers rows the
// collector writes after a table's batch loop has finished but before
// the connection row goes. The cascade catches the table with a foreign
// key; the table without one would keep its rows for good, as the id
// is no longer marked once the row is gone, so the purge sweeps it once
// more after the row is removed.
func TestPurgeConnection_SweepsLateRowsWithoutForeignKey(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	victim := seedPurgeConnection(t, pool, "victim", 4)
	keeper := seedPurgeConnection(t, pool, "keeper", 3)
	if err := ds.DeleteConnection(ctx, victim); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}

	lateWrite := func(ctx context.Context) error {
		if _, err := pool.Exec(ctx, `INSERT INTO metrics.purge_test_nofk
            (connection_id, n) SELECT $1, g FROM generate_series(500, 506) g`,
			victim); err != nil {
			return err
		}
		_, err := pool.Exec(ctx, `INSERT INTO metrics.purge_test_part
            (connection_id, n) VALUES ($1, 50), ($1, 150)`, victim)
		return err
	}
	if err := ds.purgeConnection(ctx, victim, 2, lateWrite); err != nil {
		t.Fatalf("purgeConnection: %v", err)
	}

	for _, table := range []string{"metrics.purge_test_nofk", "metrics.purge_test_part"} {
		if got := countPurgeRows(t, pool, table, victim); got != 0 {
			t.Errorf("%s rows written late = %d, want 0", table, got)
		}
	}
	if got := countPurgeRows(t, pool, "metrics.purge_test_nofk", keeper); got != 3 {
		t.Errorf("keeper no-foreign-key rows = %d, want 3", got)
	}
}

// TestPurgeConnection_HookAndFinalSweepFailures covers the two failure
// points around the row removal: a failing hook leaves the connection
// marked for the next pass, and a failing final sweep is reported after
// the row is gone.
func TestPurgeConnection_HookAndFinalSweepFailures(t *testing.T) {
	ds, pool, cleanup := newConnPurgeTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	victim := seedPurgeConnection(t, pool, "victim", 2)
	if err := ds.DeleteConnection(ctx, victim); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}

	hookErr := errors.New("hook failed")
	err := ds.purgeConnection(ctx, victim, 10, func(context.Context) error {
		return hookErr
	})
	if !errors.Is(err, hookErr) {
		t.Fatalf("purgeConnection with a failing hook = %v, want %v", err, hookErr)
	}
	ids, err := ds.ListDeletedConnectionIDs(ctx)
	if err != nil {
		t.Fatalf("ListDeletedConnectionIDs: %v", err)
	}
	if !slices.Equal(ids, []int{victim}) {
		t.Fatalf("marked connections after a failed hook = %v, want [%d]", ids, victim)
	}

	// Renaming the table makes the final sweep fail, as the purge still
	// holds the name it listed before the row went.
	breakSweep := func(ctx context.Context) error {
		_, err := pool.Exec(ctx,
			`ALTER TABLE metrics.purge_test_nofk RENAME TO purge_test_nofk_moved`)
		return err
	}
	err = ds.purgeConnection(ctx, victim, 10, breakSweep)
	// Drop the moved table before the deferred teardown closes the pool.
	if _, dropErr := pool.Exec(ctx,
		`DROP TABLE IF EXISTS metrics.purge_test_nofk_moved`); dropErr != nil {
		t.Errorf("drop the moved table: %v", dropErr)
	}
	if err == nil || !strings.Contains(err.Error(), "purge_test_nofk") {
		t.Fatalf("purgeConnection with a failing final sweep = %v, want a purge_test_nofk error", err)
	}
	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM connections WHERE id = $1`, victim,
	).Scan(&rows); err != nil {
		t.Fatalf("count connection rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("connection rows after the final sweep failed = %d, want 0", rows)
	}
}
