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
	"time"

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

	// Each query goes through injectLimitOffset first, as the tools do,
	// so the cases where its LIMIT is missing or ineffective are covered.
	capTests := []struct {
		name          string
		query         string
		wantRows      int
		wantTruncated bool
	}{
		{
			name:          "truncates to the limit",
			query:         "SELECT g FROM generate_series(1, 5) g",
			wantRows:      3,
			wantTruncated: true,
		},
		{
			name:     "does not truncate a result that fits",
			query:    "SELECT g FROM generate_series(1, 3) g",
			wantRows: 3,
		},
		{
			name:          "caps a trailing comment that hides the injected limit",
			query:         "SELECT g FROM generate_series(1, 5000) g -- trailing",
			wantRows:      3,
			wantTruncated: true,
		},
		{
			name:          "caps an oversized caller limit",
			query:         "SELECT g FROM generate_series(1, 5000) g LIMIT 5000",
			wantRows:      3,
			wantTruncated: true,
		},
	}
	for _, tt := range capTests {
		t.Run(tt.name, func(t *testing.T) {
			sqlQuery, _, _ := injectLimitOffset(tt.query, 3, 0)
			qr, errResp := executeReadOnlyQuery(ctx, pool, sqlQuery, 3, "")
			if errResp != nil {
				t.Fatalf("unexpected error: %v", errResp.Content)
			}
			if qr.WasTruncated != tt.wantTruncated || len(qr.Rows) != tt.wantRows {
				t.Errorf("truncated=%v rows=%d, want truncated=%v rows=%d",
					qr.WasTruncated, len(qr.Rows), tt.wantTruncated, tt.wantRows)
			}
			if len(qr.ColumnNames) != 1 || qr.ColumnNames[0] != "g" {
				t.Errorf("columns = %v", qr.ColumnNames)
			}
		})
	}

	t.Run("reports a query error with the prefix", func(t *testing.T) {
		_, errResp := executeReadOnlyQuery(ctx, pool, "SELEC 1", 10, "pre: ")
		if errResp == nil || !errResp.IsError {
			t.Fatal("expected an error response")
		}
		if text := errResp.Content[0].Text; !strings.HasPrefix(text, "pre: ") {
			t.Errorf("unexpected message: %q", text)
		}
	})

	t.Run("reports an error raised while iterating", func(t *testing.T) {
		_, errResp := executeReadOnlyQuery(ctx, pool,
			"SELECT 1 / (g - 3) FROM generate_series(1, 5) g", 10, "")
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
		_, errResp := executeReadOnlyQuery(ctx, closed, "SELECT 1", 10, "")
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
		"SELECT 1 AS commit_failure_probe", 10, "")
	if errResp == nil || !errResp.IsError {
		t.Fatal("expected an error response")
	}
	if text := errResp.Content[0].Text; !strings.Contains(text, "Failed to commit transaction") {
		t.Errorf("unexpected message: %q", text)
	}
}

// newSingleConnTestPool opens a pool of at most one connection on the
// test database, so successive queries share a backend unless that
// connection is closed.
func newSingleConnTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := newHelperTestPool(t)
	cfg := base.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// backendPID returns the PID of the backend serving pool's connection.
func backendPID(t *testing.T, pool *pgxpool.Pool) int32 {
	t.Helper()
	qr, errResp := executeReadOnlyQuery(context.Background(), pool,
		"SELECT pg_backend_pid()", 0, "")
	if errResp != nil {
		t.Fatalf("pg_backend_pid: %v", errResp.Content)
	}
	pid, ok := qr.Rows[0][0].(int32)
	if !ok {
		t.Fatalf("pg_backend_pid returned %T", qr.Rows[0][0])
	}
	return pid
}

// TestExecuteReadOnlyQueryStopsAtCapIntegration checks that a statement
// still producing rows when the cap is reached is stopped rather than
// read to the end, and that one already bounded by the injected LIMIT
// keeps its pooled connection.
func TestExecuteReadOnlyQueryStopsAtCapIntegration(t *testing.T) {
	pool := newSingleConnTestPool(t)
	observer := newHelperTestPool(t)
	ctx := context.Background()

	t.Run("an unbounded statement is stopped", func(t *testing.T) {
		before := backendPID(t, pool)

		// Reading 200 million rows takes far longer than the 10s
		// statement timeout, which is what a full drain ends in.
		query := "SELECT generate_series(1, 200000000) /* cap_stop_probe */ AS g " +
			"LIMIT 200000000"
		start := time.Now()
		qr, errResp := executeReadOnlyQuery(ctx, pool, query, 3, "")
		elapsed := time.Since(start)
		if errResp != nil {
			t.Fatalf("unexpected error: %v", errResp.Content)
		}
		if !qr.WasTruncated || len(qr.Rows) != 3 {
			t.Fatalf("truncated=%v rows=%d, want 3 truncated rows",
				qr.WasTruncated, len(qr.Rows))
		}
		if elapsed > 3*time.Second {
			t.Errorf("query took %v, want it stopped shortly after the cap", elapsed)
		}

		// The server abandons the statement rather than running on.
		deadline := time.Now().Add(5 * time.Second)
		for {
			var running int
			err := observer.QueryRow(ctx,
				"SELECT count(*) FROM pg_stat_activity "+
					"WHERE query LIKE '%cap_stop_' || 'probe%' AND state = 'active' "+
					"AND pid <> pg_backend_pid()").Scan(&running)
			if err != nil {
				t.Fatalf("pg_stat_activity: %v", err)
			}
			if running == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the capped statement is still running on the server")
			}
			time.Sleep(50 * time.Millisecond)
		}

		// The pool replaces the closed connection.
		if after := backendPID(t, pool); after == before {
			t.Errorf("backend %d survived; want the capped statement's connection closed", before)
		}
	})

	t.Run("a statement bounded by the injected limit keeps its connection", func(t *testing.T) {
		before := backendPID(t, pool)
		sqlQuery, _, _ := injectLimitOffset("SELECT g FROM generate_series(1, 1000) g", 3, 0)
		qr, errResp := executeReadOnlyQuery(ctx, pool, sqlQuery, 3, "")
		if errResp != nil {
			t.Fatalf("unexpected error: %v", errResp.Content)
		}
		if !qr.WasTruncated || len(qr.Rows) != 3 {
			t.Fatalf("truncated=%v rows=%d, want 3 truncated rows",
				qr.WasTruncated, len(qr.Rows))
		}
		if after := backendPID(t, pool); after != before {
			t.Errorf("backend changed from %d to %d; want the connection kept", before, after)
		}
	})

	t.Run("a caller canceled while draining is reported", func(t *testing.T) {
		// With a long grace, the caller's own cancellation ends the
		// drain; that is an error, not a result stopped at the cap.
		saved := capDrainGrace
		capDrainGrace = time.Minute
		defer func() { capDrainGrace = saved }()

		callerCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		timer := time.AfterFunc(500*time.Millisecond, cancel)
		defer timer.Stop()

		_, errResp := executeReadOnlyQuery(callerCtx, pool,
			"SELECT generate_series(1, 200000000) AS g LIMIT 200000000", 3, "")
		if errResp == nil || !errResp.IsError {
			t.Fatal("expected an error for the canceled caller")
		}
		if text := errResp.Content[0].Text; !strings.Contains(text, "Error iterating rows") {
			t.Errorf("unexpected message: %q", text)
		}
	})
}

// TestInjectedPaginationAfterTrailingCommentIntegration checks that the
// injected LIMIT and OFFSET still apply behind a trailing line comment,
// so the second page returns rows 4 to 6 rather than 1 to 3 again.
func TestInjectedPaginationAfterTrailingCommentIntegration(t *testing.T) {
	pool := newHelperTestPool(t)

	sqlQuery, hadLimit, hadOffset := injectLimitOffset(
		"SELECT g FROM generate_series(1, 10) g -- page through these", 3, 3)
	if hadLimit || hadOffset {
		t.Fatalf("hadLimit=%v hadOffset=%v, want both injected", hadLimit, hadOffset)
	}
	qr, errResp := executeReadOnlyQuery(context.Background(), pool, sqlQuery, 3, "")
	if errResp != nil {
		t.Fatalf("unexpected error: %v", errResp.Content)
	}
	var got []int32
	for _, row := range qr.Rows {
		got = append(got, row[0].(int32))
	}
	if len(got) != 3 || got[0] != 4 || got[2] != 6 || !qr.WasTruncated {
		t.Errorf("rows = %v truncated=%v, want rows 4 to 6 with more available",
			got, qr.WasTruncated)
	}
}
