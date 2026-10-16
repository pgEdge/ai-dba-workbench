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

	"github.com/jackc/pgx/v5"
)

// explainCommandCases lists every combination of execute_explain's
// options, including the one that used to build "EXPLAIN () ...".
var explainCommandCases = []struct {
	name    string
	analyze bool
	buffers bool
	format  string
	want    string
}{
	{"no options", false, false, "text",
		"EXPLAIN (COSTS TRUE) SELECT 1"},
	{"analyze only", true, false, "text",
		"EXPLAIN (COSTS TRUE, ANALYZE TRUE) SELECT 1"},
	{"buffers only", false, true, "text",
		"EXPLAIN (COSTS TRUE, BUFFERS TRUE) SELECT 1"},
	{"json only", false, false, "json",
		"EXPLAIN (COSTS TRUE, FORMAT JSON) SELECT 1"},
	{"everything", true, true, "json",
		"EXPLAIN (COSTS TRUE, ANALYZE TRUE, BUFFERS TRUE, FORMAT JSON) SELECT 1"},
}

func TestBuildExplainCommand(t *testing.T) {
	for _, tt := range explainCommandCases {
		t.Run(tt.name, func(t *testing.T) {
			got := buildExplainCommand("SELECT 1", tt.analyze, tt.buffers, tt.format)
			if got != tt.want {
				t.Errorf("buildExplainCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildExplainCommand_RunsOnServer checks that PostgreSQL accepts
// every command buildExplainCommand builds, and that a query starting
// with ANALYZE is a syntax error after the closed option list.
func TestBuildExplainCommand_RunsOnServer(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping database test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to the test database: %v", err)
	}
	defer conn.Close(ctx)

	for _, tt := range explainCommandCases {
		rows, err := conn.Query(ctx, tt.want)
		if err == nil {
			rows.Close()
			err = rows.Err()
		}
		if err != nil {
			t.Errorf("%s: %q failed: %v", tt.name, tt.want, err)
		}
	}

	_, err = conn.Exec(ctx, buildExplainCommand("ANALYZE SELECT 1", false, false, "text"))
	if err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("a leading ANALYZE was accepted as an option: %v", err)
	}
}
