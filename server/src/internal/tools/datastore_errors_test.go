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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDefaultQueryErrorFormatter(t *testing.T) {
	format := defaultQueryErrorFormatter("Query failed")
	boom := errors.New("boom")
	for stage, want := range map[string]string{
		queryStageRow:     "Error reading row: boom",
		queryStageIterate: "Error iterating rows: boom",
		queryStageCommit:  "Failed to commit transaction: boom",
		queryStageExecute: "Query failed",
	} {
		if got := format(stage, boom); !strings.HasPrefix(got, want) {
			t.Errorf("stage %s: got %q, want prefix %q", stage, got, want)
		}
	}
}

func TestDatastoreQueryErrorFormatter(t *testing.T) {
	format := datastoreQueryErrorFormatter("ds_probe")
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"statement timeout": {&pgconn.PgError{Code: sqlstateQueryCanceled, Message: "canceling statement"}, datastoreQueryTimeoutMsg},
		"deadline":          {fmt.Errorf("wrapped: %w", context.DeadlineExceeded), datastoreQueryTimeoutMsg},
		"data error":        {&pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type integer: "secret"`}, datastoreQueryFailedMsg},
		"other":             {errors.New("connection reset"), datastoreQueryFailedMsg},
	} {
		if got := format(queryStageExecute, tc.err); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestCheckDatastoreExplain(t *testing.T) {
	for name, tc := range map[string]struct {
		explain bool
		vis     visibleSet
		refused bool
	}{
		"every connection":          {true, visibleSet{All: true}, false},
		"some connections":          {true, visibleSet{IDs: []int{1, 2}}, true},
		"no connections":            {true, visibleSet{}, true},
		"rows for some connections": {false, visibleSet{IDs: []int{1}}, false},
	} {
		err := checkDatastoreExplain(datastoreRequest{Explain: tc.explain}, tc.vis)
		if tc.refused != errors.Is(err, errDatastoreExplainRestricted) {
			t.Errorf("%s: got %v, refused want %v", name, err, tc.refused)
		}
	}
	if errDatastoreExplainRestricted.Error() != "explain is available only to callers who can access every connection" {
		t.Errorf("message changed: %q", errDatastoreExplainRestricted)
	}
}

func TestDatastoreRowKeyWithoutID(t *testing.T) {
	src := datastoreSource{
		Name: "no_id", Table: "no_id", Kind: datastoreKindConfiguration,
		Columns: []datastoreColumn{col("name", typeText), col("value", typeText)},
	}
	if got := datastoreRowKey(src); len(got) != 1 || got[0] != "name" {
		t.Errorf("datastoreRowKey = %v, want [name]", got)
	}
}

// TestDatastoreDatabaseFailuresIntegration covers the database failures
// the handler reports without running the caller's statement.
func TestDatastoreDatabaseFailuresIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	done, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := datastoreConnectionsExist(done, pool, []int{1}); err == nil {
		t.Error("datastoreConnectionsExist should fail once its context is done")
	}
	if _, resp := executeQueryWith(done, pool, "SELECT 1", 10,
		datastoreQueryErrorFormatter("alerts")); resp == nil || !resp.IsError {
		t.Error("executeQueryWith should fail to begin once its context is done")
	}

	// Cancel the context once the statement has finished, so that the
	// statement runs and the commit fails.
	tracer := &txRollbackTracer{cancelAfter: "ds_commit_check"}
	tracedPool, closeTraced := newTracedToolsPool(t, tracer)
	defer closeTraced()
	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	tracer.mu.Lock()
	tracer.cancel = cancelCtx
	tracer.mu.Unlock()
	var stages []string
	record := func(stage string, err error) string {
		stages = append(stages, stage)
		return "failed"
	}
	if _, resp := executeQueryWith(ctx, tracedPool, "SELECT 1 AS ds_commit_check", 10,
		record); resp == nil || !resp.IsError {
		t.Error("executeQueryWith should report the failed commit")
	}
	if len(stages) != 1 || stages[0] != queryStageCommit {
		t.Errorf("stages = %v, want [commit]", stages)
	}
}
