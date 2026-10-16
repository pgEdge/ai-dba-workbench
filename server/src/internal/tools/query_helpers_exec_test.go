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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newHelperTestPool opens a pool on the test database, skipping when
// none is configured.
func newHelperTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestExecuteReadOnlyQueryIntegration(t *testing.T) {
	pool := newHelperTestPool(t)
	ctx := context.Background()

	t.Run("truncates to the limit", func(t *testing.T) {
		qr, errResp := executeReadOnlyQuery(ctx, pool,
			"SELECT g FROM generate_series(1, 5) g", 3, false, "")
		if errResp != nil {
			t.Fatalf("unexpected error: %v", errResp.Content)
		}
		if !qr.WasTruncated || len(qr.Rows) != 3 {
			t.Errorf("truncated=%v rows=%d, want truncated with 3 rows",
				qr.WasTruncated, len(qr.Rows))
		}
		if len(qr.ColumnNames) != 1 || qr.ColumnNames[0] != "g" {
			t.Errorf("columns = %v", qr.ColumnNames)
		}
	})

	t.Run("keeps an existing limit", func(t *testing.T) {
		qr, errResp := executeReadOnlyQuery(ctx, pool,
			"SELECT g FROM generate_series(1, 5) g LIMIT 5", 3, true, "")
		if errResp != nil {
			t.Fatalf("unexpected error: %v", errResp.Content)
		}
		if qr.WasTruncated || len(qr.Rows) != 5 {
			t.Errorf("truncated=%v rows=%d, want 5 untruncated rows",
				qr.WasTruncated, len(qr.Rows))
		}
	})

	t.Run("reports a query error with the prefix", func(t *testing.T) {
		_, errResp := executeReadOnlyQuery(ctx, pool, "SELEC 1", 10, false, "pre: ")
		if errResp == nil || !errResp.IsError {
			t.Fatal("expected an error response")
		}
		if text := errResp.Content[0].Text; !strings.HasPrefix(text, "pre: ") {
			t.Errorf("unexpected message: %q", text)
		}
	})

	t.Run("reports an error raised while iterating", func(t *testing.T) {
		_, errResp := executeReadOnlyQuery(ctx, pool,
			"SELECT 1 / (g - 3) FROM generate_series(1, 5) g", 10, false, "")
		if errResp == nil || !errResp.IsError {
			t.Fatal("expected an error response")
		}
		if text := errResp.Content[0].Text; !strings.Contains(text, "Error iterating rows") {
			t.Errorf("unexpected message: %q", text)
		}
	})

	t.Run("reports a begin failure", func(t *testing.T) {
		closed := newHelperTestPool(t)
		closed.Close()
		_, errResp := executeReadOnlyQuery(ctx, closed, "SELECT 1", 10, false, "")
		if errResp == nil || !errResp.IsError {
			t.Fatal("expected an error response")
		}
	})
}

// TestExecuteReadOnlyQueryCommitFailureIntegration cancels the context
// once the query has finished, so the commit fails and is reported.
func TestExecuteReadOnlyQueryCommitFailureIntegration(t *testing.T) {
	tracer := &txRollbackTracer{cancelAfter: "commit_failure_probe"}
	pool, closePool := newTracedToolsPool(t, tracer)
	defer closePool()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	_, errResp := executeReadOnlyQuery(ctx, pool,
		"SELECT 1 AS commit_failure_probe", 10, false, "")
	if errResp == nil || !errResp.IsError {
		t.Fatal("expected an error response")
	}
	if text := errResp.Content[0].Text; !strings.Contains(text, "Failed to commit transaction") {
		t.Errorf("unexpected message: %q", text)
	}
}
