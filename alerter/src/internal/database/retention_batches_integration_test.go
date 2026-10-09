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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The retention deletes run in bounded batches, each its own short
// transaction, so that a large retention period no longer holds back the
// xmin horizon for the whole datastore (GitHub issue #615). These tests
// check that the batching deletes every qualifying row and nothing else,
// that it stops on a canceled context, and that the cascade into
// anomaly_embeddings still happens.

const (
	retentionSeedCandidatesSQL = `
		INSERT INTO anomaly_candidates
			(connection_id, metric_name, metric_value, z_score, processed_at)
		SELECT $1, 'm', 1.0, 5.0, $2
		FROM generate_series(1, $3)
	`

	retentionSeedUnprocessedCandidateSQL = `
		INSERT INTO anomaly_candidates
			(connection_id, metric_name, metric_value, z_score, detected_at)
		VALUES ($1, 'm', 1.0, 5.0, $2)
	`

	retentionSeedAlertsSQL = `
		INSERT INTO alerts
			(alert_type, connection_id, severity, title, description,
			 status, triggered_at, cleared_at)
		SELECT 'threshold', $1, 'warning', 't', 'd', $2, $3, $4
		FROM generate_series(1, $5)
	`

	retentionSeedEmbeddingsSQL = `
		INSERT INTO anomaly_embeddings (candidate_id, model_name)
		SELECT id, 'test-model' FROM anomaly_candidates
	`

	retentionCountCandidatesSQL = `SELECT COUNT(*) FROM anomaly_candidates`
	retentionCountAlertsSQL     = `SELECT COUNT(*) FROM alerts`
	retentionCountEmbeddingsSQL = `SELECT COUNT(*) FROM anomaly_embeddings`

	// retentionStatementLogSetupSQL creates the table and trigger
	// function that record how many rows each DELETE statement removes.
	retentionStatementLogSetupSQL = `
		CREATE TABLE IF NOT EXISTS retention_statement_log (rows bigint NOT NULL);
		TRUNCATE retention_statement_log;
		CREATE OR REPLACE FUNCTION retention_statement_log_rows()
		RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			INSERT INTO retention_statement_log
				SELECT count(*) FROM old_rows;
			RETURN NULL;
		END;
		$$;
	`

	// The statement-level triggers that feed retention_statement_log.
	retentionLogCandidateDeletesSQL = `
		CREATE TRIGGER retention_statement_log AFTER DELETE ON anomaly_candidates
		REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT
		EXECUTE FUNCTION retention_statement_log_rows()
	`
	retentionLogAlertDeletesSQL = `
		CREATE TRIGGER retention_statement_log AFTER DELETE ON alerts
		REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT
		EXECUTE FUNCTION retention_statement_log_rows()
	`

	retentionStatementLogTeardownSQL = `
		DROP FUNCTION IF EXISTS retention_statement_log_rows() CASCADE;
		DROP TABLE IF EXISTS retention_statement_log;
	`

	// retentionStatementLogStatsSQL counts the DELETE statements that
	// removed rows, and the most rows any one of them removed.
	retentionStatementLogStatsSQL = `
		SELECT count(*), coalesce(max(rows), 0)
		FROM retention_statement_log
		WHERE rows > 0
	`
)

// logDeleteStatements installs triggerSQL, one of the statement-level
// AFTER DELETE triggers above, which records the number of rows each
// DELETE statement removes, so a test can check how a sweep was split
// into statements. The returned function drops the log table, the function and the trigger;
// defer it after the datastore's cleanup so it runs before the pool
// closes.
func logDeleteStatements(t *testing.T, pool *pgxpool.Pool, triggerSQL string) func() {
	t.Helper()
	ctx := context.Background()
	teardown := func() {
		if _, err := pool.Exec(context.Background(), retentionStatementLogTeardownSQL); err != nil {
			t.Logf("drop statement log: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, retentionStatementLogSetupSQL); err != nil {
		teardown()
		t.Fatalf("create statement log: %v", err)
	}
	if _, err := pool.Exec(ctx, triggerSQL); err != nil {
		teardown()
		t.Fatalf("create statement log trigger: %v", err)
	}
	return teardown
}

// assertBatchedStatements checks that the logged DELETE statements
// split total rows into batches of at most retentionDeleteBatchSize,
// which needs at least ceil(total / retentionDeleteBatchSize) of them.
func assertBatchedStatements(t *testing.T, pool *pgxpool.Pool, total int) {
	t.Helper()
	var statements, largest int64
	if err := pool.QueryRow(context.Background(), retentionStatementLogStatsSQL).
		Scan(&statements, &largest); err != nil {
		t.Fatalf("read statement log: %v", err)
	}
	want := int64((total + retentionDeleteBatchSize - 1) / retentionDeleteBatchSize)
	if statements < want || largest > retentionDeleteBatchSize {
		t.Errorf("statements = %d, largest = %d rows; want at least %d statements of at most %d rows",
			statements, largest, want, retentionDeleteBatchSize)
	}
}

// retentionCount runs a COUNT(*) query and returns the result.
func retentionCount(t *testing.T, ds *Datastore, sql string) int64 {
	t.Helper()
	var n int64
	if err := ds.pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return n
}

// TestDeleteInBatches_LoopsUntilNothingLeft drives the batching helper
// with a batch far smaller than the qualifying set, so that it has to
// loop, and checks it removes every aged row, counts them all, and
// leaves the recent and the unprocessed candidates alone.
func TestDeleteInBatches_LoopsUntilNothingLeft(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "retention-batches")
	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)

	if _, err := pool.Exec(ctx, retentionSeedCandidatesSQL, connID, old, 7); err != nil {
		t.Fatalf("seed old candidates: %v", err)
	}
	if _, err := pool.Exec(ctx, retentionSeedCandidatesSQL, connID, time.Now(), 2); err != nil {
		t.Fatalf("seed recent candidates: %v", err)
	}
	if _, err := pool.Exec(ctx, retentionSeedUnprocessedCandidateSQL, connID, old); err != nil {
		t.Fatalf("seed unprocessed candidate: %v", err)
	}

	deleted, err := ds.deleteInBatches(ctx, deleteOldAnomalyCandidatesBatchSQL, cutoff, 3)
	if err != nil {
		t.Fatalf("deleteInBatches: %v", err)
	}
	if deleted != 7 {
		t.Errorf("deleted = %d, want 7", deleted)
	}
	if got := retentionCount(t, ds, retentionCountCandidatesSQL); got != 3 {
		t.Errorf("candidates remaining = %d, want 3 (two recent, one unprocessed)", got)
	}

	// A second sweep finds nothing and says so.
	deleted, err = ds.deleteInBatches(ctx, deleteOldAnomalyCandidatesBatchSQL, cutoff, 3)
	if err != nil || deleted != 0 {
		t.Errorf("second sweep = (%d, %v), want (0, nil)", deleted, err)
	}
}

// TestDeleteInBatches_AlertsKeepActiveAndRecent checks the alert batch
// statement against every combination the predicate distinguishes.
func TestDeleteInBatches_AlertsKeepActiveAndRecent(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "retention-alerts")
	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)

	seeds := []struct {
		status    string
		triggered time.Time
		cleared   *time.Time
		count     int
	}{
		{"cleared", old, &old, 4},      // aged out on cleared_at
		{"acknowledged", old, nil, 3},  // aged out on triggered_at
		{"active", old, nil, 2},        // never reaped while active
		{"cleared", old, timePtr(), 1}, // cleared recently, kept
	}
	for _, s := range seeds {
		if _, err := pool.Exec(ctx, retentionSeedAlertsSQL,
			connID, s.status, s.triggered, s.cleared, s.count); err != nil {
			t.Fatalf("seed %s alerts: %v", s.status, err)
		}
	}

	deleted, err := ds.deleteInBatches(ctx, deleteOldAlertsBatchSQL, cutoff, 2)
	if err != nil {
		t.Fatalf("deleteInBatches: %v", err)
	}
	if deleted != 7 {
		t.Errorf("deleted = %d, want 7", deleted)
	}
	if got := retentionCount(t, ds, retentionCountAlertsSQL); got != 3 {
		t.Errorf("alerts remaining = %d, want 3", got)
	}
}

// timePtr returns a pointer to the current time.
func timePtr() *time.Time {
	now := time.Now()
	return &now
}

// TestDeleteOldAnomalyCandidates_MoreThanOneBatch goes through the
// exported method with more aged rows than one batch holds, and checks
// the cascade into anomaly_embeddings when pgvector is installed.
func TestDeleteOldAnomalyCandidates_MoreThanOneBatch(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "retention-many")
	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)
	aged := retentionDeleteBatchSize*2 + 5

	if _, err := pool.Exec(ctx, retentionSeedCandidatesSQL, connID, old, aged); err != nil {
		t.Fatalf("seed old candidates: %v", err)
	}
	if _, err := pool.Exec(ctx, retentionSeedCandidatesSQL, connID, time.Now(), 1); err != nil {
		t.Fatalf("seed recent candidate: %v", err)
	}

	withEmbeddings := pgvectorAvailable(ctx, pool) &&
		createAnomalyEmbeddingsTable(ctx, pool) == nil
	if withEmbeddings {
		if _, err := pool.Exec(ctx, retentionSeedEmbeddingsSQL); err != nil {
			t.Fatalf("seed embeddings: %v", err)
		}
	}

	defer logDeleteStatements(t, pool, retentionLogCandidateDeletesSQL)()

	deleted, err := ds.DeleteOldAnomalyCandidates(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldAnomalyCandidates: %v", err)
	}
	if deleted != int64(aged) {
		t.Errorf("deleted = %d, want %d", deleted, aged)
	}
	assertBatchedStatements(t, pool, aged)
	if got := retentionCount(t, ds, retentionCountCandidatesSQL); got != 1 {
		t.Errorf("candidates remaining = %d, want 1", got)
	}
	if withEmbeddings {
		if got := retentionCount(t, ds, retentionCountEmbeddingsSQL); got != 1 {
			t.Errorf("embeddings remaining = %d, want 1 (the cascade should take the rest)", got)
		}
	} else {
		t.Log("pgvector unavailable; cascade into anomaly_embeddings not checked")
	}
}

// TestDeleteOldAlerts_MoreThanOneBatch goes through the exported method
// with more aged alerts than one batch holds, and checks that they are
// removed in several statements of at most retentionDeleteBatchSize rows.
func TestDeleteOldAlerts_MoreThanOneBatch(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "retention-many-alerts")
	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)
	aged := retentionDeleteBatchSize*2 + 5

	if _, err := pool.Exec(ctx, retentionSeedAlertsSQL,
		connID, "cleared", old, old, aged); err != nil {
		t.Fatalf("seed old alerts: %v", err)
	}
	if _, err := pool.Exec(ctx, retentionSeedAlertsSQL,
		connID, "active", old, nil, 1); err != nil {
		t.Fatalf("seed active alert: %v", err)
	}

	defer logDeleteStatements(t, pool, retentionLogAlertDeletesSQL)()

	deleted, err := ds.DeleteOldAlerts(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldAlerts: %v", err)
	}
	if deleted != int64(aged) {
		t.Errorf("deleted = %d, want %d", deleted, aged)
	}
	assertBatchedStatements(t, pool, aged)
	if got := retentionCount(t, ds, retentionCountAlertsSQL); got != 1 {
		t.Errorf("alerts remaining = %d, want 1 (the active alert)", got)
	}
}

// TestRetentionDeletes_StopOnCanceledContext checks that a canceled
// context stops both sweeps before they delete anything, returning
// context.Canceled wrapped with the operation's name.
func TestRetentionDeletes_StopOnCanceledContext(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "retention-cancel")
	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)

	if _, err := pool.Exec(ctx, retentionSeedCandidatesSQL, connID, old, 3); err != nil {
		t.Fatalf("seed candidates: %v", err)
	}
	if _, err := pool.Exec(ctx, retentionSeedAlertsSQL,
		connID, "cleared", old, old, 3); err != nil {
		t.Fatalf("seed alerts: %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	tests := []struct {
		name  string
		run   func(context.Context, time.Time) (int64, error)
		want  string
		count string
	}{
		{"alerts", ds.DeleteOldAlerts, "failed to delete old alerts", retentionCountAlertsSQL},
		{"candidates", ds.DeleteOldAnomalyCandidates, "failed to delete old anomaly candidates", retentionCountCandidatesSQL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleted, err := tt.run(canceled, cutoff)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.want)
			}
			if deleted != 0 {
				t.Errorf("deleted = %d, want 0", deleted)
			}
			if got := retentionCount(t, ds, tt.count); got != 3 {
				t.Errorf("rows remaining = %d, want 3", got)
			}
		})
	}
}
