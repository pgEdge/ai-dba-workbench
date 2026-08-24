/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txRollbackTracer cancels the caller's context as soon as a chosen
// statement completes, and records the context state seen when the
// ROLLBACK is issued. It reproduces the issue #381 race: a client that
// disappears mid-request cancels the context whilst a transaction is
// still open.
type txRollbackTracer struct {
	mu sync.Mutex

	cancelAfter string
	cancel      context.CancelFunc

	lastSQL             string
	rollbacks           int
	rollbackCtxCanceled bool
}

func (tr *txRollbackTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tr.lastSQL = data.SQL
	if strings.EqualFold(strings.TrimSpace(data.SQL), "rollback") {
		tr.rollbacks++
		if ctx.Err() != nil {
			tr.rollbackCtxCanceled = true
		}
	}
	return ctx
}

func (tr *txRollbackTracer) TraceQueryEnd(
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

	if match && cancel != nil {
		cancel()
	}
}

func (tr *txRollbackTracer) snapshot() (rollbacks int, canceled bool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.rollbacks, tr.rollbackCtxCanceled
}

// newTracedToolsPool returns a single-connection pool with the tracer
// installed. One connection makes the backend-PID comparison below
// meaningful: pgx discards a connection whose rollback failed, so the
// pool would have to dial a new backend.
func newTracedToolsPool(t *testing.T, tracer *txRollbackTracer) (*pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping transaction rollback integration test")
	}

	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Skipf("Could not parse test database connection string: %v", err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	return pool, pool.Close
}

// toolsBackendPID returns the PID of the backend serving the pool's
// single connection.
func toolsBackendPID(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var pid int
	if err := pool.QueryRow(context.Background(),
		`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("Failed to read backend pid: %v", err)
	}
	return pid
}

// assertCleanRollback checks the invariant issue #381 established: the
// ROLLBACK ran, it did not run on a canceled context, and the pooled
// connection was returned rather than destroyed.
func assertCleanRollback(
	t *testing.T,
	tracer *txRollbackTracer,
	pool *pgxpool.Pool,
	pidBefore int,
) {
	t.Helper()

	rollbacks, canceled := tracer.snapshot()
	if rollbacks == 0 {
		t.Fatal("no ROLLBACK was issued; the test did not exercise the rollback path")
	}
	if canceled {
		t.Error("rollback ran on the canceled request context; it must use a " +
			"non-cancelable context so pgx cannot discard the pooled connection")
	}
	if pidAfter := toolsBackendPID(t, pool); pidAfter != pidBefore {
		t.Errorf("backend pid changed from %d to %d: the pooled connection was "+
			"discarded by a failed rollback", pidBefore, pidAfter)
	}
}

// TestBeginReadOnlyTxCleanupIgnoresCanceledContext covers the cleanup
// closure returned by BeginReadOnlyTx. The request context is canceled
// once transaction setup finishes, so the rollback that cleanup performs
// for an uncommitted transaction must run on a non-cancelable context.
func TestBeginReadOnlyTxCleanupIgnoresCanceledContext(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "set local statement_timeout"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	pidBefore := toolsBackendPID(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	rot, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
	if errResp != nil {
		t.Fatalf("BeginReadOnlyTx returned error response: %v", errResp)
	}
	if rot == nil {
		t.Fatal("expected non-nil ManagedTx")
	}
	if ctx.Err() == nil {
		t.Fatal("request context was not canceled; the test did not exercise the race")
	}

	// No Commit(), so cleanup must roll the transaction back.
	cleanup()

	assertCleanRollback(t, tracer, pool, pidBefore)
}

// TestBeginTxCleanupIgnoresCanceledContext mirrors the read-only case
// for the read-write constructor.
func TestBeginTxCleanupIgnoresCanceledContext(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "set local statement_timeout"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	pidBefore := toolsBackendPID(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	mt, errResp, cleanup := BeginTx(ctx, pool)
	if errResp != nil {
		t.Fatalf("BeginTx returned error response: %v", errResp)
	}
	if mt == nil {
		t.Fatal("expected non-nil ManagedTx")
	}

	cleanup()

	assertCleanRollback(t, tracer, pool, pidBefore)
}

// TestBeginReadOnlyTxSetupFailureRollsBackCleanly covers the
// setup-failure rollback: canceling the context immediately after BEGIN
// makes "SET TRANSACTION READ ONLY" fail, so the constructor rolls back
// before returning its MCP error response. That rollback must also
// ignore the canceled context.
func TestBeginReadOnlyTxSetupFailureRollsBackCleanly(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "begin"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	pidBefore := toolsBackendPID(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	rot, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
	defer cleanup()

	if errResp == nil {
		t.Fatal("expected an error response when transaction setup fails")
	}
	if rot != nil {
		t.Error("expected a nil ManagedTx when transaction setup fails")
	}

	assertCleanRollback(t, tracer, pool, pidBefore)
}

// TestBeginTxSetupFailureRollsBackCleanly covers the same setup-failure
// rollback for the read-write constructor, where the failing statement
// is the statement_timeout guard.
func TestBeginTxSetupFailureRollsBackCleanly(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "begin"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	pidBefore := toolsBackendPID(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	mt, errResp, cleanup := BeginTx(ctx, pool)
	defer cleanup()

	if errResp == nil {
		t.Fatal("expected an error response when transaction setup fails")
	}
	if mt != nil {
		t.Error("expected a nil ManagedTx when transaction setup fails")
	}

	assertCleanRollback(t, tracer, pool, pidBefore)
}

// TestBeginReadOnlyTxTimeoutSetupFailureRollsBackCleanly covers the
// second setup-failure rollback in BeginReadOnlyTx, where the read-only
// statement succeeds and the statement_timeout guard is the statement
// that fails.
func TestBeginReadOnlyTxTimeoutSetupFailureRollsBackCleanly(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "set transaction read only"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	pidBefore := toolsBackendPID(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	rot, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
	defer cleanup()

	if errResp == nil {
		t.Fatal("expected an error response when the statement timeout guard fails")
	}
	if rot != nil {
		t.Error("expected a nil ManagedTx when transaction setup fails")
	}

	assertCleanRollback(t, tracer, pool, pidBefore)
}

// TestBeginTxConstructorsRejectClosedPool covers the begin-failure
// branch of both constructors, where no transaction exists yet and the
// returned cleanup must be a harmless no-op.
func TestBeginTxConstructorsRejectClosedPool(t *testing.T) {
	tracer := &txRollbackTracer{}
	pool, _ := newTracedToolsPool(t, tracer)
	pool.Close()

	ctx := context.Background()

	rot, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
	if errResp == nil {
		t.Error("BeginReadOnlyTx on a closed pool returned no error response")
	}
	if rot != nil {
		t.Error("BeginReadOnlyTx on a closed pool returned a ManagedTx")
	}
	cleanup()

	mt, errResp, cleanup := BeginTx(ctx, pool)
	if errResp == nil {
		t.Error("BeginTx on a closed pool returned no error response")
	}
	if mt != nil {
		t.Error("BeginTx on a closed pool returned a ManagedTx")
	}
	cleanup()

	if rollbacks, _ := tracer.snapshot(); rollbacks != 0 {
		t.Errorf("rollbacks issued without a transaction = %d, want 0", rollbacks)
	}
}

// TestBeginReadOnlyTxCleanupRollsBackOnPanic covers the recover branch
// of the cleanup closure: a panic must still roll the transaction back
// on a non-cancelable context, and must then be re-raised so the
// caller's own recovery logic still sees it.
func TestBeginReadOnlyTxCleanupRollsBackOnPanic(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "set local statement_timeout"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	pidBefore := toolsBackendPID(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	recovered := func() (r any) {
		defer func() { r = recover() }()

		_, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
		if errResp != nil {
			t.Fatalf("BeginReadOnlyTx returned error response: %v", errResp)
		}
		defer cleanup()

		panic("tool handler exploded")
	}()

	if recovered != "tool handler exploded" {
		t.Fatalf("recovered value = %v, want the original panic to be re-raised", recovered)
	}

	assertCleanRollback(t, tracer, pool, pidBefore)
}

// TestBeginTxCleanupRollsBackOnPanic mirrors the panic path for the
// read-write constructor.
func TestBeginTxCleanupRollsBackOnPanic(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "set local statement_timeout"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	pidBefore := toolsBackendPID(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	recovered := func() (r any) {
		defer func() { r = recover() }()

		_, errResp, cleanup := BeginTx(ctx, pool)
		if errResp != nil {
			t.Fatalf("BeginTx returned error response: %v", errResp)
		}
		defer cleanup()

		panic("tool handler exploded")
	}()

	if recovered != "tool handler exploded" {
		t.Fatalf("recovered value = %v, want the original panic to be re-raised", recovered)
	}

	assertCleanRollback(t, tracer, pool, pidBefore)
}
