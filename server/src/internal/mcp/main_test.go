/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// traceFile is the trace file TestHandleHTTPRequestDebugAndTracing
// enables tracing with. tracing.Initialize opens its file only once per
// process, so the file must outlive any single test: a t.TempDir() one
// is removed after the first pass of a -count=2 run, and the second
// pass then finds no file to read.
var traceFile string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcp-trace-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create the trace directory: %v\n", err)
		os.Exit(1)
	}
	traceFile = filepath.Join(dir, "trace.jsonl")

	code := m.Run()

	_ = os.RemoveAll(dir)
	os.Exit(code)
}
