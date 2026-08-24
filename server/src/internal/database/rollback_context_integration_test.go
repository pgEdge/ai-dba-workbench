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
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// rollbackCtxTracer is a pgx.QueryTracer that reproduces the race the
// deferred-rollback hardening exists for: it cancels the caller's
// request context as soon as a chosen statement finishes, so the
// deferred rollback runs while that context is already canceled. It
// records the context state observed when the ROLLBACK statement is
// issued, which is the property under test.
type rollbackCtxTracer struct {
	mu sync.Mutex

	// cancelAfter is matched (case-insensitively, as a substring)
	// against the SQL of each statement; the request context is
	// canceled when a matching statement completes.
	cancelAfter string
	cancel      context.CancelFunc

	lastSQL             string
	rollbackSeen        bool
	rollbackCtxCanceled bool
}

func (tr *rollbackCtxTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tr.lastSQL = data.SQL
	if strings.EqualFold(strings.TrimSpace(data.SQL), "rollback") {
		tr.rollbackSeen = true
		tr.rollbackCtxCanceled = ctx.Err() != nil
	}
	return ctx
}

func (tr *rollbackCtxTracer) TraceQueryEnd(
	_ context.Context,
	_ *pgx.Conn,
	_ pgx.TraceQueryEndData,
) {
	tr.mu.Lock()
	sql := tr.lastSQL
	cancel := tr.cancel
	match := tr.cancelAfter != "" &&
		strings.Contains(strings.ToLower(sql), strings.ToLower(tr.cancelAfter))
	tr.mu.Unlock()

	// Canceling here, rather than mid-statement, keeps the pooled
	// connection healthy: the statement has already completed, so the
	// only remaining use of the context is the deferred rollback.
	if match && cancel != nil {
		cancel()
	}
}

// newRollbackCtxTestDatastore builds a *Datastore over a single-connection
// pool with the tracer installed. A single connection makes the
// backend-PID assertion below meaningful: if the rollback kills the
// connection, the pool must dial a new backend to serve the next query.
func newRollbackCtxTestDatastore(
	t *testing.T,
	tracer *rollbackCtxTracer,
) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping rollback context integration test")
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Skipf("Could not parse test database connection string: %v", err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, unackAlertTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create rollback context test schema: %v", err)
	}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), unackAlertTestTeardown); err != nil {
			t.Logf("rollback context teardown failed: %v", err)
		}
		pool.Close()
	}

	return NewTestDatastore(pool), pool, cleanup
}

// backendPID returns the PID of the Postgres backend currently serving
// the pool's single connection.
func backendPID(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var pid int
	if err := pool.QueryRow(context.Background(),
		`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("Failed to read backend pid: %v", err)
	}
	return pid
}

// TestDeferredRollbackIgnoresCanceledRequestContext is the regression
// test for issue #381. A request context that is canceled while a
// transaction is still open must not reach tx.Rollback: pgx v5 fails
// the rollback outright on a canceled context and then calls
// conn.die(), which leaks the pooled connection in an aborted
// transaction state (see jackc/pgx#2470).
//
// The test drives UnacknowledgeAlert against an alert that is already
// active, so the function takes its ErrAlertNotAcknowledged early
// return and the deferred rollback is the statement that actually ends
// the transaction. The tracer cancels the request context the moment
// the preceding SELECT completes, which is precisely the race the
// hardening defends against.
func TestDeferredRollbackIgnoresCanceledRequestContext(t *testing.T) {
	tracer := &rollbackCtxTracer{cancelAfter: "select status from alerts"}
	ds, pool, cleanup := newRollbackCtxTestDatastore(t, tracer)
	defer cleanup()

	pidBefore := backendPID(t, pool)

	connID := insertUnackTestConnection(t, pool, "rollback-ctx-test")

	var alertID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (
			alert_type, connection_id, severity, title, description,
			status, triggered_at
		) VALUES (
			'threshold', $1, 'warning', 'active alert', 'description',
			'active', CURRENT_TIMESTAMP
		) RETURNING id
	`, connID).Scan(&alertID); err != nil {
		t.Fatalf("Failed to insert active alert: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	err := ds.UnacknowledgeAlert(ctx, alertID)

	// Semantics are unchanged: the caller still sees the sentinel that
	// maps to HTTP 409, not a context error.
	if !errors.Is(err, ErrAlertNotAcknowledged) {
		t.Fatalf("UnacknowledgeAlert error = %v, want errors.Is ErrAlertNotAcknowledged", err)
	}

	tracer.mu.Lock()
	rollbackSeen := tracer.rollbackSeen
	rollbackCtxCanceled := tracer.rollbackCtxCanceled
	tracer.mu.Unlock()

	if ctx.Err() == nil {
		t.Fatal("request context was not canceled; the test did not exercise the race")
	}
	if !rollbackSeen {
		t.Fatal("no ROLLBACK statement was issued; the test did not exercise the deferred rollback")
	}
	if rollbackCtxCanceled {
		t.Error("deferred rollback ran on the canceled request context; " +
			"it must use a non-cancelable context so pgx cannot kill the pooled connection")
	}

	// The rollback must have succeeded, leaving the pooled connection
	// reusable. A failed rollback calls conn.die(), so the pool would
	// dial a fresh backend with a different PID.
	if pidAfter := backendPID(t, pool); pidAfter != pidBefore {
		t.Errorf("backend pid changed from %d to %d: the pooled connection was "+
			"discarded, which is the leak issue #381 guards against",
			pidBefore, pidAfter)
	}

	// The connection must also be out of any aborted transaction.
	var txStatus string
	if err := pool.QueryRow(context.Background(),
		`SELECT current_setting('transaction_isolation')`).Scan(&txStatus); err != nil {
		t.Fatalf("pooled connection unusable after rollback: %v", err)
	}
}
