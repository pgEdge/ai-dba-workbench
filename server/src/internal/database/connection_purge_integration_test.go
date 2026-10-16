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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// issue669PurgeSchema creates two metrics tables with a cascading
// foreign key to connections, one partitioned by week like the
// collector's and one plain, plus a table outside metrics whose rows
// the purge must leave to the cascade. The cluster dismiss schema's
// connections table lacks is_monitored, so it is added here.
const issue669PurgeSchema = `
    ALTER TABLE connections
        ADD COLUMN IF NOT EXISTS is_monitored BOOLEAN NOT NULL DEFAULT FALSE;

    CREATE SCHEMA IF NOT EXISTS metrics;

    CREATE TABLE metrics.issue669_partitioned (
        connection_id INTEGER NOT NULL
            REFERENCES connections(id) ON DELETE CASCADE,
        n INTEGER NOT NULL,
        collected_at TIMESTAMPTZ NOT NULL,
        PRIMARY KEY (connection_id, collected_at, n)
    ) PARTITION BY RANGE (collected_at);
    CREATE TABLE metrics.issue669_partitioned_w1
        PARTITION OF metrics.issue669_partitioned
        FOR VALUES FROM ('2026-01-05') TO ('2026-01-12');
    CREATE TABLE metrics.issue669_partitioned_w2
        PARTITION OF metrics.issue669_partitioned
        FOR VALUES FROM ('2026-01-12') TO ('2026-01-19');

    CREATE TABLE metrics.issue669_plain (
        connection_id INTEGER NOT NULL
            REFERENCES connections(id) ON DELETE CASCADE,
        n INTEGER NOT NULL
    );

    CREATE TABLE public.issue669_outside (
        connection_id INTEGER NOT NULL
            REFERENCES connections(id) ON DELETE CASCADE
    );`

const issue669PurgeTeardown = `
    DROP TABLE IF EXISTS metrics.issue669_partitioned;
    DROP TABLE IF EXISTS metrics.issue669_plain;
    DROP TABLE IF EXISTS public.issue669_outside;`

// newIssue669PurgeDatastore extends the cluster dismiss schema, which
// provides connections, with the tables above.
func newIssue669PurgeDatastore(t *testing.T) (*Datastore, *pgxpool.Pool) {
	t.Helper()
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, issue669PurgeTeardown+issue669PurgeSchema); err != nil {
		cleanup()
		t.Fatalf("creating purge test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), issue669PurgeTeardown); err != nil {
			t.Logf("purge test teardown failed: %v", err)
		}
		cleanup()
	})
	return ds, pool
}

// seedIssue669Metrics writes rows rows for connection id into each
// table, spread across both weekly partitions.
func seedIssue669Metrics(t *testing.T, pool *pgxpool.Pool, id, rows int) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO metrics.issue669_partitioned
            SELECT $1, n, '2026-01-05'::timestamptz + (n % 14) * INTERVAL '1 day'
            FROM generate_series(1, $2) n`,
		`INSERT INTO metrics.issue669_plain SELECT $1, n FROM generate_series(1, $2) n`,
		`INSERT INTO public.issue669_outside SELECT $1 FROM generate_series(1, 3 + 0 * $2)`,
	} {
		if _, err := pool.Exec(ctx, stmt, id, rows); err != nil {
			t.Fatalf("seeding metrics: %v", err)
		}
	}
}

func countIssue669Rows(t *testing.T, pool *pgxpool.Pool, table string, id int) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE connection_id = $1`, id,
	).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// TestPurgeConnectionMetrics is the datastore regression test for issue
// #669: the purge clears every metrics table, partitioned or not, in
// more than one batch, and leaves other connections' rows and tables
// outside metrics alone.
func TestPurgeConnectionMetrics(t *testing.T) {
	ds, pool := newIssue669PurgeDatastore(t)
	target := insertConnectionDeleteTestConnection(t, pool, "purge-target", nil)
	other := insertConnectionDeleteTestConnection(t, pool, "purge-other", nil)

	rows := ConnectionPurgeBatchSize*2 + 7
	seedIssue669Metrics(t, pool, target, rows)
	seedIssue669Metrics(t, pool, other, 5)

	if err := ds.PurgeConnectionMetrics(context.Background(), target); err != nil {
		t.Fatalf("PurgeConnectionMetrics: %v", err)
	}

	for _, table := range []string{"metrics.issue669_partitioned", "metrics.issue669_plain"} {
		if n := countIssue669Rows(t, pool, table, target); n != 0 {
			t.Errorf("%s still holds %d rows for the purged connection", table, n)
		}
		if n := countIssue669Rows(t, pool, table, other); n != 5 {
			t.Errorf("%s holds %d rows for the other connection, want 5", table, n)
		}
	}
	if n := countIssue669Rows(t, pool, "public.issue669_outside", target); n != 3 {
		t.Errorf("table outside metrics holds %d rows, want 3 left for the cascade", n)
	}

	// The connection row is untouched; DeleteConnection then removes
	// it and cascades the rest.
	if err := ds.DeleteConnection(context.Background(), target); err != nil {
		t.Fatalf("DeleteConnection after purge: %v", err)
	}
	if n := countIssue669Rows(t, pool, "public.issue669_outside", target); n != 0 {
		t.Errorf("cascade left %d rows outside metrics", n)
	}
}

// TestPurgeConnectionMetrics_Errors covers the failure branches: the
// catalog query and a batch delete.
func TestPurgeConnectionMetrics_Errors(t *testing.T) {
	t.Run("batch delete fails", func(t *testing.T) {
		ds, pool := newIssue669PurgeDatastore(t)
		id := insertConnectionDeleteTestConnection(t, pool, "purge-fail", nil)
		seedIssue669Metrics(t, pool, id, 3)
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `
            CREATE FUNCTION metrics.issue669_block() RETURNS trigger
            LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'blocked by test'; END; $$;
            CREATE TRIGGER issue669_block BEFORE DELETE ON metrics.issue669_plain
            FOR EACH ROW EXECUTE FUNCTION metrics.issue669_block();
        `); err != nil {
			t.Fatalf("installing trigger: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(),
				`DROP FUNCTION IF EXISTS metrics.issue669_block() CASCADE`)
		})

		err := ds.PurgeConnectionMetrics(ctx, id)
		if err == nil || !strings.Contains(err.Error(), "issue669_plain") {
			t.Fatalf("error = %v, want a purge failure naming issue669_plain", err)
		}
	})

	t.Run("catalog query fails", func(t *testing.T) {
		ds, _ := newIssue669PurgeDatastore(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := ds.PurgeConnectionMetrics(ctx, 1)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
}

// TestDisableConnectionMonitoring checks the flag is cleared, a missing
// connection is reported as such, and a database failure is wrapped.
func TestDisableConnectionMonitoring(t *testing.T) {
	ds, pool := newIssue669PurgeDatastore(t)
	ctx := context.Background()
	id := insertConnectionDeleteTestConnection(t, pool, "disable-target", nil)
	if _, err := pool.Exec(ctx,
		`UPDATE connections SET is_monitored = TRUE WHERE id = $1`, id); err != nil {
		t.Fatalf("enabling monitoring: %v", err)
	}

	if err := ds.DisableConnectionMonitoring(ctx, id); err != nil {
		t.Fatalf("DisableConnectionMonitoring: %v", err)
	}
	var monitored bool
	if err := pool.QueryRow(ctx,
		`SELECT is_monitored FROM connections WHERE id = $1`, id,
	).Scan(&monitored); err != nil {
		t.Fatalf("reading is_monitored: %v", err)
	}
	if monitored {
		t.Error("connection still monitored")
	}

	if err := ds.DisableConnectionMonitoring(ctx, 999999); !errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("missing connection: error = %v, want ErrConnectionNotFound", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	err := ds.DisableConnectionMonitoring(canceled, id)
	if err == nil || errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("canceled context: error = %v, want a wrapped database error", err)
	}
}
