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
